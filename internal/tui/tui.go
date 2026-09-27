// Package tui is the human face: one Bubbletea model over the verb package. It holds no
// site knowledge and runs nothing itself; every effect is a verb call returned as a tea.Cmd.
package tui

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/binlecode/ting/internal/config"
	"github.com/binlecode/ting/internal/verb"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Options is what the entry point resolved from flags and the config chain.
type Options struct {
	Engines   []verb.Engine
	Engine    int // index into Engines
	Query     string
	Search    verb.SearchOpts
	Play      verb.PlayOpts
	Loop      string // off | seq | one
	PageRows  int
	Batch     int // TING_FETCH_BATCH: the step each list edge grows or shrinks the row count by
	Keys      string
	ListMode  string
	RowIndex  bool
	Resource  bool
	Colors    bool
	Theme     string
	BG        Background
	TrueColor bool
	Pinned    func(key string) bool // set by the environment: never written back
	Cover     Cover
	Save      func([]config.Pref) error // writes preferences back; nil = never
	Lang      string
	ASCII     bool
	AmbigWide bool
	AdoptFrom []verb.Player // live players at startup: exactly one is adopted, never several
}

// The three rotations the keys walk. Written here, not configurable: a config key per cycle
// was retired with the other tuning knobs.
var (
	modeCycle    = []string{"audio", "video", "fast"}
	qualityCycle = []string{"auto", "medium", "high"}
	loopCycle    = []string{"off", "seq", "one"}
	sortCycle    = []string{"relevance", "view_count", "duration"}
)

func next(cycle []string, cur string) string {
	for i, v := range cycle {
		if v == cur {
			return cycle[(i+1)%len(cycle)]
		}
	}
	return cycle[0]
}

// fetchKind says what an in-flight search is for, because each commits differently.
type fetchKind int

const (
	fetchNew    fetchKind = iota // a new query: rows replaced, cursor to the top
	fetchMore                    // one batch more: cursor kept, and stepped onto the new rows
	fetchSort                    // o: same query, next sort field
	fetchEngine                  // e: same query, next source
)

// fetchReq is a search's whole intent. Nothing on screen changes until it succeeds, so a
// failure has nothing to roll back: the old engine, sort and row count were never replaced.
type fetchReq struct {
	gen    int
	kind   fetchKind
	query  string
	engine int
	opts   verb.SearchOpts
}

// Model is the whole UI state.
type Model struct {
	suite *verb.Suite
	opt   Options
	ctx   context.Context
	s     strs
	g     glyphs
	p     palette
	w     width

	// The rows. all is what the source returned; rows is what is on screen (all, or the
	// live filter's matches of it).
	src        source
	label      string // a stored source's name for itself, on the title line
	plName     string // the playlist on screen, as the store spells it
	total      int    // a parts or chapters list's length, for "part k/total"
	totalFmt   string // a parts list's summed length, when every part has one
	stash      stash
	info       *info
	chapFollow int
	all        []row
	rows       []row
	query      string
	cursor     int
	top        int // scroll mode's window top; page mode derives its page from the cursor
	psize      int // the last frame's page size
	jump       string

	filterOn bool
	filter   string

	prompting bool
	askKind   askKind
	askLabel  string
	askHead   string
	pick      []verb.Playlist
	payload   verb.QueueItem
	input     textinput.Model

	undoStore string // playlist | queue: the store holding this session's copy
	undoEnd   time.Time
	undoHead  string
	undoLabel string
	undoStop  bool // a stop the write implied, held until the offer closes

	searchGen int
	busy      string // the fetch in flight, as a line the frame carries until it lands
	pending   *fetchReq
	noticeL   string
	noticeT   string
	auth      string // "", signed-in browser name, "anon" or "blocked"
	fatal     error

	// The player on the banner. owned = this session started it.
	playerID   string
	owned      bool
	last       *verb.Event
	lastAt     time.Time
	watcher    *verb.Watcher
	watchGen   int
	starting   bool
	loading    bool // launched, not yet audible
	live       bool
	startedAt  time.Time
	playTitle  string
	playURL    string
	playEngine string
	cpu, mem   *float64
	keepOnQuit bool

	// Volume presses coalesce: one --set-volume in flight at most, and only the newest target.
	volTarget   *int
	volInFlight bool
	volQueued   bool

	q *[2]int // the queue's (pos, len)

	cover coverState

	dirty    map[string]bool // preferences a key changed and the file does not have yet
	said     map[string]bool // pinned keys already told about
	flushing bool            // a write-back is scheduled
	saveOff  bool            // the file could not be written: say so once, stop trying

	spin    int
	ticking bool

	width, height int
}

// New builds the model. ctx bounds every verb call and the watch process.
func New(ctx context.Context, suite *verb.Suite, opt Options) *Model {
	g := glyphsUTF
	if opt.ASCII {
		g = glyphsASCII
	}
	s := strsEN
	if opt.Lang == "zh" {
		s = strsZH
	}
	ti := textinput.New()
	ti.CharLimit = 400
	ti.Prompt = ""
	m := &Model{suite: suite, opt: opt, ctx: ctx, s: s, g: g, p: paletteFor(opt.Colors, opt.Theme, opt.BG, opt.TrueColor),
		w: newWidth(opt.AmbigWide), input: ti, width: 80, height: 24}
	if m.opt.Batch <= 0 {
		m.opt.Batch = 20
	}
	if m.opt.Search.N <= 0 {
		m.opt.Search.N = 20
	}
	m.query = opt.Query
	m.chapFollow = -1
	m.dirty, m.said = map[string]bool{}, map[string]bool{}
	m.cover = coverState{on: opt.Cover.On, cw: opt.Cover.CW, ch: opt.Cover.CH,
		done: map[string]*coverImg{}, failed: map[string]bool{}}
	if m.opt.Pinned == nil {
		m.opt.Pinned = func(string) bool { return false }
	}
	if m.query == "" {
		m.ask(askSearch, "", "", nil)
	}
	return m
}

// SessionPlayer is the player q stops on the way out: whatever is on the banner, adopted or
// started here, unless Q asked to leave it playing.
func (m *Model) SessionPlayer() string {
	if m.keepOnQuit {
		return ""
	}
	return m.playerID
}

// Fatal is why the session could not start (the first search broke rather than came back
// empty); nil otherwise.
func (m *Model) Fatal() error { return m.fatal }

// Close releases the watch process (not the player).
func (m *Model) Close() {
	if m.watcher != nil {
		m.watcher.Close()
		m.watcher = nil
	}
}

func (m *Model) engine() verb.Engine { return m.opt.Engines[m.opt.Engine] }

func (m *Model) notice(label, text string) { m.noticeL, m.noticeT = label, text }

// ── messages ────────────────────────────────────────────────────────────────────────────

type searchDoneMsg struct {
	req fetchReq
	res *verb.SearchResult
	err error
}

type authMsg struct {
	engine string
	auth   string
}

type playDoneMsg struct {
	st    *verb.Started
	title string
	url   string
	eng   string
	live  bool
	err   error
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
	label string // notice label on failure; "" = say nothing
	text  string // notice text on failure; "" = the error itself
	err   error
}

type volDoneMsg struct{ err error }

// noticeMsg is a verb that succeeded and has something to say about it.
type noticeMsg struct{ label, text string }

// armMsg is a write that succeeded off the update loop and may carry an undo offer.
type armMsg struct {
	store, head, label string
	w                  *verb.Written
}

type tickMsg struct{}

// ── commands ────────────────────────────────────────────────────────────────────────────

// fetch starts one search. The busy line is the frame's own "…" for it; only one search is
// in flight, and a key that would start another while it runs is ignored.
func (m *Model) fetch(kind fetchKind, query string, engine int, o verb.SearchOpts, busy string) tea.Cmd {
	m.searchGen++
	req := fetchReq{gen: m.searchGen, kind: kind, query: query, engine: engine, opts: o}
	m.pending = &req
	m.busy = busy
	eng, s, ctx := m.opt.Engines[engine].Name, m.suite, m.ctx
	return func() tea.Msg {
		res, err := s.Search(ctx, eng, query, o)
		return searchDoneMsg{req: req, res: res, err: err}
	}
}

func (m *Model) authCmd() tea.Cmd {
	eng, s, ctx := m.engine().Name, m.suite, m.ctx
	return func() tea.Msg {
		a, err := s.Auth(ctx, eng)
		if err != nil {
			return authMsg{engine: eng}
		}
		v := ""
		switch a.Auth {
		case "cookie":
			v = a.CookieBrowser
			if v == "" {
				v = "cookie"
			}
			if a.CookieReadable != nil && !*a.CookieReadable {
				v = "blocked"
			}
		case "anonymous":
			v = "anon"
		}
		return authMsg{engine: eng, auth: v}
	}
}

// playCmd stops whatever is on the banner first — a switch, not a second voice — then starts
// the row, or, in loop seq, the rows from the cursor down as one queue.
func (m *Model) playCmd() tea.Cmd {
	if m.cursor >= len(m.rows) || m.starting {
		return nil
	}
	r := m.rows[m.cursor]
	o := m.opt.Play
	o.Loop = "off"
	if m.opt.Loop == "one" {
		o.Loop = "one"
	}
	var queue []verb.QueueItem
	if m.opt.Loop == "seq" {
		for _, q := range m.rows[m.cursor:] {
			queue = append(queue, verb.QueueItem{Engine: q.Engine, URL: q.URL, Title: q.Title, Duration: q.Duration})
		}
	}
	prev, s, ctx := m.playerID, m.suite, m.ctx
	m.starting = true
	return func() tea.Msg {
		if prev != "" {
			_ = s.Stop(ctx, prev)
		}
		var st *verb.Started
		var err error
		if queue != nil {
			st, err = s.PlayQueue(ctx, queue, o)
		} else {
			st, err = s.Play(ctx, r.Engine, r.URL, o)
		}
		return playDoneMsg{st: st, title: r.Title, url: r.URL, eng: r.Engine, live: r.isLive(), err: err}
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

// verbCmd runs one call against the player on the banner.
func (m *Model) verbCmd(label, text string, f func(context.Context, string) error) tea.Cmd {
	if m.playerID == "" {
		return nil
	}
	id, ctx := m.playerID, m.ctx
	return func() tea.Msg { return verbDoneMsg{label: label, text: text, err: f(ctx, id)} }
}

func (m *Model) volCmd(v int) tea.Cmd {
	id, s, ctx := m.playerID, m.suite, m.ctx
	m.volInFlight = true
	return func() tea.Msg { return volDoneMsg{err: s.SetVolume(ctx, id, v)} }
}

func tick() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

// ensureTick keeps one tick chain alive while the banner has something that moves.
func (m *Model) ensureTick() tea.Cmd {
	if m.ticking || !m.moving() {
		return nil
	}
	m.ticking = true
	return tick()
}

func (m *Model) moving() bool {
	return m.playerID != "" && (m.loading || m.playing()) || m.starting || m.busy != "" ||
		!m.undoEnd.IsZero() || m.cover.inFlight != ""
}

// ── update ──────────────────────────────────────────────────────────────────────────────

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.authCmd()}
	switch {
	case m.prompting:
		cmds = append(cmds, textinput.Blink)
	case urlTarget(m.query) != "":
		m.query = urlTarget(m.query)
		cmds = append(cmds, m.loadURL(m.query))
	default:
		cmds = append(cmds, m.fetch(fetchNew, m.query, m.opt.Engine, m.opt.Search,
			m.s.Searching+` "`+m.query+`"`+m.g.Ell))
	}
	// Adoption is a startup verdict with the core's own 0/1/many rule: exactly one live
	// player is unambiguous; several is a notice and an empty banner, so the next Enter is
	// the user's own choice.
	switch n := len(m.opt.AdoptFrom); {
	case n == 1:
		p := m.opt.AdoptFrom[0]
		m.playerID, m.owned = p.ID, false
		m.playURL, m.playEngine = p.URL, p.Engine
		if p.Title != nil {
			m.playTitle = clean(*p.Title)
		} else {
			m.playTitle = p.URL
		}
		m.startedAt = time.Now()
		if p.Position != nil {
			m.startedAt = m.startedAt.Add(-time.Duration(*p.Position) * time.Second)
		}
		cmds = append(cmds, m.watchCmd(p.ID))
	case n > 1:
		m.notice(m.s.AdoptAct+":", m.s.AdoptMany)
	}
	return tea.Batch(append(cmds, m.ensureTick())...)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	return m, tea.Batch(cmd, m.coverCmd(), m.flushCmd())
}

type flushMsg struct{}

// flushCmd writes the changed preferences back a second after the last change: late enough
// that a burst of t or l presses is one rewrite, soon enough that the file holds the choice
// while the session is still open.
func (m *Model) flushCmd() tea.Cmd {
	if m.flushing || len(m.dirty) == 0 || m.opt.Save == nil || m.saveOff {
		return nil
	}
	m.flushing = true
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return flushMsg{} })
}

// SaveFailed is whether a write-back failed this session, which the way out repeats: the
// notice was on a frame that is gone.
func (m *Model) SaveFailed() bool { return m.saveOff }

// Flush writes whatever is still unwritten; the way out calls it last.
func (m *Model) Flush() error {
	prefs := m.Prefs()
	if len(prefs) == 0 || m.opt.Save == nil || m.saveOff {
		return nil
	}
	if err := m.opt.Save(prefs); err != nil {
		m.saveOff = true
		return err
	}
	m.dirty = map[string]bool{}
	return nil
}

// coverCmd starts the focused row's cover when the frame would draw one and it is neither
// here, nor failed, nor already on its way — off the key loop, which is correctness: a
// five-second download in line is a keyboard that does not answer for five seconds.
func (m *Model) coverCmd() tea.Cmd {
	c := &m.cover
	if !c.on || c.inFlight != "" || m.prompting && m.all == nil || len(m.rows) == 0 {
		return nil
	}
	if !m.layout().coverGate {
		return nil
	}
	u := m.rows[m.cursor].Thumb
	if u == "" || c.done[u] != nil || c.failed[u] {
		return nil
	}
	return tea.Batch(c.fetch(m.ctx, u), m.ensureTick())
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case flushMsg:
		m.flushing = false
		if m.Flush() != nil {
			m.notice(m.s.PrefAct+":", m.s.PrefFailed)
		}
		return m, nil

	case coverMsg:
		m.cover.inFlight = ""
		if msg.img != nil {
			m.cover.done[msg.url] = msg.img
		} else {
			m.cover.failed[msg.url] = true
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case authMsg:
		if msg.engine == m.engine().Name {
			m.auth = msg.auth
		}
		return m, nil

	case searchDoneMsg:
		return m, m.searchDone(msg)

	case playDoneMsg:
		m.starting = false
		if msg.err != nil {
			m.notice(m.s.PlayFailed+":", errText(msg.err))
			m.clearPlayer()
			return m, nil
		}
		m.playerID, m.owned, m.last = msg.st.ID, true, nil
		m.loading, m.live, m.startedAt = true, msg.live, time.Now()
		m.playTitle, m.playURL, m.playEngine = msg.title, msg.url, msg.eng
		m.cpu, m.mem, m.volTarget = nil, nil, nil
		return m, tea.Batch(m.watchCmd(msg.st.ID), m.ensureTick())

	case watchStartedMsg:
		if msg.gen != m.watchGen {
			if msg.w != nil {
				msg.w.Close()
			}
			return m, nil
		}
		if msg.err != nil {
			m.notice(m.s.AdoptAct+":", errText(msg.err))
			return m, nil
		}
		m.watcher = msg.w
		return m, nextEvent(msg.gen, msg.w)

	case watchEventMsg:
		return m, m.watchEvent(msg)

	case verbDoneMsg:
		if msg.err != nil && msg.label != "" {
			t := msg.text
			if t == "" {
				t = errText(msg.err)
			}
			m.notice(msg.label, t)
		}
		return m, nil

	case noticeMsg:
		m.notice(msg.label, msg.text)
		return m, nil

	case armMsg:
		m.noteQueue(msg.w)
		m.arm(msg.store, msg.w, msg.head, msg.label)
		return m, m.ensureTick()

	case partsMsg:
		m.pending, m.busy = nil, ""
		m.partsDone(msg)
		return m, nil

	case infoMsg:
		m.pending, m.busy = nil, ""
		m.infoDone(msg)
		return m, nil

	case urlMsg:
		m.pending, m.busy = nil, ""
		m.urlDone(msg)
		return m, nil

	case volDoneMsg:
		m.volInFlight = false
		if m.volQueued && m.volTarget != nil && m.playerID != "" {
			m.volQueued = false
			return m, m.volCmd(*m.volTarget)
		}
		return m, nil

	case tickMsg:
		m.spin++
		if !m.undoEnd.IsZero() && !m.undoActive() {
			m.undoForget()
		}
		m.followChapter()
		if m.moving() {
			return m, tick()
		}
		m.ticking = false
		return m, nil

	case tea.KeyMsg:
		// Keys typed faster than one read — a double-tapped ?, 12j at speed, a held key —
		// arrive as one message carrying every rune. They are keystrokes, not text: a paste
		// comes bracketed. So each rune is its own key.
		if msg.Type == tea.KeyRunes && !msg.Paste && len(msg.Runes) > 1 {
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				_, c := m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: msg.Alt})
				cmds = append(cmds, c)
			}
			return m, tea.Batch(cmds...)
		}
		// A notice is the frame's content until the next key, which clears it.
		m.noticeL, m.noticeT = "", ""
		var cmd tea.Cmd
		switch {
		case m.prompting:
			cmd = m.updatePrompt(msg)
		case m.filterOn:
			cmd = m.updateFilter(msg)
		default:
			cmd = m.updateList(msg)
		}
		return m, tea.Batch(cmd, m.ensureTick())
	}
	return m, nil
}

func (m *Model) searchDone(msg searchDoneMsg) tea.Cmd {
	if m.pending == nil || msg.req.gen != m.pending.gen {
		return nil
	}
	m.pending, m.busy = nil, ""
	req := msg.req
	eng := m.opt.Engines[req.engine].Name
	if msg.err != nil {
		// Under -j an engine says WHY in the envelope and what a person can do about it on
		// stderr; the reason leads and the advice follows. A first search that broke has
		// nothing on screen to steer, so it ends the session with that line.
		text := errText(msg.err)
		var ve *verb.Error
		if errors.As(msg.err, &ve) {
			note := verb.EngineMsg(eng, ve.Stderr)
			switch {
			case ve.Reason != "" && note != "":
				text = "search failed (" + ve.Reason + "): " + note
			case ve.Reason != "":
				text = "search failed (" + ve.Reason + ")"
			case note != "":
				text = note
			}
		}
		if req.kind == fetchNew && m.all == nil {
			m.fatal = errors.New(text)
			return tea.Quit
		}
		m.notice(m.s.SearchAct+":", text)
		return nil
	}
	if msg.res.Note != "" {
		m.notice(m.s.SearchAct+":", msg.res.Note)
	}
	if len(msg.res.Results) == 0 {
		// Nothing matched: a notice, and the rows on screen stay — except on the first
		// search, where there are none to keep and the list is simply empty, with the n / e
		// / b keys that fix it all still there.
		text := "no results"
		if req.kind == fetchNew {
			text = `no results for "` + req.query + `"`
		}
		m.notice(m.s.SearchAct+":", text)
		if m.all == nil {
			m.all, m.rows, m.query = []row{}, []row{}, req.query
		}
		return nil
	}
	rows := rowsFromSearch(msg.res)
	var cmd tea.Cmd
	switch req.kind {
	case fetchMore:
		grew := len(rows) > len(m.all)
		cur := m.cursor
		m.all, m.rows = rows, rows
		m.opt.Search.N = req.opts.N
		m.mark("TING_SEARCH_RESULTS")
		m.cursor = cur
		if grew {
			m.stepOntoNew(cur)
		}
		return nil
	case fetchEngine:
		m.opt.Engine = req.engine
		m.auth = ""
		cmd = m.authCmd()
		m.mark("TING_DEFAULT_ENGINE")
	case fetchSort:
		m.opt.Search.Sort = req.opts.Sort
		m.mark("TING_SORT_FIELD")
	case fetchNew:
		m.query = req.query
	}
	m.all, m.rows = rows, rows
	m.cursor, m.top = 0, 0
	m.filterOn, m.filter = false, ""
	return cmd
}

// stepOntoNew moves onto the first row the fetch brought, the way the page arm steps onto the
// first new page: in scroll mode the row after the cursor, in page mode the next page's top.
func (m *Model) stepOntoNew(cur int) {
	if m.opt.ListMode == "scroll" {
		m.cursor = cur + 1
		return
	}
	ps := m.layout().psize
	m.cursor = (cur/ps + 1) * ps
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
}

func (m *Model) watchEvent(msg watchEventMsg) tea.Cmd {
	if msg.gen != m.watchGen {
		return nil
	}
	if !msg.ok {
		// The stream closed: after `end`, or because --watch itself failed (a player that
		// died before the watch attached answers 4, which is just "it is gone").
		if m.watcher != nil {
			if err := m.watcher.Err(); err != nil && m.last == nil {
				var ve *verb.Error
				if !errors.As(err, &ve) || ve.Kind() != verb.NotEffective {
					m.notice(m.s.AdoptAct+":", errText(err))
				}
			}
			m.watcher = nil
		}
		m.clearPlayer()
		return nil
	}
	ev := msg.ev
	m.last, m.lastAt = &ev, time.Now()
	if ev.Ready {
		m.loading = false
	}
	if ev.CPU != nil {
		m.cpu, m.mem = ev.CPU, ev.Mem
	}
	if ev.Queue != nil {
		m.q = &[2]int{ev.Queue.Pos, ev.Queue.Len}
	}
	if ev.URL != "" && ev.URL != m.playURL {
		// A queue moved on: the banner follows the player, not the row that started it.
		m.playURL, m.startedAt, m.live = ev.URL, time.Now(), false
		if ev.Title == nil {
			m.playTitle = ev.URL
		}
	}
	if ev.Engine != "" {
		m.playEngine = ev.Engine
	}
	if ev.Title != nil {
		m.playTitle = clean(*ev.Title)
	}
	if !m.volInFlight && !m.volQueued {
		m.volTarget = nil
	}
	if ev.Event == "end" && ev.Reason != nil {
		m.notice(m.s.AdoptAct+":", "player ended: "+*ev.Reason)
	}
	return tea.Batch(nextEvent(msg.gen, m.watcher), m.ensureTick())
}

// mark records that a key changed a preference. One the environment pins is never written
// back, and says so once.
func (m *Model) mark(key string) {
	if m.opt.Pinned(key) {
		if !m.said[key] {
			m.said[key] = true
			m.notice(m.s.PrefAct+":", key+" "+m.s.PrefPinned)
		}
		return
	}
	m.dirty[key] = true
}

// Prefs is every preference a key changed this session, with its value now.
func (m *Model) Prefs() []config.Pref {
	val := map[string]string{
		"TING_DEFAULT_ENGINE": m.engine().Name, "TING_THEME": m.opt.Theme,
		"TING_PLAY_MODE": m.opt.Play.Mode, "TING_PLAY_QUALITY": m.opt.Play.Quality,
		"TING_SORT_FIELD": m.opt.Search.Sort, "TING_LANG": m.opt.Lang,
		"TING_SEARCH_RESULTS": strconv.Itoa(m.opt.Search.N), "TING_KEYS": m.opt.Keys,
		"TING_ROW_INDEX": map[bool]string{true: "on", false: "off"}[m.opt.RowIndex],
		"TING_LIST_MODE": m.opt.ListMode, "TING_LOOP_MODE": m.opt.Loop,
	}
	var out []config.Pref
	for _, k := range prefKeys {
		if m.dirty[k] {
			out = append(out, config.Pref{Key: k, Val: val[k]})
		}
	}
	return out
}

// prefKeys is the eleven the keys change, in the order a new file lists them.
var prefKeys = []string{"TING_DEFAULT_ENGINE", "TING_THEME", "TING_PLAY_MODE", "TING_PLAY_QUALITY",
	"TING_SORT_FIELD", "TING_LANG", "TING_SEARCH_RESULTS", "TING_KEYS", "TING_ROW_INDEX",
	"TING_LIST_MODE", "TING_LOOP_MODE"}

// stopNow stops the player on the banner in line: the stops a store write implied, which
// must not be lost to a closing session. A player that already ended (4) is stopped.
func (m *Model) stopNow() {
	if m.playerID == "" {
		return
	}
	err := m.suite.Stop(m.ctx, m.playerID)
	var ve *verb.Error
	if err != nil && !(errors.As(err, &ve) && ve.Kind() == verb.NotEffective) {
		m.notice(m.s.AdoptAct+":", "the player could not be stopped and is still playing — stop it with: ting-play --stop --id "+m.playerID)
		return
	}
	if m.watcher != nil {
		m.watcher.Close()
		m.watcher = nil
	}
	m.watchGen++
	m.clearPlayer()
}

func (m *Model) clearPlayer() {
	m.playerID, m.owned, m.last = "", false, nil
	m.loading, m.live, m.playTitle, m.playURL, m.playEngine = false, false, "", "", ""
	m.cpu, m.mem, m.volTarget, m.volQueued, m.q = nil, nil, nil, false, nil
}

func (m *Model) playing() bool {
	return m.playerID != "" && m.last != nil && m.last.Ready && !m.last.Paused
}

func (m *Model) paused() bool { return m.last != nil && m.last.Paused }

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

// errText is a verb error as one line for the frame.
func errText(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

var pid = os.Getpid()

func timeSince(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return time.Since(t).Seconds()
}
