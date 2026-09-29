package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/binlecode/ting/internal/verb"
)

func int64Ptr(i int64) *int64 { return &i }

func makeTestLofiModel(cols, rows int, lang string, ascii bool) *Model {
	pos := 7.0
	dur := 22257.0
	ch := "stereo"
	sr := 48000
	br := 139000

	m := &Model{
		suite: &verb.Suite{},
		s:     strsZH,
		opt: Options{
			Engines: []verb.Engine{
				{Name: "yt", Flags: []string{"--search", "--transcript", "--feed"}},
			},
			Lang:      lang,
			Theme:     "minimal",
			ASCII:     ascii,
			RowIndex:  false,
			ListMode:  "scroll",
			Resource:  true,
		},
		g:          glyphsASCII,
		p:          paletteFor(false, "minimal", Background{}, false),
		w:          newWidth(false),
		width:      cols,
		height:     rows,
		query:      "lofi hip hop",
		src:        srcSearch,
		playerID:   "p100",
		playURL:    "https://www.youtube.com/watch?v=n61ULEU7CO0",
		playEngine: "yt",
		playTitle:  "Best of lofi hip hop 2021 [beats to relax/study to]",
		startedAt:  time.Now().Add(-7 * time.Second),
		lastAt:     time.Now(),
		last: &verb.Event{
			Ready:    true,
			Paused:   false,
			Position: &pos,
			Duration: &dur,
			Media: &verb.Media{
				AudioCodec:   strPtr("opus"),
				Channels:     &ch,
				SampleRate:   &sr,
				AudioBitrate: &br,
			},
		},
		rows: []row{
			{ID: "n61ULEU7CO0", Views: int64Ptr(57696650), Engine: "yt", URL: "https://www.youtube.com/watch?v=n61ULEU7CO0", Title: "Best of lofi hip hop 2021 [beats to relax/study to]", Duration: floatPtr(22258.0), Channel: "Lofi Girl", Desc: "Listen on Spotify, Apple music and more https://fanlink.tv/BestofLofi2021 The new Lofi Girl compilation “Best of 2021” is out now ..."},
			{Engine: "yt", URL: "https://www.youtube.com/watch?v=jfKfPfyJRdk", Title: "lofi hip hop radio beats to relax/study to", Live: "live", Channel: "Lofi Girl"},
			{Engine: "yt", URL: "https://www.youtube.com/watch?v=r1", Title: "Ｎｉｇｈｔ Ｄｒｉｖｅ ~ lofi hip hop mix ~ beats to chill / drive to", Duration: floatPtr(88623.0), Channel: "Lofi Girl"},
			{Engine: "yt", URL: "https://www.youtube.com/watch?v=r2", Title: "90's Chill Lofi Study Music Lofi Rain Chillhop Beats Lofi Rain Playlist", Duration: floatPtr(42825.0), Channel: "Lofi Girl"},
			{Engine: "yt", URL: "https://www.youtube.com/watch?v=r3", Title: "1 A.M Study Session [lofi hip hop]", Duration: floatPtr(3674.0), Channel: "Lofi Girl"},
			{Engine: "yt", URL: "https://www.youtube.com/watch?v=r4", Title: "90's Chill Lofi Chill Music Lofi Rain Hip Hop Beats Lofi Rain Playlist", Duration: floatPtr(6235.0), Channel: "Lofi Girl"},
			{Engine: "yt", URL: "https://www.youtube.com/watch?v=r5", Title: "remember when lofi hip-hop was chill like this.", Duration: floatPtr(3621.0), Channel: "Lofi Girl"},
			{Engine: "yt", URL: "https://www.youtube.com/watch?v=r6", Title: "Work Lofi - R&B That Sparks a Mood [rnb , lofi hiphop]", Duration: floatPtr(12184.0), Channel: "Lofi Girl"},
			{Engine: "yt", URL: "https://www.youtube.com/watch?v=r7", Title: "Chill Lofi Mix [chill lo-fi hip hop beats]", Duration: floatPtr(6292.0), Channel: "Lofi Girl"},
			{Engine: "yt", URL: "https://www.youtube.com/watch?v=r8", Title: "Chill Study Beats 4 • jazz & lofi hiphop Mix [2017]", Duration: floatPtr(7275.0), Channel: "Lofi Girl"},
			{Engine: "yt", URL: "https://www.youtube.com/watch?v=r9", Title: "remember when lofi hip-hop was smooth like this.", Duration: floatPtr(3722.0), Channel: "Lofi Girl"},
			{Engine: "yt", URL: "https://www.youtube.com/watch?v=r10", Title: "Upbeat Lofi Mix Beats to Boost Your Energy & Focus", Duration: floatPtr(16073.0), Channel: "Lofi Girl"},
			{Engine: "yt", URL: "https://www.youtube.com/watch?v=r11", Title: "lofi hip hop mix beats to relax/study to (Part 1)", Duration: floatPtr(10241.0), Channel: "Lofi Girl"},
			{Engine: "yt", URL: "https://www.youtube.com/watch?v=r12", Title: "𝐏𝐥ａｙｌｉｓｔ Tokyo Lo-fi Hiphop Chill Beats for Study & Relax", Duration: floatPtr(10808.0), Channel: "Lofi Girl"},
		},
		cursor: 0,
		dock:   newDock(),
		navbar: newNavbar(),
		stage:  newStage(),
		inspector: newInspector(),
	}
	if lang == "en" {
		m.s = strsEN
	}
	if !ascii {
		m.g = glyphsUTF
		m.p = paletteFor(true, "minimal", Background{}, true)
	}
	m.all = m.rows
	cpu, mem := 32.0, 102.0
	m.cpu = &cpu
	m.mem = &mem
	m.auth = "in"
	return m
}

func TestRenderDocPanes(t *testing.T) {
	tmpDir := filepath.Join("..", "..", "tmp")
	_ = os.MkdirAll(tmpDir, 0755)

	// 1. main-dock-100 (100x26, ASCII, ZH)
	m1 := makeTestLofiModel(100, 26, "zh", true)
	f1 := m1.View()
	_ = os.WriteFile(filepath.Join(tmpDir, "main-dock-100.raw.txt"), []byte(f1+"\n"), 0644)

	// 2. stage-mode-100 (100x26, ASCII, ZH)
	m2 := makeTestLofiModel(100, 26, "zh", true)
	m2.stageMode = true
	f2 := m2.View()
	_ = os.WriteFile(filepath.Join(tmpDir, "stage-mode-100.raw.txt"), []byte(f2+"\n"), 0644)

	// 3. queue-100 (100x26, ASCII, ZH)
	m3 := makeTestLofiModel(100, 26, "zh", true)
	m3.src = srcQueue
	m3.label = "待播队列"
	m3.noticeL = "队列:"
	m3.noticeT = "已加入队列 -> lofi hip hop radio beats to relax/study to"
	m3.undoHead = "队列:"
	m3.undoLabel = "已加入队列 -> lofi hip hop radio beats to relax/study to"
	m3.undoEnd = time.Now().Add(3 * time.Second)
	m3.rows = []row{
		{ID: "n61ULEU7CO0", Views: int64Ptr(57696650), Engine: "yt", URL: "https://www.youtube.com/watch?v=n61ULEU7CO0", Title: "Best of lofi hip hop 2021 [beats to relax/study to]", Duration: floatPtr(22257.0), Channel: "Lofi Girl"},
		{ID: "jfKfPfyJRdk", Engine: "yt", URL: "https://www.youtube.com/watch?v=jfKfPfyJRdk", Title: "lofi hip hop radio beats to relax/study to", Duration: floatPtr(0.0), Channel: "Lofi Girl"},
	}
	m3.all = m3.rows
	m3.cursor = 0
	f3 := m3.View()
	_ = os.WriteFile(filepath.Join(tmpDir, "queue-100.raw.txt"), []byte(f3+"\n"), 0644)

	// 4. compact-62 (62x20, ASCII, ZH)
	m4 := makeTestLofiModel(62, 20, "zh", true)
	cpu4, mem4 := 1.0, 116.0
	m4.cpu = &cpu4
	m4.mem = &mem4
	m4.cursor = 0
	f4 := m4.View()
	_ = os.WriteFile(filepath.Join(tmpDir, "compact-62.raw.txt"), []byte(f4+"\n"), 0644)

	// 5. wide-130 (130x26, ASCII, ZH)
	m5 := makeTestLofiModel(130, 26, "zh", true)
	m5.currentLyric = &lyricTrack{
		url:    m5.playURL,
		status: lyricReady,
		segments: []verb.Segment{
			{Start: 0.0, Duration: 5.0, Text: "Rain falling on the roof"},
			{Start: 5.0, Duration: 6.0, Text: "Neon lights blur in mist"},
			{Start: 12.0, Duration: 4.0, Text: "Coffee aroma in the room"},
		},
	}
	m5.lyricActiveIdx = 1
	f5 := m5.View()
	_ = os.WriteFile(filepath.Join(tmpDir, "wide-130.raw.txt"), []byte(f5+"\n"), 0644)
}
