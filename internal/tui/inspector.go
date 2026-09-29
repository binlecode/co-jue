package tui

import (
	"strings"

	"github.com/binlecode/ting/internal/tui/layout"
	"github.com/binlecode/ting/internal/verb"
)

// inspectorModel manages the right-hand inspection window (synced lyrics and Kitty cover box).
type inspectorModel struct {
	bounds      layout.Rect
	lyrics      []verb.Segment
	activeLyric int
	interlude   bool
	loading     bool
	trackTitle  string
	albumName   string
	artistName  string
	audioSpec   string
	thumbURL    string
}

func newInspector() inspectorModel {
	return inspectorModel{
		activeLyric: -1,
	}
}

func (ins *inspectorModel) SetBounds(r layout.Rect) {
	ins.bounds = r
}

func (ins *inspectorModel) UpdateTrack(title, album, artist, audioSpec, thumbURL string) {
	ins.trackTitle = title
	ins.albumName = album
	ins.artistName = artist
	ins.audioSpec = audioSpec
	ins.thumbURL = thumbURL
}

func (ins *inspectorModel) UpdateLyrics(segments []verb.Segment, activeIdx int, interlude bool, loading bool) {
	ins.lyrics = segments
	ins.activeLyric = activeIdx
	ins.interlude = interlude
	ins.loading = loading
}

// CoverBox returns the absolute screen coordinates (1-based row and col) for two-pass Kitty
// placement: the first line under the header, which View leaves blank for it. bounds.Y is the
// 0-based screen row the column starts on, set by the frame that draws it.
func (ins *inspectorModel) CoverBox() (row, col, w, h int, ok bool) {
	if ins.bounds.Empty() || ins.thumbURL == "" || ins.bounds.H < 1+coverRows {
		return 0, 0, 0, 0, false
	}
	return ins.bounds.Y + 2, ins.bounds.X + 1, min(coverCols, ins.bounds.W), min(coverRows, ins.bounds.H), true
}

// View outputs pure text with blank padding for the cover art, strictly avoiding raw Kitty escapes
// inside the multi-column text stream to prevent Lipgloss line-splitting corruption.
// coverOn is whether a cover can be drawn at all: without it there is no image to make room
// for, and the blank block would only push the lyrics down.
func (ins *inspectorModel) View(p palette, s strs, g glyphs, w width, coverOn bool) string {
	if ins.bounds.Empty() {
		return ""
	}

	var sb strings.Builder

	// Header
	sb.WriteString(p.Bold + s.InspectorTitle + p.Reset + "\n")

	// Cover art placeholder (blank area so two-pass Kitty placement has a clean canvas). The
	// image is always coverRows tall, so the room left for it is too.
	if ins.thumbURL != "" && coverOn && ins.bounds.H >= 1+coverRows {
		for i := 0; i < coverRows; i++ {
			sb.WriteString("\n")
		}
	}

	// Track meta
	if ins.trackTitle != "" {
		sb.WriteString(p.Bold + p.Accent + w.trunc(ins.trackTitle, ins.bounds.W, g.Ell) + p.Reset + "\n")
	}
	if ins.artistName != "" {
		sb.WriteString(p.Secondary + w.trunc(ins.artistName, ins.bounds.W, g.Ell) + p.Reset + "\n")
	}
	if ins.albumName != "" {
		sb.WriteString(p.Secondary + w.trunc(ins.albumName, ins.bounds.W, g.Ell) + p.Reset + "\n")
	}
	if ins.audioSpec != "" {
		sb.WriteString(p.Muted + w.trunc(ins.audioSpec, ins.bounds.W, g.Ell) + p.Reset + "\n")
	}
	sb.WriteString(p.Muted + strings.Repeat("-", min(ins.bounds.W, 30)) + p.Reset + "\n")

	// Synced lyric stream
	if len(ins.lyrics) > 0 {
		active := ins.activeLyric
		if active < 0 {
			active = 0
		}
		start := max(0, active-1)
		end := min(len(ins.lyrics), active+3)
		for i := start; i < end; i++ {
			lineText := ins.lyrics[i].Text
			switch {
			case ins.interlude && i == active:
				sb.WriteString(p.Muted + "  " + w.trunc(lineText, ins.bounds.W-2, g.Ell) + p.Reset + "\n")
				sb.WriteString(p.Accent + s.LyricInterlude + p.Reset + "\n")
			case i == active:
				sb.WriteString(p.Bold + p.Accent + "> " + w.trunc(lineText, ins.bounds.W-2, g.Ell) + p.Reset + "\n")
			case i < active:
				sb.WriteString(p.Muted + "  " + w.trunc(lineText, ins.bounds.W-2, g.Ell) + p.Reset + "\n")
			default:
				sb.WriteString(p.Secondary + "  " + w.trunc(lineText, ins.bounds.W-2, g.Ell) + p.Reset + "\n")
			}
		}
	} else if ins.loading {
		sb.WriteString(p.Muted + "  " + s.LyricLoading + p.Reset + "\n")
	} else if ins.trackTitle != "" {
		sb.WriteString(p.Muted + "  " + s.NoLyricsAvail + p.Reset + "\n")
	}

	return sb.String()
}
