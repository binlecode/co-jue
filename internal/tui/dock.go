package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/binlecode/ting/internal/tui/layout"
)

// PlayerClock represents the unified monotonic clock snapshot for the player.
type PlayerClock struct {
	Active    bool      // whether a player is active (playerID != "")
	Playing   bool      // ready and not paused
	Paused    bool      // paused
	Buffering bool      // starting or loading
	Live      bool      // live stream
	Pos       float64   // extrapolated position in seconds
	Dur       float64   // total duration in seconds
	StartedAt time.Time // when playback started (for live timer)
}

// dockModel manages the persistent audio control dock anchored at the bottom of the screen.
type dockModel struct {
	bounds layout.Rect
}

func newDock() dockModel {
	return dockModel{}
}

func (d *dockModel) SetBounds(r layout.Rect) {
	d.bounds = r
}

// Banner produces the Now-Playing line: state, title, and on the right the clock, footprint,
// and key hints when the hint block has no room.
func (d *dockModel) Banner(m *Model, f frame, clock PlayerClock) string {
	if !clock.Active {
		return ""
	}

	p, g := m.p, m.g
	icon, label, color := g.Play, m.s.Playing, p.Play
	switch {
	case clock.Buffering || m.last == nil:
		icon, label, color = g.Spin[m.spin%len(g.Spin)], m.s.Starting, p.Dim+p.Accent
	case clock.Paused:
		icon, label, color = g.Pause, m.s.Paused, p.PauseC
	}
	head := icon + " " + label + ": "

	tail := ""
	switch {
	case clock.Live:
		tail = g.TimeL + fmtSec(timeSince(clock.StartedAt)) + " " + g.Live + g.TimeR
	default:
		cur := "--:--"
		if clock.Pos >= 0 {
			cur = fmtSec(clock.Pos)
		}
		total := "--:--"
		if clock.Dur > 0 {
			total = fmtSec(clock.Dur)
		}
		tail = g.TimeL + cur + "/" + total + g.TimeR
	}

	res := ""
	if m.opt.Resource && m.cpu != nil && m.mem != nil {
		res = fmt.Sprintf(" %s %s %.0f%% %s %.0fM", g.Sep, g.CPU, *m.cpu, g.RAM, *m.mem)
	}

	hintS := ""
	if !f.navOK && m.opt.Keys != "hidden" {
		hintS = "  [Space " + m.s.Pause + " | s " + m.s.Stop + " | -/= " + m.s.Vol + "]"
	}

	gap := 2
	room := func() int {
		return f.rightEdge - m.w.of(head) - m.w.of(tail) - m.w.of(res) - m.w.of(hintS) - gap
	}
	if room() < layoutMinField {
		hintS = ""
	}
	if room() < layoutMinField {
		res = ""
	}
	// Never drop tail (time info) if room is tight; truncate title with ellipsis instead
	availForTitle := f.rightEdge - m.w.of(head) - m.w.of(tail) - m.w.of(res) - m.w.of(hintS) - gap
	if availForTitle < 0 {
		availForTitle = 0
	}
	title := m.w.trunc(m.playTitle, availForTitle, g.Ell)
	right := tail + res + hintS
	out := color + icon + " " + label + ":" + p.Reset + " " + p.Accent + title + p.Reset
	if right != "" {
		pad := f.rightEdge - m.w.of(head+title) - m.w.of(right)
		if pad < 1 {
			pad = 1
		}
		var rightParts strings.Builder
		if tail != "" {
			rightParts.WriteString(p.Secondary + tail + p.Reset)
		}
		if res != "" {
			rightParts.WriteString(p.Muted + res + p.Reset)
		}
		if hintS != "" {
			rightParts.WriteString(p.Muted + hintS + p.Reset)
		}
		out += strings.Repeat(" ", pad) + rightParts.String()
	}
	return out
}

// ProgressBar renders the 1/8 sub-character progress bar for the given width.
func (d *dockModel) ProgressBar(m *Model, width int, clock PlayerClock) string {
	if width < 10 {
		width = 10
	}
	frac := 0.0
	if clock.Dur > 0 {
		frac = min(max(clock.Pos/clock.Dur, 0), 1)
	}
	filled, part := barCells(frac, width, len(m.g.Part)+1)

	// Focused chapter's span when list is chapters of what is playing
	a, b := -1, -1
	if st, end := m.chapterSpan(); clock.Dur > 0 && end > st {
		a = min(int(st*float64(width)/clock.Dur), width-1)
		b = min(int(end*float64(width)/clock.Dur)-1, width-1)
		if b <= a+1 {
			b = -1
		}
	}

	var played, rest strings.Builder
	for c := 0; c < width; c++ {
		switch {
		case c == a || c == b:
			if c < filled {
				played.WriteString(" ")
			} else {
				rest.WriteString(" ")
			}
		case c < filled:
			played.WriteString(m.g.Fill)
		case c == filled && part > 0:
			played.WriteString(m.g.Part[part-1])
		default:
			rest.WriteString(m.g.Rest)
		}
	}
	return m.p.Accent + played.String() + m.p.Reset + m.p.Muted + rest.String() + m.p.Reset
}

// MediaSpecs formats audio decoding parameters (codec, bitrate, sample rate, channels, volume, loop).
func (d *dockModel) MediaSpecs(m *Model, clock PlayerClock) string {
	if !clock.Active || m.last == nil {
		return ""
	}

	var parts []string

	// Audio codec & bitrate
	if m.last.Media != nil {
		med := m.last.Media
		spec := ""
		if med.AudioBitrate != nil && *med.AudioBitrate > 0 {
			spec = fmt.Sprintf("%dk", *med.AudioBitrate/1000)
		}
		if med.AudioCodec != nil && *med.AudioCodec != "" {
			if spec != "" {
				spec += " " + *med.AudioCodec
			} else {
				spec = *med.AudioCodec
			}
		}
		if spec != "" {
			parts = append(parts, spec)
		}
	}

	// Volume
	vol := ""
	if m.volTarget != nil {
		vol = fmt.Sprintf("%d%%", *m.volTarget)
	} else if m.last.Volume != nil {
		vol = fmt.Sprintf("%.0f%%", *m.last.Volume)
	} else if m.opt.Play.Volume != "" {
		vol = m.opt.Play.Volume + "%"
	}
	if vol != "" {
		parts = append(parts, m.s.Vol+" "+vol)
	}

	// Loop mode
	loop := m.opt.Loop
	if m.last.Loop != "" {
		loop = m.last.Loop
	}
	if loop != "off" {
		parts = append(parts, "["+loop+"]")
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "  "+m.g.Sep+"  ")
}

// View composes the dock block. If no player is active, it returns empty string.
func (d *dockModel) View(m *Model, f frame, clock PlayerClock) string {
	if !clock.Active {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(d.Banner(m, f, clock))

	if f.barH > 0 {
		sb.WriteByte('\n')
		sb.WriteString(d.ProgressBar(m, f.cols-1, clock))
	}

	return sb.String()
}
