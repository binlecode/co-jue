package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/binlecode/ting/internal/tui/layout"
)

// The layout floors, the shell TUI's LAYOUT_* constants: the narrowest frame drawn, the
// narrowest a field may be squeezed to before it is dropped, the floor any budget is clamped
// to, the row kept for the cursor, and the rows the list must keep before the hint block and
// the details block are allowed on screen.
const (
	layoutMin        = 24
	layoutMinField   = 12
	layoutMinBudget  = 8
	layoutCursorRow  = 1
	layoutNavKeep    = 4
	layoutDetailKeep = 2
	keycapSep        = "   "
)

// hint is one key-hint cell: the key and what it does.
type hint struct{ key, label string }

// frame is one frame's layout: every height the budget spends, decided once so the keys that
// page (which need the page size) and the renderer agree on it.
type frame struct {
	cols, rightEdge, ambig int
	status                 []string
	statusInline           bool
	statusLines            [][]string // the status block when it does not fit on the title line
	nav                    []hint
	navLines               [][]hint
	navOK                  bool
	barH                   int
	chromeH                int
	rowGap, hintGap        int
	filterHint             bool
	caretH                 int
	details                []string
	metaH                  int  // number of meta/media lines in details
	lyricRow               int  // -1 when none, else index in details
	lyricInter             bool // whether interlude
	lyricTrans             bool // whether transitioning
	coverGate              bool // this frame draws a cover column (its rows charged either way)
	footH                  int  // the rows under the list: gaps, hint block, filter hint, caret
	psize, start, end      int
}

func (m *Model) ambigW() int {
	if m.opt.AmbigWide {
		return 2
	}
	return 1
}

// statusItems is the title line's right-hand segment. A field at its default takes no cell.
func (m *Model) statusItems() []string {
	var it []string
	if m.src == srcPlaylists {
		// The library's rows are lists, which have no engine of their own.
		it = []string{strconv.Itoa(len(m.rows)) + " " + m.s.PLOpen}
	} else if m.src == srcRemotePlaylists {
		it = []string{m.engine().Name, strconv.Itoa(len(m.rows)) + " " + m.s.RemotePLOpen}
	} else if m.src == srcSearch {
		sort := map[string]string{"relevance": m.s.SortRelevance, "view_count": m.s.SortViews,
			"duration": m.s.SortDur}[m.opt.Search.Sort]
		// The engine of the rows on screen, which a pasted URL can make other than the
		// session's.
		eng := m.engine().Name
		if len(m.all) > 0 {
			eng = engines(m.all)
		}
		it = []string{eng, strconv.Itoa(len(m.rows)) + " " + m.s.UResults, sort}
		switch m.auth {
		case "":
		case "anon":
			it = append(it, m.s.AuthAnon)
		case "blocked":
			it = append(it, m.s.AuthBlocked)
		default:
			it = append(it, m.s.AuthIn)
		}
	} else {
		// A stored list is mixed-source, so it names its engines and no one auth: a single
		// token there would be false for half of it.
		unit := m.s.UItems
		switch m.src {
		case srcChapters:
			unit = m.s.UChap
		case srcParts:
			unit = m.s.PartsKey
		}
		it = []string{engines(m.all), strconv.Itoa(len(m.rows)) + " " + unit}
		if m.src == srcParts && m.totalFmt != "" {
			it = append(it, m.s.Total+" "+m.totalFmt)
		}
	}
	if m.infoFocused() {
		if m.info.uploaded != "" {
			it = append(it, m.info.uploaded)
		}
		if m.info.likes != "" {
			it = append(it, m.s.Likes+" "+m.info.likes)
		}
	}
	if m.opt.Search.MinDur > 0 {
		it = append(it, m.g.GE+strconv.Itoa(m.opt.Search.MinDur)+"s")
	}
	if m.opt.Search.MaxDur > 0 {
		it = append(it, m.g.LE+strconv.Itoa(m.opt.Search.MaxDur)+"s")
	}
	it = append(it, map[string]string{"audio": m.s.ModeAudio, "fast": m.s.ModeFast,
		"video": m.s.ModeVideo}[m.opt.Play.Mode])
	if m.opt.Play.Quality != "auto" {
		it = append(it, m.s.Quality+" "+m.opt.Play.Quality)
	}
	if m.opt.Play.Volume != "" {
		it = append(it, m.s.Vol+" "+m.opt.Play.Volume)
	}
	switch m.opt.Loop {
	case "seq":
		it = append(it, m.s.Loop+" "+m.s.LoopSeq)
	case "one":
		it = append(it, m.s.Loop+" "+m.s.LoopOne)
	}
	if q := m.queue(); q != nil && q[1] > 1 {
		it = append(it, fmt.Sprintf("%s %d/%d", m.s.QAct, q[0]+1, q[1]))
	}
	return it
}

// statusLine renders the status items, secondary, the engine first like any other.
func (m *Model) statusLine(items []string) string {
	return m.p.Secondary + strings.Join(items, " "+m.p.Muted+m.g.Sep+m.p.Secondary+" ") + m.p.Reset
}

// queue is the running player's queue (pos, len), from whichever said it last: an event or
// the envelope of the verb that changed it.
func (m *Model) queue() *[2]int {
	if m.playerID == "" {
		return nil
	}
	return m.q
}

// navItems is the key-hint block. Three tiers: core is this view's own job, full is every
// key, hidden prints none and hands its rows back to the list. A key that cannot act here
// takes no cell.
func (m *Model) navItems() []hint {
	if m.opt.Keys == "hidden" {
		return nil
	}
	full := m.opt.Keys == "full"
	search := m.src == srcSearch
	it := []hint{{m.g.AV, m.s.Select}}
	if m.opt.ListMode == "page" {
		it = append(it, hint{m.g.AH, m.s.Page})
	}
	switch m.src {
	case srcQueue:
		it = append(it, hint{m.g.Enter, m.s.QPlayNow})
	case srcPlaylists, srcRemotePlaylists:
		it = append(it, hint{m.g.Enter, m.s.PLOpenKey})
	default:
		it = append(it, hint{m.g.Enter, m.s.Play})
	}
	if full {
		it = append(it, hint{"Nj", m.s.Jump}, hint{"#", m.s.RowNum}, hint{m.g.Tab, m.s.ListMode},
			hint{"v", m.s.Mode}, hint{"f", m.s.Quality}, hint{"n", m.s.Search})
		if search {
			it = append(it, hint{"o", m.s.Sort})
		}
	}
	it = append(it, hint{"/", m.s.Filter}, hint{"z", m.s.UndoKey})
	if full {
		if search && len(m.opt.Engines) > 1 {
			it = append(it, hint{"e", m.s.EngineKey})
		}
		it = append(it, hint{"l", m.s.LangKey})
		if m.opt.Colors {
			it = append(it, hint{"t", m.s.ThemeKey})
		}
		it = append(it, hint{"-/=", m.s.Vol}, hint{"Space", m.s.Pause},
			hint{"[ ]", m.s.Seek}, hint{"r", m.s.Loop}, hint{"s", m.s.Stop})
	}
	it = append(it, hint{"q", m.s.Quit})
	if m.suite.TPlaylist != "" {
		switch {
		case m.src == srcPlaylist:
			if full {
				it = append(it, hint{"a", m.s.PLAdd}, hint{"d", m.s.PLRmKey}, hint{"D", m.s.PLDelKey}, hint{"R", m.s.PLRenameKey})
			}
			it = append(it, hint{"b", m.s.BackSearch})
		case m.src == srcPlaylists:
			it = append(it, hint{"D", m.s.PLDelKey})
			if full {
				it = append(it, hint{"R", m.s.PLRenameKey})
			}
			it = append(it, hint{"b", m.s.BackSearch})
		case m.src == srcContainer:
			if full {
				it = append(it, hint{"a", m.s.PLAdd})
			}
			it = append(it, hint{"b", m.s.BackSearch})
		case m.src == srcRemotePlaylists:
			if full {
				it = append(it, hint{"b", m.s.PLOpen})
			}
		case full:
			it = append(it, hint{"a", m.s.PLAdd}, hint{"b", m.s.PLOpen})
		}
	}
	if m.src == srcRemotePlaylists {
		it = append(it, hint{"B", m.s.BackSearch})
	} else if full && search && m.engine().Has("--playlists") {
		it = append(it, hint{"B", m.s.RemotePLOpen})
	}
	if m.suite.THistory != "" {
		if m.src == srcHistory {
			it = append(it, hint{"h", m.s.BackSearch})
		} else if full {
			it = append(it, hint{"h", m.s.HistKey})
		}
	}
	focusEng := ""
	if len(m.rows) > 0 && m.cursor < len(m.rows) {
		focusEng = m.rows[m.cursor].Engine
	}
	if m.src == srcRelated {
		it = append(it, hint{"g", m.s.BackSearch})
	} else if full && search && m.engineHas(focusEng, "--related") {
		it = append(it, hint{"g", m.s.RelatedKey})
	}
	if m.src == srcParts {
		it = append(it, hint{"c", m.s.BackSearch})
	} else if full && search && m.engineHas(focusEng, "--items") {
		it = append(it, hint{"c", m.s.PartsKey})
	}
	if m.src == srcChapters {
		it = append(it, hint{"i", m.s.BackSearch})
	} else if full && search && m.engineHas(focusEng, "--info") {
		it = append(it, hint{"i", m.s.ChapKey})
	}
	if full && m.playerID != "" {
		it = append(it, hint{"+", m.s.QAdd}, hint{">", m.s.QSkip})
	}
	if m.src == srcQueue {
		it = append(it, hint{"x", m.s.QRmKey}, hint{"pP", m.s.QMvKey})
		if full {
			it = append(it, hint{"X", m.s.QClearKey})
		}
		it = append(it, hint{"u", m.s.BackSearch})
	} else if q := m.queue(); full && m.playerID != "" && q != nil && q[1] > 1 {
		it = append(it, hint{"u", m.s.QKey})
	}
	return append(it, hint{"?", m.s.Keys})
}

// wrapHints packs cells into lines of at most width, each line starting after indent.
func (m *Model) wrapHints(items []hint, width int, indent, sep string) [][]hint {
	var lines [][]hint
	var cur []hint
	used := m.w.of(indent)
	for _, h := range items {
		iw := m.w.of(h.key)
		if h.label != "" {
			iw += 1 + m.w.of(h.label)
		}
		if len(cur) > 0 && used+m.w.of(sep)+iw > width {
			lines = append(lines, cur)
			cur, used = nil, m.w.of(indent)
		}
		if len(cur) > 0 {
			used += m.w.of(sep)
		}
		cur = append(cur, h)
		used += iw
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

func (m *Model) headLead() string {
	lead := brand(m.opt.Lang, m.opt.ASCII)
	if m.g.Note != "" {
		lead = m.g.Note + " " + lead
	}
	return lead + "  " + m.src.field() + "="
}

// layout spends the terminal's height the way the shell TUI's renderer does: chrome at the
// two ends, the rows take what is left, and when there is not enough the gaps go first.
func (m *Model) layout() frame {
	m.geom = layout.Compute(m.width, m.height)
	m.dock.SetBounds(m.geom.Dock)
	m.navbar.SetBounds(m.geom.Navbar)
	m.stage.SetBounds(m.geom.Stage)
	m.inspector.SetBounds(m.geom.Inspector)

	f := frame{ambig: m.ambigW(), lyricRow: -1}
	f.cols = m.width
	if f.cols < layoutMin {
		f.cols = layoutMin
	}
	lines := m.height
	f.rightEdge = f.cols - f.ambig - 1

	f.status = m.statusItems()
	// The page readout needs the page size, which is what this function computes, so it is
	// read off the previous frame's — as the shell TUI does. A resize or a mode switch is off
	// by one frame at most, and the next key redraws it right.
	if n, ps := len(m.rows), m.psize; m.opt.ListMode == "page" && ps > 0 && n > ps {
		f.status = append(f.status, fmt.Sprintf("%s %d/%d", m.s.UPage, m.cursor/ps+1, (n+ps-1)/ps))
	}
	stW := m.w.of(strings.Join(f.status, " "+m.g.Sep+" "))
	tabsW := 0
	if f.cols >= 96 {
		tabsW = m.w.of(m.navbar.PlainHeaderTabs(m.s)) + 2
	}
	qRoom := f.rightEdge - m.w.of(m.headLead()) - tabsW
	f.statusInline = qRoom-stW-2 >= layoutMinField
	f.chromeH = 1
	if !f.statusInline {
		f.statusLines = m.statusBlock(f.status, f.cols)
		f.chromeH += len(f.statusLines)
	}
	if m.noticeL != "" || m.noticeT != "" || m.busy != "" || m.undoActive() {
		f.chromeH++
	}

	f.nav = m.navItems()
	if len(f.nav) > 0 {
		f.navLines = m.wrapHints(f.nav, f.cols, "  ", keycapSep)
	}
	navNeed := layoutNavKeep
	if len(m.rows) > 1 {
		navNeed++
	}
	if m.playerID != "" && !m.live {
		f.barH = 1
	}
	f.navOK = len(f.nav) > 0 && lines-f.chromeH-1-f.barH-len(f.navLines) >= navNeed
	if m.playerID != "" {
		f.chromeH++
	}
	f.chromeH += f.barH + 1 // the bar, and the blank line under the chrome

	f.rowGap = 1
	if m.filterOn {
		f.filterHint = true
		f.caretH = (m.w.of("> "+m.filter) + f.cols - 1) / f.cols
		if f.caretH < 1 {
			f.caretH = 1
		}
	}
	if m.prompting {
		f.caretH = 1 + m.pickLines()
	}
	if f.navOK {
		f.hintGap = 1
	}
	foot := func() int {
		n := f.rowGap + f.hintGap + f.caretH
		if f.navOK {
			n += len(f.navLines)
		}
		if f.filterHint {
			n++
		}
		return n
	}
	m.clampCursor()
	if len(m.rows) > 0 {
		// The gate asks how many rows the list keeps and how many columns the text keeps,
		// not how big the terminal is. With it open the block is the box's height whether or
		// not this row has a cover, so moving across rows never reflows the list.
		dw := f.cols
		if m.cover.on && f.rightEdge-coverCols-1 >= coverMinText &&
			lines-f.chromeH-foot()-layoutCursorRow-coverRows >= coverMinRows {
			f.coverGate, dw = true, f.rightEdge-coverCols-1
		}
		d, metaH, lyricIdx, isInter, isTrans := m.detailLines(dw, m.cursor)
		if f.coverGate {
			for len(d) < coverRows {
				d = append(d, "")
			}
		}
		if lines-f.chromeH-foot()-layoutCursorRow-len(d) >= layoutDetailKeep {
			f.details = d
			f.metaH = metaH
			f.lyricRow = lyricIdx
			f.lyricInter = isInter
			f.lyricTrans = isTrans
		} else {
			f.coverGate = false
			f.lyricRow = -1
		}
	}
	detH := len(f.details)
	if m.geom.Mode == layout.Wide {
		// In wide mode details live in the right inspector column, not under the stage rows.
		detH = 0
	}
	avail := lines - f.chromeH - foot() - detH - layoutCursorRow
	for avail < 1 {
		switch {
		case f.rowGap > 0:
			f.rowGap = 0
		case f.hintGap > 0:
			f.hintGap = 0
		case f.filterHint:
			f.filterHint = false
		default:
			avail = 1
		}
		if avail < 1 {
			avail = lines - f.chromeH - foot() - detH - layoutCursorRow
		}
	}
	f.footH = foot()
	f.psize = avail
	if m.opt.ListMode == "page" && m.opt.PageRows < f.psize {
		f.psize = m.opt.PageRows
	}
	if f.psize < 1 {
		f.psize = 1
	}
	n := len(m.rows)
	if m.opt.ListMode == "scroll" {
		vmax := n - f.psize
		if vmax < 0 {
			vmax = 0
		}
		if m.top > vmax {
			m.top = vmax
		}
		if m.cursor < m.top {
			m.top = m.cursor
		}
		if m.cursor > m.top+f.psize-1 {
			m.top = m.cursor - f.psize + 1
		}
		if m.top < 0 {
			m.top = 0
		}
		f.start = m.top
	} else {
		f.start = m.cursor / f.psize * f.psize
	}
	f.end = f.start + f.psize
	if f.end > n {
		f.end = n
	}
	m.psize = f.psize
	return f
}

func (m *Model) statusBlock(items []string, cols int) [][]string {
	hs := make([]hint, len(items))
	for i, s := range items {
		hs[i] = hint{key: s}
	}
	var out [][]string
	for _, l := range m.wrapHints(hs, cols, "", " "+m.g.Sep+" ") {
		var line []string
		for _, h := range l {
			line = append(line, h.key)
		}
		out = append(out, line)
	}
	return out
}

func (m *Model) clampCursor() {
	if m.cursor > len(m.rows)-1 {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// playingRow is the row the player is on, by engine and handle, or -1.
func (m *Model) playingRow(r row) bool {
	return m.playerID != "" && m.playURL != "" && r.Engine == m.playEngine &&
		handleKey(r.URL) == handleKey(m.playURL)
}

// detailLines is the focused row's details block, plain text: the meta line, what the player
// is decoding when this row is the one playing, and up to two lines of description (or
// the active synchronized lyric line when peeking on the playing row).
func (m *Model) detailLines(cols, i int) ([]string, int, int, bool, bool) {
	r := m.rows[i]
	if m.src == srcPlaylists || m.src == srcRemotePlaylists {
		// A list's details are its count and when it last changed, already on Channel.
		return []string{m.w.trunc(r.Channel, cols-2, m.g.Ell)}, 1, -1, false, false
	}
	var dur, views string
	if r.isLive() {
		dur, views = m.g.Live, "live now"
	} else {
		dur = shortDur(r.Duration)
		if r.Views != nil {
			views = commas(*r.Views) + " views"
		}
	}
	sep := " " + m.g.Sep + " "
	meta := ""
	if r.N > 0 && m.total > 0 {
		switch m.src {
		case srcChapters:
			meta = fmt.Sprintf("%s %d/%d%s", m.s.ChapKey, r.N, m.total, sep)
		case srcParts:
			meta = fmt.Sprintf("%s %d/%d%s", m.s.PartsKey, r.N, m.total, sep)
		}
	}
	if r.Channel != "" {
		meta += r.Channel + sep
	}
	meta += dur
	if views != "" {
		meta += sep + views
	}
	switch r.Access {
	case "preview":
		meta += sep + "30s"
	case "paywalled":
		meta += sep + "VIP"
	}
	if r.ID != "" {
		meta += sep + r.ID
	}
	out := []string{m.w.trunc(meta, cols-2, m.g.Ell)}
	if m.playingRow(r) && m.last != nil {
		if ml := mediaLine(m.last.Media, m.g.Sep); ml != "" {
			out = append(out, m.w.trunc(ml, cols-2, m.g.Ell))
		}
	}
	metaH := len(out)
	lyricIdx := -1
	isInter, isTrans := false, false
	if m.playingRow(r) {
		if l, inter, trans, ok := m.lyricLine(cols); ok {
			lyricIdx = len(out)
			isInter, isTrans = inter, trans
			out = append(out, l)
		}
	}
	if lyricIdx < 0 && r.Desc != "" {
		d := m.w.trunc(r.Desc, (cols-2)*2-2, m.g.Ell)
		out = append(out, m.w.wrap(d, cols-2, 2)...)
	}
	return out, metaH, lyricIdx, isInter, isTrans
}

// ── render ──────────────────────────────────────────────────────────────────────────────

func (m *Model) View() string {
	p, g := m.p, m.g
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }

	if m.prompting && m.all == nil {
		// The first prompt: nothing to draw yet but the question, which names the engine and
		// not a site.
		b.WriteString(g.Caret + " " + m.s.PromptSearch + " " + m.engine().Name + ": " + m.input.View())
		return b.String()
	}

	if m.all == nil {
		// Before the first search lands there is no list to draw — just what is happening.
		// The title line is the ready marker (query='…'), so it must not appear early.
		return m.p.Secondary + m.g.Spin[m.spin%len(m.g.Spin)] + " " + m.busy + m.p.Reset
	}

	if m.stageMode {
		v := m.stageModeView()
		if m.cover.on {
			v += kittyDel(false)
		}
		return v
	}

	// The page readout reads the page size off the previous frame; the first frame, or one
	// after a resize, lays out twice so its own count is right.
	ps := m.psize
	f := m.layout()
	if m.psize != ps {
		f = m.layout()
	}
	m.updateInspector()

	// Title line, with the status segment right-aligned when it fits.
	lead := brand(m.opt.Lang, m.opt.ASCII)
	if g.Note != "" {
		lead = g.Note + " " + lead
	}
	tabs := ""
	tabsW := 0
	if f.cols >= 96 {
		tabs = m.navbar.HeaderTabs(p, m.s) + "  "
		tabsW = m.w.of(m.navbar.PlainHeaderTabs(m.s)) + 2
	}
	stPlain := strings.Join(f.status, " "+g.Sep+" ")
	qRoom := f.rightEdge - m.w.of(m.headLead()) - tabsW
	if f.statusInline {
		qRoom -= m.w.of(stPlain) + 2
	}
	label := m.query
	if m.src != srcSearch {
		label = m.label
	}
	q := m.w.trunc("'"+label+"'", qRoom, g.Ell)
	shown := lead
	if m.opt.Lang == "zh" && p.on && !m.opt.ASCII && m.playing() {
		// The zh wordmark lights while sound is coming out: its character on the playing
		// row's ground.
		shown = strings.Replace(lead, "【 听 】", "【"+p.RowEnd+p.Bold+p.Accent+p.RowHL+" 听 "+p.RowEnd+p.Bold+p.Accent+"】", 1)
	}
	head := p.Bold + p.Accent + shown + p.Reset + "  " + tabs + m.src.field() + "=" + q
	if f.statusInline {
		gap := f.rightEdge - m.w.of(m.headLead()+q) - tabsW - m.w.of(stPlain)
		if gap < 1 {
			gap = 1
		}
		head += strings.Repeat(" ", gap) + m.statusLine(f.status)
	}
	line(head)
	for _, l := range f.statusLines {
		line(m.statusLine(l))
	}

	// The notice line: a notice until the next key, else the fetch in flight. While an undo
	// is on offer it says so, for exactly as long as it can.
	nl, nt, nu := m.noticeL, m.noticeT, ""
	if m.undoActive() {
		nu = " " + g.Sep + " " + m.s.UndoHint
		if nl == "" && nt == "" {
			nl, nt = m.undoHead, m.undoLabel
		}
	}
	switch {
	case nl != "" || nt != "":
		t := m.w.trunc(nt, f.rightEdge-m.w.of(nl+" ")-m.w.of(nu), g.Ell)
		tail := ""
		if nu != "" {
			tail = p.Muted + nu + p.Reset
		}
		line(p.Bold + nl + p.Reset + " " + p.Secondary + t + p.Reset + tail)
	case m.busy != "":
		b := m.busy
		if m.held {
			b += " " + g.Sep + " " + m.s.BusyHeld
		}
		line(p.Secondary + g.Spin[m.spin%len(g.Spin)] + " " + m.w.trunc(b, f.rightEdge-2, g.Ell) + p.Reset)
	}

	if dockStr := m.dock.View(m, f, m.playerClock()); dockStr != "" {
		for _, dl := range strings.Split(dockStr, "\n") {
			line(dl)
		}
	}
	line("")

	if m.geom.Mode == layout.Wide && m.geom.Stage.W > 0 {
		// The columns get what the chrome and the foot leave, counted on this frame: the
		// geometry's Stage.H assumes a one-line header and no foot, and a block taller than
		// the terminal scrolls the title line off the top. The jump readout is one more line.
		contentH := m.height - f.chromeH - f.footH
		if m.jump != "" {
			contentH--
		}
		if contentH < 1 {
			contentH = 1
		}
		m.inspector.bounds.Y, m.inspector.bounds.H = f.chromeH, contentH
		var stageB strings.Builder
		stageF := f
		stageF.cols = m.geom.Stage.W
		stageF.rightEdge = m.geom.Stage.W - f.ambig - 1
		m.renderRows(&stageB, stageF)
		inspStr := m.inspector.View(m.p, m.s, m.g, m.w, m.cover.on)
		colWidths := []int{m.geom.Stage.W, m.geom.Inspector.W}
		joined := layout.JoinColumns([]string{strings.TrimSuffix(stageB.String(), "\n"), inspStr}, colWidths, contentH, layout.GutterWidth, m.w.of)
		// The cover rides on the block's first line, after the join: its bytes are not text
		// and must not be measured as a column's width.
		b.WriteString(m.coverEscape(f))
		for _, l := range strings.Split(joined, "\n") {
			line(l)
		}
	} else {
		m.renderRows(&b, f)
		img := m.coverEscape(f)
		if f.rowGap > 0 {
			line(img)
			img = ""
		}
		for i, d := range f.details {
			var l string
			switch {
			case i == f.lyricRow:
				style := p.Accent
				if f.lyricInter {
					style = p.Muted
				} else if f.lyricTrans && p.on && !p.mono {
					style = p.Bold + p.Accent
				}
				l = img + "  " + style + d + p.Reset
			case i < f.metaH:
				l = img + "  " + p.Secondary + d + p.Reset
			default:
				l = img + "  " + p.Muted + d + p.Reset
			}
			img = ""
			if f.coverGate && i == coverRows/2 && m.coverLoading() {
				t := g.Spin[m.spin%len(g.Spin)] + " loading pic..."
				pad := f.rightEdge - coverCols + 1 + (coverCols-m.w.of(t))/2 - 2 - m.w.of(d)
				l += strings.Repeat(" ", max(1, pad)) + p.Muted + t + p.Reset
			}
			line(l)
		}
		if img != "" {
			line(img)
		}
	}
	if f.navOK {
		if f.hintGap > 0 {
			line("")
		}
		for _, l := range f.navLines {
			var cells []string
			for _, h := range l {
				keyStyle := ""
				if p.mono {
					keyStyle = p.Bold
				}
				c := p.Keycap + keyStyle + h.key + p.Reset
				if h.label != "" {
					c += " " + p.Muted + h.label + p.Reset
				}
				cells = append(cells, c)
			}
			line("  " + strings.Join(cells, keycapSep))
		}
	}
	switch {
	case m.filterOn:
		if f.filterHint {
			line(p.Muted + m.w.trunc(m.s.FilterHint, f.cols, g.Ell) + p.Reset)
		}
		b.WriteString(p.Mark + ">" + p.Reset + " " + m.filter)
	case m.prompting:
		m.renderPick(&b)
		label := m.askLabel
		if m.askKind == askSearch {
			label = m.s.NewSearch
		}
		b.WriteString(p.Bold + g.Caret + " " + label + ":" + p.Reset + " " + m.input.View())
	case m.jump != "":
		b.WriteString(strings.Repeat(" ", max(0, f.rightEdge-len(m.jump))) + p.Muted + m.jump + p.Reset)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (m *Model) renderRows(b *strings.Builder, f frame) {
	p, g := m.p, m.g
	n := len(m.rows)
	if n == 0 {
		t := m.s.NoMatch + " " + g.Dash + " " + m.s.NoMatchNew + " " + g.Sep + " " + m.s.NoMatchFilt +
			" " + g.Sep + " " + m.s.NoMatchSrc
		b.WriteString(p.Muted + m.w.trunc(t, f.cols, g.Ell) + p.Reset + "\n")
		return
	}
	markW := m.w.of(g.Cursor) + 1
	markOn := g.Cursor + strings.Repeat(" ", markW-m.w.of(g.Cursor))
	markOff := strings.Repeat(" ", markW)
	iw := len(strconv.Itoa(n)) + 1 + markW
	pfxW := markW
	if m.opt.RowIndex {
		pfxW = iw + 1
	}
	vis := f.end - f.start
	thLen, thTop := 0, 0
	if n > vis && vis > 0 {
		thLen = (vis*vis + n/2) / n
		if thLen < 1 {
			thLen = 1
		}
		thTop = (vis*f.start + n/2) / n
		if thTop > vis-thLen {
			thTop = vis - thLen
		}
		if thTop < 0 {
			thTop = 0
		}
	}
	playing := -1
	chap := m.playingChapter()
	for i := f.start; i < f.end; i++ {
		r := m.rows[i]
		var on bool
		switch m.src {
		case srcQueue:
			on = m.playerID != "" && r.N == m.queuePos()
		case srcChapters:
			on = chap >= 0 && r.Sec == chap
		default:
			on = m.playingRow(r)
		}
		if on {
			playing = i
			break
		}
	}
	for i := f.start; i < f.end; i++ {
		r := m.rows[i]
		rail := r.rail()
		railW := m.w.of(rail)
		rowW := f.rightEdge - pfxW - 2 - railW
		if rowW < layoutMinBudget {
			rowW = layoutMinBudget
		}
		title := m.w.trunc(r.Title, rowW, g.Ell)
		mark := markOff
		if i == m.cursor {
			mark = markOn
		}
		num := ""
		if m.opt.RowIndex {
			num = fmt.Sprintf("%*s ", iw-markW, strconv.Itoa(i+1)+".")
		}
		gut := g.Track
		if thLen > 0 && i-f.start >= thTop && i-f.start < thTop+thLen {
			gut = g.Thumb
		}
		// The rail ends at the right edge, then one blank, then the scrollbar.
		fill := strings.Repeat(" ", max(1, f.rightEdge-railW-m.w.of(mark+num+title)))
		railText := p.Secondary + rail + p.Reset
		gutText := p.Muted + gut + p.Reset
		switch {
		case i == playing:
			b.WriteString(p.RowHL + mark + num + p.Bold + title + fill + rail + p.RowEnd + " " + gutText)
		case i == m.cursor:
			b.WriteString(p.Mark + mark + num + p.Reset + p.Bold + title + p.Reset + fill + railText + " " + gutText)
		default:
			b.WriteString(mark + p.Muted + num + p.Reset + title + fill + railText + " " + gutText)
		}
		b.WriteString("\n")
	}
}

// banner is the Now-Playing line: state, title, and on the right the clock, the footprint,
// and — only when the hint block has no room — the three keys that act on it.
func (m *Model) banner(f frame) string {
	return m.dock.Banner(m, f, m.playerClock())
}

func (m *Model) progressBar(width int) string {
	return m.dock.ProgressBar(m, width, m.playerClock())
}

// barCells is how much of a width-cell bar frac covers: whole cells, then the steps (of
// `steps` per cell) into the next one. With one step per cell there is no partial cell,
// which is the ASCII bar; a full bar has no partial cell either, so it never runs past width.
func barCells(frac float64, width, steps int) (int, int) {
	n := int(min(max(frac, 0), 1) * float64(width*steps))
	return n / steps, n % steps
}

// chapterSpan is the focused chapter's start and end, when the list is that item's chapters
// and the item is what is playing; zeros otherwise.
func (m *Model) chapterSpan() (float64, float64) {
	if m.src != srcChapters || m.info == nil || m.live || m.cursor >= len(m.rows) {
		return 0, 0
	}
	r := m.rows[m.cursor]
	if r.Engine != m.playEngine || handleKey(r.URL) != handleKey(m.playURL) {
		return 0, 0
	}
	for _, c := range m.info.chapters {
		if int(c.Start) == r.Sec && c.End != nil {
			return c.Start, *c.End
		}
	}
	return 0, 0
}

const pickMaxVisible = 8

// pickLines is the height of the playlist picker drawn above a/b's prompt.
func (m *Model) pickLines() int {
	if len(m.pick) == 0 {
		return 0
	}
	return min(len(m.pick), pickMaxVisible) + 1
}

// renderPick is the picker: a numbered list, so an answer is one keystroke rather than a
// recollection, with the names in a column measured in cells (a CJK name does not line up
// in a column counted in bytes). A narrow pane drops the date first, then cuts the names.
func (m *Model) renderPick(b *strings.Builder) {
	n := len(m.pick)
	if n == 0 {
		return
	}
	cols := max(m.width, layoutMin)
	numW := len(strconv.Itoa(n))
	metas := make([]string, n)
	nameW, metaW := 0, 0
	for i, pl := range m.pick {
		unit := m.s.PLItems
		if pl.Count == 1 {
			unit = m.s.PLItem
		}
		metas[i] = fmt.Sprintf("%d %s", pl.Count, unit)
		if cols >= 46 && len(pl.UpdatedAt) >= 10 {
			metas[i] += "  " + pl.UpdatedAt[:10]
		}
		nameW = max(nameW, m.w.of(pl.Name))
		metaW = max(metaW, m.w.of(metas[i]))
	}
	nameW = min(nameW, max(cols-3-numW-1-2-metaW, layoutMinBudget))

	if n > pickMaxVisible {
		b.WriteString(fmt.Sprintf("  %s%s%s  %s(%d/%d)%s\n", m.p.Bold, m.askHead, m.p.Reset, m.p.Muted, m.pickCursor+1, n, m.p.Reset))
	} else {
		b.WriteString("  " + m.p.Bold + m.askHead + m.p.Reset + "\n")
	}

	start := 0
	end := n
	if n > pickMaxVisible {
		start = m.pickCursor - pickMaxVisible/2
		if start < 0 {
			start = 0
		}
		if start+pickMaxVisible > n {
			start = n - pickMaxVisible
		}
		end = start + pickMaxVisible
	}

	for i := start; i < end; i++ {
		pl := m.pick[i]
		prefix := "  "
		highlight := ""
		reset := ""
		if i == m.pickCursor {
			prefix = m.p.Accent + m.g.Arrow + " " + m.p.Reset
			highlight = m.p.Bold
			reset = m.p.Reset
		}
		b.WriteString(fmt.Sprintf("%s%*d. %s%s%s  %s\n", prefix, numW, i+1, highlight, m.w.pad(m.w.trunc(pl.Name, nameW, m.g.Ell), nameW), reset, metas[i]))
	}
}

// coverEscape is this frame's cover bytes: the focused row's image, placed flush with the
// right edge at the first details row (the cursor jumps there and back), or a delete.
func (m *Model) coverEscape(f frame) string {
	if !m.cover.on {
		return ""
	}
	if m.stageMode {
		return kittyDel(false)
	}
	if m.geom.Mode == layout.Wide && !m.geom.Inspector.Empty() {
		// Placed by absolute position, so the cursor the renderer is tracking is saved
		// around it and put back.
		if row, col, _, _, ok := m.inspector.CoverBox(); ok {
			if img := m.cover.done[m.inspector.thumbURL]; img != nil {
				return kittyDel(false) + "\x1b7" + fmt.Sprintf("\x1b[%d;%dH", row, col) + kittyPut(img.b64) + "\x1b8"
			}
		}
		return kittyDel(false)
	}
	if f.coverGate && len(m.rows) > 0 && m.cursor < len(m.rows) {
		if img := m.cover.done[m.rows[m.cursor].Thumb]; img != nil {
			col := max(1, f.rightEdge-img.cols+1)
			down := ""
			if f.rowGap > 0 {
				down = "\x1b[1B"
			}
			up := ""
			if down != "" {
				up = "\x1b[1A"
			}
			return kittyDel(false) + down + fmt.Sprintf("\x1b[%dG", col) + kittyPut(img.b64) + up + "\x1b[1G"
		}
	}
	return kittyDel(false)
}

func (m *Model) updateInspector() {
	title := m.playTitle
	artist := ""
	album := ""
	spec := m.dock.MediaSpecs(m, m.playerClock())
	thumb := ""

	if m.playerID != "" {
		lyr := m.currentLyric
		if lyr == nil && m.playURL != "" {
			lyr = m.lyricsCache[m.playURL]
		}

		if lyr != nil && lyr.status == lyricReady && len(lyr.segments) > 0 {
			clock := m.playerClock()
			idx, inter := m.activeLyricIndex(clock.Pos)
			m.inspector.UpdateLyrics(lyr.segments, idx, inter, false)
		} else if (lyr != nil && lyr.status == lyricLoading) || (m.lyricInFlight != "" && m.lyricInFlight == m.playURL) {
			m.inspector.UpdateLyrics(nil, -1, false, true)
		} else {
			m.inspector.UpdateLyrics(nil, -1, false, false)
		}
		if len(m.rows) > 0 && m.cursor < len(m.rows) {
			thumb = m.rows[m.cursor].Thumb
			artist = m.rows[m.cursor].Engine
		}
	} else if len(m.rows) > 0 && m.cursor < len(m.rows) {
		r := m.rows[m.cursor]
		title = r.Title
		thumb = r.Thumb
		artist = r.Engine
		m.inspector.UpdateLyrics(nil, -1, false, false)
	}
	m.inspector.UpdateTrack(title, album, artist, spec, thumb)
}

func (m *Model) coverLoading() bool {
	if len(m.rows) == 0 || m.cursor >= len(m.rows) {
		return false
	}
	u := m.rows[m.cursor].Thumb
	return u != "" && m.cover.done[u] == nil && !m.cover.missed(u)
}
