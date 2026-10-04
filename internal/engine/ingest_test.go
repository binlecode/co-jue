package engine

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestParseTime(t *testing.T) {
	good := map[string]float64{
		"0":       0,
		"845.5":   845.5,
		" 12 ":    12,
		"14:05":   845,
		"1:02:03": 3723,
		"0:00.25": 0.25,
	}
	for in, want := range good {
		got, err := ParseTime(in)
		if err != nil || got != want {
			t.Errorf("ParseTime(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	// NaN and Inf parse as floats; they are no position, and NaN would fail every comparison.
	for _, in := range []string{"", "-1", "abc", "1:xx", "1::2", "1:-2", "NaN", "nan", "inf", "+Inf", "1:nan", "1e400"} {
		if got, err := ParseTime(in); err == nil {
			t.Errorf("ParseTime(%q) = %v, want an error", in, got)
		}
	}
}

func TestParseRange(t *testing.T) {
	good := []struct {
		in         string
		start, end float64
	}{
		{"600-900", 600, 900},
		{"10:00-15:00", 600, 900},
		{"1:02:03-1:02:10", 3723, 3730},
		{"9:59-600.5", 599, 600.5}, // the two sides may mix forms
		{"0-0.5", 0, 0.5},
	}
	for _, c := range good {
		s, e, err := ParseRange(c.in)
		if err != nil || s != c.start || e != c.end {
			t.Errorf("ParseRange(%q) = %v, %v, %v; want %v, %v", c.in, s, e, err, c.start, c.end)
		}
	}
	// Empty and reversed windows select nothing and are a usage error, not an empty result.
	for _, in := range []string{"900-600", "600-600", "600", "600-", "-900", "a-b", "10:00-9:00", "-5-10", "", "nan-10", "0-inf"} {
		if s, e, err := ParseRange(in); err == nil {
			t.Errorf("ParseRange(%q) = %v, %v; want an error", in, s, e)
		}
	}
}

func TestParseJSON3(t *testing.T) {
	body := `{"events":[
		{"tStartMs":0,"dDurationMs":5000},
		{"tStartMs":1000,"dDurationMs":3000,"segs":[{"utf8":"hello "},{"utf8":"world"}]},
		{"tStartMs":4000,"dDurationMs":1000,"segs":[{"utf8":"\n"}]},
		{"tStartMs":4500,"dDurationMs":2000,"segs":[{"utf8":"hello world"}]},
		{"tStartMs":6000,"dDurationMs":4000,"segs":[{"utf8":"second​\nline\t "}]},
		{"tStartMs":8000,"dDurationMs":1000,"segs":[{"utf8":"third"}]},
		{"tStartMs":20000,"dDurationMs":1000,"segs":[{"utf8":"third"}]}
	]}`
	got, err := parseJSON3([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	want := []Cue{
		// The 4.5s repeat of a line still showing is folded into the first cue, which now ends
		// where the next line begins (6.0), not where the repeat ended (6.5).
		{Start: 1, End: 6, Text: "hello world"},
		// Newline, tab and zero-width space are gone; the overlap into "third" is trimmed.
		{Start: 6, End: 8, Text: "second line"},
		{Start: 8, End: 9, Text: "third"},
		// The same text 11s later is said again, not a rolling repeat.
		{Start: 20, End: 21, Text: "third"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseJSON3:\n got %+v\nwant %+v", got, want)
	}

	// Millisecond fields come back as seconds rounded to the millisecond.
	got, _ = parseJSON3([]byte(`{"events":[{"tStartMs":160959.4,"dDurationMs":2560,"segs":[{"utf8":"x"}]}]}`))
	if len(got) != 1 || got[0].Start != 160.959 || got[0].End != 163.519 {
		t.Errorf("ms rounding: got %+v", got)
	}

	var f *Fail
	if _, err := parseJSON3([]byte("<html>")); !errors.As(err, &f) || f.Code != 2 {
		t.Errorf("unreadable track: err = %v, want a Fail with code 2", err)
	}
}

func TestParseLRC(t *testing.T) {
	lrc := "[ar:Artist]\n" + // metadata tag, not a time
		"[00:20.542]初めてのルーブルは\n" +
		"[00:22.56][01:10.00]chorus\n" + // one line, two times
		"[00:24.5]\n" + // empty tagged line: only ends the line before
		"[00:40:25]after a long gap\n" + // colon before the fraction
		"[00:45]last"
	want := []Cue{
		{Start: 20.542, End: 22.56, Text: "初めてのルーブルは"},
		{Start: 22.56, End: 24.5, Text: "chorus"},
		{Start: 40.25, End: 45, Text: "after a long gap"},
		// 70 is 25s on: across an instrumental gap the line is capped at 6s.
		{Start: 45, End: 51, Text: "last"},
		// The repeat of a multi-tag line is sorted into place; the last cue is a point.
		{Start: 70, End: 70, Text: "chorus"},
	}
	if got := parseLRC(lrc); !reflect.DeepEqual(got, want) {
		t.Errorf("parseLRC:\n got %+v\nwant %+v", got, want)
	}

	// The gap cap starts past 8s: exactly 8 is kept as is.
	got := parseLRC("[00:00.00]a\n[00:08.00]b\n[00:17.00]c")
	if got[0].End != 8 || got[1].End != 14 {
		t.Errorf("gap cap: got %+v, want a ending at 8 and b capped to 14", got)
	}
	if got := parseLRC("no tags here\n[ti:Title]"); len(got) != 0 {
		t.Errorf("untimed lyric: got %+v, want no cues", got)
	}
}

func TestNeteaseID(t *testing.T) {
	cases := map[string]string{
		"https://music.163.com/song?id=1824020871":         "1824020871",
		"https://music.163.com/#/song?id=1824020871":       "1824020871",
		"https://y.music.163.com/m/song?app=x&id=42":       "42",
		"https://music.163.com/song/1824020871/?userid=99": "1824020871",
		"https://www.youtube.com/watch?id=1824020871":      "",
		"https://music.163.com.evil.example/song?id=1":     "",
		"https://music.163.com/playlist?pid=5":             "",
		// A playlist or album carries an id= too: it is not a song.
		"https://music.163.com/playlist?id=5":   "",
		"https://music.163.com/#/playlist?id=5": "",
		"https://music.163.com/#/album?id=5":    "",
		"https://music.163.com/album/5":         "",
		"https://music.163.com/song?id=abc":     "",
	}
	for in, want := range cases {
		if got := neteaseID(in); got != want {
			t.Errorf("neteaseID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPickTrack(t *testing.T) {
	tracks := func(keys ...string) map[string]json.RawMessage {
		m := map[string]json.RawMessage{}
		for _, k := range keys {
			m[k] = json.RawMessage(`[]`)
		}
		return m
	}
	cases := []struct {
		name         string
		lang         string
		manual, auto []string
		want         string
		wantAuto     bool
	}{
		{"human track in the media's language wins", "en", []string{"fr", "en"}, []string{"en-orig", "en"}, "en", false},
		{"regional variant of the language", "en", []string{"en-US"}, nil, "en-US", false},
		{"human translation loses to ASR of the original", "en", []string{"fr"}, []string{"de", "en-orig", "en"}, "en-orig", true},
		{"plain ASR in the media's language", "en", nil, []string{"de", "en"}, "en", true},
		{"machine translation only: nothing", "en", []string{"fr"}, []string{"de", "fr"}, "", false},
		{"language unknown: English first", "", []string{"de", "zh-Hans", "en-GB"}, nil, "en-GB", false},
		{"language unknown: Chinese next", "", []string{"de", "zh-Hans"}, nil, "zh-Hans", false},
		{"language unknown: first in sort order", "", []string{"ko", "de"}, nil, "de", false},
		{"language unknown: -orig ASR still beats a human guess", "", []string{"en"}, []string{"ja-orig"}, "ja-orig", true},
		{"live chat is not a subtitle", "", []string{"live_chat"}, nil, "", false},
		{"danmaku is not a subtitle", "", []string{"danmaku", "ja"}, nil, "ja", false},
		{"nothing at all", "zh", nil, nil, "", false},
	}
	for _, c := range cases {
		info := &rawInfo{Language: c.lang, Subtitles: tracks(c.manual...), AutoCaps: tracks(c.auto...)}
		got, auto := pickTrack(info)
		if got != c.want || auto != c.wantAuto {
			t.Errorf("%s: pickTrack = %q, %v; want %q, %v", c.name, got, auto, c.want, c.wantAuto)
		}
	}
}

func TestYtdlpFail(t *testing.T) {
	cases := []struct {
		stderr string
		code   int
	}{
		{"ERROR: [generic] 'notaurl' is not a valid URL", 1},
		{"ERROR: Unsupported URL: https://example.com/", 1},
		{"ERROR: [youtube] x: Video unavailable", 2},
		{"ERROR: Unable to download webpage: <urlopen error [Errno 8]>", 2},
	}
	for _, c := range cases {
		if f := ytdlpFail("WARNING: noise\n"+c.stderr+"\n", errors.New("exit status 1")); f.Code != c.code || f.Msg != "yt-dlp: "+c.stderr {
			t.Errorf("ytdlpFail(%q) = %d %q, want %d", c.stderr, f.Code, f.Msg, c.code)
		}
	}
}

func TestClip(t *testing.T) {
	var cues []Cue
	for i := 0; i < MaxCues+50; i++ {
		cues = append(cues, Cue{Start: float64(i), End: float64(i + 1), Text: "x"})
	}
	cases := []struct {
		name       string
		start, end float64
		hasRange   bool
		n          int
		truncated  bool
	}{
		{"no range: capped", 0, 0, false, MaxCues, true},
		// A window wider than the whole talk must not slip past the cap.
		{"wide range: capped too", 0, 1e6, true, MaxCues, true},
		{"narrow range: whole window", 10, 20, true, 10, false},
		{"range past the end: empty, not nil", 1e5, 1e6, true, 0, false},
	}
	for _, c := range cases {
		got, tr := clip(cues, c.start, c.end, c.hasRange)
		if len(got) != c.n || tr != c.truncated || got == nil {
			t.Errorf("%s: %d cues, truncated %v; want %d, %v", c.name, len(got), tr, c.n, c.truncated)
		}
	}
	if got, tr := clip(cues[:5], 0, 0, false); len(got) != 5 || tr {
		t.Errorf("short transcript: %d cues, truncated %v; want 5, false", len(got), tr)
	}
}
