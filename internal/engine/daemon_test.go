package engine

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// isolate points os.TempDir at a fresh directory and returns where runtimeDir will look.
func isolate(t *testing.T) (base, dir string) {
	t.Helper()
	base = t.TempDir()
	t.Setenv("TMPDIR", base)
	return base, filepath.Join(base, fmt.Sprintf("ting-%d", os.Getuid()))
}

func wantRefused(t *testing.T, err error, msg string) {
	t.Helper()
	var f *Fail
	if !errors.As(err, &f) || f.Code != 2 || !strings.Contains(f.Msg, msg) {
		t.Errorf("err = %v, want exit-2 refusal mentioning %q", err, msg)
	}
}

func TestRuntimeDirCreates0700(t *testing.T) {
	_, want := isolate(t)
	got, err := runtimeDir(true)
	if err != nil || got != want {
		t.Fatalf("runtimeDir(true) = %q, %v; want %q", got, err, want)
	}
	fi, _ := os.Lstat(want)
	if !fi.IsDir() || fi.Mode().Perm() != 0700 {
		t.Errorf("created %v, want a 0700 directory", fi.Mode())
	}
	// A second caller finds it and accepts it.
	if got, err := runtimeDir(true); err != nil || got != want {
		t.Errorf("runtimeDir on an existing 0700 dir = %q, %v", got, err)
	}
}

func TestRuntimeDirAbsentIsNotCreated(t *testing.T) {
	_, dir := isolate(t)
	if _, err := runtimeDir(false); err != errNoDir {
		t.Errorf("err = %v, want errNoDir", err)
	}
	// status and control read through Connect: nothing running, and nothing left behind.
	if c, err := Connect(); c != nil || err != nil {
		t.Errorf("Connect = %v, %v; want nil, nil", c, err)
	}
	if r, err := Status(); err != nil || r.State != "idle" {
		t.Errorf("Status = %+v, %v; want idle", r, err)
	}
	if _, err := Control("pause", 0, ""); err == nil {
		t.Error("Control with no player succeeded")
	} else if f := (*Fail)(nil); !errors.As(err, &f) || f.Code != 4 || f.Status != "not_playing" {
		t.Errorf("Control err = %v, want exit 4 not_playing", err)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("read-only verbs created %s (err %v)", dir, err)
	}
}

func TestRuntimeDirRefusesLooseMode(t *testing.T) {
	for _, mode := range []os.FileMode{0755, 0750, 0711, 0777} {
		_, dir := isolate(t)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		os.Chmod(dir, mode) // set after Mkdir: the umask would mask it
		for _, create := range []bool{true, false} {
			_, err := runtimeDir(create)
			wantRefused(t, err, "want 0700")
		}
	}
}

func TestRuntimeDirRefusesSymlink(t *testing.T) {
	base, dir := isolate(t)
	// The target itself would pass every check: only the link is wrong.
	target := filepath.Join(base, "elsewhere")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, dir); err != nil {
		t.Fatal(err)
	}
	for _, create := range []bool{true, false} {
		_, err := runtimeDir(create)
		wantRefused(t, err, "is a symlink")
	}
	if _, err := scratchDir(); err == nil {
		t.Error("scratchDir wrote through a symlinked runtime dir")
	}
	if left, _ := os.ReadDir(target); len(left) != 0 {
		t.Errorf("symlink target got %d entries, want none", len(left))
	}
}

func TestRuntimeDirRefusesFile(t *testing.T) {
	_, dir := isolate(t)
	if err := os.WriteFile(dir, nil, 0700); err != nil {
		t.Fatal(err)
	}
	_, err := runtimeDir(true)
	wantRefused(t, err, "not a directory")
}

func TestScratchDirIsInsideRuntimeDir(t *testing.T) {
	_, dir := isolate(t)
	s, err := scratchDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(s) != dir || !strings.HasPrefix(filepath.Base(s), "scratch-") {
		t.Errorf("scratchDir = %q, want a scratch-* entry of %q", s, dir)
	}
	if fi, _ := os.Lstat(s); fi.Mode().Perm() != 0700 {
		t.Errorf("scratch dir mode %v, want 0700", fi.Mode().Perm())
	}
}

// fakeMPV listens where Connect dials and answers like mpv: reply(cmd) gives the lines to send
// back for each command (the reply and any events), and every command is kept in order. The
// runtime dir sits under /tmp: a t.TempDir path overflows the 104-byte socket address.
type fakeMPV struct {
	mu   sync.Mutex
	cmds [][]any
}

func startFakeMPV(t *testing.T, reply func(cmd []any, id int64) []string) *fakeMPV {
	t.Helper()
	base, err := os.MkdirTemp("/tmp", "ting-t")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	t.Setenv("TMPDIR", base)
	dir, err := runtimeDir(true)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "mpv.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f := &fakeMPV{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				for {
					line, err := r.ReadBytes('\n')
					if err != nil {
						return
					}
					var req struct {
						Command   []any `json:"command"`
						RequestID int64 `json:"request_id"`
					}
					json.Unmarshal(line, &req)
					f.mu.Lock()
					f.cmds = append(f.cmds, req.Command)
					f.mu.Unlock()
					for _, l := range reply(req.Command, req.RequestID) {
						conn.Write([]byte(l + "\n"))
					}
				}
			}()
		}
	}()
	return f
}

// sent is every command but the property reads, as compact JSON.
func (f *fakeMPV) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.cmds {
		if c[0] != "get_property" {
			b, _ := json.Marshal(c)
			out = append(out, string(b))
		}
	}
	return out
}

func answer(id int64, data any) string {
	b, _ := json.Marshal(map[string]any{"request_id": id, "error": "success", "data": data})
	return string(b)
}

func unavailable(id int64) string {
	return fmt.Sprintf(`{"request_id":%d,"error":"property unavailable","data":null}`, id)
}

// props answers get_property from m (absent is unavailable) and every other command with success.
func props(m map[string]any) func([]any, int64) []string {
	return func(cmd []any, id int64) []string {
		if cmd[0] != "get_property" {
			return []string{answer(id, nil)}
		}
		if v, ok := m[cmd[1].(string)]; ok {
			return []string{answer(id, v)}
		}
		return []string{unavailable(id)}
	}
}

func TestStatusStates(t *testing.T) {
	cases := []struct {
		name  string
		props map[string]any
		want  string
	}{
		{"playhead moving", map[string]any{"idle-active": false, "pause": false, "time-pos": 3.5}, "playing"},
		// Between loadfile and the first frame mpv has no time-pos: nothing sounds yet.
		{"no playhead yet", map[string]any{"idle-active": false, "pause": false}, "loading"},
		{"paused before the first frame", map[string]any{"idle-active": false, "pause": true}, "paused"},
		{"paused", map[string]any{"idle-active": false, "pause": true, "time-pos": 3.5}, "paused"},
		{"idle", map[string]any{"idle-active": true}, "idle"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			startFakeMPV(t, props(c.props))
			r, err := Status()
			if err != nil || r.State != c.want {
				t.Errorf("Status = %+v, %v; want state %q", r, err, c.want)
			}
		})
	}
}

func TestControlSeekModes(t *testing.T) {
	for _, mode := range []string{"relative", "absolute"} {
		f := startFakeMPV(t, props(map[string]any{"idle-active": false}))
		if _, err := Control("seek", -15, mode); err != nil {
			t.Fatalf("Control(seek, %s) = %v", mode, err)
		}
		want := fmt.Sprintf(`["seek",-15,%q]`, mode)
		if got := f.sent(); len(got) != 1 || got[0] != want {
			t.Errorf("sent %v, want [%s]", got, want)
		}
	}
}

func TestControlBadActionIsUsage(t *testing.T) {
	// With no player and with one: a bad action is the caller's mistake either way, and
	// reaches no player.
	isolate(t)
	for _, live := range []bool{false, true} {
		var f *fakeMPV
		if live {
			f = startFakeMPV(t, props(map[string]any{"idle-active": false}))
		}
		for _, c := range []struct{ action, mode string }{{"rewind", ""}, {"seek", ""}, {"seek", "sideways"}} {
			var fl *Fail
			if _, err := Control(c.action, 1, c.mode); !errors.As(err, &fl) || fl.Code != 1 {
				t.Errorf("live=%v Control(%q, %q) err = %v, want exit 1", live, c.action, c.mode, err)
			}
		}
		if f != nil {
			f.mu.Lock()
			if len(f.cmds) != 0 {
				t.Errorf("bad actions sent %v to the player", f.cmds)
			}
			f.mu.Unlock()
		}
	}
}

func TestPlayUnpausesTheNewTrack(t *testing.T) {
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "yt-dlp"), []byte("#!/bin/sh\n"), 0755)
	t.Setenv("PATH", bin)
	// The player was paused by control pause: the next play must still sound.
	f := startFakeMPV(t, func(cmd []any, id int64) []string {
		switch cmd[0] {
		case "get_property":
			return []string{answer(id, false)}
		case "loadfile":
			return []string{answer(id, map[string]any{"playlist_entry_id": 2}),
				`{"event":"start-file","playlist_entry_id":2}`, `{"event":"file-loaded"}`}
		}
		return []string{answer(id, nil)}
	})
	if _, err := Play("x.wav", 30); err != nil {
		t.Fatalf("Play = %v", err)
	}
	want := []string{`["loadfile","x.wav","replace",-1,"start=30"]`, `["set_property","pause",false]`}
	if got := f.sent(); !reflect.DeepEqual(got, want) {
		t.Errorf("sent %v, want %v", got, want)
	}
}
