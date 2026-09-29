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

// CoverBox returns the absolute screen coordinates (1-based row and col) for two-pass Kitty placement.
func (ins *inspectorModel) CoverBox() (row, col, w, h int, ok bool) {
	if ins.bounds.Empty() || ins.thumbURL == "" {
		return 0, 0, 0, 0, false
	}
	// Placed at the top of the inspector rectangle
	return ins.bounds.Y + 1, ins.bounds.X + 1, min(14, ins.bounds.W), min(7, ins.bounds.H), true
}

// View outputs pure text with blank padding for the cover art, strictly avoiding raw Kitty escapes
// inside the multi-column text stream to prevent Lipgloss line-splitting corruption.
func (ins *inspectorModel) View(p palette, s strs, g glyphs, w width) string {
	if ins.bounds.Empty() {
		return ""
	}

	var sb strings.Builder

	// Header
	sb.WriteString(p.Bold + s.InspectorTitle + p.Reset + "\n")

	// Cover art placeholder (blank area so two-pass Kitty placement has a clean canvas)
	coverH := 7
	if ins.bounds.H < 18 {
		coverH = 4
	}
	if ins.thumbURL != "" {
		for i := 0; i < coverH; i++ {
			sb.WriteString(strings.Repeat(" ", ins.bounds.W) + "\n")
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
