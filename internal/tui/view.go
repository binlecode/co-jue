package tui

import (
	"fmt"
	"strconv"
	"strings"
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
	sort := map[string]string{"relevance": m.s.SortRelevance, "view_count": m.s.SortViews,
		"duration": m.s.SortDur}[m.opt.Search.Sort]
	it := []string{m.engine().Name, strconv.Itoa(len(m.rows)) + " " + m.s.UResults, sort}
	switch m.auth {
	case "":
	case "anon":
		it = append(it, m.s.AuthAnon)
	case "blocked":
		it = append(it, m.s.AuthBlocked)
	default:
		it = append(it, m.s.AuthIn)
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
		it = append(it, fmt.Sprintf("%s %d/%d", m.s.QAct, q[0], q[1]))
	}
	return it
}

func (m *Model) queue() *[2]int {
	if m.last == nil || m.last.Queue == nil {
		return nil
	}
	return &[2]int{m.last.Queue.Pos, m.last.Queue.Len}
}

// navItems is the key-hint block. Three tiers: core is this view's own job, full is every
// key, hidden prints none and hands its rows back to the list. A key that cannot act here
// takes no cell.
func (m *Model) navItems() []hint {
	if m.opt.Keys == "hidden" {
		return nil
	}
	full := m.opt.Keys == "full"
	it := []hint{{m.g.AV, m.s.Select}}
	if m.opt.ListMode == "page" {
		it = append(it, hint{m.g.AH, m.s.Page})
	}
	it = append(it, hint{m.g.Enter, m.s.Play})
	if full {
		it = append(it, hint{"Nj", m.s.Jump}, hint{"#", m.s.RowNum}, hint{m.g.Tab, m.s.ListMode},
			hint{"v", m.s.Mode}, hint{"f", m.s.Quality}, hint{"n", m.s.Search}, hint{"o", m.s.Sort})
	}
	it = append(it, hint{"/", m.s.Filter})
	if full {
		if len(m.opt.Engines) > 1 {
			it = append(it, hint{"e", m.s.EngineKey})
		}
		it = append(it, hint{"l", m.s.LangKey}, hint{"-/=", m.s.Vol}, hint{"Space", m.s.Pause},
			hint{"[ ]", m.s.Seek}, hint{"r", m.s.Loop}, hint{"s", m.s.Stop})
	}
	it = append(it, hint{"q", m.s.Quit})
	if full && m.playerID != "" {
		it = append(it, hint{"+", m.s.QAdd}, hint{">", m.s.QSkip})
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
	return lead + "  query="
}

// layout spends the terminal's height the way the shell TUI's renderer does: chrome at the
// two ends, the rows take what is left, and when there is not enough the gaps go first.
func (m *Model) layout() frame {
	f := frame{ambig: m.ambigW()}
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
	qRoom := f.rightEdge - m.w.of(m.headLead())
	f.statusInline = qRoom-stW-2 >= layoutMinField
	f.chromeH = 1
	if !f.statusInline {
		f.statusLines = m.statusBlock(f.status, f.cols)
		f.chromeH += len(f.statusLines)
	}
	if m.noticeL != "" || m.noticeT != "" || m.busy != "" {
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
		f.caretH = 1
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
		d := m.detailLines(f.cols, m.cursor)
		if lines-f.chromeH-foot()-layoutCursorRow-len(d) >= layoutDetailKeep {
			f.details = d
		}
	}
	avail := lines - f.chromeH - foot() - len(f.details) - layoutCursorRow
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
			avail = lines - f.chromeH - foot() - len(f.details) - layoutCursorRow
		}
	}
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
// is decoding when this row is the one playing, and up to two lines of description.
func (m *Model) detailLines(cols, i int) []string {
	r := m.rows[i]
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
	if r.Channel != "" {
		meta = r.Channel + sep
	}
	meta += dur
	if views != "" {
		meta += sep + views
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
	if r.Desc != "" {
		d := m.w.trunc(r.Desc, (cols-2)*2-2, m.g.Ell)
		out = append(out, m.w.wrap(d, cols-2, 2)...)
	}
	return out
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

	f := m.layout()

	// Title line, with the status segment right-aligned when it fits.
	lead := brand(m.opt.Lang, m.opt.ASCII)
	if g.Note != "" {
		lead = g.Note + " " + lead
	}
	stPlain := strings.Join(f.status, " "+g.Sep+" ")
	qRoom := f.rightEdge - m.w.of(m.headLead())
	if f.statusInline {
		qRoom -= m.w.of(stPlain) + 2
	}
	q := m.w.trunc("'"+m.query+"'", qRoom, g.Ell)
	head := p.Bold + p.Accent + lead + p.Reset + "  query=" + q
	if f.statusInline {
		gap := f.rightEdge - m.w.of(m.headLead()+q) - m.w.of(stPlain)
		if gap < 1 {
			gap = 1
		}
		head += strings.Repeat(" ", gap) + p.Dim + stPlain + p.Reset
	}
	line(head)
	for _, l := range f.statusLines {
		line(p.Dim + strings.Join(l, " "+g.Sep+" ") + p.Reset)
	}

	// The notice line: a notice until the next key, else the fetch in flight.
	switch {
	case m.noticeL != "" || m.noticeT != "":
		t := m.w.trunc(m.noticeT, f.rightEdge-m.w.of(m.noticeL+" "), g.Ell)
		line(p.Bold + m.noticeL + p.Reset + " " + p.Dim + t + p.Reset)
	case m.busy != "":
		line(p.Dim + g.Spin[m.spin%len(g.Spin)] + " " + m.w.trunc(m.busy, f.rightEdge-2, g.Ell) + p.Reset)
	}

	if m.playerID != "" {
		line(m.banner(f))
	}
	if f.barH > 0 {
		line(m.progressBar(f.cols - 1))
	}
	line("")

	m.renderRows(&b, f)
	if f.rowGap > 0 {
		line("")
	}
	for _, d := range f.details {
		line("  " + p.Dim + d + p.Reset)
	}
	if f.navOK {
		if f.hintGap > 0 {
			line("")
		}
		for _, l := range f.navLines {
			var cells []string
			for _, h := range l {
				c := p.Keycap + p.Bold + h.key + p.Reset
				if h.label != "" {
					c += " " + p.Dim + h.label + p.Reset
				}
				cells = append(cells, c)
			}
			line("  " + strings.Join(cells, keycapSep))
		}
	}
	switch {
	case m.filterOn:
		if f.filterHint {
			line(p.Dim + m.w.trunc(m.s.FilterHint, f.cols, g.Ell) + p.Reset)
		}
		b.WriteString(p.Mark + ">" + p.Reset + " " + m.filter)
	case m.prompting:
		b.WriteString(p.Bold + g.Caret + " " + m.s.NewSearch + ":" + p.Reset + " " + m.input.View())
	case m.jump != "":
		b.WriteString(strings.Repeat(" ", max(0, f.rightEdge-len(m.jump))) + p.Dim + m.jump + p.Reset)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (m *Model) renderRows(b *strings.Builder, f frame) {
	p, g := m.p, m.g
	n := len(m.rows)
	if n == 0 {
		t := m.s.NoMatch + " " + g.Dash + " " + m.s.NoMatchNew + " " + g.Sep + " " + m.s.NoMatchFilt +
			" " + g.Sep + " " + m.s.NoMatchSrc
		b.WriteString(p.Dim + m.w.trunc(t, f.cols, g.Ell) + p.Reset + "\n")
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
	for i := f.start; i < f.end; i++ {
		if m.playingRow(m.rows[i]) {
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
		switch {
		case i == playing:
			b.WriteString(p.RowHL + mark + num + p.Bold + title + fill + rail + p.RowEnd + " " + p.Dim + gut + p.Reset)
		case i == m.cursor:
			b.WriteString(p.Mark + mark + num + p.Reset + p.Bold + title + p.Reset + fill + p.Dim + rail + p.Reset + " " + gut)
		default:
			b.WriteString(mark + num + title + fill + p.Dim + rail + " " + gut + p.Reset)
		}
		b.WriteString("\n")
	}
}

// banner is the Now-Playing line: state, title, and on the right the clock, the footprint,
// and — only when the hint block has no room — the three keys that act on it.
func (m *Model) banner(f frame) string {
	p, g := m.p, m.g
	icon, label, color := g.Play, m.s.Playing, p.Play
	switch {
	case m.starting || m.loading || m.last == nil:
		icon, label, color = g.Spin[m.spin%len(g.Spin)], m.s.Starting, p.Dim+p.Accent
	case m.paused():
		icon, label, color = g.Pause, m.s.Paused, p.PauseC
	}
	head := icon + " " + label + ": "
	tail := ""
	switch {
	case m.live:
		tail = g.TimeL + fmtSec(timeSince(m.startedAt)) + " " + g.Live + g.TimeR
	default:
		cur := "--:--"
		if pos, ok := m.position(); ok {
			cur = fmtSec(pos)
		}
		total := "--:--"
		if m.last != nil && m.last.Duration != nil {
			total = fmtSec(*m.last.Duration)
		}
		tail = g.TimeL + cur + "/" + total + g.TimeR
	}
	res := ""
	if m.opt.Resource && m.cpu != nil && m.mem != nil {
		res = fmt.Sprintf(" %s %s %.0f%% %s %.0fM", g.Sep, g.CPU, *m.cpu, g.RAM, *m.mem)
	}
	hintS := ""
	if !f.navOK && m.opt.Keys != "hidden" {
		hintS = "  [Space " + m.s.Pause + " | s " + m.s.Stop + " | -/= " + m.s.Vol + "]"
	}
	gap := 2
	room := func() int {
		return f.rightEdge - m.w.of(head) - m.w.of(tail) - m.w.of(res) - m.w.of(hintS) - gap
	}
	if room() < layoutMinField {
		hintS = ""
	}
	if room() < layoutMinField {
		res = ""
	}
	if room() < layoutMinBudget {
		tail, gap = "", 0
	}
	title := m.w.trunc(m.playTitle, room(), g.Ell)
	right := tail + res + hintS
	out := color + icon + " " + label + ":" + p.Reset + " " + p.Accent + title + p.Reset
	if right != "" {
		pad := f.rightEdge - m.w.of(head+title) - m.w.of(right)
		if pad < 1 {
			pad = 1
		}
		out += strings.Repeat(" ", pad) + p.Dim + right + p.Reset
	}
	return out
}

func (m *Model) progressBar(width int) string {
	if width < 10 {
		width = 10
	}
	pct := 0
	if pos, ok := m.position(); ok && m.last.Duration != nil && *m.last.Duration > 0 {
		pct = int(pos * 100 / *m.last.Duration)
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct * width / 100
	return m.p.Accent + strings.Repeat(m.g.Fill, filled) + m.p.Reset +
		m.p.Dim + m.p.Accent + strings.Repeat(m.g.Rest, width-filled) + m.p.Reset
}
