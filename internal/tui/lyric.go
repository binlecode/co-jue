package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/binlecode/ting/internal/verb"
	tea "github.com/charmbracelet/bubbletea"
)

type lyricStatus int

const (
	lyricNone lyricStatus = iota
	lyricLoading
	lyricReady
)

const lyricTransitionDuration = 150 * time.Millisecond

type lyricTrack struct {
	url      string
	status   lyricStatus
	segments []verb.Segment
}

type lyricDoneMsg struct {
	url      string
	segments []verb.Segment
	err      error
}

// engineHasTranscript reports whether the named engine declares --transcript in its flags.
func (m *Model) engineHasTranscript(engine string) bool {
	for _, e := range m.opt.Engines {
		if e.Name == engine {
			return e.Has("--transcript")
		}
	}
	return false
}

// triggerLyricCmd initiates fetching transcript segments for url on engine if supported.
func (m *Model) triggerLyricCmd(engine, url string) tea.Cmd {
	if url == "" || engine == "" {
		return nil
	}
	if !m.engineHasTranscript(engine) {
		m.lyricsCache[url] = &lyricTrack{url: url, status: lyricNone}
		m.currentLyric = m.lyricsCache[url]
		return nil
	}
	if cached, ok := m.lyricsCache[url]; ok {
		m.currentLyric = cached
		return nil
	}
	if m.lyricInFlight == url {
		return nil
	}
	m.currentLyric = &lyricTrack{url: url, status: lyricLoading}
	m.lyricInFlight = url
	s, ctx := m.suite, m.ctx
	return func() tea.Msg {
		tr, err := s.Transcript(ctx, engine, url)
		var segs []verb.Segment
		if err == nil && tr != nil {
			segs = tr.Segments
		}
		return lyricDoneMsg{url: url, segments: segs, err: err}
	}
}

// lyricLine locates the active transcript line by extrapolating current position and performing
// binary search on segments. Returns (line, isInterlude, isTransition, ok).
func (m *Model) lyricLine(cols int) (string, bool, bool, bool) {
	if m.playURL != "" && m.lyricsCache[m.playURL] != nil {
		m.currentLyric = m.lyricsCache[m.playURL]
	}
	if m.currentLyric == nil || m.currentLyric.status != lyricReady || len(m.currentLyric.segments) == 0 {
		return "", false, false, false
	}
	pos, ok := m.position()
	if !ok || pos < 0 {
		pos = 0
	}
	segs := m.currentLyric.segments
	n := len(segs)

	// Intro: before the first segment
	if pos < segs[0].Start {
		m.lyricActiveIdx = -1
		return m.g.LyricInter, true, false, true
	}

	// Binary search for the segment with start <= pos
	idx := sort.Search(n, func(j int) bool {
		return segs[j].Start > pos
	}) - 1
	if idx < 0 {
		idx = 0
	}

	cur := segs[idx]
	st := cur.Start
	dur := cur.Duration

	// Check if in an interlude
	if dur > 0 && pos >= st+dur {
		if idx < n-1 && segs[idx+1].Start > st+dur {
			m.lyricActiveIdx = -1
			return m.g.LyricInter, true, false, true
		}
		if idx == n-1 {
			m.lyricActiveIdx = -1
			return m.g.LyricInter, true, false, true
		}
	}

	// Segment text is active
	rawText := strings.TrimSpace(clean(cur.Text))
	if rawText == "" {
		m.lyricActiveIdx = -1
		return m.g.LyricInter, true, false, true
	}

	// Transition detection: line switch within lyricTransitionDuration
	isTrans := false
	now := time.Now()
	if idx != m.lyricActiveIdx {
		m.lyricActiveIdx = idx
		m.lyricSwitchedAt = now
		isTrans = true
	} else if now.Sub(m.lyricSwitchedAt) < lyricTransitionDuration {
		isTrans = true
	}

	prefix := m.g.Lyric
	avail := cols - 2 - m.w.of(prefix)
	truncated := m.w.trunc(rawText, avail, m.g.Ell)
	return prefix + truncated, false, isTrans, true
}

// activeLyricIndex finds the current segment index for pos. Returns (index, isInterlude).
func (m *Model) activeLyricIndex(pos float64) (int, bool) {
	lyr := m.currentLyric
	if lyr == nil && m.playURL != "" {
		lyr = m.lyricsCache[m.playURL]
	}
	if lyr == nil || lyr.status != lyricReady || len(lyr.segments) == 0 {
		return -1, false
	}
	segs := lyr.segments
	n := len(segs)
	if pos < segs[0].Start {
		return -1, true
	}
	idx := sort.Search(n, func(j int) bool {
		return segs[j].Start > pos
	}) - 1
	if idx < 0 {
		idx = 0
	}
	cur := segs[idx]
	if cur.Duration > 0 && pos >= cur.Start+cur.Duration {
		return idx, true
	}
	return idx, false
}
