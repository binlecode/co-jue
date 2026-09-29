package layout

import (
	"strings"
	"testing"
)

func TestBreakpoints(t *testing.T) {
	tests := []struct {
		cols, rows int
		wantMode   Breakpoint
	}{
		{62, 20, Compact},
		{80, 24, Compact},
		{84, 24, Compact},
		{85, 24, Standard},
		{90, 24, Standard},
		{95, 30, Standard},
		{96, 30, Wide},
		{100, 30, Wide},
		{124, 30, Wide},
		{125, 40, Wide},
		{160, 50, Wide},
	}

	for _, tt := range tests {
		g := Compute(tt.cols, tt.rows)
		if g.Mode != tt.wantMode {
			t.Errorf("Compute(%d, %d).Mode = %v, want %v", tt.cols, tt.rows, g.Mode, tt.wantMode)
		}
	}
}

func TestCompactMode(t *testing.T) {
	// Narrow screen: 62x20 as used in drive.sh
	g := Compute(62, 20)

	if g.Mode != Compact {
		t.Fatalf("want Compact, got %v", g.Mode)
	}
	if !g.Navbar.Empty() {
		t.Errorf("Navbar in compact mode should be empty, got %+v", g.Navbar)
	}
	if !g.Inspector.Empty() {
		t.Errorf("Inspector in compact mode should be empty, got %+v", g.Inspector)
	}
	if g.Stage.W != 62 {
		t.Errorf("Stage width in compact mode should be full cols (62), got %d", g.Stage.W)
	}
	if g.Dock.H != 2 {
		t.Errorf("Dock height for rows=20 should be 2, got %d", g.Dock.H)
	}
	if g.Stage.H+g.Header.H+g.Dock.H != 20 {
		t.Errorf("Vertical heights do not sum to rows: header=%d, stage=%d, dock=%d, total=%d",
			g.Header.H, g.Stage.H, g.Dock.H, g.Stage.H+g.Header.H+g.Dock.H)
	}
}

func TestStandardMode(t *testing.T) {
	// Standard screen: 90x24 (85 <= cols < 96)
	g := Compute(90, 24)

	if g.Mode != Standard {
		t.Fatalf("want Standard, got %v", g.Mode)
	}
	if !g.Navbar.Empty() {
		t.Errorf("Navbar in standard mode should be empty (collapsed), got %+v", g.Navbar)
	}
	if !g.Inspector.Empty() {
		t.Errorf("Inspector in standard mode should be empty, got %+v", g.Inspector)
	}
	if g.Stage.W != 90 {
		t.Errorf("Stage width in standard mode should be full cols (90), got %d", g.Stage.W)
	}
	if g.Stage.H+g.Header.H+g.Dock.H != 24 {
		t.Errorf("Vertical heights do not sum to rows: header=%d, stage=%d, dock=%d, total=%d",
			g.Header.H, g.Stage.H, g.Dock.H, g.Stage.H+g.Header.H+g.Dock.H)
	}
}

func TestWideMode(t *testing.T) {
	// Wide screen: 140x40 (cols >= 96, dual-column Stage + Inspector)
	g := Compute(140, 40)

	if g.Mode != Wide {
		t.Fatalf("want Wide, got %v", g.Mode)
	}
	if !g.Navbar.Empty() {
		t.Errorf("Navbar in wide mode should be empty (moved to header tabs), got %+v", g.Navbar)
	}
	if g.Inspector.Empty() {
		t.Errorf("Inspector in wide mode should not be empty, got %+v", g.Inspector)
	}
	if g.Stage.X != 0 {
		t.Errorf("Stage in wide mode should start at X=0, got %d", g.Stage.X)
	}
	totalW := g.Stage.W + GutterWidth + g.Inspector.W
	if totalW != 140 {
		t.Errorf("Dual columns do not sum to cols: stage=%d, insp=%d, sum=%d",
			g.Stage.W, g.Inspector.W, totalW)
	}
	if g.Stage.H+g.Header.H+g.Dock.H != 40 {
		t.Errorf("Vertical heights do not sum to rows: header=%d, stage=%d, dock=%d, total=%d",
			g.Header.H, g.Stage.H, g.Dock.H, g.Stage.H+g.Header.H+g.Dock.H)
	}
}

func TestClamping(t *testing.T) {
	// Degenerate small terminal
	g := Compute(10, 5)

	if g.Cols != 24 || g.Rows != 10 {
		t.Errorf("Did not clamp minimum dimensions: cols=%d, rows=%d", g.Cols, g.Rows)
	}
	if g.Stage.W <= 0 || g.Stage.H <= 0 {
		t.Errorf("Stage dimensions invalid on clamped terminal: %+v", g.Stage)
	}
}

func TestJoinColumnsPadding(t *testing.T) {
	// Uneven lines in column 0 (width 6):
	// line 0: "Short" (5 chars) -> should be padded with 1 space
	// line 1: "Loongg" (6 chars) -> exact width
	// line 2: "A" (1 char) -> should be padded with 5 spaces
	col1 := "Short\nLoongg\nA"
	col2 := "Col2Line1\nCol2Line2\nCol2Line3"

	colWidths := []int{6, 10}
	gutter := 2

	res := JoinColumns([]string{col1, col2}, colWidths, 3, gutter, nil)
	lines := strings.Split(res, "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d", len(lines))
	}

	// Line 0: "Short " (6) + "  " (2) + "Col2Line1" (9)
	want0 := "Short   Col2Line1"
	if lines[0] != want0 {
		t.Errorf("Line 0 = %q, want %q", lines[0], want0)
	}

	// Line 1: "Loongg" (6) + "  " (2) + "Col2Line2" (9)
	want1 := "Loongg  Col2Line2"
	if lines[1] != want1 {
		t.Errorf("Line 1 = %q, want %q", lines[1], want1)
	}

	// Line 2: "A     " (6) + "  " (2) + "Col2Line3" (9)
	want2 := "A       Col2Line3"
	if lines[2] != want2 {
		t.Errorf("Line 2 = %q, want %q", lines[2], want2)
	}
}
