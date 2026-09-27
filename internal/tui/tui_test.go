package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/binlecode/ting/internal/verb"
	tea "github.com/charmbracelet/bubbletea"
)

// These drive the model over the checkout's real ting-play — a real search, real rows — and
// read back what a key did. Isolated the way the verb tests are: nothing seeded, only moved.
func model(t *testing.T, opt Options) *Model {
	t.Helper()
	if testing.Short() {
		t.Skip("real search: network")
	}
	root, err := filepath.Abs("../../tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, "go-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TING_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("TMPDIR", dir)
	t.Setenv("TING_CONFIG", filepath.Join(dir, "no-config"))
	t.Setenv("TING_HISTORY", "0")
	t.Setenv("TING_ENGINE_DIR", filepath.Join(dir, "no-engines"))
	shell, _ := filepath.Abs("../../shell")
	s, err := verb.LocateIn(shell)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	engines, err := s.Engines(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range engines {
		if e.Name == "yt" {
			opt.Engine = i
		}
	}
	opt.Engines = engines
	if opt.Play.Mode == "" {
		opt.Play = verb.PlayOpts{Mode: "audio", Quality: "auto"}
	}
	if opt.Search.Sort == "" {
		opt.Search.Sort = "relevance"
	}
	if opt.Keys == "" {
		opt.Keys = "core"
	}
	if opt.ListMode == "" {
		opt.ListMode = "scroll"
	}
	if opt.PageRows == 0 {
		opt.PageRows = 10
	}
	opt.Loop, opt.Lang = "off", "en"
	m := New(ctx, s, opt)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

// run executes a command the way the program loop would, feeding this package's messages
// back, so a fetch a key started has landed when it returns. Ticks and cursor blinks are
// dropped: they only schedule the next frame.
func run(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			run(m, c)
		}
	case searchDoneMsg, authMsg, playDoneMsg, verbDoneMsg, noticeMsg, volDoneMsg:
		_, next := m.Update(msg)
		run(m, next)
	}
}

func key(m *Model, k string) {
	var msg tea.KeyMsg
	switch k {
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	_, cmd := m.Update(msg)
	run(m, cmd)
	m.View()
}

func start(t *testing.T, m *Model) {
	t.Helper()
	run(m, m.Init())
	m.View()
	if len(m.rows) == 0 {
		t.Fatalf("the first search brought no rows (notice %q %q)", m.noticeL, m.noticeT)
	}
}

// Page mode's two edges move the row count by one batch: → past the last page fetches one
// more and steps onto the first new page; ← on page 1 drops one, locally, down to the floor.
func TestPageEdgesMoveTheRowCount(t *testing.T) {
	m := model(t, Options{Query: "jazz piano", Search: verb.SearchOpts{N: 20}, Batch: 20, ListMode: "page"})
	start(t, m)
	ps := m.layout().psize
	if len(m.rows) <= ps {
		t.Fatalf("%d rows do not make two pages of %d", len(m.rows), ps)
	}
	pages := (len(m.rows) + ps - 1) / ps
	for i := 1; i < pages; i++ {
		key(m, "right")
	}
	before := len(m.rows)
	key(m, "right")
	if m.opt.Search.N != 40 {
		t.Fatalf("→ past the last page asked for %d rows, want 40", m.opt.Search.N)
	}
	if len(m.rows) > before && m.cursor != pages*ps {
		t.Errorf("after growing from %d to %d rows the cursor is on row %d, want the first new page (%d)",
			before, len(m.rows), m.cursor, pages*ps)
	}
	for m.cursor >= ps {
		key(m, "left")
	}
	key(m, "left")
	if m.opt.Search.N != 20 || len(m.rows) > 20 {
		t.Fatalf("← on page 1 left N=%d with %d rows, want 20", m.opt.Search.N, len(m.rows))
	}
	key(m, "left")
	if m.opt.Search.N != 20 {
		t.Errorf("← at the floor moved N to %d", m.opt.Search.N)
	}
}

// The live filter narrows by every token, never fetches off its own end, and Esc puts every
// row back.
func TestFilterNarrowsAndRestores(t *testing.T) {
	m := model(t, Options{Query: "lofi hip hop", Search: verb.SearchOpts{N: 20}, Batch: 20})
	start(t, m)
	all := len(m.rows)
	word := strings.Fields(strings.ToLower(m.rows[all-1].Title))[0]
	key(m, "/")
	for _, r := range word {
		key(m, string(r))
	}
	if len(m.rows) == 0 || len(m.rows) > all {
		t.Fatalf("filter %q kept %d of %d rows", word, len(m.rows), all)
	}
	for _, r := range m.rows {
		if !r.matches([]string{word}) {
			t.Errorf("row %q does not contain %q", r.Title, word)
		}
	}
	for i := 0; i < len(m.rows)+2; i++ {
		key(m, "down")
	}
	if m.opt.Search.N != 20 || m.pending != nil {
		t.Errorf("running off the matches fetched (N=%d)", m.opt.Search.N)
	}
	key(m, "esc")
	if m.filterOn || len(m.rows) != all {
		t.Errorf("Esc left the filter %v with %d of %d rows", m.filterOn, len(m.rows), all)
	}
}

// Nj goes to the absolute row number; a digit then any other key drops the count.
func TestJumpIsAbsolute(t *testing.T) {
	m := model(t, Options{Query: "lofi hip hop", Search: verb.SearchOpts{N: 20}, Batch: 20})
	start(t, m)
	key(m, "1")
	key(m, "2")
	key(m, "j")
	if m.cursor != 11 {
		t.Fatalf("12j put the cursor on row %d, want 12", m.cursor+1)
	}
	key(m, "3")
	key(m, "#")
	key(m, "j")
	if m.cursor != 12 {
		t.Errorf("3 # j should drop the count and move down one, cursor on row %d", m.cursor+1)
	}
	key(m, "9")
	key(m, "9")
	key(m, "j")
	if m.cursor != 12 {
		t.Errorf("99j past the end moved the cursor to row %d", m.cursor+1)
	}
}

// o re-fetches under the next sort field and says so on the status line; the field only
// changes when the fetch succeeds.
func TestSortCycles(t *testing.T) {
	m := model(t, Options{Query: "lofi hip hop", Search: verb.SearchOpts{N: 10}, Batch: 10})
	start(t, m)
	key(m, "o")
	if m.opt.Search.Sort != "view_count" {
		t.Fatalf("o left the sort on %q", m.opt.Search.Sort)
	}
	if !strings.Contains(strings.SplitN(m.View(), "\n", 2)[0], m.s.SortViews) {
		t.Errorf("the title line does not name the new sort: %q", strings.SplitN(m.View(), "\n", 2)[0])
	}
}
