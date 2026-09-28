package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/binlecode/ting/internal/verb"
	tea "github.com/charmbracelet/bubbletea"
)

// source is where the rows on screen came from. One renderer draws them all; every source but
// search is left by the key that entered it.
type source int

const (
	srcSearch source = iota
	srcPlaylist
	srcContainer // a pasted container URL: read-only, left by b like a playlist
	srcHistory
	srcParts
	srcChapters
	srcQueue
	srcRelated
)

// field is the title line's name for the source's label ("query='…'").
func (s source) field() string {
	return [...]string{"query", "playlist", "playlist", "history", "parts", "chapters", "queue", "related"}[s]
}

// stash is the one search the stored views return to. Going back replays it, never the
// query: a second run of the same query need not give the same rows.
type stash struct {
	all         []row
	query       string
	cursor, top int
}

// info is the one --info on hand: the focused row's upload date and likes on the status
// line, and its chapters.
type info struct {
	engine, url, title, uploaded, likes string
	chapters                            []verb.Chapter
}

// openRows puts a stored source on screen, stashing the search first if that is what is
// being left. Only a search is ever stashed, so the way back always lands on results.
func (m *Model) openRows(src source, label string, rows []row) {
	if m.src == srcSearch {
		m.stash = stash{all: m.all, query: m.query, cursor: m.cursor, top: m.top}
	}
	m.src, m.label = src, label
	m.all, m.rows = rows, rows
	m.cursor, m.top = 0, 0
	m.filterOn, m.filter = false, ""
}

func (m *Model) backToSearch() {
	if m.src == srcSearch {
		return
	}
	st := m.stash
	m.src, m.label, m.plName, m.total, m.totalFmt = srcSearch, "", "", 0, ""
	m.all, m.rows, m.query, m.cursor, m.top = st.all, st.all, st.query, st.cursor, st.top
	if m.all == nil {
		m.all, m.rows = []row{}, []row{}
	}
	m.filterOn, m.filter = false, ""
}

func (m *Model) searchOnly() bool {
	if m.src == srcSearch {
		return true
	}
	m.notice(m.s.PLAct+":", m.s.PLSearchOnly)
	return false
}

// storeMsg is the last line of a store's own error, less its name prefix.
func storeMsg(err error, fallback string) string {
	var ve *verb.Error
	if errors.As(err, &ve) {
		msg := strings.TrimSpace(ve.Stderr)
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		for _, p := range []string{"ting-playlist: ", "ting-history: ", "ting-play: ", "Error: "} {
			msg = strings.TrimPrefix(msg, p)
		}
		if msg != "" {
			return msg
		}
		if ve.Reason != "" {
			return ve.Reason
		}
	}
	return fallback
}

func reason(err error) string {
	var ve *verb.Error
	if errors.As(err, &ve) {
		return ve.Reason
	}
	return ""
}

// The two stores and the queue are local files behind a lock: their verbs answer in
// milliseconds, so they run in line. Only the engine verbs (network) are commands.
func (m *Model) call() context.Context { return m.ctx }

// ── playlists ───────────────────────────────────────────────────────────────────────────

func (m *Model) haveStore() bool {
	if m.suite.TPlaylist != "" {
		return true
	}
	m.notice(m.s.PLAct+":", m.s.PLAbsent)
	return false
}

// openPlaylist shows a stored playlist; false (with its notice) when it cannot.
func (m *Model) openPlaylist(name string) bool {
	l, err := m.suite.PlaylistShow(m.call(), name)
	if err != nil {
		m.notice(m.s.PLAct+":", storeMsg(err, m.s.Failed))
		return false
	}
	if l.Count == 0 {
		m.notice(m.s.PLAct+":", m.s.PLEmpty)
		return false
	}
	m.openRows(srcPlaylist, name, rowsFromItems(l, false))
	m.plName = name
	return true
}

// reloadPlaylist re-reads the list on screen and keeps the cursor where it was; a list that
// is gone (or empty now) returns to the results.
func (m *Model) reloadPlaylist(name string) {
	keep := m.cursor
	if !m.openPlaylist(name) {
		m.backToSearch()
		return
	}
	m.cursor = keep
	m.clampCursor()
}

// focusedIndex is the store's index of the focused row, found by url and occurrence (a
// playlist may hold one url twice), never by the cursor: a filtered view would delete the
// wrong row. -1 when the screen and the store disagree.
func (m *Model) focusedIndex() int {
	l, err := m.suite.PlaylistShow(m.call(), m.plName)
	if err != nil || m.cursor >= len(m.rows) {
		return -1
	}
	u, k := m.rows[m.cursor].URL, 0
	for i := 0; i < m.cursor; i++ {
		if m.rows[i].URL == u {
			k++
		}
	}
	for i, it := range l.Items {
		if it.URL == u {
			if k == 0 {
				return i
			}
			k--
		}
	}
	return -1
}

func (m *Model) item(r row) verb.QueueItem {
	return verb.QueueItem{Engine: r.Engine, URL: r.URL, Title: r.Title, Duration: r.Duration}
}

// addToPlaylist is a: with no playlist yet it asks for a name, else it offers the picker.
func (m *Model) addToPlaylist() tea.Cmd {
	if len(m.rows) == 0 || !m.haveStore() {
		return nil
	}
	ls, err := m.suite.PlaylistLs(m.call())
	if err != nil {
		m.notice(m.s.PLAct+":", storeMsg(err, m.s.Failed))
		return nil
	}
	m.payload = m.item(m.rows[m.cursor])
	if len(ls) == 0 {
		return m.ask(askNew, m.s.PLPromptNew, "", nil)
	}
	return m.ask(askAdd, m.s.PLPromptAdd, m.s.PLAdd, ls)
}

func (m *Model) doAdd(name string) {
	w, err := m.suite.PlaylistAdd(m.call(), name, []verb.QueueItem{m.payload}, pid)
	if err != nil {
		m.notice(m.s.PLAct+":", storeMsg(err, m.s.Failed))
		return
	}
	m.arm("playlist", w, m.s.PLAct+":", m.s.PLAdded+" "+m.g.Arrow+" "+name)
}

// browsePlaylists is b: a toggle — from a playlist (or a pasted container) back to the
// results, otherwise the picker.
func (m *Model) browsePlaylists() tea.Cmd {
	if m.src == srcPlaylist || m.src == srcContainer {
		m.backToSearch()
		return nil
	}
	if !m.haveStore() {
		return nil
	}
	ls, err := m.suite.PlaylistLs(m.call())
	if err != nil {
		m.notice(m.s.PLAct+":", storeMsg(err, m.s.Failed))
		return nil
	}
	if len(ls) == 0 {
		m.notice(m.s.PLAct+":", m.s.PLNone)
		return nil
	}
	return m.ask(askOpen, m.s.PLPromptOpen, m.s.PLOpen, ls)
}

// browseRemotePlaylists is B: a toggle — from a container back to search, otherwise
// fetch the user's online playlists under account.
func (m *Model) browseRemotePlaylists() tea.Cmd {
	if m.src == srcContainer || m.src == srcPlaylist {
		m.backToSearch()
		return nil
	}
	eng := m.engine()
	if !eng.Has("--playlists") {
		m.notice(m.s.RemotePLAct+":", eng.Name+" "+m.g.Dash+" "+m.s.RemotePLNotSupported)
		return nil
	}
	if m.hold() {
		return nil
	}
	return m.engineCall(m.s.RemotePLAct, func(ctx context.Context) tea.Msg {
		res, err := m.suite.RemotePlaylists(ctx, eng.Name, 50)
		return remotePlaylistsMsg{res: res, err: err}
	})
}

type remotePlaylistsMsg struct {
	res *verb.RemotePlaylistsResult
	err error
}

func (m *Model) remotePlaylistsDone(msg remotePlaylistsMsg) tea.Cmd {
	eng := m.engine().Name
	if msg.err != nil {
		var ve *verb.Error
		if errors.As(msg.err, &ve) {
			if ve.Reason == "cookies" {
				m.notice(m.s.RemotePLAct+":", m.s.RemotePLNoCookies)
			} else if ve.Stderr != "" {
				m.notice(m.s.RemotePLAct+":", verb.EngineMsg(eng, ve.Stderr))
			} else if ve.Reason != "" {
				m.notice(m.s.RemotePLAct+":", ve.Reason)
			} else {
				m.notice(m.s.RemotePLAct+":", storeMsg(msg.err, m.s.Failed))
			}
		} else {
			m.notice(m.s.RemotePLAct+":", storeMsg(msg.err, m.s.Failed))
		}
		return nil
	}
	if msg.res != nil && msg.res.Note != "" {
		m.notice(m.s.RemotePLAct+":", msg.res.Note)
	}
	if msg.res == nil || len(msg.res.Playlists) == 0 {
		m.notice(m.s.RemotePLAct+":", m.s.PLNoneRemote)
		return nil
	}
	m.remotePick = msg.res.Playlists
	pick := make([]verb.Playlist, len(m.remotePick))
	for i, pl := range m.remotePick {
		cnt := 0
		if pl.Count != nil {
			cnt = *pl.Count
		}
		pick[i] = verb.Playlist{Name: pl.Title, Count: cnt}
	}
	return m.ask(askRemoteOpen, m.s.RemotePLPrompt, m.s.RemotePLAct, pick)
}

func (m *Model) playlistOnly() bool {
	if m.src == srcPlaylist {
		return true
	}
	m.notice(m.s.PLAct+":", m.s.PLListOnly)
	return false
}

// removeFromPlaylist is d. Nothing asks first — z takes it back. A removed row that is
// playing stops, but only once the undo offer has closed: the stopped player is the one
// thing the undo could not put back.
func (m *Model) removeFromPlaylist() {
	if len(m.rows) == 0 || !m.playlistOnly() || !m.haveStore() {
		return
	}
	idx := m.focusedIndex()
	if idx < 0 {
		m.notice(m.s.PLAct+":", m.s.Failed)
		return
	}
	r := m.rows[m.cursor]
	playing := m.playingRow(r)
	w, err := m.suite.PlaylistRm(m.call(), m.plName, idx, pid)
	if err != nil {
		m.notice(m.s.PLAct+":", storeMsg(err, m.s.Failed))
		return
	}
	if m.arm("playlist", w, m.s.PLAct+":", m.s.PLRemoved+" "+m.g.Arrow+" "+m.plName+": "+r.Title) {
		m.undoStop = playing
	} else if playing {
		m.stopNow()
	}
	m.reloadPlaylist(m.plName)
}

// deletePlaylist is D: the whole list on screen, then back to the results.
func (m *Model) deletePlaylist() {
	if !m.playlistOnly() || !m.haveStore() {
		return
	}
	playing := false
	for _, r := range m.all {
		playing = playing || m.playingRow(r)
	}
	name := m.plName
	w, err := m.suite.PlaylistDel(m.call(), name, pid)
	if err != nil {
		m.notice(m.s.PLAct+":", storeMsg(err, m.s.Failed))
		return
	}
	if m.arm("playlist", w, m.s.PLAct+":", m.s.PLDeleted+" "+m.g.Arrow+" "+name) {
		m.undoStop = playing
	} else if playing {
		m.stopNow()
	}
	m.backToSearch()
}

func (m *Model) renamePlaylist() tea.Cmd {
	if !m.playlistOnly() || !m.haveStore() {
		return nil
	}
	return m.ask(askRename, m.s.PLRenamePrompt, "", nil)
}

func (m *Model) doRename(to string) {
	from := m.plName
	if to == from {
		return
	}
	w, err := m.suite.PlaylistRename(m.call(), from, to, pid)
	if err != nil {
		m.notice(m.s.PLAct+":", storeMsg(err, m.s.Failed))
		return
	}
	m.plName, m.label = to, to
	m.arm("playlist", w, m.s.PLAct+":", m.s.PLRenamed+" "+m.g.Arrow+" "+to)
}

// ── history, parts, chapters, queue ─────────────────────────────────────────────────────

func (m *Model) openHistory() {
	if m.src == srcHistory {
		m.backToSearch()
		return
	}
	if m.suite.THistory == "" {
		m.notice(m.s.HistAct+":", m.s.HistAbsent)
		return
	}
	l, err := m.suite.HistoryLs(m.call(), 50)
	if err != nil {
		m.notice(m.s.HistAct+":", storeMsg(err, m.s.Failed))
		return
	}
	if l.Count == 0 {
		m.notice(m.s.HistAct+":", m.s.HistEmpty)
		return
	}
	m.openRows(srcHistory, m.s.HistLabel, rowsFromItems(l, false))
}

// engineHas is whether the focused row's own engine accepts a flag.
func (m *Model) engineHas(name, flag string) bool {
	for _, e := range m.opt.Engines {
		if e.Name == name {
			return e.Has(flag)
		}
	}
	return false
}

// openParts is c: the focused row's parts, when its engine has --items. One part is not a
// view — the row on screen already is that part.
func (m *Model) openParts() tea.Cmd {
	if m.src == srcParts {
		m.backToSearch()
		return nil
	}
	if len(m.rows) == 0 || !m.engineHas(m.rows[m.cursor].Engine, "--items") || !m.searchOnly() || m.hold() {
		return nil
	}
	r := m.rows[m.cursor]
	return m.engineCall(m.s.PartsAct, func(ctx context.Context) tea.Msg {
		l, err := m.suite.Items(ctx, r.Engine, r.URL)
		return partsMsg{l: l, title: r.Title, err: err}
	})
}

type partsMsg struct {
	l     *verb.ItemList
	title string
	err   error
}

func (m *Model) partsDone(msg partsMsg) {
	if msg.err != nil {
		m.notice(m.s.PartsAct+":", storeMsg(msg.err, m.s.Failed))
		return
	}
	n := len(msg.l.Items)
	if msg.l.Total != nil {
		n = *msg.l.Total
	}
	if n <= 1 {
		m.notice(m.s.PartsAct+":", m.s.PartsOne)
		return
	}
	title := msg.l.Title
	if title == "" {
		title = msg.title
	}
	m.openRows(srcParts, clean(title), rowsFromItems(msg.l, true))
	m.total = n
	// The collection's length is only a fact when every part is here and every part has one;
	// a partial sum printed as total= would be a wrong number.
	m.totalFmt = ""
	if !msg.l.HasMore {
		sum, ok := 0.0, true
		for _, it := range msg.l.Items {
			if it.Duration == nil {
				ok = false
				break
			}
			sum += *it.Duration
		}
		if ok {
			m.totalFmt = fmtDurLong(int(sum))
		}
	}
}

// openChapters is i: one --info for the focused row; its chapters become rows whose handle
// carries the offset, so Enter, + and a all inherit it.
func (m *Model) openChapters() tea.Cmd {
	if m.src == srcChapters {
		m.backToSearch()
		return nil
	}
	if len(m.rows) == 0 || !m.engineHas(m.rows[m.cursor].Engine, "--info") || !m.searchOnly() || m.hold() {
		return nil
	}
	r := m.rows[m.cursor]
	if m.info != nil && m.info.engine == r.Engine && m.info.url == r.URL {
		m.showChapters()
		return nil
	}
	return m.engineCall(m.s.InfoAct, func(ctx context.Context) tea.Msg {
		i, err := m.suite.Info(ctx, r.Engine, r.URL)
		return infoMsg{i: i, engine: r.Engine, url: r.URL, err: err}
	})
}

type infoMsg struct {
	i           *verb.Info
	engine, url string
	err         error
}

func (m *Model) infoDone(msg infoMsg) {
	if msg.err != nil {
		m.notice(m.s.InfoAct+":", storeMsg(msg.err, m.s.Failed))
		return
	}
	in := &info{engine: msg.engine, url: msg.url, title: clean(msg.i.Title), chapters: msg.i.Chapters}
	if d := msg.i.UploadDate; len(d) == 8 {
		in.uploaded = d[:4] + "-" + d[4:6] + "-" + d[6:]
	} else {
		in.uploaded = d
	}
	if msg.i.LikeCount != nil {
		in.likes = strconv.FormatInt(*msg.i.LikeCount, 10)
	}
	m.info = in
	m.showChapters()
}

// openRelated is g: related recommendations for the focused row, when its engine has --related.
func (m *Model) openRelated() tea.Cmd {
	if m.src == srcRelated {
		m.backToSearch()
		return nil
	}
	if len(m.rows) == 0 || !m.searchOnly() || m.hold() {
		return nil
	}
	r := m.rows[m.cursor]
	if !m.engineHas(r.Engine, "--related") {
		m.notice(m.s.RelatedAct+":", m.s.RelatedNoCap)
		return nil
	}
	n := m.opt.Search.N
	return m.engineCall(m.s.RelatedAct, func(ctx context.Context) tea.Msg {
		res, err := m.suite.Related(ctx, r.Engine, r.ID, n)
		return relatedMsg{res: res, title: r.Title, err: err}
	})
}

type relatedMsg struct {
	res   *verb.SearchResult
	title string
	err   error
}

func (m *Model) relatedDone(msg relatedMsg) {
	if msg.err != nil {
		m.notice(m.s.RelatedAct+":", storeMsg(msg.err, m.s.Failed))
		return
	}
	if msg.res.Note != "" {
		m.notice(m.s.RelatedAct+":", msg.res.Note)
	}
	if len(msg.res.Results) == 0 {
		m.notice(m.s.RelatedAct+":", m.s.NoResults)
		return
	}
	m.openRows(srcRelated, clean(msg.title), rowsFromSearch(msg.res))
}

func (m *Model) loadFeed(feedType string) tea.Cmd {
	eng := m.opt.Engines[m.opt.Engine].Name
	n := m.opt.Search.N
	return m.engineCall(m.s.FeedAct, func(ctx context.Context) tea.Msg {
		res, err := m.suite.Feed(ctx, eng, feedType, n)
		return feedMsg{res: res, err: err}
	})
}

type feedMsg struct {
	res *verb.SearchResult
	err error
}

func (m *Model) feedDone(msg feedMsg) tea.Cmd {
	eng := m.opt.Engines[m.opt.Engine].Name
	if msg.err != nil {
		var ve *verb.Error
		if errors.As(msg.err, &ve) {
			if ve.Stderr != "" {
				m.notice(m.s.FeedAct+":", verb.EngineMsg(eng, ve.Stderr))
			} else if ve.Reason != "" {
				m.notice(m.s.FeedAct+":", ve.Reason)
			} else {
				m.notice(m.s.FeedAct+":", storeMsg(msg.err, m.s.Failed))
			}
		} else {
			m.notice(m.s.FeedAct+":", storeMsg(msg.err, m.s.Failed))
		}
		if !m.prompting {
			return m.ask(askSearch, "", "", nil)
		}
		return nil
	}
	if msg.res.Note != "" {
		m.notice(m.s.FeedAct+":", msg.res.Note)
	}
	if len(msg.res.Results) == 0 {
		m.notice(m.s.FeedAct+":", m.s.NoResults)
		if !m.prompting {
			return m.ask(askSearch, "", "", nil)
		}
		return nil
	}
	rows := rowsFromSearch(msg.res)
	if m.src != srcSearch {
		m.stash = stash{all: rows, query: msg.res.Query, cursor: 0, top: 0}
	} else {
		m.all, m.rows = rows, rows
		m.query = msg.res.Query
		m.cursor, m.top = 0, 0
		m.filterOn, m.filter = false, ""
	}
	return nil
}

func (m *Model) showChapters() {
	in := m.info
	if len(in.chapters) == 0 {
		m.notice(m.s.InfoAct+":", m.s.ChapNone)
		return
	}
	// The rail is the chapter's span, both clocks padded to one width so the arrows align.
	var starts, ends []string
	ws, we := 0, 0
	for _, c := range in.chapters {
		s := clock(int(c.Start))
		starts = append(starts, s)
		ws = max(ws, len(s))
		e := ""
		if c.End != nil && *c.End > c.Start {
			e = clock(int(*c.End))
		}
		ends = append(ends, e)
		we = max(we, len(e))
	}
	var rows []row
	for i, c := range in.chapters {
		rail := fmt.Sprintf("%*s %s", ws, starts[i], m.g.Arrow)
		if we > 0 {
			rail += fmt.Sprintf(" %*s", we, ends[i])
		}
		var dur *float64
		if c.End != nil && *c.End > c.Start {
			d := float64(int(*c.End) - int(c.Start))
			dur = &d
		}
		sec := int(c.Start)
		rows = append(rows, row{Title: oneline(c.Title), URL: chapterURL(in.url, sec), Engine: in.engine,
			Duration: dur, Rail: rail, N: i + 1, Sec: sec})
	}
	m.openRows(srcChapters, in.title, rows)
	m.total = len(rows)
	m.chapFollow = -1
	m.followChapter()
}

// chapterURL is the item's handle carrying t=SEC; an existing t= is dropped first.
func chapterURL(u string, sec int) string {
	k := handleKey(u)
	if strings.Contains(k, "?") {
		return k + "&t=" + strconv.Itoa(sec)
	}
	return k + "?t=" + strconv.Itoa(sec)
}

// infoFocused is whether the --info on hand is the focused row's.
func (m *Model) infoFocused() bool {
	if m.info == nil || len(m.rows) == 0 {
		return false
	}
	r := m.rows[m.cursor]
	return r.Engine == m.info.engine && handleKey(r.URL) == handleKey(m.info.url)
}

// playingChapter is the chapter the playhead is in, by its start, or -1.
func (m *Model) playingChapter() int {
	if m.src != srcChapters || m.info == nil || m.live || m.playEngine != m.info.engine ||
		handleKey(m.playURL) != handleKey(m.info.url) {
		return -1
	}
	pos, ok := m.position()
	if !ok {
		return -1
	}
	sec := -1
	for _, c := range m.info.chapters {
		if c.Start > pos {
			break
		}
		sec = int(c.Start)
	}
	return sec
}

// followChapter moves the cursor with the playhead — but only while the cursor is still on
// the chapter it last followed to, so a user who moved away is not dragged back.
func (m *Model) followChapter() {
	sec := m.playingChapter()
	if sec < 0 || sec == m.chapFollow {
		return
	}
	if m.chapFollow >= 0 && (m.cursor >= len(m.rows) || m.rows[m.cursor].Sec != m.chapFollow) {
		m.chapFollow = sec
		return
	}
	m.chapFollow = sec
	for i, r := range m.rows {
		if r.Sec == sec {
			m.cursor = i
			return
		}
	}
}

// playChapter is Enter on a chapter: a seek when that item is already playing, else a play
// from the chapter's handle.
func (m *Model) playChapter() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	r := m.rows[m.cursor]
	if m.playerID != "" && r.Engine == m.playEngine && handleKey(r.URL) == handleKey(m.playURL) {
		return m.verbCmd("", "", func(ctx context.Context, id string) error { return m.suite.SeekTo(ctx, id, r.Sec) })
	}
	return m.playCmd()
}

// openQueue is u: the running player's queue, cursor on the track that is playing.
func (m *Model) openQueue() {
	if m.src == srcQueue {
		m.backToSearch()
		return
	}
	if m.src != srcSearch {
		m.notice(m.s.QAct+":", m.s.QElsewhere)
		return
	}
	if m.playerID == "" {
		m.notice(m.s.QAct+":", m.s.QNone)
		return
	}
	l, err := m.suite.QueueShow(m.call(), m.playerID)
	if err != nil {
		m.notice(m.s.QAct+":", storeMsg(err, m.s.QNone))
		return
	}
	if l.Len <= 1 {
		m.notice(m.s.QAct+":", m.s.QOne)
		return
	}
	m.openRows(srcQueue, m.s.QLabel, m.queueRows(l))
	m.cursor = l.Pos
	m.clampCursor()
}

// queueRows fills the playing item's title from the banner when the queue has none for it.
func (m *Model) queueRows(l *verb.ItemList) []row {
	rows := rowsFromItems(l, false)
	m.q = &[2]int{l.Pos, l.Len}
	known := map[string]string{}
	for _, r := range append(append([]row{}, m.stash.all...), m.all...) {
		known[r.Engine+" "+handleKey(r.URL)] = r.Title
	}
	for i := range rows {
		if t, ok := known[rows[i].Engine+" "+handleKey(rows[i].URL)]; ok && rows[i].Title == rows[i].URL {
			rows[i].Title = t
		}
		if rows[i].N != l.Pos {
			continue
		}
		if rows[i].Title == rows[i].URL && m.playTitle != "" {
			rows[i].Title = m.playTitle
		}
		if rows[i].Duration == nil && m.last != nil {
			rows[i].Duration = m.last.Duration
		}
	}
	return rows
}

func (m *Model) reloadQueue(keep int) {
	l, err := m.suite.QueueShow(m.call(), m.playerID)
	if err != nil || m.playerID == "" {
		m.backToSearch()
		m.notice(m.s.QAct+":", m.s.QNone)
		return
	}
	m.all = m.queueRows(l)
	m.rows, m.filterOn, m.filter = m.all, false, ""
	m.cursor = keep
	m.clampCursor()
}

func (m *Model) queuePos() int {
	if q := m.queue(); q != nil {
		return q[0]
	}
	return -1
}

// noteQueue takes the queue's shape from a verb's envelope: the player's events only say it
// at the next boundary, and the status line should not lag the key that changed it.
func (m *Model) noteQueue(w *verb.Written) {
	if w != nil && w.Queue != nil {
		m.q = &[2]int{w.Queue.Pos, w.Queue.Len}
	}
}

// queueEdit runs one queue verb on the focused row. A stale index or one out of range
// re-reads the queue and says so: the player moved on while the user was looking.
func (m *Model) queueEdit(keep int, undoLabel string, f func() (*verb.Written, error)) {
	w, err := f()
	if err != nil {
		if r := reason(err); r == "queue_stale" || r == "queue_range" {
			m.reloadQueue(keep)
			m.notice(m.s.QAct+":", m.s.QStale)
			return
		}
		m.notice(m.s.QAct+":", storeMsg(err, m.s.Failed))
		return
	}
	m.noteQueue(w)
	m.reloadQueue(keep)
	if undoLabel != "" {
		m.arm("queue", w, m.s.QAct+":", undoLabel)
	}
}

func (m *Model) queueKey(k string) {
	if len(m.rows) == 0 || m.cursor >= len(m.rows) {
		return
	}
	r, pos, id, ctx := m.rows[m.cursor], m.queuePos(), m.playerID, m.call()
	switch k {
	case "x":
		if r.N == pos {
			m.notice(m.s.QAct+":", m.s.QPlayingRow)
			return
		}
		m.queueEdit(r.N, m.s.QRemoved+" "+m.g.Arrow+" "+r.Title, func() (*verb.Written, error) {
			return m.suite.QueueRm(ctx, id, r.N, r.URL, pid)
		})
	case "X":
		m.queueEdit(pos, m.s.QCleared, func() (*verb.Written, error) { return m.suite.QueueClear(ctx, id, pid) })
	case "p", "P":
		to := r.N - 1
		if k == "P" {
			to = r.N + 1
		}
		if to <= pos || to >= len(m.rows) {
			return
		}
		m.queueEdit(to, "", func() (*verb.Written, error) { return m.suite.QueueMv(ctx, id, r.N, to, r.URL) })
	case "enter":
		if r.N == pos {
			return
		}
		m.queueEdit(pos+1, "", func() (*verb.Written, error) { return m.suite.QueueJump(ctx, id, r.N, r.URL) })
	}
}

// ── undo ────────────────────────────────────────────────────────────────────────────────

// arm opens the 3-second offer a write's envelope carries, replacing any earlier one; false
// (and a plain notice) when the store gave none.
func (m *Model) arm(store string, w *verb.Written, head, label string) bool {
	dl := w.Deadline()
	if dl == 0 {
		m.notice(head, label)
		return false
	}
	if m.undoStore != "" && m.undoStore != store {
		m.undoDiscard()
	}
	m.undoForget()
	m.undoStore, m.undoEnd, m.undoHead, m.undoLabel = store, time.Unix(dl, 0), head, label
	return true
}

func (m *Model) undoActive() bool { return !m.undoEnd.IsZero() && time.Now().Before(m.undoEnd) }

// undoForget closes the offer; a stop the write implied happens now.
func (m *Model) undoForget() {
	m.undoEnd, m.undoHead, m.undoLabel = time.Time{}, "", ""
	if m.undoStop {
		m.undoStop = false
		m.stopNow()
	}
}

// undoDiscard drops the store's copy, so an offer the user never took leaves nothing behind.
func (m *Model) undoDiscard() {
	switch m.undoStore {
	case "queue":
		_, _ = m.suite.QueueUndo(m.call(), pid, true)
	case "playlist":
		if m.suite.TPlaylist != "" {
			_, _ = m.suite.PlaylistUndo(m.call(), pid, true)
		}
	}
	m.undoStore = ""
}

// Discard is the way out: drop any copy this session still holds.
func (m *Model) Discard() { m.undoDiscard() }

func (m *Model) undo() {
	if !m.undoActive() {
		m.notice(m.s.UndoAct+":", m.s.UndoNone)
		return
	}
	store := m.undoStore
	var w *verb.Written
	var err error
	if store == "queue" {
		w, err = m.suite.QueueUndo(m.call(), pid, false)
	} else {
		w, err = m.suite.PlaylistUndo(m.call(), pid, false)
	}
	if err != nil {
		var ve *verb.Error
		if errors.As(err, &ve) && ve.Code == 4 && ve.Reason != "locked" {
			if ve.Reason == "undo_none" {
				m.undoStore = ""
			}
			m.undoForget()
			switch ve.Reason {
			case "undo_stale":
				m.notice(m.s.UndoAct+":", m.s.UndoStale)
			case "undo_none", "undo_expired":
				m.notice(m.s.UndoAct+":", m.s.UndoNone)
			default:
				m.notice(m.s.UndoAct+":", storeMsg(err, m.s.Failed))
			}
			return
		}
		m.notice(m.s.UndoAct+":", storeMsg(err, m.s.Failed))
		return
	}
	m.undoStore, m.undoStop = "", false
	m.undoForget()
	if store == "queue" {
		m.noteQueue(w)
		if m.src == srcQueue {
			m.reloadQueue(m.cursor)
		}
	} else {
		m.undoShowPlaylist(w)
	}
	m.notice(m.s.UndoAct+":", m.s.UndoDone)
}

// undoShowPlaylist puts the restored list where the eye is: a rename restored under the
// list's old name follows it; the list on screen reloads with the cursor on the restored
// row; a list deleted from the results reopens.
func (m *Model) undoShowPlaylist(w *verb.Written) {
	if w == nil || w.Name == "" {
		return
	}
	if w.From != "" && m.src == srcPlaylist && m.plName == w.From {
		m.plName, m.label = w.Name, w.Name
	}
	place := func() {
		if w.Index != nil {
			m.cursor = *w.Index
			m.clampCursor()
		}
	}
	switch {
	case m.src == srcPlaylist && m.plName == w.Name:
		m.reloadPlaylist(w.Name)
		place()
	case m.src == srcSearch && (w.Undone == "del" || w.Undone == "rm"):
		if m.openPlaylist(w.Name) {
			place()
		}
	}
}

// ── URL targets ─────────────────────────────────────────────────────────────────────────

// urlTarget is a pasted or typed text's URL, if it is one: the first http(s):// run (quotes
// and brackets off), or a single token starting www. Nothing else: a query like lofi.mix/2024
// is shaped like host/path and is a search. Which site a URL names is not this file's
// knowledge; ting-play routes a URL to its engine.
func urlTarget(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `"'`)
	for _, scheme := range []string{"https://", "http://"} {
		if i := strings.Index(s, scheme); i >= 0 {
			u := s[i:]
			if j := strings.IndexAny(u, " \t\n\"'<>()"); j >= 0 {
				u = u[:j]
			}
			return u
		}
	}
	if strings.HasPrefix(s, "www.") && !strings.ContainsAny(s, " \t") {
		return s
	}
	return ""
}

type urlMsg struct {
	url  string
	l    *verb.ItemList
	i    *verb.Info
	err  error
	what string
}

// loadURL asks ting-play what a URL is, by the envelopes and exit codes alone: --items
// first (a container answers fast; a track is refused with 1), and a single-part answer or
// that refusal falls to --info, whose one row replaces the results.
func (m *Model) loadURL(u string) tea.Cmd {
	if m.hold() {
		return nil
	}
	s := m.suite
	return m.engineCall(m.s.URLAct, func(ctx context.Context) tea.Msg {
		l, err := s.Items(ctx, "", u)
		if err == nil && len(l.Items) > 1 {
			return urlMsg{url: u, l: l}
		}
		var ve *verb.Error
		if err != nil && !(errors.As(err, &ve) && ve.Kind() == verb.Usage) {
			return urlMsg{url: u, err: err, what: "container"}
		}
		i, err := s.Info(ctx, "", u)
		return urlMsg{url: u, i: i, err: err}
	})
}

func (m *Model) urlDone(msg urlMsg) {
	if m.all == nil {
		// A URL given at startup: whatever it turns out to be, the list exists from here on.
		m.all, m.rows = []row{}, []row{}
	}
	switch {
	case msg.err != nil:
		label := m.s.URLAct
		if msg.what == "container" {
			label = m.s.ContainerAct
		}
		m.notice(label+":", storeMsg(msg.err, m.s.Failed))
	case msg.l != nil:
		title := clean(msg.l.Title)
		if title == "" {
			title = m.s.ContainerAct
		}
		m.openRows(srcContainer, title, rowsFromItems(msg.l, false))
	default:
		i := msg.i
		res := &verb.SearchResult{Engine: i.Engine, Results: []verb.Result{i.Result}}
		if res.Results[0].Channel == "" {
			res.Results[0].Channel = i.Uploader
		}
		m.backToSearch()
		m.all, m.rows = rowsFromSearch(res), rowsFromSearch(res)
		m.query, m.cursor, m.top = msg.url, 0, 0
	}
}

// hold is the one-fetch rule for a key that would start another: while one is in flight the
// key is turned away, and the busy line says so — a paste or an i dropped without a word
// reads as a key that does nothing.
func (m *Model) hold() bool {
	if m.pending == nil {
		return false
	}
	m.held = true
	return true
}

// engineCall runs one network verb with the busy line up, and brings its message back.
// It holds the one fetch slot, so no second fetch starts under it.
func (m *Model) engineCall(what string, f func(context.Context) tea.Msg) tea.Cmd {
	m.busy, m.held = what+m.g.Ell, false
	m.pending = &fetchReq{gen: -1}
	ctx := m.ctx
	return func() tea.Msg { return f(ctx) }
}

// clipboard is the system clipboard, from whichever reader the platform has.
func clipboard() string {
	for _, c := range [][]string{{"pbpaste"}, {"wl-paste"}, {"xclip", "-selection", "clipboard", "-o"}, {"xsel", "-b", "-o"}} {
		if p, err := exec.LookPath(c[0]); err == nil {
			out, err := exec.Command(p, c[1:]...).Output()
			if err == nil {
				return string(out)
			}
		}
	}
	return ""
}

// pasted routes text the user pasted: a URL loads, anything else opens the prompt with it.
func (m *Model) pasted(text string) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if u := urlTarget(text); u != "" {
		return m.loadURL(u)
	}
	return m.openPrompt(text)
}
