package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/binlecode/ting/internal/verb"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// updateList is the list's key table. Its shape is the shell TUI's two blocks: the universal
// keys (playback and the display toggles, which belong to the player or the renderer and so
// work in every row source) and the list keys (moving, acting on a row, fetching). The digits
// live in the list block only — with / open they are query text, and the filter has its own
// reader — so the filter is safe by construction.
func (m *Model) updateList(k tea.KeyMsg) tea.Cmd {
	key := k.String()
	if k.Paste {
		return m.pasted(string(k.Runes))
	}

	if m.stageMode {
		switch key {
		case "esc", "F", "q", "Q":
			m.stageMode = false
			return nil
		case " ":
			return m.togglePause()
		case "s", "S":
			return m.stop()
		case "-":
			return m.adjustVolume(-5)
		case "=":
			return m.adjustVolume(5)
		case "[":
			return m.verbCmd("", "", func(ctx context.Context, id string) error { return m.suite.Seek(ctx, id, -10) })
		case "]":
			return m.verbCmd("", "", func(ctx context.Context, id string) error { return m.suite.Seek(ctx, id, 10) })
		case "r":
			return m.cycleLoop()
		default:
			return nil
		}
	}

	if m.leader == "w" {
		m.leader = ""
		switch key {
		case "1":
			return m.switchWorkspace(WsSearch)
		case "2":
			return m.switchWorkspace(WsFeeds)
		case "3":
			return m.switchWorkspace(WsQueue)
		case "4":
			return m.switchWorkspace(WsPlaylists)
		case "5":
			return m.switchWorkspace(WsHistory)
		default:
			return nil
		}
	}

	// The jump count dies on any key that neither builds it nor ends it — vim's rule, applied
	// in one place in front of both blocks rather than once per arm.
	if !isDigit(key) && key != "j" && key != "J" {
		m.jump = ""
	}

	switch key {
	case " ":
		return m.togglePause()
	case "s", "S":
		return m.stop()
	case "r":
		return m.cycleLoop()
	case "-":
		return m.adjustVolume(-5)
	case "=":
		return m.adjustVolume(5)
	case "[":
		return m.verbCmd("", "", func(ctx context.Context, id string) error { return m.suite.Seek(ctx, id, -10) })
	case "]":
		return m.verbCmd("", "", func(ctx context.Context, id string) error { return m.suite.Seek(ctx, id, 10) })
	case "?", "？":
		m.opt.Keys = next([]string{"core", "full", "hidden"}, m.opt.Keys)
		m.mark("TING_KEYS")
		return nil
	case "#", "＃":
		m.opt.RowIndex = !m.opt.RowIndex
		m.mark("TING_ROW_INDEX")
		return nil
	case "tab":
		// The cursor is the anchor across the switch: page mode takes the page it falls in,
		// scroll mode pulls its window to it on the next frame.
		m.opt.ListMode = next([]string{"scroll", "page"}, m.opt.ListMode)
		m.mark("TING_LIST_MODE")
		return nil
	}

	switch key {
	case "ctrl+c", "q":
		return tea.Quit
	case "up", "k", "K", "down", "J", "left", "right":
		return m.move(key)
	case "j":
		// Two keys in one, told apart by the register and never by a clock.
		if m.jump != "" {
			m.jumpTo(m.jump)
			m.jump = ""
			return nil
		}
		return m.move(key)
	case "enter":
		switch m.src {
		case srcChapters:
			return m.playChapter()
		case srcQueue:
			m.queueKey("enter")
			return nil
		case srcPlaylists:
			m.openFocusedPlaylist()
			return nil
		case srcRemotePlaylists:
			return m.openFocusedRemotePlaylist()
		}
		return m.playCmd()
	case "esc":
		if m.src == srcPlaylist {
			m.openPlaylists()
		} else if m.src == srcContainer && m.stage.Has(WsPlaylists) && m.stage.State(WsPlaylists).Src == srcRemotePlaylists {
			st := m.stage.State(WsPlaylists)
			m.src, m.label = st.Src, st.Label
			m.all, m.rows = st.All, st.Rows
			m.cursor, m.top = st.Cursor, st.Top
			m.filter, m.filterOn = st.Filter, st.FilterOn
			m.clampCursor()
		} else if m.src == srcPlaylists || m.src == srcRemotePlaylists || m.src == srcContainer {
			m.backToSearch()
		}
		return nil
	case "n", "N":
		return m.openPrompt("")
	case "o", "O":
		if !m.searchOnly() {
			return nil
		}
		return m.cycleSort()
	case "e", "E":
		if !m.searchOnly() {
			return nil
		}
		return m.cycleEngine()
	case "v", "V":
		m.opt.Play.Mode = next(modeCycle, m.opt.Play.Mode)
		m.mark("TING_PLAY_MODE")
	case "f":
		m.opt.Play.Quality = next(qualityCycle, m.opt.Play.Quality)
		m.mark("TING_PLAY_QUALITY")
	case "F":
		m.stageMode = true
		return nil
	case "l", "L":
		if m.opt.Lang == "zh" {
			m.opt.Lang, m.s = "en", strsEN
		} else {
			m.opt.Lang, m.s = "zh", strsZH
		}
		m.mark("TING_LANG")
	case "t", "T":
		// No-op with colours off: --color never and NO_COLOR are not repainted mid-session.
		if m.opt.Colors {
			m.opt.Theme = next(ThemeCycle, m.opt.Theme)
			m.p = paletteFor(true, m.opt.Theme, m.opt.BG, m.opt.TrueColor)
			m.mark("TING_THEME")
		}
	case "+":
		if m.src == srcPlaylists || m.src == srcRemotePlaylists {
			m.notice(m.s.QAct+":", m.s.PLListOnly)
			return nil
		}
		return m.enqueue()
	case ">":
		return m.skip()
	case "/":
		m.filterOn, m.filter = true, ""
		m.applyFilter()
	case "a", "A":
		// The library's rows are lists, not tracks: there is nothing under the cursor to add.
		if m.src == srcPlaylists || m.src == srcRemotePlaylists {
			m.notice(m.s.PLAct+":", m.s.PLListOnly)
			return nil
		}
		return m.addToPlaylist()
	case "b":
		m.browsePlaylists()
	case "B":
		return m.browseRemotePlaylists()
	case "d":
		m.removeFromPlaylist()
	case "D":
		m.deletePlaylist()
	case "R":
		return m.renamePlaylist()
	case "z", "Z":
		m.undo()
	case "h", "H":
		m.openHistory()
	case "g":
		return m.openRelated()
	case "w", "W":
		m.leader = "w"
		m.notice(m.s.WorkspaceAct, m.s.WorkspaceHint)
		return nil
	case "ctrl+n":
		return m.switchWorkspaceNext()
	case "ctrl+p":
		return m.switchWorkspacePrev()
	case "c", "C":
		return m.openParts()
	case "i", "I":
		return m.openChapters()
	case "u", "U":
		m.openQueue()
	case "x", "X":
		// Queue-position keys act only with the queue on screen: an unguarded x would be a
		// destructive key that fires from anywhere.
		if m.src == srcQueue {
			m.queueKey(key)
		}
	case "p", "P":
		if m.src == srcQueue {
			m.queueKey(key)
			return nil
		}
		return m.pasted(clipboard())
	case "ctrl+v":
		return m.pasted(clipboard())
	default:
		// Six digits is an arithmetic guard, not a semantic one; out of range is jumpTo's
		// business.
		if isDigit(key) && len(m.jump) < 6 {
			m.jump += key
		}
	}
	return nil
}

func isDigit(k string) bool { return len(k) == 1 && k[0] >= '0' && k[0] <= '9' }

// move is both list modes. Page mode has two axes (rows and pages) and its two edges fetch or
// drop one batch; scroll mode has one axis, so the ends of the list carry those two instead,
// and ←/→ are a silent no-op. Growing and shrinking are search-only and never under a filter:
// the page a filter shows is a page of matches, and running off its end is not a request for
// more rows.
func (m *Model) move(key string) tea.Cmd {
	n := len(m.rows)
	up := key == "up" || key == "k" || key == "K"
	down := key == "down" || key == "j" || key == "J"
	edges := !m.filterOn && m.src == srcSearch
	if m.opt.ListMode == "scroll" {
		switch {
		case up && m.cursor > 0:
			m.cursor--
		case up && edges:
			m.fewer()
		case down && m.cursor < n-1:
			m.cursor++
		case down && edges:
			return m.more()
		}
		return nil
	}
	ps := m.layout().psize
	page, pages := m.cursor/ps, (n+ps-1)/ps
	switch {
	case up && m.cursor > 0:
		m.cursor--
	case down && m.cursor < n-1:
		m.cursor++
	case key == "right" && page < pages-1:
		m.cursor = (page + 1) * ps
	case key == "right" && edges:
		return m.more()
	case key == "left" && page > 0:
		m.cursor = (page - 1) * ps
	case key == "left" && edges:
		m.fewer()
	}
	return nil
}

// more fetches one batch more of the same query; the cursor steps onto the new rows only if
// the fetch actually brought some.
func (m *Model) more() tea.Cmd {
	if !m.searched() || m.hold() {
		return nil
	}
	o := m.opt.Search
	o.N += m.opt.Batch
	return m.fetch(fetchMore, m.query, m.opt.Engine, o,
		m.s.MoreAct+" "+m.g.Dash+" "+m.s.Refetch+` "`+m.query+`" `+m.g.Sep+" "+strconv.Itoa(o.N)+" "+m.s.ResultsN+m.g.Ell)
}

// fewer drops one batch from the tail, locally: the rows are already here. The floor is
// max(batch, one screen), and what is stored is how many were asked for, not how many came.
func (m *Model) fewer() {
	if m.hold() {
		return
	}
	floor := m.opt.Batch
	if ps := m.layout().psize; ps > floor {
		floor = ps
	}
	target := m.opt.Search.N - m.opt.Batch
	if target < floor {
		target = floor
	}
	if target >= m.opt.Search.N {
		return
	}
	m.opt.Search.N = target
	m.mark("TING_SEARCH_RESULTS")
	if len(m.all) > target {
		m.all = m.all[:target]
	}
	m.rows = m.all
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
}

// jumpTo is `Nj`: the ABSOLUTE row number # prints; 0 and out of range do nothing.
func (m *Model) jumpTo(num string) {
	n := 0
	for _, c := range num {
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > len(m.rows) {
		return
	}
	m.cursor = n - 1
}

// searched is whether the list is a search's, so a re-fetch has a query to send: a URL
// opened in its place keeps the URL as its header, and no engine searches for that.
func (m *Model) searched() bool {
	return m.query != "" && urlTarget(m.query) == "" && m.feed == ""
}

func (m *Model) cycleSort() tea.Cmd {
	if !m.searched() || m.hold() {
		return nil
	}
	o := m.opt.Search
	o.Sort = next(sortCycle, o.Sort)
	return m.fetch(fetchSort, m.query, m.opt.Engine, o,
		m.s.SortAct+" "+m.g.Arrow+" "+o.Sort+" "+m.g.Dash+" "+m.s.Refetch+` "`+m.query+`"`+m.g.Ell)
}

func (m *Model) cycleEngine() tea.Cmd {
	if len(m.opt.Engines) < 2 || m.hold() {
		return nil
	}
	e := (m.opt.Engine + 1) % len(m.opt.Engines)
	if !m.searched() {
		m.opt.Engine, m.auth = e, ""
		// A URL's row keeps its own engine on the status line, so the switch would not
		// show at all: say where it went.
		if m.query != "" {
			m.notice(m.s.EngineAct+":", m.opt.Engines[e].Name+" "+m.g.Dash+" "+m.s.NextSearch)
		}
		return m.authCmd()
	}
	return m.fetch(fetchEngine, m.query, e, m.opt.Search,
		m.s.EngineAct+" "+m.g.Arrow+" "+m.opt.Engines[e].Name+" "+m.g.Dash+" "+m.s.Refetch+` "`+m.query+`"`+m.g.Ell)
}

func (m *Model) togglePause() tea.Cmd {
	if m.playerID == "" {
		return nil
	}
	if m.paused() {
		return m.verbCmd("", "", m.suite.Resume)
	}
	return m.verbCmd("", "", m.suite.Pause)
}

func (m *Model) stop() tea.Cmd {
	if m.playerID == "" {
		return nil
	}
	id, s, ctx := m.playerID, m.suite, m.ctx
	stopLost := m.s.AdoptAct + ":"
	return func() tea.Msg {
		err := s.Stop(ctx, id)
		if err != nil {
			var ve *verb.Error
			// 4 is "not playing": it already ended, which is what s asked for.
			if errors.As(err, &ve) && ve.Kind() == verb.NotEffective {
				err = nil
			}
		}
		return verbDoneMsg{label: stopLost, text: "the player could not be stopped and is still playing — stop it with: ting-play --stop --id " + id, err: err}
	}
}

// adjustVolume coalesces held presses: the target moves from the newest one, at most one
// --set-volume is in flight, and a press while it is only replaces the value it will send
// next. The starting point is the player's own volume from --watch.
func (m *Model) adjustVolume(delta int) tea.Cmd {
	if m.playerID == "" {
		return nil
	}
	var base int
	switch {
	case m.volTarget != nil:
		base = *m.volTarget
	case m.last != nil && m.last.Volume != nil:
		base = int(*m.last.Volume)
	default:
		return nil
	}
	t := base + delta
	if t < 0 {
		t = 0
	}
	if t > 100 {
		t = 100
	}
	m.volTarget = &t
	if m.volInFlight {
		m.volQueued = true
		return nil
	}
	return m.volCmd(t)
}

// cycleLoop walks off → seq → one. off and one are the player's own states and go to a
// running player at once; seq is how the next Enter builds its launch, so it says so.
func (m *Model) cycleLoop() tea.Cmd {
	m.opt.Loop = next(loopCycle, m.opt.Loop)
	m.mark("TING_LOOP_MODE")
	switch m.opt.Loop {
	case "one":
		m.notice(m.s.LoopAct+":", m.s.LoopOne)
	case "seq":
		m.notice(m.s.LoopAct+":", m.s.LoopSeq+m.s.LoopNext)
	default:
		m.notice(m.s.LoopAct+":", m.s.LoopOff)
	}
	push := "off"
	if m.opt.Loop == "one" {
		push = "one"
	}
	return m.verbCmd("", "", func(ctx context.Context, id string) error { return m.suite.SetLoop(ctx, id, push) })
}

func (m *Model) enqueue() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	if m.playerID == "" {
		m.notice(m.s.QAct+":", m.s.QNone)
		return nil
	}
	r := m.rows[m.cursor]
	id, s, ctx := m.playerID, m.suite, m.ctx
	label, done := m.s.QAct+":", m.s.QAdded+" "+m.g.Arrow+" "+r.Title
	failed, it := m.s.Failed, m.item(r)
	return func() tea.Msg {
		w, err := s.Enqueue(ctx, id, []verb.QueueItem{it}, pid)
		if err != nil {
			return verbDoneMsg{label: label, text: failed, err: err}
		}
		return armMsg{store: "queue", w: w, head: label, label: done}
	}
}

func (m *Model) skip() tea.Cmd {
	if m.playerID == "" {
		m.notice(m.s.QAct+":", m.s.QNone)
		return nil
	}
	return m.verbCmd(m.s.QAct+":", m.s.QEnd, m.suite.Next)
}

// ── filter ──────────────────────────────────────────────────────────────────────────────

// updateFilter is the live filter's own reader: every printable key is query text, the
// arrows move over the matches, Enter plays, Esc leaves and puts every row back.
func (m *Model) updateFilter(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		m.filterOn, m.filter = false, ""
		m.applyFilter()
		return nil
	case "enter":
		if u := urlTarget(m.filter); u != "" {
			m.filterOn, m.filter = false, ""
			m.applyFilter()
			return m.loadURL(u)
		}
		switch m.src {
		case srcChapters:
			return m.playChapter()
		case srcPlaylists:
			m.openFocusedPlaylist()
			return nil
		case srcRemotePlaylists:
			cmd := m.openFocusedRemotePlaylist()
			m.filterOn, m.filter = false, ""
			m.applyFilter()
			return cmd
		}
		return m.playCmd()
	case "ctrl+v":
		m.filter += strings.TrimSpace(clipboard())
		m.applyFilter()
		return nil
	case "up", "down", "left", "right":
		return m.move(k.String())
	case "backspace":
		if r := []rune(m.filter); len(r) > 0 {
			m.filter = string(r[:len(r)-1])
			m.applyFilter()
		}
		return nil
	case "tab":
		return nil
	}
	if k.Type == tea.KeyRunes || k.Type == tea.KeySpace || k.Paste {
		m.filter += string(k.Runes)
		m.applyFilter()
	}
	return nil
}

func (m *Model) applyFilter() {
	m.cursor, m.top = 0, 0
	tokens := strings.Fields(m.filter)
	if len(tokens) == 0 {
		m.rows = m.all
		return
	}
	var out []row
	for _, r := range m.all {
		if r.matches(tokens) {
			out = append(out, r)
		}
	}
	m.rows = out
}

// ── prompt ──────────────────────────────────────────────────────────────────────────────

// askKind is what the one line reader is asking for. One reader for all of them, so Esc,
// editing and wide characters mean the same thing at every prompt.
type askKind int

const (
	askSearch askKind = iota // the startup query and n
	askNew                   // a: the first playlist's name
	askAdd                   // a: which playlist (number or name)
	askRename                // R: the new name
)

// ask opens the prompt. pick is the numbered list a chooses from, drawn above it.
func (m *Model) ask(kind askKind, label, head string, pick []verb.Playlist) tea.Cmd {
	m.prompting, m.askKind, m.askLabel, m.askHead, m.pick, m.pickCursor = true, kind, label, head, pick, 0
	m.input.SetValue("")
	m.input.Focus()
	return textinput.Blink
}

func (m *Model) openPrompt(text string) tea.Cmd {
	cmd := m.ask(askSearch, "", "", nil)
	m.input.SetValue(text)
	m.input.CursorEnd()
	return cmd
}

// updatePrompt reads the line. Esc or an empty line cancels; the startup prompt, with
// nothing on screen yet, cancels to a clean quit.
func (m *Model) updatePrompt(k tea.KeyMsg) tea.Cmd {
	switch k.Type {
	case tea.KeyCtrlC:
		return tea.Quit
	case tea.KeyEsc:
		return m.cancelPrompt()
	case tea.KeyUp:
		if len(m.pick) > 0 {
			m.pickCursor = (m.pickCursor - 1 + len(m.pick)) % len(m.pick)
			return nil
		}
	case tea.KeyDown:
		if len(m.pick) > 0 {
			m.pickCursor = (m.pickCursor + 1) % len(m.pick)
			return nil
		}
	case tea.KeyRunes:
		if len(m.pick) > 0 && m.input.Value() == "" {
			if string(k.Runes) == "j" || string(k.Runes) == "J" {
				m.pickCursor = (m.pickCursor + 1) % len(m.pick)
				return nil
			}
			if string(k.Runes) == "k" || string(k.Runes) == "K" {
				m.pickCursor = (m.pickCursor - 1 + len(m.pick)) % len(m.pick)
				return nil
			}
		}
	case tea.KeyEnter:
		v := strings.TrimSpace(m.input.Value())
		if v == "" && len(m.pick) > 0 {
			v = strconv.Itoa(m.pickCursor + 1)
		}
		if v == "" {
			return m.cancelPrompt()
		}
		kind, pick := m.askKind, m.pick
		// A search behind a fetch in flight keeps the prompt and its text: closing it would
		// throw away what was typed for a key that did nothing.
		if kind == askSearch && m.hold() {
			return nil
		}
		m.prompting, m.pick, m.pickCursor = false, nil, 0
		m.input.Blur()
		switch kind {
		case askSearch:
			if u := urlTarget(v); u != "" {
				if m.all == nil {
					m.query = u
				}
				return m.loadURL(u)
			}
			return m.fetch(fetchNew, v, m.opt.Engine, m.opt.Search, m.s.Searching+` "`+v+`"`+m.g.Ell)
		case askNew:
			m.doAdd(v)
		case askAdd:
			m.doAdd(pickName(v, pick))
		case askRename:
			m.doRename(v)
		}
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return cmd
}

// pickName resolves an answer to the picker: a number on the list is that row's name (it
// wins over a list literally named "7", which is still reachable by its own number);
// anything else is a name.
func pickName(v string, pick []verb.Playlist) string {
	if n, err := strconv.Atoi(v); err == nil && len(v) <= 9 && n >= 1 && n <= len(pick) {
		return pick[n-1].Name
	}
	return v
}

func (m *Model) cancelPrompt() tea.Cmd {
	m.prompting, m.pick, m.pickCursor = false, nil, 0
	m.input.Blur()
	if m.all == nil && m.pending == nil && m.busy == "" {
		return tea.Quit
	}
	return nil
}
