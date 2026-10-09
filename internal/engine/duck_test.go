package engine

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWriteDuckHelper(t *testing.T) {
	_, dir := isolate(t)
	if _, err := runtimeDir(true); err != nil {
		t.Fatal(err)
	}
	// A link planted at the path, aimed outside: it is replaced, its target left alone.
	bait := filepath.Join(t.TempDir(), "bait")
	os.WriteFile(bait, []byte("bait"), 0600)
	if err := os.Symlink(bait, filepath.Join(dir, "jue_duck.lua")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // twice: a rewrite over its own file is the same file
		path, err := writeDuckHelper(dir)
		if err != nil || path != filepath.Join(dir, "jue_duck.lua") {
			t.Fatalf("writeDuckHelper = %q, %v", path, err)
		}
		fi, _ := os.Lstat(path)
		if !fi.Mode().IsRegular() || fi.Mode().Perm() != 0600 {
			t.Errorf("helper is %v, want a 0600 regular file", fi.Mode())
		}
		if b, _ := os.ReadFile(path); !bytes.Equal(b, duckHelper) {
			t.Error("helper on disk differs from the embedded one")
		}
	}
	if b, _ := os.ReadFile(bait); string(b) != "bait" {
		t.Errorf("symlink target rewritten to %q", b)
	}
	if left, _ := os.ReadDir(dir); len(left) != 1 {
		t.Errorf("runtime dir holds %d entries, want the helper alone", len(left))
	}
}

// The marker the helper sets is the one Duck reads: the two sides must not drift.
func TestDuckHelperEmbedsMarker(t *testing.T) {
	s := string(duckHelper)
	if !strings.Contains(s, `mp.set_property("`+duckMarker+`", PROTOCOL)`) || !strings.Contains(s, `PROTOCOL = "`+duckProtocol+`"`) {
		t.Errorf("embedded helper does not set %s to %q", duckMarker, duckProtocol)
	}
}

// duckMPV is a playing player whose helper reports marker (nil: absent) and answers
// script-message-to with delivery.
func duckMPV(marker any, delivery string) func([]any, int64) []string {
	return func(cmd []any, id int64) []string {
		switch {
		case cmd[0] == "get_property" && cmd[1] == "playlist-pos":
			return []string{answer(id, 0)}
		case cmd[0] == "get_property" && cmd[1] == duckMarker && marker != nil:
			return []string{answer(id, marker)}
		case cmd[0] == "get_property":
			return []string{fmt.Sprintf(`{"request_id":%d,"error":"property not found"}`, id)}
		}
		return []string{fmt.Sprintf(`{"request_id":%d,"error":%q}`, id, delivery)}
	}
}

func TestDuckWire(t *testing.T) {
	for _, c := range []struct {
		req    DuckRequest
		action string
		want   string
	}{
		{DuckRequest{Mode: "pulse", Level: 20, Hold: 5, Fade: 200}, "duck", `["script-message-to","jue_duck","duck","20","5","200","400"]`},
		{DuckRequest{Mode: "on", Level: 20, Hold: 30, Fade: 200}, "duck_on", `["script-message-to","jue_duck","duck","20","30","200","400"]`},
		{DuckRequest{Mode: "off", Fade: 400}, "duck_off", `["script-message-to","jue_duck","unduck","400"]`},
		{DuckRequest{Mode: "pulse", Level: 12.5, Hold: 1.5, Fade: 0}, "duck", `["script-message-to","jue_duck","duck","12.5","1.5","0","0"]`},
	} {
		f := startFakeMPV(t, duckMPV(duckProtocol, "success"))
		r, err := Duck(c.req)
		if err != nil || r.Status != "ok" || r.Action != c.action {
			t.Errorf("Duck(%+v) = %+v, %v; want action %s", c.req, r, err, c.action)
		}
		f.mu.Lock()
		var reads []any
		for _, cmd := range f.cmds {
			if cmd[0] == "get_property" {
				reads = append(reads, cmd[1])
			}
		}
		f.mu.Unlock()
		if !reflect.DeepEqual(reads, []any{"playlist-pos", duckMarker}) {
			t.Errorf("read %v, want the idle gate and the marker once each", reads)
		}
		if got := f.sent(); len(got) != 1 || got[0] != c.want {
			t.Errorf("sent %v, want [%s]", got, c.want)
		}
	}
}

func TestDuckIdleIsNotPlaying(t *testing.T) {
	_, dir := isolate(t)
	_, err := Duck(DuckRequest{Mode: "off", Fade: 400})
	wantFail(t, err, 4, "not_playing", "no player running")
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("duck created %s (err %v)", dir, err)
	}

	f := startFakeMPV(t, props(map[string]any{"playlist-pos": -1, duckMarker: duckProtocol}))
	_, err = Duck(DuckRequest{Mode: "pulse", Level: 20, Hold: 5, Fade: 200})
	wantFail(t, err, 4, "not_playing", "player is idle")
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.cmds) != 1 {
		t.Errorf("idle player got %v, want the idle gate alone", f.cmds)
	}
}

func TestDuckMarker(t *testing.T) {
	for _, c := range []struct {
		marker any
		msg    string
	}{{nil, "duck helper not loaded"}, {"0", `protocol "0", want 1`}} {
		f := startFakeMPV(t, duckMPV(c.marker, "success"))
		_, err := Duck(DuckRequest{Mode: "on", Level: 20, Hold: 30, Fade: 200})
		wantFail(t, err, 4, "unavailable", c.msg)
		if got := f.sent(); len(got) != 0 {
			t.Errorf("marker %v: sent %v, want nothing", c.marker, got)
		}
	}

	// The player hangs up while the marker is read.
	startFakeMPV(t, func(cmd []any, id int64) []string {
		if cmd[1] == "playlist-pos" {
			return []string{answer(id, 0)}
		}
		return nil
	})
	_, err := Duck(DuckRequest{Mode: "off", Fade: 400})
	wantFail(t, err, 4, "not_playing", "player went away")
}

func TestDuckDeliveryRefused(t *testing.T) {
	startFakeMPV(t, duckMPV(duckProtocol, "error running command"))
	_, err := Duck(DuckRequest{Mode: "pulse", Level: 20, Hold: 5, Fade: 200})
	wantFail(t, err, 4, "unavailable", "refused delivery")
}

// BenchmarkDuckRoundTrip is one Duck over a real Unix socket: dial, idle gate, marker and
// script-message-to. It reports, it does not gate: load jitter is not a regression.
func BenchmarkDuckRoundTrip(b *testing.B) {
	startFakeMPV(b, duckMPV(duckProtocol, "success"))
	req := DuckRequest{Mode: "pulse", Level: 20, Hold: 5, Fade: 200}
	for b.Loop() {
		if _, err := Duck(req); err != nil {
			b.Fatal(err)
		}
	}
}
