package tui

import (
	"strings"
	"testing"

	"github.com/binlecode/ting/internal/verb"
)

// barAt is the bar for a paused player at pos of dur, with no colours, so the frame is glyphs.
func barAt(g glyphs, pos, dur float64, width int) string {
	m := &Model{g: g, last: &verb.Event{Position: &pos, Duration: &dur}}
	return m.progressBar(width)
}

func TestBarCells(t *testing.T) {
	for _, c := range []struct {
		frac               float64
		width, steps, f, p int
	}{
		{0, 20, 8, 0, 0},
		{1, 20, 8, 20, 0},
		{-0.5, 20, 8, 0, 0},
		{1.5, 20, 8, 20, 0},
		{1.0 / 160, 20, 8, 0, 1},  // one eighth of the first cell
		{0.5 / 20, 20, 8, 0, 4},   // half the first cell
		{7.99 / 160, 20, 8, 0, 7}, // just short of a whole cell
		{0.5 + 3.0/160, 20, 8, 10, 3},
		{0.999999, 20, 8, 19, 7}, // the last cell, never past it
		{0.55, 20, 1, 11, 0},     // ASCII: whole cells only
	} {
		if f, p := barCells(c.frac, c.width, c.steps); f != c.f || p != c.p {
			t.Errorf("barCells(%v, %d, %d) = %d, %d; want %d, %d", c.frac, c.width, c.steps, f, p, c.f, c.p)
		}
	}
}

func TestProgressBarSubCell(t *testing.T) {
	for _, c := range []struct {
		pos  float64
		want string
	}{
		{0, strings.Repeat("─", 20)},
		{100, strings.Repeat("█", 20)},
		{130, strings.Repeat("█", 20)}, // past the end is the end
		{-5, strings.Repeat("─", 20)},
		{0.625, "▏" + strings.Repeat("─", 19)},                         // 1/8 of a 5 s cell
		{52.5, strings.Repeat("█", 10) + "▌" + strings.Repeat("─", 9)}, // half into cell 11
		{99.9, strings.Repeat("█", 19) + "▉"},                          // 7/8 of the last cell
		{50, strings.Repeat("█", 10) + strings.Repeat("─", 10)},        // on a cell edge: no partial
	} {
		if got := barAt(glyphsUTF, c.pos, 100, 20); got != c.want {
			t.Errorf("pos %v: %q, want %q", c.pos, got, c.want)
		}
	}
	// Every step of a cell is a distinct glyph, and the bar is always exactly width cells.
	w := newWidth(false)
	prev := ""
	for i := 0; i <= 8*20; i++ {
		got := barAt(glyphsUTF, float64(i)*100/160, 100, 20)
		if n := w.of(got); n != 20 {
			t.Fatalf("step %d: %d cells, want 20: %q", i, n, got)
		}
		if got == prev {
			t.Errorf("step %d draws the same bar as step %d: %q", i, i-1, got)
		}
		prev = got
	}
}

func TestProgressBarASCII(t *testing.T) {
	if got, want := barAt(glyphsASCII, 52.5, 100, 20), strings.Repeat("=", 10)+strings.Repeat("-", 10); got != want {
		t.Errorf("ascii: %q, want %q", got, want)
	}
	if got := barAt(glyphsASCII, 100, 100, 20); got != strings.Repeat("=", 20) {
		t.Errorf("ascii full: %q", got)
	}
}

func TestProgressBarNarrowAndUnknown(t *testing.T) {
	w := newWidth(false)
	for _, width := range []int{-3, 0, 1, 9} {
		if got := barAt(glyphsUTF, 33, 100, width); w.of(got) != 10 {
			t.Errorf("width %d: %d cells, want the 10-cell floor: %q", width, w.of(got), got)
		}
	}
	// No duration, or no position: an empty bar, not a guess.
	m := &Model{g: glyphsUTF, last: &verb.Event{}}
	if got := m.progressBar(20); got != strings.Repeat("─", 20) {
		t.Errorf("no duration: %q", got)
	}
	m.last = nil
	if got := m.progressBar(20); got != strings.Repeat("─", 20) {
		t.Errorf("no event: %q", got)
	}
}

// The chapter marks are blank cells at the span's ends; the partial cell never paints over one.
func TestProgressBarChapterMarksSurvive(t *testing.T) {
	url := "https://www.youtube.com/watch?v=abc"
	end := 60.0
	bar := func(pos float64) string {
		dur := 100.0
		return (&Model{
			g: glyphsUTF, last: &verb.Event{Position: &pos, Duration: &dur},
			src: srcChapters, playEngine: "yt", playURL: url,
			info: &info{chapters: []verb.Chapter{{Start: 20, End: &end}}},
			rows: []row{{Engine: "yt", URL: url + "&t=20", Sec: 20}},
		}).progressBar(20)
	}
	cells := func(s string) []string { return strings.Split(s, "") }
	for _, pos := range []float64{0, 21, 22.5, 40, 57.5, 58.1, 61, 100} {
		c := cells(bar(pos))
		if len(c) != 20 {
			t.Fatalf("pos %v: %d cells", pos, len(c))
		}
		if c[4] != " " || c[11] != " " {
			t.Errorf("pos %v: marks at 4 and 11 gone: %q", pos, bar(pos))
		}
	}
	// 22.5 of 100 over 20 cells is cell 4 half-played — that cell is the mark, so it stays blank.
	if got, want := bar(22.5), strings.Repeat("█", 4)+" "+strings.Repeat("─", 6)+" "+strings.Repeat("─", 8); got != want {
		t.Errorf("head on a mark: %q, want %q", got, want)
	}
}
