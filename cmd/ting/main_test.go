package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/binlecode/ting/internal/tui"
)

// The t line of the help names the cycle it walks; bash and the first Go build both let it drift.
func TestHelpStatesThemeCycle(t *testing.T) {
	m := regexp.MustCompile(`(?s)\n  t +cycle palette family live \(([^)]*)\)`).FindStringSubmatch(usage)
	if m == nil {
		t.Fatal("help has no t line")
	}
	got := strings.Fields(strings.ReplaceAll(m[1], "→", " "))
	if strings.Join(got, " ") != strings.Join(tui.ThemeCycle, " ") {
		t.Fatalf("help says t walks %v, the key walks %v", got, tui.ThemeCycle)
	}
}

func TestParseArgsEmptyQueryStrings(t *testing.T) {
	f := parseArgs([]string{""})
	cleanedQuery := strings.TrimSpace(strings.Join(f.query, " "))
	if cleanedQuery != "" {
		t.Fatalf("expected empty query, got %q", cleanedQuery)
	}

	f2 := parseArgs([]string{"   "})
	cleanedQuery2 := strings.TrimSpace(strings.Join(f2.query, " "))
	if cleanedQuery2 != "" {
		t.Fatalf("expected empty query, got %q", cleanedQuery2)
	}
}
