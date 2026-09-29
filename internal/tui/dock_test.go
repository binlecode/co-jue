package tui

import (
	"strings"
	"testing"

	"github.com/binlecode/ting/internal/tui/layout"
	"github.com/binlecode/ting/internal/verb"
)

func TestDockModelBannerIdle(t *testing.T) {
	m := &Model{
		g: glyphsUTF,
		s: strsEN,
		w: newWidth(false),
	}
	f := frame{rightEdge: 80, cols: 80}
	clock := PlayerClock{Active: false}

	dock := newDock()
	if banner := dock.Banner(m, f, clock); banner != "" {
		t.Errorf("Idle dock banner should be empty, got %q", banner)
	}
}

func TestDockModelBannerPlaying(t *testing.T) {
	dur := 240.0
	pos := 65.0
	m := &Model{
		g:         glyphsUTF,
		s:         strsEN,
		w:         newWidth(false),
		playTitle: "Lofi Hip Hop Beats",
		last: &verb.Event{
			Ready:    true,
			Paused:   false,
			Duration: &dur,
			Position: &pos,
		},
	}
	f := frame{rightEdge: 80, cols: 80, navOK: true}
	clock := PlayerClock{
		Active:  true,
		Playing: true,
		Pos:     pos,
		Dur:     dur,
	}

	dock := newDock()
	banner := dock.Banner(m, f, clock)
	if !strings.Contains(banner, "Playing: ") {
		t.Errorf("Playing banner must contain 'Playing: ', got %q", banner)
	}
	if !strings.Contains(banner, "01:05/04:00") {
		t.Errorf("Playing banner must contain time readout '01:05/04:00', got %q", banner)
	}
	if !strings.Contains(banner, "Lofi Hip Hop Beats") {
		t.Errorf("Playing banner must contain title, got %q", banner)
	}
}

func TestDockModelNarrowScreenPreservesTime(t *testing.T) {
	dur := 180.0
	pos := 30.0
	longTitle := "A Very Very Very Very Very Very Very Very Very Long Song Title That Will Truncate"
	m := &Model{
		g:         glyphsUTF,
		s:         strsEN,
		w:         newWidth(false),
		playTitle: longTitle,
		last: &verb.Event{
			Ready:    true,
			Paused:   false,
			Duration: &dur,
			Position: &pos,
		},
	}
	// Narrow screen: 45 cols
	f := frame{rightEdge: 45, cols: 45}
	clock := PlayerClock{
		Active:  true,
		Playing: true,
		Pos:     pos,
		Dur:     dur,
	}

	dock := newDock()
	banner := dock.Banner(m, f, clock)
	if !strings.Contains(banner, "00:30/03:00") {
		t.Errorf("Narrow screen banner must preserve time readout, got %q", banner)
	}
	if !strings.Contains(banner, "Playing: ") {
		t.Errorf("Narrow screen banner must contain 'Playing: ', got %q", banner)
	}
}

func TestDockModelMediaSpecs(t *testing.T) {
	codec := "aac"
	bitrate := 128000
	vol := 85.0
	m := &Model{
		g: glyphsUTF,
		s: strsEN,
		last: &verb.Event{
			Volume: &vol,
			Loop:   "seq",
			Media: &verb.Media{
				AudioCodec:   &codec,
				AudioBitrate: &bitrate,
			},
		},
	}
	clock := PlayerClock{Active: true}

	dock := newDock()
	specs := dock.MediaSpecs(m, clock)
	if !strings.Contains(specs, "128k aac") {
		t.Errorf("MediaSpecs must contain '128k aac', got %q", specs)
	}
	if !strings.Contains(specs, "85%") {
		t.Errorf("MediaSpecs must contain volume '85%%', got %q", specs)
	}
	if !strings.Contains(specs, "[seq]") {
		t.Errorf("MediaSpecs must contain '[seq]', got %q", specs)
	}
}

func TestDockModelView(t *testing.T) {
	dur := 100.0
	pos := 50.0
	m := &Model{
		g:         glyphsUTF,
		s:         strsEN,
		w:         newWidth(false),
		playTitle: "Test Song",
		last: &verb.Event{
			Ready:    true,
			Paused:   false,
			Duration: &dur,
			Position: &pos,
		},
	}
	f := frame{rightEdge: 60, cols: 60, barH: 1}
	clock := PlayerClock{
		Active:  true,
		Playing: true,
		Pos:     pos,
		Dur:     dur,
	}

	dock := newDock()
	dock.SetBounds(layout.Rect{X: 0, Y: 18, W: 60, H: 2})
	v := dock.View(m, f, clock)

	lines := strings.Split(v, "\n")
	if len(lines) < 2 {
		t.Fatalf("Dock View with barH=1 should have at least 2 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "Playing: Test Song") {
		t.Errorf("Line 0 should contain banner, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "█") {
		t.Errorf("Line 1 should contain progress bar, got %q", lines[1])
	}
}
