package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/binlecode/ting/internal/verb"
	"github.com/charmbracelet/bubbles/textinput"
)

func TestFullTUIFrameVisual(t *testing.T) {
	pos := 25.5
	dur := 100.0
	ch := "stereo"
	sr := 44100
	br := 920000

	m := &Model{
		suite: &verb.Suite{},
		s:     strsZH,
		opt: Options{
			Engines: []verb.Engine{
				{Name: "ne", Flags: []string{"--search", "--transcript"}},
			},
			Lang:  "zh",
			Theme: "minimal",
		},
		g:          glyphsUTF,
		p:          paletteFor(true, "minimal", Background{}, true),
		w:          newWidth(false),
		width:      80,
		height:     24,
		query:      "周杰伦",
		src:        srcSearch,
		playerID:   "p123",
		playURL:    "https://music.163.com/song?id=1824020871",
		playEngine: "ne",
		playTitle:  "One Last Kiss",
		startedAt:  time.Now().Add(-25 * time.Second),
		lastAt:     time.Now(),
		last: &verb.Event{
			Ready:    true,
			Paused:   false,
			Position: &pos,
			Duration: &dur,
			Media: &verb.Media{
				AudioCodec:   strPtr("flac"),
				Channels:     &ch,
				SampleRate:   &sr,
				AudioBitrate: &br,
			},
		},
		rows: []row{
			{
				Engine:   "ne",
				URL:      "https://music.163.com/song?id=1824020871",
				Title:    "One Last Kiss (在新世纪福音战士剧场版中绽放)",
				Channel:  "宇多田光",
				Duration: floatPtr(230.0),
				Desc:     "这是长篇的曲目简介，在平时光标选中的时候展示",
			},
			{
				Engine:   "ne",
				URL:      "https://music.163.com/song?id=2",
				Title:    "第二首歌曲",
				Channel:  "艺术家",
				Duration: floatPtr(180.0),
				Desc:     "第二首歌的详细描述内容",
			},
		},
		all: []row{
			{Engine: "ne", URL: "https://music.163.com/song?id=1824020871"},
			{Engine: "ne", URL: "https://music.163.com/song?id=2"},
		},
		cursor: 0,
		currentLyric: &lyricTrack{
			url:    "https://music.163.com/song?id=1824020871",
			status: lyricReady,
			segments: []verb.Segment{
				{Start: 20.0, Duration: 4.0, Text: "初めてのルーブルは"},
				{Start: 24.0, Duration: 5.0, Text: "私だけのモナリザ"},
				{Start: 30.0, Duration: 4.0, Text: "もうとっくに出会ってたから"},
			},
		},
		lyricActiveIdx: 1,
	}

	// 1. Verify frame when cursor is on playing track:
	// Should show sub-cell progress bar AND lyric peeking line!
	frame1 := m.View()
	t.Logf("=== TUI Frame 1 (Focus on Playing Row, 25.5%% Progress, Synced Lyric) ===\n%s\n", frame1)

	// Assertions for frame 1:
	if !strings.Contains(frame1, "One Last Kiss") {
		t.Errorf("frame1 missing song title")
	}
	// Sub-cell progress bar should contain block element
	if !strings.Contains(frame1, "█") {
		t.Errorf("frame1 missing progress bar full block")
	}
	// Lyric peeking should contain the active lyric line
	if !strings.Contains(frame1, "私だけのモナリザ") {
		t.Errorf("frame1 missing active lyric '私だけのモナリザ'")
	}
	if !strings.Contains(frame1, "♪ ") {
		t.Errorf("frame1 missing lyric icon '♪ '")
	}
	// Lyric should replace description
	if strings.Contains(frame1, "这是长篇的曲目简介") {
		t.Errorf("frame1 should NOT contain Description when lyric is peeking")
	}

	// 2. Move cursor to row 1 (Non-playing row):
	// Should show description of row 1, NOT lyrics!
	m.cursor = 1
	frame2 := m.View()
	t.Logf("=== TUI Frame 2 (Cursor moved to Row 1 - Non-playing, Description restored) ===\n%s\n", frame2)

	if !strings.Contains(frame2, "第二首歌的详细描述内容") {
		t.Errorf("frame2 missing Description for non-playing row")
	}
	if strings.Contains(frame2, "私だけのモナリザ") {
		t.Errorf("frame2 should NOT contain lyric when cursor is on non-playing row")
	}

	// 3. Move cursor back to playing row, but test Interlude (pos = 29.5s in [29.0, 30.0]):
	m.cursor = 0
	pos = 29.5
	frame3 := m.View()
	t.Logf("=== TUI Frame 3 (Interlude State: ♪  · · ·) ===\n%s\n", frame3)

	if !strings.Contains(frame3, "♪  · · ·") {
		t.Errorf("frame3 missing interlude indicator '♪  · · ·'")
	}

	// 4. Test ASCII mode:
	m.opt.ASCII = true
	m.g = glyphsASCII
	frame4 := m.View()
	t.Logf("=== TUI Frame 4 (ASCII Mode Fallback: '=' / '-' / '>  ...') ===\n%s\n", frame4)

	if !strings.Contains(frame4, ">  ...") {
		t.Errorf("frame4 missing ASCII interlude '>  ...'")
	}
	if !strings.Contains(frame4, "===") {
		t.Errorf("frame4 missing ASCII progress bar '='")
	}
}

func TestRemotePlaylistsFrameVisual(t *testing.T) {
	m := &Model{
		suite: &verb.Suite{},
		s:     strsZH,
		opt: Options{
			Engines: []verb.Engine{
				{Name: "yt", Flags: []string{"--search", "--playlists"}},
			},
			Lang:  "zh",
			Theme: "minimal",
			Keys:  "full",
		},
		g:         glyphsUTF,
		p:         paletteFor(true, "minimal", Background{}, true),
		w:         newWidth(false),
		width:     80,
		height:    24,
		query:     "lofi",
		src:       srcSearch,
		prompting: true,
		askKind:   askRemoteOpen,
		askLabel:  strsZH.RemotePLPrompt,
		askHead:   strsZH.RemotePLAct,
		pick: []verb.Playlist{
			{Name: "Liked videos", Count: 12},
			{Name: "Watch later", Count: 5},
		},
		remotePick: []verb.RemotePlaylist{
			{ID: "LL", Title: "Liked videos", URL: "https://www.youtube.com/playlist?list=LL"},
			{ID: "WL", Title: "Watch later", URL: "https://www.youtube.com/playlist?list=WL"},
		},
		rows: []row{
			{Engine: "yt", Title: "Song 1", Duration: floatPtr(120.0)},
		},
	}
	m.all = m.rows
	m.input = textinput.New()
	m.input.Focus()

	frame := m.View()
	t.Logf("=== TUI Frame Remote Playlists Picker ===\n%s\n", frame)

	if !strings.Contains(frame, "在线歌单") {
		t.Errorf("frame missing picker title '在线歌单'")
	}
	if !strings.Contains(frame, "Liked videos") || !strings.Contains(frame, "Watch later") {
		t.Errorf("frame missing playlist names in picker")
	}
	if !strings.Contains(frame, "12 首") || !strings.Contains(frame, "5 首") {
		t.Errorf("frame missing playlist counts in picker")
	}
	if !strings.Contains(frame, "打开哪个在线歌单") {
		t.Errorf("frame missing prompt label '打开哪个在线歌单'")
	}
}

func TestDetailLinesChapterPrefixWithChannel(t *testing.T) {
	dur := 180.0
	m := &Model{
		src:   srcChapters,
		total: 5,
		s:     strsZH,
		g:     glyphsUTF,
		w:     newWidth(false),
		rows: []row{
			{Title: "Chapter 2", Channel: "Artist Name", Duration: &dur, N: 2, ID: "ch2"},
		},
	}
	d, _, _, _, _ := m.detailLines(80, 0)
	if len(d) == 0 {
		t.Fatalf("expected details lines")
	}
	meta := d[0]
	if !strings.Contains(meta, "章节 2/5") {
		t.Errorf("meta line should contain '章节 2/5', got %q", meta)
	}
	if !strings.Contains(meta, "Artist Name") {
		t.Errorf("meta line should contain channel 'Artist Name', got %q", meta)
	}
}
