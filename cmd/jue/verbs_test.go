package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/binlecode/co-jue/internal/engine"
)

func TestParseDuck(t *testing.T) {
	ok := []struct {
		args string
		want engine.DuckRequest
	}{
		{"", engine.DuckRequest{Mode: "pulse", Level: 20, Hold: 5, Fade: 200}},
		{"on", engine.DuckRequest{Mode: "on", Level: 20, Hold: 30, Fade: 200}},
		{"off", engine.DuckRequest{Mode: "off", Fade: 400}},
		// Suffixes are the bare number.
		{"--fade 200ms --duration 5s", engine.DuckRequest{Mode: "pulse", Level: 20, Hold: 5, Fade: 200}},
		{"--duration 1.5s", engine.DuckRequest{Mode: "pulse", Level: 20, Hold: 1.5, Fade: 200}},
		{"--duration 1.5", engine.DuckRequest{Mode: "pulse", Level: 20, Hold: 1.5, Fade: 200}},
		// Bounds are inclusive except duration's zero.
		{"--level 0 --fade 0", engine.DuckRequest{Mode: "pulse", Level: 0, Hold: 5, Fade: 0}},
		{"--level 100 --duration 30 --fade 2000", engine.DuckRequest{Mode: "pulse", Level: 100, Hold: 30, Fade: 2000}},
		{"on --level 12.5 --fade 50", engine.DuckRequest{Mode: "on", Level: 12.5, Hold: 30, Fade: 50}},
		{"off --fade 0", engine.DuckRequest{Mode: "off", Fade: 0}},
	}
	for _, c := range ok {
		got, err := parseDuck(strings.Fields(c.args))
		if err != nil || got != c.want {
			t.Errorf("parseDuck(%q) = %+v, %v; want %+v", c.args, got, err, c.want)
		}
	}
	bad := []string{
		"--level 101", "--level -1", "--level nan", "--level inf", "--level 1e309",
		"--duration 0", "--duration 31", "--duration nan", "--duration 1e309", "--duration 5ms",
		"--fade -1", "--fade 2001", "--fade inf", "--fade NaN", "--fade 1s",
		"--level 20 --level 30", "--fade 100 --fade 200",
		"on --duration 5", "off --level 20", "off --duration 5",
		"sideways", "on off", "--level 20 on",
		"--level", "--bogus 1",
	}
	for _, args := range bad {
		_, err := parseDuck(strings.Fields(args))
		var f *engine.Fail
		if !errors.As(err, &f) || f.Code != 1 {
			t.Errorf("parseDuck(%q) err = %v, want exit 1", args, err)
		}
	}
}

// TestVerbUsageGates is every verb's grammar: a malformed command line is exit 1 before any
// tool, network or player is reached. PATH and TMPDIR are emptied, so a case that slipped
// past its gate would answer 2 or 4, not 1.
func TestVerbUsageGates(t *testing.T) {
	t.Setenv("PATH", "")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	const u = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	bad := map[string][]string{
		"inspect":    {"", u + " " + u, u + " --bogus x"},
		"transcript": {"", u + " --range", u + " --range 30-10", u + " --range nan-10", u + " --bogus 1"},
		"frame": {u, "--at 10", u + " --at", u + " --at abc", u + " --at 1e999", u + " --at nan",
			u + " --at 10 --width -1", u + " --at 10 --width 3841", u + " --at 10 --width x",
			u + " --at 10 --quality 0", u + " --at 10 --quality 101", u + " --at 10 --foo bar",
			"--option-inject --at 10"},
		"play":   {"", u + " --start", u + " --start soon", u + " --start inf", u + " " + u},
		"queue":  {"", "shuffle", "add", "add " + u + " " + u, "list now", "clear now"},
		"status": {"now"},
		"control": {"", "rewind", "pause now", "stop now", "next 1", "seek", "seek 1 2", "seek nan",
			"seek +inf", "seek ++5", "seek +-5", "volume", "volume 101", "volume -1", "volume nan"},
		"events": {"--until", "--until bogus", "--timeout", "--timeout 0", "--timeout -5",
			"--timeout nan", "--timeout inf", "extra"},
	}
	for verb, cases := range bad {
		for _, args := range cases {
			_, err := verbs[verb](strings.Fields(args))
			var f *engine.Fail
			if !errors.As(err, &f) || f.Code != 1 || f.Status != "error" {
				t.Errorf("jue %s %s: err = %v, want exit 1 status error", verb, args, err)
			}
		}
	}
	// A usage error leaves no trace: no runtime dir, socket, lock or scratch file.
	left, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range left {
		t.Errorf("usage errors left %s in TMPDIR", e.Name())
	}
}
