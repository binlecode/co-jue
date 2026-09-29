package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/binlecode/ting/internal/tui/layout"
	"github.com/binlecode/ting/internal/verb"
	tea "github.com/charmbracelet/bubbletea"
)

func TestStageModeToggle(t *testing.T) {
	ctx := context.Background()
	suite := &verb.Suite{}
	m := New(ctx, suite, Options{
		Engines: []verb.Engine{{Name: "yt"}},
		Query:   "ambient",
	})
	dur := 180.0
	pos := 45.0
	m.playTitle = "Midnight Flow"
	m.playerID = "test-player-1"
	m.all = []row{{Title: "Track A"}}
	m.rows = m.all
	m.last = &verb.Event{
		Ready:    true,
		Paused:   false,
		Duration: &dur,
		Position: &pos,
	}

	if m.stageMode {
		t.Fatalf("Initially stageMode should be false")
	}

	// Press Shift+F to enter Stage Mode
	m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	if !m.stageMode {
		t.Fatalf("Expected stageMode to be true after pressing F")
	}

	// In Stage Mode, View() should render stageModeView
	view := m.View()
	if !strings.Contains(view, "Back to Workbench") {
		t.Errorf("Stage Mode view must contain exit hint 'Back to Workbench', got: %s", view)
	}
	if !strings.Contains(view, "Midnight Flow") {
		t.Errorf("Stage Mode view must contain centered track title, got: %s", view)
	}

	// Press Esc to exit Stage Mode
	m.updateList(tea.KeyMsg{Type: tea.KeyEsc})
	if m.stageMode {
		t.Fatalf("Expected stageMode to be false after pressing Esc")
	}

	// In normal mode, View() should render workbench
	view = m.View()
	if strings.Contains(view, "Back to Workbench") {
		t.Errorf("Workbench view should NOT contain 'Back to Workbench'")
	}
}

func TestStageModeControls(t *testing.T) {
	ctx := context.Background()
	suite := &verb.Suite{}
	m := New(ctx, suite, Options{
		Engines: []verb.Engine{{Name: "yt"}},
		Query:   "ambient",
		Loop:    "off",
	})
	dur := 180.0
	pos := 45.0
	m.playTitle = "Midnight Flow"
	m.playerID = "test-player-1"
	m.all = []row{{Title: "Track A"}}
	m.rows = m.all
	m.last = &verb.Event{
		Ready:    true,
		Paused:   false,
		Duration: &dur,
		Position: &pos,
	}

	// Enter stage mode
	m.stageMode = true

	// Toggle pause
	m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	// Pause command was queued or sent without crashing
	if !m.stageMode {
		t.Errorf("Controls in stage mode should not exit stage mode")
	}

	// Cycle loop
	m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if m.opt.Loop != "seq" {
		t.Errorf("Expected loop mode to advance to 'seq', got %q", m.opt.Loop)
	}

	// Exit with 'Q'
	m.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	if m.stageMode {
		t.Errorf("Expected stageMode to be false after pressing Q")
	}
}

func TestStageModeKittyDelEmitted(t *testing.T) {
	ctx := context.Background()
	suite := &verb.Suite{}
	m := New(ctx, suite, Options{
		Engines: []verb.Engine{{Name: "yt"}},
		Query:   "ambient",
		Cover:   Cover{On: true, CW: 8, CH: 16},
	})
	dur := 180.0
	pos := 45.0
	m.playTitle = "Midnight Flow"
	m.playerID = "test-player-1"
	m.all = []row{{Title: "Track A", Thumb: "http://thumb.jpg"}}
	m.rows = m.all
	m.last = &verb.Event{
		Ready:    true,
		Duration: &dur,
		Position: &pos,
	}
	m.cover.on = true
	m.stageMode = true

	view := m.View()
	// Must contain kittyDel escape sequence to clear physical screen
	expectedDel := kittyDel(false)
	if !strings.Contains(view, expectedDel) {
		t.Errorf("Stage Mode with cover.on=true must emit kittyDel escape sequence %q, view ends with: %q",
			expectedDel, view[max(0, len(view)-30):])
	}
}

func TestInspectorSyncedLyrics(t *testing.T) {
	ins := newInspector()
	ins.SetBounds(mRect(0, 0, 36, 20))
	ins.UpdateTrack("Deep Ambient", "Album X", "Artist Y", "128k aac", "http://thumb.jpg")

	segs := []verb.Segment{
		{Start: 0, Duration: 5, Text: "First line of lyric"},
		{Start: 5, Duration: 5, Text: "Second line active"},
		{Start: 10, Duration: 5, Text: "Third line future"},
	}
	ins.UpdateLyrics(segs, 1, false, false)

	p := paletteFor(true, "kanagawa", Background{Light: false}, true)
	s := strsEN
	g := glyphsUTF
	w := newWidth(false)

	v := ins.View(p, s, g, w)
	if !strings.Contains(v, "NOW PLAYING / LYRIC STREAM") {
		t.Errorf("Inspector view should contain header, got: %s", v)
	}
	if !strings.Contains(v, "Deep Ambient") {
		t.Errorf("Inspector view should contain track title, got: %s", v)
	}
	if !strings.Contains(v, "Second line active") {
		t.Errorf("Inspector view should contain active lyric line, got: %s", v)
	}
	if !strings.Contains(v, "> Second line active") {
		t.Errorf("Active lyric line should have '> ' indicator, got: %s", v)
	}

	// Loading state test
	ins.UpdateLyrics(nil, -1, false, true)
	vLoading := ins.View(p, s, g, w)
	if !strings.Contains(vLoading, "(loading lyrics...)") {
		t.Errorf("Inspector loading view should contain loading indicator, got: %s", vLoading)
	}

	// No lyrics available test
	ins.UpdateLyrics(nil, -1, false, false)
	vNone := ins.View(p, s, g, w)
	if !strings.Contains(vNone, "(no synchronized lyrics available)") {
		t.Errorf("Inspector no-lyrics view should contain no lyrics indicator, got: %s", vNone)
	}
}

func mRect(x, y, w, h int) layout.Rect {
	return layout.Rect{X: x, Y: y, W: w, H: h}
}
