// Package tui is the human face: one Bubbletea model over the verb package. It holds no
// site knowledge and runs nothing itself; every effect is a verb call returned as a tea.Cmd.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/binlecode/ting/internal/verb"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// Options is what the entry point resolved from flags and the config chain.
type Options struct {
	Engines   []verb.Engine
	Engine    int // index into Engines
	Query     string
	Search    verb.SearchOpts
	Play      verb.PlayOpts
	PageRows  int
	Version   string
	AdoptFrom []verb.Player // live players at startup; the newest is adopted, never stopped
}

type view int

const (
	viewInput view = iota
	viewList
)

// Model is the whole UI state.
type Model struct {
	suite *verb.Suite
	opt   Options
	ctx   context.Context

	view    view
	input   textinput.Model
	query   string
	results []verb.Result
	cursor  int
	top     int

	searching bool
	searchGen int
	note      string
	errMsg    string

	// The player on the banner. owned = this session started it, so q stops it; an adopted
	// one is left running.
	playerID string
	owned    bool
	last     *verb.Event
	lastAt   time.Time
	watcher  *verb.Watcher
	watchGen int
	starting bool
	ticking  bool // one tick chain at a time

	width, height int
}

// New builds the model. ctx bounds every verb call and the watch process.
func New(ctx context.Context, suite *verb.Suite, opt Options) *Model {
	ti := textinput.New()
	ti.Prompt = "search: "
	ti.CharLimit = 200
	m := &Model{suite: suite, opt: opt, ctx: ctx, input: ti, width: 80, height: 24}
	if opt.PageRows <= 0 {
		m.opt.PageRows = 10
	}
	if opt.Query == "" {
		m.view = viewInput
		m.input.Focus()
	} else {
		m.view = viewList
		m.query = opt.Query
	}
	return m
}

// SessionPlayer is the player q should stop on the way out: one this session started.
func (m *Model) SessionPlayer() string {
	if m.owned {
		return m.playerID
	}
	return ""
}

// Close releases the watch process (not the player).
func (m *Model) Close() {
	if m.watcher != nil {
		m.watcher.Close()
		m.watcher = nil
	}
}

func (m *Model) engine() verb.Engine { return m.opt.Engines[m.opt.Engine] }

// ── messages ────────────────────────────────────────────────────────────────────────────

type searchDoneMsg struct {
	gen int
	res *verb.SearchResult
	err error
}

type playDoneMsg struct {
	st  *verb.Started
	err error
}

type watchStartedMsg struct {
	gen int
	w   *verb.Watcher
	err error
}

type watchEventMsg struct {
	gen int
	ev  verb.Event
	ok  bool
}

type verbDoneMsg struct {
	what string
	err  error
}

type tickMsg struct{}

// ── commands ────────────────────────────────────────────────────────────────────────────

func (m *Model) searchCmd() tea.Cmd {
	m.searchGen++
	m.searching = true
	m.errMsg = ""
	gen, eng, q, o := m.searchGen, m.engine(), m.query, m.opt.Search
	ctx := m.ctx
	return func() tea.Msg {
		res, err := verb.Search(ctx, eng, q, o)
		return searchDoneMsg{gen: gen, res: res, err: err}
	}
}

// playCmd stops whatever is on the banner first — a switch, not a second voice — then starts
// the row. The stop is best-effort: a player that already ended answers 4, which is fine.
func (m *Model) playCmd(r verb.Result) tea.Cmd {
	prev, eng, o, s, ctx := m.playerID, m.engine().Name, m.opt.Play, m.suite, m.ctx
	m.starting = true
	m.errMsg = ""
	return func() tea.Msg {
		if prev != "" {
			_ = s.Stop(ctx, prev)
		}
		st, err := s.Play(ctx, eng, r.URL, o)
		return playDoneMsg{st: st, err: err}
	}
}

func (m *Model) watchCmd(id string) tea.Cmd {
	if m.watcher != nil {
		m.watcher.Close()
		m.watcher = nil
	}
	m.watchGen++
	gen, s, ctx := m.watchGen, m.suite, m.ctx
	return func() tea.Msg {
		w, err := s.Watch(ctx, id)
		return watchStartedMsg{gen: gen, w: w, err: err}
	}
}

func nextEvent(gen int, w *verb.Watcher) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-w.Events
		return watchEventMsg{gen: gen, ev: ev, ok: ok}
	}
}

func (m *Model) verbCmd(what string, f func(context.Context, string) error) tea.Cmd {
	id, ctx := m.playerID, m.ctx
	return func() tea.Msg { return verbDoneMsg{what: what, err: f(ctx, id)} }
}

func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

// ── update ──────────────────────────────────────────────────────────────────────────────

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink}
	if m.view == viewList {
		cmds = append(cmds, m.searchCmd())
	}
	if n := len(m.opt.AdoptFrom); n > 0 {
		p := m.opt.AdoptFrom[n-1]
		m.playerID, m.owned = p.ID, false
		cmds = append(cmds, m.watchCmd(p.ID))
	}
	return tea.Batch(cmds...)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampScroll()
		return m, nil

	case searchDoneMsg:
		if msg.gen != m.searchGen {
			return m, nil
		}
		m.searching = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.results, m.cursor, m.top = msg.res.Results, 0, 0
		if len(m.results) == 0 {
			m.note = "no results"
		} else {
			m.note = ""
		}
		return m, nil

	case playDoneMsg:
		m.starting = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			m.playerID, m.owned, m.last = "", false, nil
			return m, nil
		}
		m.playerID, m.owned, m.last = msg.st.ID, true, nil
		if msg.st.Title != nil {
			t := *msg.st.Title
			m.last = &verb.Event{Event: "started", ID: msg.st.ID, Title: &t}
		}
		return m, m.watchCmd(msg.st.ID)

	case watchStartedMsg:
		if msg.gen != m.watchGen {
			if msg.w != nil {
				msg.w.Close()
			}
			return m, nil
		}
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.watcher = msg.w
		return m, nextEvent(msg.gen, msg.w)

	case watchEventMsg:
		if msg.gen != m.watchGen {
			return m, nil
		}
		if !msg.ok {
			// The stream closed: either after `end`, or because --watch itself failed
			// (a player that died before the watch attached answers 4).
			if m.watcher != nil {
				if err := m.watcher.Err(); err != nil && m.last == nil {
					var ve *verb.Error
					if !errors.As(err, &ve) || ve.Kind() != verb.NotEffective {
						m.errMsg = err.Error()
					}
				}
				m.watcher = nil
			}
			m.playerID, m.owned = "", false
			return m, nil
		}
		ev := msg.ev
		m.last, m.lastAt = &ev, time.Now()
		if ev.Event == "end" {
			if ev.Reason != nil {
				m.errMsg = "player ended: " + *ev.Reason
			}
		}
		cmds := []tea.Cmd{nextEvent(msg.gen, m.watcher)}
		if m.playing() && !m.ticking {
			m.ticking = true
			cmds = append(cmds, tick())
		}
		return m, tea.Batch(cmds...)

	case verbDoneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		}
		return m, nil

	case tickMsg:
		if m.playing() {
			return m, tick()
		}
		m.ticking = false
		return m, nil

	case tea.KeyMsg:
		if m.view == viewInput {
			return m.updateInput(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m *Model) updateInput(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if m.query == "" && len(m.results) == 0 {
			return m, tea.Quit
		}
		m.view = viewList
		m.input.Blur()
		return m, nil
	case tea.KeyEnter:
		q := strings.TrimSpace(m.input.Value())
		if q == "" {
			return m, nil
		}
		m.query = q
		m.view = viewList
		m.input.Blur()
		return m, m.searchCmd()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m *Model) updateList(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		m.clampScroll()
	case "down", "j":
		if m.cursor < len(m.results)-1 {
			m.cursor++
		}
		m.clampScroll()
	case "enter":
		if m.cursor < len(m.results) && !m.starting {
			return m, m.playCmd(m.results[m.cursor])
		}
	case " ":
		if m.playerID == "" {
			return m, nil
		}
		if m.last != nil && m.last.Paused {
			return m, m.verbCmd("resume", m.suite.Resume)
		}
		return m, m.verbCmd("pause", m.suite.Pause)
	case "s":
		if m.playerID != "" {
			return m, m.verbCmd("stop", m.suite.Stop)
		}
	case "n":
		m.view = viewInput
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink
	case "e":
		if len(m.opt.Engines) > 1 {
			m.opt.Engine = (m.opt.Engine + 1) % len(m.opt.Engines)
			if m.query != "" {
				return m, m.searchCmd()
			}
		}
	}
	return m, nil
}

// ── view ────────────────────────────────────────────────────────────────────────────────

var (
	accent = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	dim    = lipgloss.NewStyle().Faint(true)
	bold   = lipgloss.NewStyle().Bold(true)
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

// listRows is how many result rows fit: header, blank, banner, hints, message.
func (m *Model) listRows() int {
	n := m.height - 5
	if n > m.opt.PageRows {
		n = m.opt.PageRows
	}
	if n < 1 {
		n = 1
	}
	return n
}

func (m *Model) clampScroll() {
	rows := m.listRows()
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+rows {
		m.top = m.cursor - rows + 1
	}
}

func (m *Model) playing() bool {
	return m.playerID != "" && m.last != nil && m.last.Ready && !m.last.Paused
}

// position extrapolates from the last event with the monotonic clock, and stops while paused.
func (m *Model) position() (float64, bool) {
	if m.last == nil || m.last.Position == nil {
		return 0, false
	}
	p := *m.last.Position
	if m.playing() {
		p += time.Since(m.lastAt).Seconds()
	}
	if m.last.Duration != nil && p > *m.last.Duration {
		p = *m.last.Duration
	}
	return p, true
}

func fmtDur(sec float64) string {
	s := int(sec)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return runewidth.FillRight(runewidth.Truncate(s, w, "…"), w)
}

func (m *Model) View() string {
	w := m.width
	var b strings.Builder

	// header
	head := accent.Render("♫ ting") + dim.Render(" · ") + bold.Render(m.engine().Name)
	if m.query != "" {
		head += dim.Render(" · ") + m.query
	}
	if m.searching {
		head += dim.Render("  searching…")
	}
	b.WriteString(head + "\n")

	// body
	if m.view == viewInput {
		b.WriteString(m.input.View() + "\n")
	} else {
		b.WriteString("\n")
	}
	rows := m.listRows()
	for i := m.top; i < m.top+rows; i++ {
		if i >= len(m.results) {
			b.WriteString("\n")
			continue
		}
		r := m.results[i]
		dur := ""
		if r.Duration != nil {
			dur = fmtDur(*r.Duration)
		}
		chanW := 18
		if w < 60 {
			chanW = 0
		}
		// A fixed duration column, right-aligned, so the channel column lines up across rows
		// whose durations differ in width (a live row has none; a mix runs past ten hours).
		const durW = 8
		titleW := w - 2 - chanW - 1 - durW
		line := fit(r.Title, titleW)
		if chanW > 0 {
			line += " " + dim.Render(fit(r.Channel, chanW-1))
		}
		line += " " + dim.Render(fmt.Sprintf("%*s", durW, dur))
		if i == m.cursor && m.view == viewList {
			b.WriteString(accent.Render("❯ ") + line + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}

	// banner
	b.WriteString(m.banner(w) + "\n")

	// hints
	b.WriteString(dim.Render(fit("enter play · space pause · s stop · n search · e source · q quit", w)) + "\n")

	// message
	switch {
	case m.errMsg != "":
		b.WriteString(red.Render(fit(m.errMsg, w)))
	case m.note != "":
		b.WriteString(dim.Render(fit(m.note, w)))
	}
	return b.String()
}

func (m *Model) banner(w int) string {
	if m.starting {
		return yellow.Render("… starting")
	}
	if m.playerID == "" || m.last == nil {
		if m.playerID != "" {
			return yellow.Render("… connecting")
		}
		return dim.Render("■ stopped")
	}
	ev := m.last
	glyph := green.Render("▶")
	switch {
	case ev.Paused:
		glyph = yellow.Render("❚❚")
	case !ev.Ready:
		glyph = yellow.Render("…")
	}
	right := ""
	if p, ok := m.position(); ok {
		right = fmtDur(p)
		if ev.Duration != nil {
			right += " / " + fmtDur(*ev.Duration)
		}
	}
	if !m.owned {
		right += dim.Render("  adopted")
	}
	title := ""
	if ev.Title != nil {
		title = *ev.Title
	}
	tw := w - 3 - lipgloss.Width(right) - 1
	return glyph + " " + fit(title, tw) + " " + right
}
