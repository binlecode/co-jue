package engine

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"testing"
	"time"
)

// pipeClient is an IPCClient on one end of an in-memory connection; the test plays mpv on
// the other, reading the client's commands from cmds and writing raw lines to peer.
func pipeClient(t *testing.T) (c *IPCClient, peer net.Conn, cmds *bufio.Reader) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	return newIPCClient(a), b, bufio.NewReader(b)
}

// send writes lines from mpv's side without blocking the test: net.Pipe is synchronous.
func send(peer net.Conn, lines ...string) {
	go func() {
		for _, l := range lines {
			if _, err := peer.Write([]byte(l + "\n")); err != nil {
				return
			}
		}
	}()
}

func TestCommandMatchesRequestID(t *testing.T) {
	c, peer, cmds := pipeClient(t)
	go func() {
		line, _ := cmds.ReadBytes('\n')
		var req struct {
			Command   []any `json:"command"`
			RequestID int64 `json:"request_id"`
		}
		json.Unmarshal(line, &req)
		if req.RequestID != 1 || len(req.Command) != 2 || req.Command[0] != "get_property" {
			peer.Write([]byte(`{"request_id":1,"error":"bad request","data":null}` + "\n"))
			return
		}
		// mpv interleaves events and may still be answering an older command on this stream.
		send(peer,
			`{"event":"start-file","playlist_entry_id":7}`,
			`{"request_id":99,"error":"success","data":"stale"}`,
			`not json at all`,
			`{"event":"file-loaded"}`,
			`{"request_id":1,"error":"success","data":12.5}`)
	}()
	var v float64
	ok, err := c.Get("time-pos", &v)
	if err != nil || !ok || v != 12.5 {
		t.Fatalf("Get = %v, %v, %v; want 12.5 from the reply carrying our request_id", ok, err, v)
	}
	if len(c.eventQueue) != 2 || c.eventQueue[0]["event"] != "start-file" || c.eventQueue[1]["event"] != "file-loaded" {
		t.Errorf("event queue = %v; want both events kept in order", c.eventQueue)
	}

	// The next command gets the next id.
	go func() {
		line, _ := cmds.ReadBytes('\n')
		if !strings.Contains(string(line), `"request_id":2`) {
			t.Errorf("second command sent %s, want request_id 2", line)
		}
		send(peer, `{"request_id":2,"error":"property unavailable","data":null}`)
	}()
	ok, err = c.Get("duration", &v)
	if err != nil || ok {
		t.Errorf("Get(unavailable) = %v, %v; want ok=false and no error", ok, err)
	}
}

func ev(name string, id int64, extra string) string {
	s := fmt.Sprintf(`{"event":%q`, name)
	if id != 0 {
		s += fmt.Sprintf(`,"playlist_entry_id":%d`, id)
	}
	if extra != "" {
		s += "," + extra
	}
	return s + "}"
}

func TestWaitForPlaybackSuccess(t *testing.T) {
	cases := []struct {
		name    string
		queued  []string // read before the wait, e.g. while loadfile's reply was awaited
		wire    []string // arrive during the wait
		wantErr string   // "" = success
	}{
		{
			name:   "loaded",
			queued: []string{ev("start-file", 2, "")},
			wire:   []string{ev("audio-reconfig", 0, ""), ev("file-loaded", 0, "")},
		},
		{
			name: "an earlier entry's events are not ours",
			// file-loaded of entry 1 precedes our start-file: it must not count, and entry 1
			// ending with an error is not our failure.
			queued: []string{ev("start-file", 1, ""), ev("file-loaded", 0, ""), ev("end-file", 1, `"reason":"error","file_error":"old"`)},
			wire:   []string{ev("start-file", 2, ""), ev("file-loaded", 0, "")},
		},
		{
			name:    "load failed",
			wire:    []string{ev("start-file", 2, ""), ev("end-file", 2, `"reason":"error","file_error":"loading failed"`)},
			wantErr: "playback failed: loading failed",
		},
		{
			name:    "ended without file_error",
			wire:    []string{ev("start-file", 2, ""), ev("end-file", 2, `"reason":"quit"`)},
			wantErr: "playback ended before it started (reason: quit)",
		},
		{
			name: "replaced before it started",
			// A concurrent play's entry 3 starts first: mpv drops queued entry 2 silently.
			wire: []string{ev("start-file", 3, "")},
		},
		{
			name: "replaced after it started",
			wire: []string{ev("start-file", 2, ""), ev("end-file", 2, `"reason":"stop"`)},
		},
		{
			name: "redirect to a playlist entry",
			wire: []string{ev("start-file", 2, ""), ev("end-file", 2, `"reason":"redirect"`),
				ev("start-file", 3, ""), ev("file-loaded", 0, "")},
		},
		{
			name: "redirected entry fails",
			wire: []string{ev("start-file", 2, ""), ev("end-file", 2, `"reason":"redirect"`),
				ev("start-file", 3, ""), ev("end-file", 3, `"reason":"error","file_error":"unrecognized file format"`)},
			wantErr: "playback failed: unrecognized file format",
		},
		{
			name:    "start-file alone is not success",
			wire:    []string{ev("start-file", 2, "")},
			wantErr: "playback not ready after",
		},
		{
			name:    "a stale file-loaded alone is not success",
			queued:  []string{ev("file-loaded", 0, "")},
			wantErr: "playback not ready after",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, peer, _ := pipeClient(t)
			for _, l := range tc.queued {
				var m map[string]any
				json.Unmarshal([]byte(l), &m)
				c.eventQueue = append(c.eventQueue, m)
			}
			send(peer, tc.wire...)
			err := c.WaitForPlaybackSuccess(2, 300*time.Millisecond)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("err = %v, want success", err)
			case tc.wantErr != "" && (err == nil || !strings.HasPrefix(err.Error(), tc.wantErr)):
				t.Errorf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestWaitForPlaybackSuccessPlayerGone(t *testing.T) {
	c, peer, _ := pipeClient(t)
	send(peer, ev("start-file", 2, ""))
	go func() { time.Sleep(50 * time.Millisecond); peer.Close() }()
	err := c.WaitForPlaybackSuccess(2, 5*time.Second)
	if err == nil || !strings.HasPrefix(err.Error(), "player went away while loading") {
		t.Errorf("err = %v, want player went away", err)
	}
}

func TestCommandRefusesUnencodableArgs(t *testing.T) {
	c, _, cmds := pipeClient(t)
	got := make(chan string, 1)
	go func() { line, _ := cmds.ReadString('\n'); got <- line }()
	_, err := c.Command("seek", math.NaN(), "relative")
	var uv *json.UnsupportedValueError
	if !errors.As(err, &uv) {
		t.Fatalf("err = %v, want the marshal error", err)
	}
	select {
	case line := <-got:
		t.Errorf("wrote %q to the socket, want nothing", line)
	case <-time.After(50 * time.Millisecond):
	}
}
