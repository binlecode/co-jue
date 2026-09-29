package tui

import (
	"fmt"
	"strings"
)

// centerText centers s within the given width using the model's display width calculator.
func (m *Model) centerText(s string, width int) string {
	w := m.w.of(s)
	if w >= width {
		return m.w.trunc(s, width, m.g.Ell)
	}
	pad := (width - w) / 2
	return strings.Repeat(" ", pad) + s
}

// stageModeView renders the full-screen immersive karaoke stage.
// It centers the track title, artist, audio specs, large cover space, and flowing lyrics.
func (m *Model) stageModeView() string {
	cols, rows := m.width, m.height
	if cols < 24 || rows < 10 {
		cols, rows = max(cols, 24), max(rows, 10)
	}

	var sb strings.Builder

	// Top status & exit hint
	hint := m.s.StageModeBack
	topLine := m.p.Muted + hint + m.p.Reset
	sb.WriteString(m.w.pad(topLine, cols) + "\n\n")

	// Track Title
	title := m.playTitle
	if title == "" {
		title = m.s.UResults
	}
	titleLine := m.p.Bold + m.p.Accent + m.w.trunc(title, cols-4, m.g.Ell) + m.p.Reset
	sb.WriteString(m.centerText(titleLine, cols) + "\n")

	// Media Specs line
	clock := m.playerClock()
	specs := m.dock.MediaSpecs(m, clock)
	if specs != "" {
		sb.WriteString(m.centerText(m.p.Secondary+specs+m.p.Reset, cols) + "\n")
	}
	sb.WriteString("\n")

	// Synced flowing lyrics (up to 5 lines: past lines dimmed, active line bold accent, future preview)
	lyr := m.currentLyric
	if lyr == nil && m.playURL != "" {
		lyr = m.lyricsCache[m.playURL]
	}

	if lyr != nil && lyr.status == lyricReady && len(lyr.segments) > 0 {
		activeIdx, inter := m.activeLyricIndex(clock.Pos)
		start := max(0, activeIdx-2)
		end := min(len(lyr.segments), activeIdx+3)
		if activeIdx < 0 {
			start = 0
			end = min(len(lyr.segments), 4)
		}

		for i := start; i < end; i++ {
			text := lyr.segments[i].Text
			switch {
			case inter && i == activeIdx:
				sb.WriteString(m.centerText(m.p.Muted+text+m.p.Reset, cols) + "\n")
				sb.WriteString(m.centerText(m.p.Accent+m.s.LyricInterlude+m.p.Reset, cols) + "\n")
			case i == activeIdx:
				sb.WriteString(m.centerText(m.p.Bold+m.p.Accent+"> "+text+" <"+m.p.Reset, cols) + "\n")
			case i < activeIdx:
				sb.WriteString(m.centerText(m.p.Muted+text+m.p.Reset, cols) + "\n")
			default:
				sb.WriteString(m.centerText(m.p.Secondary+text+m.p.Reset, cols) + "\n")
			}
		}
	} else {
		sb.WriteString(m.centerText(m.p.Muted+m.s.NoLyricsAvail+m.p.Reset, cols) + "\n")
	}

	// Bottom Scrubber & Time readout
	if clock.Active {
		sb.WriteString("\n" + m.centerText(m.dock.ProgressBar(m, min(cols-10, 60), clock), cols) + "\n")
		timeStr := "--:-- / --:--"
		if clock.Dur > 0 {
			timeStr = fmt.Sprintf("%s / %s", fmtSec(clock.Pos), fmtSec(clock.Dur))
		}
		sb.WriteString(m.centerText(m.p.Secondary+timeStr+m.p.Reset, cols) + "\n")
	}

	lines := strings.Split(sb.String(), "\n")
	if len(lines) > rows {
		lines = lines[:rows]
	}
	return strings.Join(lines, "\n")
}
