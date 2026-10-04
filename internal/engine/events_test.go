package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestEventsSnapshotAndInitialPropertySuppression(t *testing.T) {
	c, peer, cmds := pipeClient(t)

	// Mock mpv responses for setup commands and event loop
	go func() {
		// 1. observe_property pause
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":1,"error":"success"}` + "\n"))
		// 2. observe_property chapter
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":2,"error":"success"}` + "\n"))
		// 3. StatusOn: idle-active
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":3,"error":"success","data":false}` + "\n"))
		// 4. StatusOn: pause
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":4,"error":"success","data":false}` + "\n"))
		// 5. StatusOn: path
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":5,"error":"success","data":"https://example.com/test.mp3"}` + "\n"))
		// 6. StatusOn: time-pos, duration, volume
		for i := 6; i <= 8; i++ {
			cmds.ReadBytes('\n')
			peer.Write([]byte(`{"request_id":` + string(rune('0'+i)) + `,"error":"success","data":42.0}` + "\n"))
		}

		// Now send initial property-change from mpv (should be suppressed as duplicate)
		peer.Write([]byte(`{"event":"property-change","id":1,"name":"pause","data":false}` + "\n"))

		// Send real pause state change (should be emitted)
		peer.Write([]byte(`{"event":"property-change","id":1,"name":"pause","data":true}` + "\n"))

		// mpv responds to get_property time-pos query caused by pause event
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":9,"error":"success","data":45.5}` + "\n"))

		// Terminate stream with timeout
	}()

	var buf bytes.Buffer
	_, err := eventsOn(c, "", 0.3, &buf)
	if err != nil {
		t.Fatalf("eventsOn error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (snapshot + paused), got %d: %v", len(lines), lines)
	}

	var snap map[string]any
	json.Unmarshal([]byte(lines[0]), &snap)
	if snap["event"] != "snapshot" || snap["state"] != "playing" {
		t.Errorf("expected snapshot playing, got: %s", lines[0])
	}

	var paused map[string]any
	json.Unmarshal([]byte(lines[1]), &paused)
	if paused["event"] != "paused" || paused["time_pos"] != 45.5 {
		t.Errorf("expected paused at 45.5, got: %s", lines[1])
	}
}

func TestEventsUntilModeMatching(t *testing.T) {
	c, peer, cmds := pipeClient(t)

	go func() {
		// Setup: observe_property x 2
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":1,"error":"success"}` + "\n"))
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":2,"error":"success"}` + "\n"))
		// StatusOn
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":3,"error":"success","data":false}` + "\n"))
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":4,"error":"success","data":false}` + "\n"))
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":5,"error":"success","data":"https://example.com/test.mp3"}` + "\n"))
		for i := 6; i <= 8; i++ {
			cmds.ReadBytes('\n')
			peer.Write([]byte(`{"request_id":` + string(rune('0'+i)) + `,"error":"success","data":10.0}` + "\n"))
		}

		// Send end-file eof
		peer.Write([]byte(`{"event":"end-file","reason":"eof"}` + "\n"))
	}()

	var buf bytes.Buffer
	res, err := eventsOn(c, "track_ended", 2.0, &buf)
	if err != nil {
		t.Fatalf("eventsOn error: %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("expected no stdout output in until mode, got: %s", buf.String())
	}

	ended, ok := res.(TrackEndedEvent)
	if !ok {
		t.Fatalf("expected TrackEndedEvent, got %T", res)
	}
	if ended.Event != "track_ended" || ended.Reason != "eof" {
		t.Errorf("unexpected event: %+v", ended)
	}
}

func TestEventsUntilTimeout(t *testing.T) {
	c, peer, cmds := pipeClient(t)

	go func() {
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":1,"error":"success"}` + "\n"))
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":2,"error":"success"}` + "\n"))
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":3,"error":"success","data":false}` + "\n"))
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":4,"error":"success","data":false}` + "\n"))
		cmds.ReadBytes('\n')
		peer.Write([]byte(`{"request_id":5,"error":"success","data":null}` + "\n"))
		for i := 6; i <= 8; i++ {
			cmds.ReadBytes('\n')
			peer.Write([]byte(`{"request_id":` + string(rune('0'+i)) + `,"error":"success","data":null}` + "\n"))
		}
	}()

	var buf bytes.Buffer
	_, err := eventsOn(c, "track_started", 0.1, &buf)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}

	var f *Fail
	if !errors.As(err, &f) || f.Code != 4 || f.Status != "timeout" {
		t.Fatalf("expected fail(4, timeout), got: %v", err)
	}
}

func TestEventsTrackEndedReasons(t *testing.T) {
	tests := []struct {
		mpvReason string
		wantEvent string
	}{
		{"stop", "replaced"},
		{"quit", "stopped"},
		{"error", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.mpvReason, func(t *testing.T) {
			c, peer, cmds := pipeClient(t)
			go func() {
				cmds.ReadBytes('\n')
				peer.Write([]byte(`{"request_id":1,"error":"success"}` + "\n"))
				cmds.ReadBytes('\n')
				peer.Write([]byte(`{"request_id":2,"error":"success"}` + "\n"))
				cmds.ReadBytes('\n')
				peer.Write([]byte(`{"request_id":3,"error":"success","data":false}` + "\n"))
				cmds.ReadBytes('\n')
				peer.Write([]byte(`{"request_id":4,"error":"success","data":false}` + "\n"))
				cmds.ReadBytes('\n')
				peer.Write([]byte(`{"request_id":5,"error":"success","data":"https://a.com/b.mp3"}` + "\n"))
				for i := 6; i <= 8; i++ {
					cmds.ReadBytes('\n')
					peer.Write([]byte(`{"request_id":` + string(rune('0'+i)) + `,"error":"success","data":null}` + "\n"))
				}
				peer.Write([]byte(`{"event":"end-file","reason":"` + tt.mpvReason + `"}` + "\n"))
			}()

			res, err := eventsOn(c, "track_ended", 1.0, &bytes.Buffer{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			ended := res.(TrackEndedEvent)
			if ended.Reason != tt.wantEvent {
				t.Errorf("for mpv reason %q, want %q, got %q", tt.mpvReason, tt.wantEvent, ended.Reason)
			}
		})
	}
}
