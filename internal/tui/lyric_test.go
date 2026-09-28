package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/binlecode/ting/internal/verb"
)

func TestEngineHasTranscript(t *testing.T) {
	m := &Model{
		opt: Options{
			Engines: []verb.Engine{
				{Name: "yt", Flags: []string{"--search", "--transcript"}},
				{Name: "ne", Flags: []string{"--search", "--transcript"}},
				{Name: "bili", Flags: []string{"--search", "--items"}},
			},
		},
	}
	if !m.engineHasTranscript("yt") {
		t.Errorf("yt should have transcript")
	}
	if !m.engineHasTranscript("ne") {
		t.Errorf("ne should have transcript")
	}
	if m.engineHasTranscript("bili") {
		t.Errorf("bili should NOT have transcript")
	}
	if m.engineHasTranscript("unknown") {
		t.Errorf("unknown should NOT have transcript")
	}
}

func TestLyricPeekingBinarySearch(t *testing.T) {
	m := &Model{
		g: glyphsUTF,
		w: newWidth(false),
		currentLyric: &lyricTrack{
			url:    "test://track",
			status: lyricReady,
			segments: []verb.Segment{
				{Start: 5.0, Duration: 3.0, Text: "First Line of Song"},
				{Start: 10.0, Duration: 4.0, Text: "Second Line Here"},
				{Start: 20.0, Duration: 5.0, Text: "Longer Third Line After Interlude"},
			},
		},
		lyricActiveIdx: -1,
	}

	// 1. Intro before first segment (pos = 2.0s < 5.0s)
	m.last = &verb.Event{Position: floatPtr(2.0), Duration: floatPtr(60.0)}
	m.lastAt = time.Now()
	line, isInter, isTrans, ok := m.lyricLine(60)
	if !ok || !isInter || line != m.g.LyricInter {
		t.Errorf("intro: got %q, isInter=%v, ok=%v; want %q, true, true", line, isInter, ok, m.g.LyricInter)
	}

	// 2. Active first segment (pos = 6.0s in [5.0, 8.0])
	m.last.Position = floatPtr(6.0)
	m.lastAt = time.Now()
	line, isInter, isTrans, ok = m.lyricLine(60)
	if !ok || isInter || !strings.Contains(line, "First Line of Song") || !isTrans {
		t.Errorf("first line: got %q, isInter=%v, isTrans=%v, ok=%v", line, isInter, isTrans, ok)
	}

	// 3. Interlude between line 1 and line 2 (pos = 9.0s in [8.0, 10.0])
	m.last.Position = floatPtr(9.0)
	m.lastAt = time.Now()
	line, isInter, isTrans, ok = m.lyricLine(60)
	if !ok || !isInter || line != m.g.LyricInter {
		t.Errorf("interlude 1->2: got %q, isInter=%v, ok=%v; want %q", line, isInter, ok, m.g.LyricInter)
	}

	// 4. Active second segment (pos = 11.0s in [10.0, 14.0])
	m.last.Position = floatPtr(11.0)
	m.lastAt = time.Now()
	line, isInter, isTrans, ok = m.lyricLine(60)
	if !ok || isInter || !strings.Contains(line, "Second Line Here") {
		t.Errorf("second line: got %q, isInter=%v, ok=%v", line, isInter, ok)
	}

	// 5. Long interlude between line 2 and line 3 (pos = 16.0s in [14.0, 20.0])
	m.last.Position = floatPtr(16.0)
	m.lastAt = time.Now()
	line, isInter, isTrans, ok = m.lyricLine(60)
	if !ok || !isInter || line != m.g.LyricInter {
		t.Errorf("interlude 2->3: got %q, isInter=%v, ok=%v; want %q", line, isInter, ok, m.g.LyricInter)
	}

	// 6. Active third segment (pos = 21.0s in [20.0, 25.0])
	m.last.Position = floatPtr(21.0)
	m.lastAt = time.Now()
	line, isInter, isTrans, ok = m.lyricLine(60)
	if !ok || isInter || !strings.Contains(line, "Longer Third Line After Interlude") {
		t.Errorf("third line: got %q, isInter=%v, ok=%v", line, isInter, ok)
	}

	// 7. Outro after last segment (pos = 26.0s >= 25.0s)
	m.last.Position = floatPtr(26.0)
	m.lastAt = time.Now()
	line, isInter, isTrans, ok = m.lyricLine(60)
	if !ok || !isInter || line != m.g.LyricInter {
		t.Errorf("outro: got %q, isInter=%v, ok=%v; want %q", line, isInter, ok, m.g.LyricInter)
	}
}

func TestLyricASCIIFallback(t *testing.T) {
	m := &Model{
		g: glyphsASCII,
		w: newWidth(false),
		currentLyric: &lyricTrack{
			url:    "test://track",
			status: lyricReady,
			segments: []verb.Segment{
				{Start: 1.0, Duration: 5.0, Text: "ASCII Lyric Line"},
			},
		},
		lyricActiveIdx: -1,
	}

	// Intro in ASCII
	m.last = &verb.Event{Position: floatPtr(0.5)}
	m.lastAt = time.Now()
	line, isInter, _, ok := m.lyricLine(60)
	if !ok || !isInter || line != ">  ..." {
		t.Errorf("got %q, want '>  ...'", line)
	}

	// Active in ASCII
	m.last.Position = floatPtr(2.0)
	m.lastAt = time.Now()
	line, isInter, _, ok = m.lyricLine(60)
	if !ok || isInter || !strings.HasPrefix(line, "> ") {
		t.Errorf("got %q, want prefix '> '", line)
	}
}

func TestDetailLinesRowBudget(t *testing.T) {
	m := &Model{
		g:          glyphsUTF,
		w:          newWidth(false),
		playerID:   "p1",
		playURL:    "https://music.163.com/song?id=123",
		playEngine: "ne",
		last: &verb.Event{
			Ready:    true,
			Position: floatPtr(5.0),
			Duration: floatPtr(200.0),
			Media: &verb.Media{
				AudioCodec:   strPtr("flac"),
				SampleRate:   intPtr(44100),
				AudioBitrate: intPtr(920000),
				Channels:     strPtr("stereo"),
			},
		},
		lastAt: time.Now(),
		rows: []row{
			{
				Engine:   "ne",
				URL:      "https://music.163.com/song?id=123",
				ID:       "123",
				Title:    "Test Song",
				Desc:     "Line 1 Description\nLine 2 Description",
				Duration: floatPtr(200.0),
			},
			{
				Engine:   "ne",
				URL:      "https://music.163.com/song?id=456",
				ID:       "456",
				Title:    "Another Song",
				Desc:     "Another Description",
				Duration: floatPtr(180.0),
			},
		},
		cursor: 0,
	}

	// Case 1: playing row with NO lyric ready -> Meta + Media + Desc (max 2 lines)
	d1, lyricIdx1, _, _ := m.detailLines(80, 0)
	if lyricIdx1 != -1 {
		t.Errorf("expected no lyric, got lyricIdx=%d", lyricIdx1)
	}
	// Meta (1) + Media (1) + Desc (up to 2) = 4 lines max
	if len(d1) > 4 {
		t.Errorf("len(d1)=%d exceeds budget 4", len(d1))
	}

	// Case 2: playing row WITH lyric ready -> Meta + Media + Lyric (Desc replaced!)
	m.currentLyric = &lyricTrack{
		url:    "https://music.163.com/song?id=123",
		status: lyricReady,
		segments: []verb.Segment{
			{Start: 2.0, Duration: 10.0, Text: "Now Playing Lyric"},
		},
	}
	d2, lyricIdx2, _, _ := m.detailLines(80, 0)
	if lyricIdx2 < 0 {
		t.Errorf("expected lyric, got %d", lyricIdx2)
	}
	if len(d2) != 3 { // Meta + Media + Lyric
		t.Errorf("len(d2)=%d, want exactly 3 (Meta+Media+Lyric)", len(d2))
	}
	if !strings.Contains(d2[lyricIdx2], "Now Playing Lyric") {
		t.Errorf("lyric line %q does not contain lyric text", d2[lyricIdx2])
	}

	// Case 3: cursor on NON-playing row -> Meta + Desc (lyricIdx must be -1)
	d3, lyricIdx3, _, _ := m.detailLines(80, 1)
	if lyricIdx3 != -1 {
		t.Errorf("non-playing row should not have lyric, got %d", lyricIdx3)
	}
	if !strings.Contains(d3[len(d3)-1], "Another Description") {
		t.Errorf("expected non-playing row description, got %+v", d3)
	}
}

func floatPtr(v float64) *float64 { return &v }
func strPtr(s string) *string     { return &s }
func intPtr(i int) *int           { return &i }

func TestRealLyricPeekingNe(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	shell, err := filepath.Abs("../../shell")
	if err != nil {
		t.Fatal(err)
	}
	s, err := verb.LocateIn(shell)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	m := New(ctx, s, Options{
		Engines: []verb.Engine{
			{Name: "ne", Flags: []string{"--search", "--transcript"}},
		},
		Engine: 0,
	})

	url := "https://music.163.com/song?id=1824020871"
	cmd := m.triggerLyricCmd("ne", url)
	if cmd == nil {
		t.Fatal("expected non-nil cmd for ne")
	}
	msg := cmd()
	done, ok := msg.(lyricDoneMsg)
	if !ok {
		t.Fatalf("expected lyricDoneMsg, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("unexpected error fetching transcript: %v", done.err)
	}
	if len(done.segments) == 0 {
		t.Fatal("expected segments, got 0")
	}

	m.Update(done)

	m.playerID = "p1"
	m.playURL = url
	m.playEngine = "ne"
	m.last = &verb.Event{
		Ready:    true,
		Position: floatPtr(25.0),
		Duration: floatPtr(230.0),
	}
	m.lastAt = time.Now()
	m.rows = []row{
		{Engine: "ne", URL: url, Title: "One Last Kiss", ID: "1824020871"},
	}
	m.cursor = 0

	lines, lyricIdx, isInter, _ := m.detailLines(80, 0)
	if lyricIdx < 0 {
		t.Fatalf("expected lyric line in details, got index %d", lyricIdx)
	}
	if isInter {
		t.Errorf("did not expect interlude at 25s")
	}
	lyricText := lines[lyricIdx]
	if !strings.Contains(lyricText, "私だけのモナリザ") {
		t.Errorf("expected lyric text '私だけのモナリザ', got %q", lyricText)
	}
}

func TestRealLyricPeekingInstrumental(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	shell, err := filepath.Abs("../../shell")
	if err != nil {
		t.Fatal(err)
	}
	s, err := verb.LocateIn(shell)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	m := New(ctx, s, Options{
		Engines: []verb.Engine{
			{Name: "ne", Flags: []string{"--search", "--transcript"}},
		},
		Engine: 0,
	})

	url := "https://music.163.com/song?id=139774"
	cmd := m.triggerLyricCmd("ne", url)
	if cmd == nil {
		t.Fatal("expected non-nil cmd for ne")
	}
	msg := cmd()
	done, ok := msg.(lyricDoneMsg)
	if !ok {
		t.Fatalf("expected lyricDoneMsg, got %T", msg)
	}
	// Instrumental returns error
	if done.err == nil {
		t.Errorf("expected instrumental error, got nil")
	}

	m.Update(done)

	m.playerID = "p1"
	m.playURL = url
	m.playEngine = "ne"
	m.last = &verb.Event{
		Ready:    true,
		Position: floatPtr(10.0),
		Duration: floatPtr(223.0),
	}
	m.lastAt = time.Now()
	m.rows = []row{
		{Engine: "ne", URL: url, Title: "The truth that you leave", ID: "139774", Desc: "Instrumental Piano"},
	}
	m.cursor = 0

	lines, lyricIdx, _, _ := m.detailLines(80, 0)
	if lyricIdx != -1 {
		t.Fatalf("expected fallback to description, got lyricIdx=%d", lyricIdx)
	}
	if !strings.Contains(lines[len(lines)-1], "Instrumental Piano") {
		t.Errorf("expected description 'Instrumental Piano', got %+v", lines)
	}
}

func TestRealLyricPeekingBili(t *testing.T) {
	shell, err := filepath.Abs("../../shell")
	if err != nil {
		t.Fatal(err)
	}
	s, err := verb.LocateIn(shell)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	m := New(ctx, s, Options{
		Engines: []verb.Engine{
			{Name: "bili", Flags: []string{"--search", "--items"}},
		},
		Engine: 0,
	})

	url := "https://www.bilibili.com/video/BV1xx"
	cmd := m.triggerLyricCmd("bili", url)
	if cmd != nil {
		t.Fatal("expected nil cmd for engine without --transcript")
	}

	m.playerID = "p1"
	m.playURL = url
	m.playEngine = "bili"
	m.last = &verb.Event{
		Ready:    true,
		Position: floatPtr(10.0),
		Duration: floatPtr(100.0),
	}
	m.lastAt = time.Now()
	m.rows = []row{
		{Engine: "bili", URL: url, Title: "Bili Video", ID: "BV1xx", Desc: "Video Description"},
	}
	m.cursor = 0

	lines, lyricIdx, _, _ := m.detailLines(80, 0)
	if lyricIdx != -1 {
		t.Fatalf("expected fallback to description for bili, got lyricIdx=%d", lyricIdx)
	}
	if !strings.Contains(lines[len(lines)-1], "Video Description") {
		t.Errorf("expected description 'Video Description', got %+v", lines)
	}
}
