package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/binlecode/ting/internal/verb"
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
		// A pasted query opens the prompt with the text in it.
		return m.openPrompt(string(k.Runes))
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
		return nil
	case "#", "＃":
		m.opt.RowIndex = !m.opt.RowIndex
		return nil
	case "tab":
		// The cursor is the anchor across the switch: page mode takes the page it falls in,
		// scroll mode pulls its window to it on the next frame.
		m.opt.ListMode = next([]string{"scroll", "page"}, m.opt.ListMode)
		return nil
	}

	switch key {
	case "ctrl+c", "q":
		return tea.Quit
	case "Q":
		m.keepOnQuit = true
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
		return m.playCmd()
	case "n", "N":
		return m.openPrompt("")
	case "o", "O":
		return m.cycleSort()
	case "e", "E":
		return m.cycleEngine()
	case "v", "V":
		m.opt.Play.Mode = next(modeCycle, m.opt.Play.Mode)
	case "f", "F":
		m.opt.Play.Quality = next(qualityCycle, m.opt.Play.Quality)
	case "l", "L":
		if m.opt.Lang == "zh" {
			m.opt.Lang, m.s = "en", strsEN
		} else {
			m.opt.Lang, m.s = "zh", strsZH
		}
	case "+":
		return m.enqueue()
	case ">":
		return m.skip()
	case "/":
		m.filterOn, m.filter = true, ""
		m.applyFilter()
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
	edges := !m.filterOn
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
	if m.pending != nil || m.query == "" {
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
	if m.pending != nil {
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

func (m *Model) cycleSort() tea.Cmd {
	if m.pending != nil || m.query == "" {
		return nil
	}
	o := m.opt.Search
	o.Sort = next(sortCycle, o.Sort)
	return m.fetch(fetchSort, m.query, m.opt.Engine, o,
		m.s.SortAct+" "+m.g.Arrow+" "+o.Sort+" "+m.g.Dash+" "+m.s.Refetch+` "`+m.query+`"`+m.g.Ell)
}

func (m *Model) cycleEngine() tea.Cmd {
	if m.pending != nil || len(m.opt.Engines) < 2 {
		return nil
	}
	e := (m.opt.Engine + 1) % len(m.opt.Engines)
	if m.query == "" {
		m.opt.Engine, m.auth = e, ""
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
	failed := m.s.Failed
	return func() tea.Msg {
		err := s.Enqueue(ctx, id, []verb.QueueItem{{Engine: r.Engine, URL: r.URL, Title: r.Title, Duration: r.Duration}}, pid)
		if err != nil {
			return verbDoneMsg{label: label, text: failed, err: err}
		}
		return noticeMsg{label: label, text: done}
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
		return m.playCmd()
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

func (m *Model) openPrompt(text string) tea.Cmd {
	m.prompting = true
	m.input.SetValue(text)
	m.input.CursorEnd()
	m.input.Focus()
	return nil
}

// updatePrompt is the search prompt, at startup and on n. Esc or an empty line cancels; at
// startup, with nothing on screen yet, cancelling is a clean quit.
func (m *Model) updatePrompt(k tea.KeyMsg) tea.Cmd {
	switch k.Type {
	case tea.KeyCtrlC:
		return tea.Quit
	case tea.KeyEsc:
		return m.cancelPrompt()
	case tea.KeyEnter:
		q := strings.TrimSpace(m.input.Value())
		if q == "" {
			return m.cancelPrompt()
		}
		m.prompting = false
		m.input.Blur()
		if m.pending != nil && m.all != nil {
			return nil
		}
		return m.fetch(fetchNew, q, m.opt.Engine, m.opt.Search, m.s.Searching+` "`+q+`"`+m.g.Ell)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return cmd
}

func (m *Model) cancelPrompt() tea.Cmd {
	m.prompting = false
	m.input.Blur()
	if m.all == nil && m.pending == nil {
		return tea.Quit
	}
	return nil
}
