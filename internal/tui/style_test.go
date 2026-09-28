package tui

import (
	"fmt"
	"strings"
	"testing"
)

func TestPaletteVisualHierarchyTiers(t *testing.T) {
	// 1. Truecolor community theme (e.g. catppuccin) on dark background
	pCatDark := paletteFor(true, "catppuccin", Background{Light: false}, true)
	if !strings.HasPrefix(pCatDark.Secondary, "\x1b[38;2;") {
		t.Errorf("catppuccin dark Secondary should be 24-bit RGB, got %q", pCatDark.Secondary)
	}
	if !strings.HasPrefix(pCatDark.Muted, "\x1b[38;2;") {
		t.Errorf("catppuccin dark Muted should be 24-bit RGB, got %q", pCatDark.Muted)
	}
	if pCatDark.Secondary == pCatDark.Muted {
		t.Errorf("Secondary and Muted should be distinct tiers, both got %q", pCatDark.Secondary)
	}
	if !strings.HasPrefix(pCatDark.Keycap, "\x1b[48;2;") {
		t.Errorf("catppuccin dark Keycap should be 24-bit RGB bg, got %q", pCatDark.Keycap)
	}

	// 2. Truecolor community theme on light background
	pCatLight := paletteFor(true, "catppuccin", Background{Light: true}, true)
	if !strings.HasPrefix(pCatLight.Secondary, "\x1b[38;2;") {
		t.Errorf("catppuccin light Secondary should be 24-bit RGB, got %q", pCatLight.Secondary)
	}
	if !strings.HasPrefix(pCatLight.Muted, "\x1b[38;2;") {
		t.Errorf("catppuccin light Muted should be 24-bit RGB, got %q", pCatLight.Muted)
	}
	if pCatLight.Secondary == pCatLight.Muted {
		t.Errorf("Secondary and Muted on light should be distinct, both got %q", pCatLight.Secondary)
	}

	// 3. minimal theme: falls back to Dim to preserve native terminal colors
	pMin := paletteFor(true, "minimal", Background{}, true)
	if pMin.Secondary != pMin.Dim || pMin.Muted != pMin.Dim {
		t.Errorf("minimal should fall back to Dim for Secondary and Muted, got sec=%q mut=%q dim=%q",
			pMin.Secondary, pMin.Muted, pMin.Dim)
	}
	if pMin.Keycap != "\x1b[100;97m" {
		t.Errorf("minimal Keycap should be 100;97m, got %q", pMin.Keycap)
	}

	// 4. mono theme: falls back to Dim, empty Keycap
	pMono := paletteFor(true, "mono", Background{}, true)
	if pMono.Secondary != pMono.Dim || pMono.Muted != pMono.Dim {
		t.Errorf("mono should fall back to Dim, got sec=%q mut=%q dim=%q", pMono.Secondary, pMono.Muted, pMono.Dim)
	}
	if pMono.Keycap != "" {
		t.Errorf("mono Keycap should be empty, got %q", pMono.Keycap)
	}

	// 5. ANSI-16 (non-truecolor)
	p16 := paletteFor(true, "nord", Background{}, false)
	if p16.Secondary != p16.Dim || p16.Muted != p16.Dim {
		t.Errorf("16-color should fall back to Dim, got sec=%q mut=%q dim=%q", p16.Secondary, p16.Muted, p16.Dim)
	}
	if p16.Keycap != "\x1b[100;97m" {
		t.Errorf("16-color Keycap should be 100;97m, got %q", p16.Keycap)
	}

	// 6. Colors disabled (--color never / NO_COLOR)
	pOff := paletteFor(false, "catppuccin", Background{}, true)
	if pOff.Secondary != "" || pOff.Muted != "" || pOff.Keycap != "" {
		t.Errorf("color off should have empty tier tokens, got sec=%q mut=%q keycap=%q",
			pOff.Secondary, pOff.Muted, pOff.Keycap)
	}
}

func parseRGBCode(s string) (rgb, bool) {
	var c rgb
	if _, err := fmt.Sscanf(s, "\x1b[38;2;%d;%d;%dm", &c.r, &c.g, &c.b); err == nil {
		return c, true
	}
	return c, false
}

func contrastRatio(c1, c2 rgb) float64 {
	l1, l2 := float64(lum8(c1)), float64(lum8(c2))
	hi, lo := max(l1, l2), min(l1, l2)
	return (hi + 500) / (lo + 500)
}

func TestPaletteContrastTiers(t *testing.T) {
	// Test on standard pure grounds and real community grounds (e.g. Catppuccin Mocha)
	mocha := rgb{30, 30, 46}
	grounds := []Background{
		{Light: false},
		{Light: true},
		{Light: false, RGB: &mocha},
	}

	for _, name := range ThemeNames {
		if name == "minimal" || name == "mono" {
			continue
		}
		for _, bg := range grounds {
			p := paletteFor(true, name, bg, true)
			secRGB, ok1 := parseRGBCode(p.Secondary)
			mutRGB, ok2 := parseRGBCode(p.Muted)
			if !ok1 || !ok2 {
				t.Fatalf("theme %s failed to parse RGB from Secondary=%q Muted=%q", name, p.Secondary, p.Muted)
			}

			groundRGB := rgb{}
			if bg.Light {
				groundRGB = rgb{255, 255, 255}
			}
			if bg.RGB != nil {
				groundRGB = *bg.RGB
			}

			crSec := contrastRatio(secRGB, groundRGB)
			crMut := contrastRatio(mutRGB, groundRGB)

			// Secondary should meet WCAG AA (>= 4.5:1)
			if crSec < 4.5 {
				t.Errorf("theme %s light=%v Secondary contrast too low: %.2f (got %v vs ground %v)",
					name, bg.Light, crSec, secRGB, groundRGB)
			}

			// Muted should meet floor (>= 3.0:1)
			if crMut < 3.0 {
				t.Errorf("theme %s light=%v Muted contrast too low: %.2f (got %v vs ground %v)",
					name, bg.Light, crMut, mutRGB, groundRGB)
			}

			// Strict hierarchy ordering: Secondary is strictly more contrasting than Muted
			if crSec <= crMut {
				t.Errorf("theme %s light=%v hierarchy inverted: crSec=%.2f <= crMut=%.2f",
					name, bg.Light, crSec, crMut)
			}
		}
	}
}
