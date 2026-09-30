package tui

import (
	"context"
	"fmt"
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
	case searchDoneMsg, authMsg, playDoneMsg, verbDoneMsg, noticeMsg, volDoneMsg, urlMsg, infoMsg, partsMsg, relatedMsg, feedMsg, remotePlaylistsMsg:
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
	lines := strings.Split(m.View(), "\n")
	headLimit := min(len(lines), m.layout().chromeH)
	found := false
	for _, l := range lines[:headLimit] {
		if strings.Contains(l, m.s.SortViews) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("header/status lines do not name the new sort %q: %q", m.s.SortViews, strings.Join(lines[:headLimit], "\n"))
	}
}

func typeLine(m *Model, s string) {
	for _, r := range s {
		key(m, string(r))
	}
	key(m, "enter")
}

// a stores the focused row, b puts the store's playlists on the stage and Enter opens the one
// under the cursor, d removes a row and z puts it back — each checked against the store
// itself, not the screen.
func TestPlaylistRoundTrip(t *testing.T) {
	m := model(t, Options{Query: "lofi hip hop", Search: verb.SearchOpts{N: 5}, Batch: 5})
	start(t, m)
	stored := func() int {
		l, err := m.suite.PlaylistShow(context.Background(), "t")
		if err != nil {
			return -1
		}
		return l.Count
	}
	first := m.rows[0].URL
	key(m, "a")
	if !m.prompting || m.askKind != askNew {
		t.Fatalf("a on an empty store did not ask for a name (kind %v)", m.askKind)
	}
	typeLine(m, "t")
	key(m, "j")
	key(m, "a")
	if m.askKind != askAdd || len(m.pick) != 1 {
		t.Fatalf("a with one playlist did not offer it (kind %v, %d listed)", m.askKind, len(m.pick))
	}
	typeLine(m, "1")
	if n := stored(); n != 2 {
		t.Fatalf("the store holds %d rows after two a's, want 2", n)
	}
	key(m, "b")
	if m.src != srcPlaylists || m.prompting || len(m.rows) != 1 || m.rows[0].Title != "t" {
		t.Fatalf("b left source %v (prompting %v) with %d rows, want the library's one", m.src, m.prompting, len(m.rows))
	}
	key(m, "enter")
	if m.src != srcPlaylist || len(m.rows) != 2 {
		t.Fatalf("b Enter left source %v with %d rows", m.src, len(m.rows))
	}
	key(m, "d")
	if n := stored(); n != 1 || m.rows[0].URL == first {
		t.Fatalf("d on the first row left %d stored, first on screen %q", n, m.rows[0].URL)
	}
	if !m.undoActive() {
		t.Fatal("d opened no undo offer")
	}
	key(m, "z")
	if n := stored(); n != 2 || m.rows[0].URL != first {
		t.Fatalf("z left %d stored, first on screen %q, want 2 and %q", n, m.rows[0].URL, first)
	}
	key(m, "b")
	if m.src != srcSearch || len(m.rows) != 5 {
		t.Errorf("b did not return to the 5 results (source %v, %d rows)", m.src, len(m.rows))
	}
}

// The library is a stage of its own: w4 opens it in place of the results (no picker at the
// foot), R renames and d deletes the list under the cursor with the library kept on screen,
// z brings a deleted list back, and Esc climbs out to the results.
func TestPlaylistLibraryStage(t *testing.T) {
	m := model(t, Options{Query: "lofi hip hop", Search: verb.SearchOpts{N: 5}, Batch: 5})
	start(t, m)
	names := func() []string {
		ls, err := m.suite.PlaylistLs(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, pl := range ls {
			out = append(out, pl.Name)
		}
		return out
	}
	for _, n := range []string{"one", "two"} {
		key(m, "a")
		typeLine(m, n)
	}
	if got := names(); len(got) != 2 {
		t.Fatalf("the store holds %v after two new lists", got)
	}
	key(m, "w")
	key(m, "4")
	if m.src != srcPlaylists || m.prompting || len(m.rows) != 2 {
		t.Fatalf("w4 left source %v (prompting %v) with %d rows, want the 2-row library", m.src, m.prompting, len(m.rows))
	}
	if !strings.Contains(m.View(), "playlists='") {
		t.Errorf("the library's title line does not name it")
	}
	key(m, "down")
	target := m.rows[m.cursor].Title
	key(m, "R")
	typeLine(m, "renamed")
	if m.src != srcPlaylists || m.rows[m.cursor].Title != "renamed" {
		t.Fatalf("R on %q left source %v with %q under the cursor", target, m.src, m.rows[m.cursor].Title)
	}
	key(m, "d")
	if m.noticeL == "" {
		t.Fatal("d on the library should show notice to use D")
	}
	key(m, "D")
	if got := names(); len(got) != 1 || got[0] == "renamed" {
		t.Fatalf("D on the library left %v in the store", got)
	}
	if m.src != srcPlaylists || len(m.rows) != 1 {
		t.Fatalf("D on the library left source %v with %d rows", m.src, len(m.rows))
	}
	key(m, "z")
	if got := names(); len(got) != 2 || len(m.rows) != 2 || m.rows[m.cursor].Title != "renamed" {
		t.Fatalf("z left %v stored and %d rows, cursor on %q", got, len(m.rows), m.rows[m.cursor].Title)
	}
	key(m, "esc")
	if m.src != srcSearch || len(m.rows) != 5 {
		t.Errorf("Esc did not return to the 5 results (source %v, %d rows)", m.src, len(m.rows))
	}
}

// A wide frame is never taller than the terminal — else the renderer drops its top, the
// title line and the tabs with it — and its first line is the title, with the key block,
// the notice and a playlist picker all drawn.
func TestWideFrameFitsTheTerminal(t *testing.T) {
	m := model(t, Options{Query: "lofi hip hop", Search: verb.SearchOpts{N: 20}, Batch: 20, Keys: "full"})
	start(t, m)
	// One list stored, so a draws its picker, and the add's undo offer is the notice line.
	key(m, "a")
	typeLine(m, "x")
	for _, sz := range [][2]int{{140, 40}, {100, 24}, {120, 30}} {
		m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		key(m, "a")
		for _, v := range []string{m.View(), func() string { key(m, "esc"); return m.View() }()} {
			lines := strings.Split(v, "\n")
			if len(lines) > sz[1] {
				t.Errorf("%dx%d: the frame is %d lines", sz[0], sz[1], len(lines))
			}
			if !strings.Contains(lines[0], "query='") && !strings.Contains(lines[1], "query='") {
				t.Errorf("%dx%d: header does not contain query: %q / %q", sz[0], sz[1], lines[0], lines[1])
			}
		}
	}
}

// A real row's thumbnail decodes and fits the box — for every engine, since they serve
// different formats (YouTube's .jpg arrives as webp).
func TestCoverFitsTheBox(t *testing.T) {
	m := model(t, Options{Query: "piano", Search: verb.SearchOpts{N: 3}, Batch: 3})
	checked := 0
	for _, e := range m.opt.Engines {
		res, err := m.suite.Search(context.Background(), e.Name, "piano", verb.SearchOpts{N: 3})
		if err != nil || len(res.Results) == 0 || res.Results[0].Thumbnail == "" {
			t.Errorf("%s: no row with a thumbnail (%v)", e.Name, err)
			continue
		}
		b, err := fetchCover(context.Background(), res.Results[0].Thumbnail)
		if err != nil {
			// The CDN, not this code: measured stalling curl and Go alike.
			t.Logf("%s: not checked, the download failed: %v", e.Name, err)
			continue
		}
		img, err := fitCover(b, 16, 36)
		if err != nil {
			t.Errorf("%s: %s did not decode: %v", e.Name, res.Results[0].Thumbnail, err)
			continue
		}
		checked++
		if img.cols < 1 || img.cols > coverCols {
			t.Errorf("%s: the cover takes %d columns, the box is %d", e.Name, img.cols, coverCols)
		}
	}
	// One stalling CDN is tolerated; every download failing leaves nothing proved.
	if checked == 0 {
		t.Error("no engine's cover downloaded: the fit is unchecked")
	}
}

// A miss is asked again only when it was the network's: nothing listening is, a thumbnail the
// CDN says does not exist is not.
func TestCoverMissRetriesOnlyTheNetwork(t *testing.T) {
	if _, retry := loadCover(context.Background(), "http://127.0.0.1:1/cover.jpg", 16, 36); !retry {
		t.Error("a refused connection is final; it should be asked again")
	}
	if testing.Short() {
		t.Skip("a real 404: network")
	}
	if img, retry := loadCover(context.Background(), "https://i.ytimg.com/vi/zzzzzzzzzzz/hq720.jpg", 16, 36); img != nil || retry {
		t.Errorf("a 404 gave img=%v retry=%v; it should be final", img != nil, retry)
	}
}

// A URL's one row has no query behind it: o, e and more have nothing to re-fetch, and e only
// picks the engine the next search uses — none of them sends the URL to a search.
func TestURLRowIsNotASearch(t *testing.T) {
	u := "https://www.youtube.com/watch?v=jNQXAC9IVRw"
	m := model(t, Options{Query: u, Search: verb.SearchOpts{N: 10}, Batch: 10})
	start(t, m)
	eng := m.opt.Engine
	for _, k := range []string{"o", "e", "down"} {
		key(m, k)
		if m.busy != "" || len(m.rows) != 1 || m.query != u {
			t.Fatalf("%s after a URL: busy %q, notice %q %q, %d rows, query %q", k, m.busy, m.noticeL, m.noticeT, len(m.rows), m.query)
		}
		if k == "e" && len(m.opt.Engines) > 1 {
			// The status line keeps the row's engine, so only the notice can show the switch.
			if m.noticeL != m.s.EngineAct+":" || !strings.Contains(m.noticeT, m.opt.Engines[m.opt.Engine].Name) {
				t.Errorf("e after a URL says %q %q; it should name the next search's engine", m.noticeL, m.noticeT)
			}
		} else if m.noticeL != "" {
			t.Errorf("%s after a URL left a notice: %q %q", k, m.noticeL, m.noticeT)
		}
	}
	if m.opt.Search.Sort != "relevance" {
		t.Errorf("o moved the sort to %q with nothing to sort", m.opt.Search.Sort)
	}
	if len(m.opt.Engines) > 1 && m.opt.Engine == eng {
		t.Error("e did not pick the next engine")
	}
}

// A key that would start a second fetch while one is in flight is turned away, and the busy
// line says so; a search typed at the prompt keeps its prompt and its text. When the fetch
// lands the line is gone, and the same key works.
func TestKeyBehindAFetchIsTold(t *testing.T) {
	m := model(t, Options{Query: "lofi hip hop", Search: verb.SearchOpts{N: 10}, Batch: 10})
	start(t, m)
	_, inflight := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if m.pending == nil {
		t.Fatal("o started no fetch")
	}
	for _, k := range []string{"i", "c", "g"} {
		key(m, k)
		if !m.held || !strings.Contains(m.View(), m.s.BusyHeld) {
			t.Fatalf("%s behind a fetch: held %v, and the frame does not say so", k, m.held)
		}
		m.held = false
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("https://youtu.be/jNQXAC9IVRw"), Paste: true})
	if cmd != nil || !m.held {
		t.Fatalf("a paste behind a fetch: cmd %v, held %v", cmd != nil, m.held)
	}
	key(m, "n")
	typeLine(m, "jazz")
	if !m.prompting || m.input.Value() != "jazz" {
		t.Fatalf("a search behind a fetch closed the prompt (prompting %v, text %q)", m.prompting, m.input.Value())
	}
	key(m, "esc")
	run(m, inflight)
	if m.pending != nil || strings.Contains(m.View(), m.s.BusyHeld) {
		t.Fatal("the fetch landed and the busy line still stands")
	}
	key(m, "i")
	if m.info == nil && m.noticeL == "" {
		t.Error("i after the fetch landed still did nothing")
	}
}

func TestURLTargetIsSchemeOrWWW(t *testing.T) {
	for in, want := range map[string]string{
		"https://youtu.be/jNQXAC9IVRw":        "https://youtu.be/jNQXAC9IVRw",
		"look: (https://b23.tv/abc) thanks":   "https://b23.tv/abc",
		"www.youtube.com/watch?v=jNQXAC9IVRw": "www.youtube.com/watch?v=jNQXAC9IVRw",
		"lofi.mix/2024":                       "",
		"youtu.be/jNQXAC9IVRw":                "",
		"www. is a word in this query":        "",
	} {
		if got := urlTarget(in); got != want {
			t.Errorf("urlTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRelatedRowSourceReversible(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	m := model(t, Options{Query: "lofi hip hop", Search: verb.SearchOpts{N: 5}, Batch: 5})
	start(t, m)

	origTitle := m.rows[0].Title
	key(m, "g")
	if m.src != srcRelated {
		t.Fatalf("g did not open srcRelated, src=%v (notice: %q %q)", m.src, m.noticeL, m.noticeT)
	}
	if len(m.rows) == 0 {
		t.Fatal("related brought no rows")
	}

	// Pressing g again in srcRelated must exit back to srcSearch
	key(m, "g")
	if m.src != srcSearch {
		t.Fatalf("g in srcRelated did not return to srcSearch, src=%v", m.src)
	}
	if len(m.rows) == 0 || m.rows[0].Title != origTitle {
		t.Fatalf("original search rows not restored: %v", m.rows)
	}
}

func TestFeedStartupSearchedFalse(t *testing.T) {
	m := model(t, Options{Feed: "home"})
	if m.query != "feed:home" {
		t.Errorf("query is %q, want feed:home", m.query)
	}
	if m.searched() {
		t.Errorf("searched() on feed:home returned true, want false")
	}
}

func TestFeedStartupFailsWithoutSearchPrompt(t *testing.T) {
	t.Setenv("TING_COOKIE_BROWSER", "none")
	shell, err := filepath.Abs("../../shell")
	if err != nil {
		t.Fatal(err)
	}
	s, err := verb.LocateIn(shell)
	if err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), s, Options{
		Engines: []verb.Engine{
			{Name: "yt", Flags: []string{"--feed", "--search"}},
			{Name: "bili", Flags: []string{"--search"}},
		},
		Feed: "home",
	})
	run(m, m.Init())
	if m.prompting {
		t.Fatalf("feed without cookies should not prompt for search, got prompting=true")
	}
	if m.noticeL != m.s.FeedAct+":" {
		t.Errorf("notice label is %q, want %q", m.noticeL, m.s.FeedAct+":")
	}
	if m.stage.Active() != WsSearch {
		t.Errorf("active workspace after failed feed should land on WsSearch, got %v", m.stage.Active())
	}
	if m.query != "" || m.feed != "" {
		t.Errorf("failed feed should clear query and feed, got query=%q feed=%q", m.query, m.feed)
	}
	if m.searched() {
		t.Errorf("searched() after failed feed should be false")
	}
	if m.all == nil || m.rows == nil {
		t.Errorf("m.all and m.rows should be non-nil empty slices")
	}
}

func TestStartupWithoutQueryNoPrompt(t *testing.T) {
	shell, err := filepath.Abs("../../shell")
	if err != nil {
		t.Fatal(err)
	}
	s, err := verb.LocateIn(shell)
	if err != nil {
		t.Fatal(err)
	}
	// Engine without --feed (bili)
	m := New(context.Background(), s, Options{
		Engines: []verb.Engine{{Name: "bili", Flags: []string{"--search"}}},
		Engine:  0,
	})
	run(m, m.Init())
	if m.prompting {
		t.Fatalf("startup on non-feed engine should not prompt for search, got prompting=true")
	}
	if m.pending != nil {
		t.Fatalf("startup on non-feed engine with empty query should not initiate fetch, got pending=%v", m.pending)
	}
	if m.stage.Active() != WsSearch {
		t.Errorf("active workspace is %v, want WsSearch", m.stage.Active())
	}
	if m.all == nil || m.rows == nil {
		t.Errorf("m.all and m.rows should be non-nil empty slices")
	}
	if strings.Contains(m.noticeT, "--search needs a query") {
		t.Errorf("startup triggered empty search: %q", m.noticeT)
	}
}

func TestRemotePlaylistsKey(t *testing.T) {
	// 1. Engine without --playlists (e.g. bili)
	m1 := model(t, Options{})
	m1.prompting = false
	m1.all, m1.rows = []row{{Title: "song", Engine: "bili"}}, []row{{Title: "song", Engine: "bili"}}
	for i, e := range m1.opt.Engines {
		if e.Name == "bili" {
			m1.opt.Engine = i
			break
		}
	}
	key(m1, "B")
	if m1.noticeL != m1.s.RemotePLAct+":" || !strings.Contains(m1.noticeT, m1.s.RemotePLNotSupported) {
		t.Fatalf("B on engine without --playlists should show unsupported notice, got %q: %q", m1.noticeL, m1.noticeT)
	}

	// 2. Engine with --playlists, but no cookies
	t.Setenv("TING_COOKIE_BROWSER", "none")
	m2 := model(t, Options{})
	m2.prompting = false
	m2.all, m2.rows = []row{{Title: "song", Engine: "yt"}}, []row{{Title: "song", Engine: "yt"}}
	for i, e := range m2.opt.Engines {
		if e.Name == "yt" {
			m2.opt.Engine = i
			break
		}
	}
	key(m2, "B")
	if m2.noticeL != m2.s.RemotePLAct+":" || !strings.Contains(m2.noticeT, m2.s.RemotePLNoCookies) {
		t.Fatalf("B with no cookies should show no-cookies notice, got %q: %q", m2.noticeL, m2.noticeT)
	}

	// 3. Engine with --playlists, but account has no playlists (count == 0)
	m3 := model(t, Options{})
	m3.prompting = false
	m3.remotePlaylistsDone(remotePlaylistsMsg{
		res: &verb.RemotePlaylistsResult{
			Status: "ok",
			Engine: "yt",
			Count:  0,
		},
	})
	if m3.noticeL != m3.s.RemotePLAct+":" || m3.noticeT != m3.s.PLNoneRemote {
		t.Fatalf("B with 0 playlists should show PLNoneRemote, got %q: %q", m3.noticeL, m3.noticeT)
	}

	// 4. Engine with --playlists, account has playlists -> opens main stage view
	m4 := model(t, Options{})
	m4.prompting = false
	m4.all, m4.rows = []row{{Title: "song", Engine: "yt"}}, []row{{Title: "song", Engine: "yt"}}
	m4.remotePlaylistsDone(remotePlaylistsMsg{
		res: &verb.RemotePlaylistsResult{
			Status: "ok",
			Engine: "yt",
			Count:  2,
			Playlists: []verb.RemotePlaylist{
				{ID: "LL", Title: "Liked videos", URL: "https://www.youtube.com/playlist?list=LL"},
				{ID: "WL", Title: "Watch later", URL: "https://www.youtube.com/playlist?list=WL"},
			},
		},
	})
	if m4.prompting || m4.src != srcRemotePlaylists || len(m4.rows) != 2 {
		t.Fatalf("remotePlaylistsDone should open srcRemotePlaylists with 2 rows, got prompting=%v src=%v rows=%d", m4.prompting, m4.src, len(m4.rows))
	}
	if m4.cursor != 0 {
		t.Fatalf("cursor should start at 0, got %d", m4.cursor)
	}

	// Move cursor down with j
	key(m4, "j")
	if m4.cursor != 1 {
		t.Fatalf("j should move cursor to 1, got %d", m4.cursor)
	}

	// Move cursor up with k
	key(m4, "k")
	if m4.cursor != 0 {
		t.Fatalf("k should move cursor to 0, got %d", m4.cursor)
	}

	// Move cursor down with down arrow
	key(m4, "down")
	if m4.cursor != 1 {
		t.Fatalf("down should move cursor to 1, got %d", m4.cursor)
	}

	// Enter on focused remote playlist loads the URL
	if cmd := m4.openFocusedRemotePlaylist(); cmd == nil {
		t.Fatalf("openFocusedRemotePlaylist should return a command to load the playlist")
	}

	// B in srcRemotePlaylists toggles back to search
	key(m4, "B")
	if m4.src != srcSearch || len(m4.rows) != 1 {
		t.Fatalf("B in srcRemotePlaylists should return to search, got src=%v rows=%d", m4.src, len(m4.rows))
	}

	// Remote playlists with thumbnail populated
	thumbPL := "https://i.ytimg.com/vi/pl1/hqdefault.jpg"
	m4.remotePlaylistsDone(remotePlaylistsMsg{
		res: &verb.RemotePlaylistsResult{
			Status: "ok",
			Engine: "yt",
			Count:  1,
			Playlists: []verb.RemotePlaylist{
				{ID: "PL1", Title: "My Playlist", URL: "https://www.youtube.com/playlist?list=PL1", Thumbnail: thumbPL},
			},
		},
	})
	if len(m4.rows) != 1 || m4.rows[0].Thumb != thumbPL {
		t.Fatalf("remote playlist row should have Thumb=%q, got %+v", thumbPL, m4.rows)
	}

	// Entering a playlist from remote playlists: urlDone sets rows with Thumb
	itemTitle := "Track in playlist"
	thumbItem := "https://i.ytimg.com/vi/item1/hqdefault.jpg"
	m4.urlDone(urlMsg{
		url: "https://www.youtube.com/playlist?list=PL1",
		l: &verb.ItemList{
			Status: "ok",
			Engine: "yt",
			Title:  "My Playlist",
			Count:  1,
			Items: []verb.Item{
				{Title: &itemTitle, URL: "https://www.youtube.com/watch?v=item1", Thumbnail: thumbItem},
			},
		},
	})
	if m4.src != srcContainer || len(m4.rows) != 1 {
		t.Fatalf("urlDone should open srcContainer with 1 row, got src=%v rows=%d", m4.src, len(m4.rows))
	}
	if m4.rows[0].Thumb != thumbItem {
		t.Fatalf("item in container should have Thumb=%q, got %q", thumbItem, m4.rows[0].Thumb)
	}
	m4.updateInspector()
	if m4.inspector.thumbURL != thumbItem {
		t.Fatalf("inspector should receive selected item's thumbURL=%q, got %q", thumbItem, m4.inspector.thumbURL)
	}

	// Test 22-item window bound
	m22 := model(t, Options{})
	m22.pick = make([]verb.Playlist, 22)
	for i := range m22.pick {
		m22.pick[i] = verb.Playlist{Name: fmt.Sprintf("PL %d", i+1)}
	}
	if pl := m22.pickLines(); pl > 9 {
		t.Fatalf("pickLines for 22 items should be capped at 9, got %d", pl)
	}
}
