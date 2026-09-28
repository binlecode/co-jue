package tui

import (
	"strings"
	"testing"

	"github.com/binlecode/ting/internal/verb"
)

func stripANSI(s string) string {
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

func TestAccessBadgeOnExceptionRowsOnly(t *testing.T) {
	dur := 230.0

	sr := &verb.SearchResult{
		Engine: "ne",
		Results: []verb.Result{
			{ID: "f", Title: "Full Track", Access: "full", Duration: &dur},
			{ID: "p", Title: "Preview Track", Access: "preview", Duration: &dur},
			{ID: "w", Title: "Paywalled Track", Access: "paywalled", Duration: &dur},
		},
	}
	rows := rowsFromSearch(sr)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	fullRow, previewRow, paywalledRow := rows[0], rows[1], rows[2]

	// Full track rail remains clean; preview and paywalled prepend micro badges
	if rail := fullRow.rail(); rail != "3:50" {
		t.Errorf("full track rail expected '3:50', got %q", rail)
	}
	if rail := previewRow.rail(); rail != "30s 3:50" {
		t.Errorf("preview track rail expected '30s 3:50', got %q", rail)
	}
	if rail := paywalledRow.rail(); rail != "VIP 3:50" {
		t.Errorf("paywalled track rail expected 'VIP 3:50', got %q", rail)
	}

	// Live filter matches rail text
	if !previewRow.matches([]string{"30s"}) {
		t.Errorf("preview row should match filter '30s'")
	}
	if fullRow.matches([]string{"30s"}) {
		t.Errorf("full row should not match filter '30s'")
	}
	if !paywalledRow.matches([]string{"VIP"}) {
		t.Errorf("paywalled row should match filter 'VIP'")
	}

	m := &Model{
		suite:  &verb.Suite{},
		rows:   rows,
		all:    rows,
		cursor: 0,
		g:      glyphsUTF,
		w:      newWidth(false),
		width:  80,
		height: 24,
		s:      strsZH,
		p:      paletteFor(true, "catppuccin", Background{}, true),
		opt: Options{
			Engines: []verb.Engine{{Name: "ne"}},
		},
	}

	// Details meta reflects exception badges only on restricted rows
	dFull, _, _, _, _ := m.detailLines(80, 0)
	if len(dFull) == 0 {
		t.Fatalf("expected non-empty details for full track")
	}
	if strings.Contains(dFull[0], "VIP") || strings.Contains(dFull[0], "30s") {
		t.Errorf("full track details should not contain VIP/30s badge, got %q", dFull[0])
	}

	dPreview, _, _, _, _ := m.detailLines(80, 1)
	if len(dPreview) == 0 {
		t.Fatalf("expected non-empty details for preview track")
	}
	if !strings.Contains(dPreview[0], "30s") {
		t.Errorf("preview track details should contain '30s', got %q", dPreview[0])
	}

	dPaywalled, _, _, _, _ := m.detailLines(80, 2)
	if len(dPaywalled) == 0 {
		t.Fatalf("expected non-empty details for paywalled track")
	}
	if !strings.Contains(dPaywalled[0], "VIP") {
		t.Errorf("paywalled track details should contain 'VIP', got %q", dPaywalled[0])
	}

	// Full frame visual test: verify line-by-line badge presence and column width
	frame := m.View()
	lines := strings.Split(frame, "\n")
	var fullLine, previewLine, paywalledLine string
	for _, l := range lines {
		plain := stripANSI(l)
		if m.w.of(plain) > m.width {
			t.Errorf("line exceeds terminal width %d: %q (len %d)", m.width, plain, m.w.of(plain))
		}
		if strings.Contains(l, "Full Track") && fullLine == "" {
			fullLine = l
		}
		if strings.Contains(l, "Preview Track") && previewLine == "" {
			previewLine = l
		}
		if strings.Contains(l, "Paywalled Track") && paywalledLine == "" {
			paywalledLine = l
		}
	}

	if fullLine == "" || !strings.Contains(fullLine, "3:50") ||
		strings.Contains(fullLine, "30s") || strings.Contains(fullLine, "VIP") {
		t.Errorf("full track line should have clean '3:50' and no badge, got %q", fullLine)
	}
	if previewLine == "" || !strings.Contains(previewLine, "30s 3:50") {
		t.Errorf("preview track line should contain '30s 3:50', got %q", previewLine)
	}
	if paywalledLine == "" || !strings.Contains(paywalledLine, "VIP 3:50") {
		t.Errorf("paywalled track line should contain 'VIP 3:50', got %q", paywalledLine)
	}
}
