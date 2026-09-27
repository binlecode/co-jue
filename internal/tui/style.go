package tui

// ThemeNames is every palette family --theme and TING_THEME accept.
var ThemeNames = []string{"minimal", "mono", "catppuccin", "tokyonight", "nord", "gruvbox",
	"onedark", "dracula", "rosepine", "everforest", "kanagawa", "solarized", "monokai"}

// palette is the SGR set one frame is drawn with. Raw sequences, not a style library: the
// frame's widths are measured on the plain text, and each field is wrapped in exactly the
// escape the shell TUI sends for it. With colours off every field but the two highlights is
// empty, the same split the shell TUI makes.
type palette struct {
	Reset, Bold, Dim, Accent, Mark, Play, PauseC, RowHL, RowEnd, Keycap string
}

func paletteFor(colors bool) palette {
	p := palette{RowHL: "\x1b[7m", RowEnd: "\x1b[0m"}
	if !colors {
		return p
	}
	p.Reset, p.Bold, p.Dim = "\x1b[0m", "\x1b[1m", "\x1b[2m"
	p.Accent, p.Mark = "\x1b[36m", "\x1b[1;36m"
	p.Play, p.PauseC = "\x1b[32m", "\x1b[33m"
	p.Keycap = "\x1b[100;97m"
	return p
}
