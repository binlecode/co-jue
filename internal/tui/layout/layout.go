// Package layout coordinates responsive multi-pane terminal geometry and whitespace gutter
// alignment, strictly isolating columns to prevent CJK width sawtooth tearing.
package layout

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// Breakpoint defines the responsive layout mode based on terminal width.
type Breakpoint int

const (
	// Compact is for narrow terminals (< 85 cols, e.g. 62x20). Single-column focused.
	Compact Breakpoint = iota
	// Standard is for medium terminals (85 <= cols < 96). Single-column focused.
	Standard
	// Wide is for wide terminals (cols >= 96). Dual-column (Stage + Inspector), workspace tabs in top header.
	Wide
)

func (b Breakpoint) String() string {
	switch b {
	case Compact:
		return "compact"
	case Standard:
		return "standard"
	case Wide:
		return "wide"
	default:
		return "unknown"
	}
}

// Rect defines a 2D bounding box on the character grid.
type Rect struct {
	X int // 0-based column index
	Y int // 0-based row index
	W int // width in character cells
	H int // height in character cells
}

// Empty reports whether the rectangle has zero area.
func (r Rect) Empty() bool {
	return r.W <= 0 || r.H <= 0
}

// Geometry is the complete calculated layout allocation for all components.
type Geometry struct {
	Cols int
	Rows int
	Mode Breakpoint

	Header    Rect
	Navbar    Rect
	Stage     Rect
	Inspector Rect
	Dock      Rect
}

// GutterWidth is the whitespace spacer between columns (pure spaces, no solid vertical bars).
const GutterWidth = 2

// Compute calculates the component bounding boxes for given terminal dimensions.
func Compute(cols, rows int) Geometry {
	if cols < 24 {
		cols = 24
	}
	if rows < 10 {
		rows = 10
	}

	var mode Breakpoint
	switch {
	case cols >= 96:
		mode = Wide
	case cols >= 85:
		mode = Standard
	default:
		mode = Compact
	}

	g := Geometry{
		Cols: cols,
		Rows: rows,
		Mode: mode,
	}

	// 1. Header is 1 line at the top
	g.Header = Rect{X: 0, Y: 0, W: cols, H: 1}

	// 2. Dock at the bottom
	dockH := 3
	if rows <= 20 {
		dockH = 2
	}
	if rows-1-dockH < 4 {
		dockH = max(1, rows-5)
	}
	g.Dock = Rect{X: 0, Y: rows - dockH, W: cols, H: dockH}

	// 3. Middle content vertical span
	contentY := g.Header.H
	contentH := g.Dock.Y - contentY
	if contentH < 1 {
		contentH = 1
	}

	// 4. Horizontal allocation by breakpoint
	switch mode {
	case Compact:
		// Single-column: Stage occupies full width
		g.Navbar = Rect{0, 0, 0, 0}
		g.Inspector = Rect{0, 0, 0, 0}
		g.Stage = Rect{X: 0, Y: contentY, W: cols, H: contentH}

	case Standard:
		// Standard mode (85 <= cols < 96):
		// Stage occupies full width for optimal CJK readability
		g.Navbar = Rect{0, 0, 0, 0}
		g.Inspector = Rect{0, 0, 0, 0}
		g.Stage = Rect{X: 0, Y: contentY, W: cols, H: contentH}

	case Wide:
		// Dual columns: Stage (flex-grow) + Inspector (34)
		// Navbar is integrated into the global top header tabs.
		g.Navbar = Rect{0, 0, 0, 0}
		inspW := 34
		stageW := cols - GutterWidth - inspW
		if stageW < 45 {
			inspW = max(28, cols-GutterWidth-45)
			stageW = cols - GutterWidth - inspW
		}

		g.Stage = Rect{X: 0, Y: contentY, W: stageW, H: contentH}
		g.Inspector = Rect{X: stageW + GutterWidth, Y: contentY, W: cols - (stageW + GutterWidth), H: contentH}
	}

	return g
}

// WidthMeasurer is a function that returns the visual character cell width of a string.
type WidthMeasurer func(string) int

// StripANSI removes ANSI SGR color/style escape codes.
func StripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if s[i] == 'm' {
				inEsc = false
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// JoinColumns horizontally joins lines of multiple columns with whitespace gutters,
// ensuring every line in each column is padded so columns never displace horizontally.
// If measure is nil, runewidth.StringWidth is used by default.
func JoinColumns(cols []string, colWidths []int, height int, gutter int, measure WidthMeasurer) string {
	if len(cols) == 0 {
		return ""
	}
	if len(cols) == 1 {
		return cols[0]
	}
	measureANSI := func(s string) int {
		plain := StripANSI(s)
		if measure != nil {
			return measure(plain)
		}
		return runewidth.StringWidth(plain)
	}

	lines := make([][]string, len(cols))
	for i, c := range cols {
		lines[i] = strings.Split(c, "\n")
	}

	gutterStr := strings.Repeat(" ", gutter)
	var sb strings.Builder

	for row := 0; row < height; row++ {
		for i := range cols {
			line := ""
			if row < len(lines[i]) {
				line = lines[i][row]
			}
			w := measureANSI(line)
			targetW := 0
			if i < len(colWidths) {
				targetW = colWidths[i]
			}

			// Pad column to its target width so following columns align strictly
			if i < len(cols)-1 && targetW > w {
				sb.WriteString(line)
				sb.WriteString(strings.Repeat(" ", targetW-w))
			} else {
				sb.WriteString(line)
			}

			if i < len(cols)-1 {
				sb.WriteString(gutterStr)
			}
		}
		if row < height-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}
