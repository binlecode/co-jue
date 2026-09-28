package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/muesli/termenv"
)

// ThemeNames is every palette family --theme and TING_THEME accept.
var ThemeNames = []string{"minimal", "mono", "catppuccin", "tokyonight", "nord", "gruvbox",
	"onedark", "dracula", "rosepine", "everforest", "kanagawa", "solarized", "monokai"}

// ThemeCycle is the order t walks: the two plain ones at its ends. The help states it too.
var ThemeCycle = []string{"minimal", "catppuccin", "tokyonight", "gruvbox", "rosepine", "nord",
	"kanagawa", "everforest", "onedark", "monokai", "solarized", "dracula", "mono"}

type rgb struct{ r, g, b int }

// hues is one community theme on one background: a signature accent and its own play and
// pause hues, each with the ANSI-16 code it falls back to without truecolor.
type hues struct {
	acc, play, pause    rgb
	acc16, play16, pa16 int
}

var themes = map[string]hues{
	"catppuccin:dark":  {rgb{203, 166, 247}, rgb{166, 227, 161}, rgb{249, 226, 175}, 35, 32, 33},
	"catppuccin:light": {rgb{136, 57, 239}, rgb{64, 160, 43}, rgb{223, 142, 29}, 35, 32, 33},
	"tokyonight:dark":  {rgb{122, 162, 247}, rgb{158, 206, 106}, rgb{224, 175, 104}, 34, 32, 33},
	"tokyonight:light": {rgb{46, 125, 233}, rgb{88, 117, 57}, rgb{140, 108, 62}, 34, 32, 33},
	"nord:dark":        {rgb{136, 192, 208}, rgb{163, 190, 140}, rgb{235, 203, 139}, 36, 32, 33},
	"nord:light":       {rgb{94, 129, 172}, rgb{163, 190, 140}, rgb{235, 203, 139}, 36, 32, 33},
	"gruvbox:dark":     {rgb{214, 93, 14}, rgb{184, 187, 38}, rgb{250, 189, 47}, 33, 32, 33},
	"gruvbox:light":    {rgb{175, 58, 3}, rgb{121, 116, 14}, rgb{181, 118, 20}, 31, 32, 33},
	"onedark:dark":     {rgb{97, 175, 239}, rgb{152, 195, 121}, rgb{229, 192, 123}, 34, 32, 33},
	"onedark:light":    {rgb{64, 120, 242}, rgb{80, 161, 79}, rgb{193, 132, 1}, 34, 32, 33},
	"dracula:dark":     {rgb{189, 147, 249}, rgb{80, 250, 123}, rgb{241, 250, 140}, 35, 32, 33},
	"dracula:light":    {rgb{100, 74, 201}, rgb{20, 113, 10}, rgb{132, 110, 21}, 35, 32, 33},
	"rosepine:dark":    {rgb{235, 188, 186}, rgb{49, 116, 143}, rgb{246, 193, 119}, 35, 36, 33},
	"rosepine:light":   {rgb{180, 99, 122}, rgb{40, 105, 131}, rgb{234, 157, 52}, 35, 36, 33},
	"everforest:dark":  {rgb{167, 192, 128}, rgb{131, 192, 146}, rgb{230, 152, 117}, 32, 36, 33},
	"everforest:light": {rgb{141, 161, 1}, rgb{53, 167, 124}, rgb{245, 125, 0}, 32, 36, 33},
	"kanagawa:dark":    {rgb{228, 104, 118}, rgb{152, 187, 108}, rgb{230, 195, 132}, 31, 32, 33},
	"kanagawa:light":   {rgb{200, 64, 83}, rgb{111, 137, 78}, rgb{204, 109, 0}, 31, 32, 33},
	"solarized:dark":   {rgb{42, 161, 152}, rgb{133, 153, 0}, rgb{181, 137, 0}, 36, 32, 33},
	"solarized:light":  {rgb{42, 161, 152}, rgb{133, 153, 0}, rgb{181, 137, 0}, 36, 32, 33},
	"monokai:dark":     {rgb{249, 38, 114}, rgb{166, 226, 46}, rgb{253, 151, 31}, 35, 32, 33},
	"monokai:light":    {rgb{249, 38, 114}, rgb{166, 226, 46}, rgb{253, 151, 31}, 35, 32, 33},
}

// Background is the terminal's ground: light or dark, and its colour when the terminal said.
type Background struct {
	Light bool
	RGB   *rgb
}

// DetectBackground resolves TING_BG: light and dark are answers; auto reads COLORFGBG,
// then asks the terminal (OSC 11) outside tmux, then settles on dark. The query is bounded:
// termenv sends a cursor report behind it and waits on select(), so a terminal that ignores
// OSC 11 answers at once, and nothing is left reading stdin afterwards.
func DetectBackground(mode string) Background {
	switch mode {
	case "light":
		return Background{Light: true}
	case "dark":
		return Background{}
	}
	if fgbg := os.Getenv("COLORFGBG"); strings.Contains(fgbg, ";") {
		if b, err := strconv.Atoi(fgbg[strings.LastIndexByte(fgbg, ';')+1:]); err == nil {
			return Background{Light: b == 15}
		}
	}
	if os.Getenv("TMUX") != "" || os.Getenv("TERM") == "dumb" {
		return Background{}
	}
	c, ok := termenv.NewOutput(os.Stdout).BackgroundColor().(termenv.RGBColor)
	if !ok {
		return Background{}
	}
	var v rgb
	if _, err := fmt.Sscanf(string(c), "#%02x%02x%02x", &v.r, &v.g, &v.b); err != nil {
		return Background{}
	}
	return Background{Light: v.r+v.g+v.b > 384, RGB: &v}
}

// palette is the SGR set one frame is drawn with. Raw sequences, not a style library: the
// frame's widths are measured on the plain text, and each field is wrapped in exactly the
// escape it needs. With colours off every field but the two highlights is empty.
type palette struct {
	Reset, Bold, Dim, Accent, Mark, Play, PauseC, RowHL, RowEnd, Keycap string
	Secondary, Muted                                                    string
	on, tc, mono                                                        bool
}

// paletteFor is a pure function of (theme, background, truecolor), so t re-resolves it live.
func paletteFor(colors bool, theme string, bg Background, truecolor bool) palette {
	p := palette{RowHL: "\x1b[7m", RowEnd: "\x1b[0m", on: colors, tc: truecolor, mono: theme == "mono"}
	if !colors {
		return p
	}
	p.Reset, p.Bold, p.Dim = "\x1b[0m", "\x1b[1m", "\x1b[2m"
	ground := rgb{}
	if bg.Light {
		ground = rgb{255, 255, 255}
	}
	if bg.RGB != nil {
		ground = *bg.RGB
	}
	far := rgb{255, 255, 255}
	if bg.Light {
		far = rgb{}
	}
	switch theme {
	case "minimal":
		p.Accent, p.Mark = "\x1b[36m", "\x1b[1;36m"
		if bg.Light {
			p.Accent, p.Mark = "\x1b[34m", "\x1b[1;34m"
		}
		p.Play, p.PauseC = "\x1b[32m", "\x1b[33m"
	case "mono":
		p.Mark, p.Play, p.PauseC = "\x1b[1m", "\x1b[1m", "\x1b[2m"
	default:
		k := "dark"
		if bg.Light {
			k = "light"
		}
		h := themes[theme+":"+k]
		if truecolor {
			a := h.acc
			p.Accent = fmt.Sprintf("\x1b[38;2;%d;%d;%dm", a.r, a.g, a.b)
			p.Mark = fmt.Sprintf("\x1b[1;38;2;%d;%d;%dm", a.r, a.g, a.b)
			// The playing row's ground is the accent mixed into the background: dark grounds
			// take 30% of it, light ones 18%.
			mix := 30
			if bg.Light {
				mix = 18
			}
			hl := blend(ground, a, mix)
			p.RowHL = fmt.Sprintf("\x1b[48;2;%d;%d;%dm", hl.r, hl.g, hl.b)
			pl, pa := tone(h.play, ground), tone(h.pause, ground)
			p.Play = fmt.Sprintf("\x1b[38;2;%d;%d;%dm", pl.r, pl.g, pl.b)
			p.PauseC = fmt.Sprintf("\x1b[38;2;%d;%d;%dm", pa.r, pa.g, pa.b)
		} else {
			p.Accent, p.Mark = fmt.Sprintf("\x1b[%dm", h.acc16), fmt.Sprintf("\x1b[1;%dm", h.acc16)
			p.Play, p.PauseC = fmt.Sprintf("\x1b[%dm", h.play16), fmt.Sprintf("\x1b[%dm", h.pa16)
		}
	}
	// Visual hierarchy tiers: Secondary (medium contrast) for duration/index/status items,
	// Muted (low contrast) for key labels/scrollbars/dividers. In truecolor community themes
	// both are derived from ground with a contrast floor; minimal, mono and 16-color
	// fall back to Dim. Key caps: a subdued ground (10% on dark, 8% on light in truecolor,
	// 14% when background is untrusted in tmux; 100;97 in 16-color; empty in mono).
	if truecolor && theme != "minimal" && theme != "mono" {
		secK, mutK := 60, 36
		capK := 10
		if bg.RGB == nil {
			capK = 14
		}
		if bg.Light {
			secK, mutK = 65, 48
			capK = 8
		}
		s := blend(ground, far, secK)
		m := tone(blend(ground, far, mutK), ground)
		p.Secondary = fmt.Sprintf("\x1b[38;2;%d;%d;%dm", s.r, s.g, s.b)
		p.Muted = fmt.Sprintf("\x1b[38;2;%d;%d;%dm", m.r, m.g, m.b)
		c := blend(ground, far, capK)
		p.Keycap = fmt.Sprintf("\x1b[48;2;%d;%d;%dm", c.r, c.g, c.b)
	} else {
		p.Secondary = p.Dim
		p.Muted = p.Dim
		if theme != "mono" {
			p.Keycap = "\x1b[100;97m"
		}
	}
	return p
}

func blend(from, to rgb, pct int) rgb {
	return rgb{from.r + (to.r-from.r)*pct/100, from.g + (to.g-from.g)*pct/100, from.b + (to.b-from.b)*pct/100}
}

// lum8 is a cheap relative luminance, 0..10000 scale, on the squared channels.
func lum8(c rgb) int {
	return (2126*(c.r*c.r/255) + 7152*(c.g*c.g/255) + 722*(c.b*c.b/255)) / 255
}

// tone walks a status hue away from the background until the pair reaches a contrast
// floor: darker on a light ground, lighter on a dark one, in 8% steps.
func tone(c, ground rgb) rgb {
	lg := lum8(ground)
	for i := 0; i < 40; i++ {
		lc := lum8(c)
		hi, lo := max(lc, lg), min(lc, lg)
		if (hi+500)*100/(lo+500) >= 350 {
			break
		}
		if lg > lc {
			c = rgb{c.r * 92 / 100, c.g * 92 / 100, c.b * 92 / 100}
			if c == (rgb{}) {
				break
			}
		} else {
			c = rgb{c.r + (255-c.r)*8/100, c.g + (255-c.g)*8/100, c.b + (255-c.b)*8/100}
			if c.r > 247 && c.g > 247 && c.b > 247 {
				break
			}
		}
	}
	return c
}
