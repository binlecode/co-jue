package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// eventsPeer plays mpv for eventsOn: every command is answered — get_property from prop (nil
// is unavailable; n counts the reads of that name so far), anything else with success — and
// once StatusOn has read time-pos, duration and volume, the script lines follow.
func eventsPeer(t *testing.T, prop func(name string, n int) any, script ...string) *IPCClient {
	t.Helper()
	c, peer, cmds := pipeClient(t)
	go func() {
		var once sync.Once
		reads := map[string]int{}
		for {
			line, err := cmds.ReadBytes('\n')
			if err != nil {
				return
			}
			var req struct {
				Command   []any `json:"command"`
				RequestID int64 `json:"request_id"`
			}
			json.Unmarshal(line, &req)
			reply := answer(req.RequestID, nil)
			if req.Command[0] == "get_property" {
				name := req.Command[1].(string)
				reads[name]++
				if v := prop(name, reads[name]); v != nil {
					reply = answer(req.RequestID, v)
				} else {
					reply = unavailable(req.RequestID)
				}
			}
			if _, err := peer.Write([]byte(reply + "\n")); err != nil {
				return
			}
			if reads["time-pos"] > 0 && reads["duration"] > 0 && reads["volume"] > 0 {
				once.Do(func() { send(peer, script...) }) // a goroutine: replies keep flowing meanwhile
			}
		}
	}()
	return c
}

// playing is a player sounding url at 42s.
func playing(url string) func(string, int) any {
	return func(name string, _ int) any {
		switch name {
		case "idle-active", "pause":
			return false
		case "path":
			return url
		}
		return 42.0
	}
}

func TestEventsSnapshotAndInitialPropertySuppression(t *testing.T) {
	c := eventsPeer(t, func(name string, n int) any {
		if name == "time-pos" && n > 1 {
			return 45.5 // read again when the pause lands
		}
		return playing("https://example.com/test.mp3")(name, n)
	},
		// mpv's initial values on observe repeat what StatusOn read: not changes.
		`{"event":"property-change","id":1,"name":"pause","data":false}`,
		`{"event":"property-change","id":3,"name":"idle-active","data":false}`,
		`{"event":"property-change","id":1,"name":"pause","data":true}`)

	var buf bytes.Buffer
	if _, err := eventsOn(c, "", 0.3, &buf); err != nil {
		t.Fatalf("eventsOn error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (snapshot + paused), got %d: %v", len(lines), lines)
	}
	var snap, paused map[string]any
	json.Unmarshal([]byte(lines[0]), &snap)
	if snap["event"] != "snapshot" || snap["state"] != "playing" {
		t.Errorf("expected snapshot playing, got: %s", lines[0])
	}
	json.Unmarshal([]byte(lines[1]), &paused)
	if paused["event"] != "paused" || paused["time_pos"] != 45.5 {
		t.Errorf("expected paused at 45.5, got: %s", lines[1])
	}
}

func TestEventsUntilModeMatching(t *testing.T) {
	c := eventsPeer(t, playing("https://example.com/test.mp3"), `{"event":"end-file","reason":"eof"}`)
	var buf bytes.Buffer
	res, err := eventsOn(c, "track_ended", 2.0, &buf)
	if err != nil {
		t.Fatalf("eventsOn error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no stdout output in until mode, got: %s", buf.String())
	}
	ended, ok := res.(TrackEndedEvent)
	if !ok || ended.Reason != "eof" || ended.URL != "https://example.com/test.mp3" {
		t.Errorf("got %T %+v, want track_ended eof", res, res)
	}
}

func TestEventsUntilTimeout(t *testing.T) {
	c := eventsPeer(t, func(name string, _ int) any {
		if name == "idle-active" || name == "pause" {
			return false
		}
		return nil
	})
	_, err := eventsOn(c, "track_started", 0.1, &bytes.Buffer{})
	var f *Fail
	if !errors.As(err, &f) || f.Code != 4 || f.Status != "timeout" {
		t.Fatalf("expected fail(4, timeout), got: %v", err)
	}
}

func TestEventsTrackEndedReasons(t *testing.T) {
	for mpvReason, want := range map[string]string{"stop": "replaced", "quit": "stopped", "error": "error"} {
		t.Run(mpvReason, func(t *testing.T) {
			c := eventsPeer(t, playing("https://a.com/b.mp3"), `{"event":"end-file","reason":"`+mpvReason+`"}`)
			res, err := eventsOn(c, "track_ended", 1.0, &bytes.Buffer{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ended := res.(TrackEndedEvent); ended.Reason != want {
				t.Errorf("for mpv reason %q, want %q, got %q", mpvReason, want, ended.Reason)
			}
		})
	}
}

// A track that fails to load is reported under its own URL, not the one before it: mpv's
// path is the new entry's from start-file on.
func TestEventsTrackEndedURLFollowsStartFile(t *testing.T) {
	c := eventsPeer(t, func(name string, n int) any {
		if name == "path" && n > 1 {
			return "https://a.com/next.mp3"
		}
		return playing("https://a.com/first.mp3")(name, n)
	},
		`{"event":"end-file","reason":"eof","playlist_entry_id":1}`,
		`{"event":"start-file","playlist_entry_id":2}`,
		`{"event":"end-file","reason":"error","playlist_entry_id":2,"file_error":"loading failed"}`)
	var buf bytes.Buffer
	if _, err := eventsOn(c, "", 0.3, &buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	want := `{"event":"track_ended","url":"https://a.com/next.mp3","reason":"error","error":"loading failed"}`
	if len(lines) != 3 || lines[2] != want {
		t.Errorf("got %v\nwant the last line %s", lines, want)
	}
}

func TestEventsQueueEnded(t *testing.T) {
	c := eventsPeer(t, playing("/a.wav"),
		`{"event":"end-file","reason":"eof","playlist_entry_id":1}`,
		`{"event":"start-file","playlist_entry_id":2}`,
		`{"event":"file-loaded"}`,
		`{"event":"end-file","reason":"eof","playlist_entry_id":2}`,
		`{"event":"idle"}`,
		`{"event":"property-change","id":3,"name":"idle-active","data":true}`)
	res, err := eventsOn(c, "queue_ended", 2.0, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(res); string(b) != `{"event":"queue_ended"}` {
		t.Errorf("got %s, want queue_ended", b)
	}
}

// Going idle because the last entry was skipped past (playlist-next force) or replaced is
// not the queue playing out.
func TestEventsQueueEndedNotOnStop(t *testing.T) {
	c := eventsPeer(t, playing("/a.wav"),
		`{"event":"end-file","reason":"stop","playlist_entry_id":1}`,
		`{"event":"property-change","id":3,"name":"idle-active","data":true}`)
	_, err := eventsOn(c, "queue_ended", 0.3, &bytes.Buffer{})
	var f *Fail
	if !errors.As(err, &f) || f.Status != "timeout" {
		t.Errorf("err = %v, want a timeout: no queue_ended after a stop", err)
	}
}

func TestEventsQueueEndedWhileIdle(t *testing.T) {
	c := eventsPeer(t, func(name string, _ int) any { return name == "idle-active" })
	_, err := eventsOn(c, "queue_ended", 2.0, &bytes.Buffer{})
	var f *Fail
	if !errors.As(err, &f) || f.Code != 4 || f.Status != "not_playing" || f.Msg != "player is idle" {
		t.Errorf("err = %v, want exit 4 not_playing player is idle", err)
	}
}

func TestEventsQueueEndedPlayerExits(t *testing.T) {
	c, peer, cmds := pipeClient(t)
	go func() {
		for i := int64(1); ; i++ {
			if _, err := cmds.ReadBytes('\n'); err != nil {
				return
			}
			peer.Write([]byte(answer(i, false) + "\n"))
			if i == 11 { // 4 observes and StatusOn's 7 reads
				peer.Close()
			}
		}
	}()
	_, err := eventsOn(c, "queue_ended", 2.0, &bytes.Buffer{})
	var f *Fail
	if !errors.As(err, &f) || f.Code != 4 || f.Msg != "player exited" {
		t.Errorf("err = %v, want exit 4 player exited", err)
	}
}

// devicePeer is a playing player with audio-device auto that never answers a read of
// audio-device-list (the baseline is the subscription's, not a Get's) and pushes script.
func devicePeer(t *testing.T, script ...string) *IPCClient {
	return eventsPeer(t, func(name string, n int) any {
		switch name {
		case "audio-device-list":
			t.Error("eventsOn read audio-device-list")
			return nil
		case "audio-device":
			return "auto"
		}
		return playing("/a.wav")(name, n)
	}, script...)
}

func devices(data string) string {
	return `{"event":"property-change","id":4,"name":"audio-device-list","data":` + data + `}`
}

const (
	twoDevs     = `[{"name":"coreaudio/Speakers","description":"MacBook Pro Speakers"},{"name":"auto","description":"Autoselect device"}]`
	twoDevsSwap = `[{"name":"auto","description":"Autoselect device"},{"name":"coreaudio/Speakers","description":"MacBook Pro Speakers"}]`
	threeDevs   = `[{"name":"coreaudio/Speakers","description":"MacBook Pro Speakers"},{"name":"coreaudio/AirPods","description":"AirPods Pro"},{"name":"auto","description":"Autoselect device"}]`
)

// deviceLines runs eventsOn briefly and returns its audio_device_changed lines.
func deviceLines(t *testing.T, c *IPCClient) []string {
	t.Helper()
	var buf bytes.Buffer
	if _, err := eventsOn(c, "", 0.15, &buf); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(l, "audio_device_changed") {
			out = append(out, l)
		}
	}
	return out
}

func TestEventsAudioDeviceBaselineIsFirstNotification(t *testing.T) {
	if got := deviceLines(t, devicePeer(t, devices(twoDevs))); len(got) != 0 {
		t.Errorf("the subscription's initial value was reported: %v", got)
	}
}

func TestEventsAudioDeviceSkipsInvalidBaseline(t *testing.T) {
	got := deviceLines(t, devicePeer(t,
		`{"event":"property-change","id":4,"name":"audio-device-list"}`,
		devices(twoDevs), devices(threeDevs)))
	if len(got) != 1 {
		t.Errorf("got %v, want one change: no data is no baseline", got)
	}
}

func TestEventsAudioDeviceChanged(t *testing.T) {
	got := deviceLines(t, devicePeer(t, devices(twoDevs), devices(threeDevs), devices(threeDevs), devices(twoDevsSwap)))
	want := []string{
		`{"event":"audio_device_changed","audio_device":"auto","devices":[{"name":"auto","description":"Autoselect device"},{"name":"coreaudio/AirPods","description":"AirPods Pro"},{"name":"coreaudio/Speakers","description":"MacBook Pro Speakers"}]}`,
		`{"event":"audio_device_changed","audio_device":"auto","devices":[{"name":"auto","description":"Autoselect device"},{"name":"coreaudio/Speakers","description":"MacBook Pro Speakers"}]}`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestEventsAudioDevicePermutation(t *testing.T) {
	if got := deviceLines(t, devicePeer(t, devices(twoDevs), devices(twoDevsSwap))); len(got) != 0 {
		t.Errorf("a reordering was reported: %v", got)
	}
	if got := deviceLines(t, devicePeer(t, devices("null"), devices("[]"))); len(got) != 0 {
		t.Errorf("null then [] was reported: %v", got)
	}
	got := deviceLines(t, devicePeer(t, devices(twoDevs), devices("null")))
	if len(got) != 1 || !strings.HasSuffix(got[0], `"devices":[]}`) {
		t.Errorf("got %v, want one change with devices []", got)
	}
}

func TestEventsUntilAudioDeviceChanged(t *testing.T) {
	// An idle player still waits: the device list is not the queue's. The list arrives once
	// StatusOn has found the player idle.
	c, peer, cmds := pipeClient(t)
	go func() {
		pushed := false
		for {
			line, err := cmds.ReadBytes('\n')
			if err != nil {
				return
			}
			var req struct {
				Command   []any `json:"command"`
				RequestID int64 `json:"request_id"`
			}
			json.Unmarshal(line, &req)
			reply := answer(req.RequestID, nil)
			if req.Command[0] == "get_property" {
				switch req.Command[1] {
				case "idle-active":
					reply = answer(req.RequestID, true)
				case "audio-device":
					reply = answer(req.RequestID, "auto")
				default:
					reply = unavailable(req.RequestID)
				}
			}
			peer.Write([]byte(reply + "\n"))
			if req.Command[0] == "get_property" && req.Command[1] == "idle-active" && !pushed {
				pushed = true
				send(peer, devices(twoDevs), devices(threeDevs))
			}
		}
	}()
	var buf bytes.Buffer
	res, err := eventsOn(c, "audio_device_changed", 2.0, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("until mode printed %s", buf.String())
	}
	if ev, ok := res.(AudioDeviceChangedEvent); !ok || len(ev.Devices) != 3 || ev.AudioDevice != "auto" {
		t.Errorf("got %T %+v, want audio_device_changed with 3 devices", res, res)
	}
}
