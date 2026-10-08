package engine

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
		"1e8":     1e8,
	}
	for in, want := range good {
		got, err := ParseTime(in)
		if err != nil || got != want {
			t.Errorf("ParseTime(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	// NaN and Inf parse as floats; they are no position, and NaN would fail every comparison.
	// Values > 1e8 exceed cumulative time bounds.
	for _, in := range []string{"", "-1", "abc", "1:xx", "1::2", "1:-2", "NaN", "nan", "inf", "+Inf", "1:nan", "1e400", "1e9", "100000001", "100000000.1", "1666667:00"} {
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
		{"yt-dlp: error: unsupported browser specified for cookies: \"nosuch\"", 1},
		{"ERROR: could not find chrome cookies database in \"/x\"", 2},
		{"ERROR: failed to load cookies", 2},
	}
	for _, c := range cases {
		if f := ytdlpFail("WARNING: noise\n"+c.stderr+"\n", errors.New("exit status 1")); f.Code != c.code || !strings.HasPrefix(f.Msg, "yt-dlp: ") || !strings.Contains(f.Msg, c.stderr) {
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

func TestNormalizeForYtdlp(t *testing.T) {
	pass := map[string]string{
		"ytsearch1:周杰伦 晴天":          "ytsearch1:周杰伦 晴天",
		"ytsearch:foo":              "ytsearch:foo",
		"ytdl://ytsearch1:foo":      "ytsearch1:foo",
		"ytdl://https://youtu.be/x": "https://youtu.be/x",
		// "search" past the head is a plain URL, not a search prefix.
		"https://www.youtube.com/results?search_query=foo": "https://www.youtube.com/results?search_query=foo",
		"/tmp/research.wav": "/tmp/research.wav",
	}
	for in, want := range pass {
		if got, err := normalizeForYtdlp(in); err != nil || got != want {
			t.Errorf("normalizeForYtdlp(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"scsearch:foo", "ytsearch5:foo", "ytsearchall:foo", "ytdl://scsearch1:foo", "bilisearch:foo"} {
		var f *Fail
		if got, err := normalizeForYtdlp(in); !errors.As(err, &f) || f.Code != 1 {
			t.Errorf("normalizeForYtdlp(%q) = %q, %v; want exit 1", in, got, err)
		}
	}
}

func TestDecodeDumpSearch(t *testing.T) {
	hit := `{"id":"dQw4w9WgXcQ","title":"Never Gonna Give You Up","language":"en","subtitles":{"en":[]}}`
	raw, info, err := decodeDump("ytsearch1:rick", []byte(`{"_type":"playlist","id":"rick","title":"rick","entries":[`+hit+`]}`))
	if err != nil || info.ID != "dQw4w9WgXcQ" || info.Language != "en" || string(raw) != hit {
		t.Errorf("search hit = %s, %+v, %v; want the entry's own bytes and fields", raw, info, err)
	}

	var f *Fail
	if _, _, err := decodeDump("ytsearch:nothing", []byte(`{"_type":"playlist","entries":[]}`)); !errors.As(err, &f) || f.Code != 4 || f.Status != "unavailable" {
		t.Errorf("empty search err = %v, want exit 4 unavailable", err)
	}

	// A playlist URL is not a search: its record is returned as yt-dlp gave it.
	list := `{"_type":"playlist","id":"PL1","title":"mix","entries":[` + hit + `]}`
	raw, info, err = decodeDump("https://www.youtube.com/playlist?list=PL1", []byte(list))
	if err != nil || info.ID != "PL1" || string(raw) != list {
		t.Errorf("playlist URL = %s, %+v, %v; want it untouched", raw, info, err)
	}
}

func TestYtdlpFailTraceback(t *testing.T) {
	// A locked store: the reason is the ERROR line, not the traceback frames printed after it.
	stderr := "Extracting cookies from chrome\nERROR: failed to load cookies\nTraceback (most recent call last):\n  File \"cookies.py\", line 164\nsqlite3.OperationalError: database is locked\n"
	f := ytdlpFail(stderr, errors.New("exit status 1"))
	if f.Code != 2 || !strings.Contains(f.Msg, "ERROR: failed to load cookies") || !strings.Contains(f.Msg, "JUE_COOKIES_FROM_BROWSER") || strings.Contains(f.Msg, "Traceback") {
		t.Errorf("locked cookie store = %d %q", f.Code, f.Msg)
	}
}

func TestCookieArgs(t *testing.T) {
	cases := map[string][]string{
		"":                 {"--cookies-from-browser", "chrome"},
		"  ":               {"--cookies-from-browser", "chrome"},
		"firefox":          {"--cookies-from-browser", "firefox"},
		"chrome:Profile 1": {"--cookies-from-browser", "chrome:Profile 1"},
		"none":             nil,
		"OFF":              nil,
	}
	for env, want := range cases {
		t.Setenv("JUE_COOKIES_FROM_BROWSER", env)
		if got := cookieArgs(); !reflect.DeepEqual(got, want) {
			t.Errorf("JUE_COOKIES_FROM_BROWSER=%q: %q, want %q", env, got, want)
		}
	}
}

func TestParseVTT(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []Cue
	}{
		{"plain, header metadata and cue ids", "\uFEFFWEBVTT\nKind: captions\nLanguage: en\n\n1\n00:00:01.000 --> 00:00:02.500\nHello world\n\nintro\n00:02.500 --> 00:04.000 align:start position:10%\nSecond line\n",
			[]Cue{{1, 2.5, "Hello world"}, {2.5, 4, "Second line"}}},
		{"karaoke, voice and class tags", "WEBVTT\n\n00:00:01.000 --> 00:00:03.000\n<v Lex Fridman>So<00:00:01.200><c> what</c><00:00:01.500><c> is</c> <b>AGI</b>?</v>\n",
			[]Cue{{1, 3, "So what is AGI?"}}},
		{"STYLE, NOTE and REGION blocks are skipped", "WEBVTT\n\nSTYLE\n::cue { color: red }\n\nNOTE a comment\nspanning --> lines? no\n\nREGION\nid:r1\n\n00:00:05.000 --> 00:00:06.000\nkept\n",
			[]Cue{{5, 6, "kept"}}},
		{"timing line inside NOTE is no cue", "WEBVTT\n\nNOTE draft\n00:00:01.000 --> 00:00:02.000\nmachine guess\n\n00:00:05.000 --> 00:00:06.000\nkept\n",
			[]Cue{{5, 6, "kept"}}},
		{"multi-line joined, whitespace folded, entities unescaped", "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n  Tom &amp; Jerry  \n say &lt;hi&gt; &quot;now&quot; &#39;ok&#39;\n",
			[]Cue{{1, 2, `Tom & Jerry say <hi> "now" 'ok'`}}},
		{"rolling repeat merged, blank cue dropped", "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nsame\n\n00:00:02.000 --> 00:00:03.000\nsame\n\n00:00:03.000 --> 00:00:04.000\n<c> </c>\n\n00:00:04.000 --> 00:00:05.000\nnext\n",
			[]Cue{{1, 3, "same"}, {4, 5, "next"}}},
		{"CRLF, runs of blank lines, no trailing newline, hours", "WEBVTT\r\n\r\n\r\n\r\n01:00:00.000 --> 01:00:01.250\r\nlate\r\n\r\n\r\n01:00:02.000 --> 01:00:03.000\r\nlater",
			[]Cue{{3600, 3601.25, "late"}, {3602, 3603, "later"}}},
		{"header runs straight into a cue, cues with no blank between", "WEBVTT\n00:00:01.000 --> 00:00:02.000\na\n00:00:02.000 --> 00:00:03.000\nb\n",
			[]Cue{{1, 2, "a"}, {2, 3, "b"}}},
		{"unclosed < is text", "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n1 < 2\n",
			[]Cue{{1, 2, "1 < 2"}}},
		{"comparison operators are text, not a tag", "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nUse x < 10 and y > 5.\n",
			[]Cue{{1, 2, "Use x < 10 and y > 5."}}},
		{"cue id starting with NOTE is a cue, NOTE<TAB> is a comment", "WEBVTT\n\nNOTEBOOK-1\n00:00:01.000 --> 00:00:02.000\nkept\n\nNOTE\tdraft\n00:00:03.000 --> 00:00:04.000\ndropped\n\nNOTE\n00:00:05.000 --> 00:00:06.000\ndropped\n",
			[]Cue{{1, 2, "kept"}}},
	}
	for _, c := range cases {
		got, err := parseVTT([]byte(c.body))
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v, %v; want %+v", c.name, got, err, c.want)
		}
	}
	if got, err := parseVTT([]byte("WEBVTT\n\nNOTE nothing here\n")); err != nil || len(got) != 0 {
		t.Errorf("header-only file = %+v, %v; want no cues, no error", got, err)
	}
	var f *Fail
	if _, err := parseVTT([]byte("<html>404</html>")); !errors.As(err, &f) || f.Code != 2 {
		t.Errorf("non-VTT body err = %v, want exit 2", err)
	}
}

func TestStripTags(t *testing.T) {
	for in, want := range map[string]string{
		"<v Lex>So<00:00:01.200><c> what</c></v>": "So what",
		"<1:02:03.000>late<b>bold</b>":            "latebold",
		"Use x < 10 and y > 5.":                   "Use x < 10 and y > 5.",
		"a <5 and b> c":                           "a <5 and b> c",
		"a <123:4> b":                             "a <123:4> b",
		"x </ y >":                                "x </ y >",
		"1 < 2":                                   "1 < 2",
		"< <i>it</i>":                             "< it",
	} {
		if got := stripTags(in); got != want {
			t.Errorf("stripTags(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSRT(t *testing.T) {
	body := "1\r\n00:00:01,000 --> 00:00:02,500\r\n<i>Hello</i> <font color=\"#fff\">there</font>\r\n\r\n2\r\n00:00:03,000 --> 00:00:04,000\r\nline one\r\nline two\r\n\r\n3\r\n00:00:04,000 --> 00:00:05,000\r\n&amp;\r\n"
	want := []Cue{{1, 2.5, "Hello there"}, {3, 4, "line one line two"}, {4, 5, "&"}}
	if got, err := parseSRT([]byte(body)); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("parseSRT = %+v, %v; want %+v", got, err, want)
	}
	var f *Fail
	if _, err := parseSRT([]byte("not subtitles at all")); !errors.As(err, &f) || f.Code != 2 {
		t.Errorf("garbage err = %v, want exit 2", err)
	}
	if got, err := parseSRT(nil); err != nil || len(got) != 0 {
		t.Errorf("empty file = %+v, %v; want no cues", got, err)
	}
}

func TestParseTiming(t *testing.T) {
	good := map[string][2]float64{
		"00:00:01.000 --> 00:00:02.000":        {1, 2},
		"00:01,500 --> 00:02,250":              {1.5, 2.25},
		"1:02:03.004 --> 1:02:04.000 line:90%": {3723.004, 3724},
		"00:00:05.000 --> 00:00:04.000":        {5, 5}, // an end before the start is a point
	}
	for in, want := range good {
		if s, e, ok := parseTiming(in); !ok || s != want[0] || e != want[1] {
			t.Errorf("parseTiming(%q) = %v, %v, %v; want %v", in, s, e, ok, want)
		}
	}
	for _, in := range []string{"", "1", "hello --> world", "5 --> 6", "00:01.000 -->", "1:2:3:4.0 --> 00:01.000", "nan:00 --> 00:01.000"} {
		if s, e, ok := parseTiming(in); ok {
			t.Errorf("parseTiming(%q) = %v, %v; want rejected", in, s, e)
		}
	}
}

func TestIsFeedURL(t *testing.T) {
	for u, want := range map[string]bool{
		"https://changelog.com/practicalai/feed":        true,
		"https://www.dwarkesh.com/feed/":                true,
		"https://feeds.libsyn.com/12345/rss":            true,
		"https://example.com/podcast.xml":               true,
		"https://example.com/show.RSS":                  true,
		"https://feeds.transistor.fm/oxide-and-friends": true,
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":   false,
		"https://lexfridman.com/feed/podcast/":          true,
		"https://anchor.fm/s/abc/podcast/rss":           true,
		"https://example.com/feedback":                  false,
		"https://example.com/news/rss-explained":        false,
		"/tmp/feed.xml":                                 false,
		"ytsearch1:feed":                                false,
	} {
		if got := isFeedURL(u); got != want {
			t.Errorf("isFeedURL(%q) = %v, want %v", u, got, want)
		}
	}
}

const testFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:podcast="https://podcastindex.org/namespace/1.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
<channel><title>Show</title><language>en-us</language>
<item><title>Ep 2</title>
  <podcast:transcript url="/ep2.json" type="application/json"/>
  <podcast:transcript url="/ep2.srt" type="application/x-subrip" language="en"/>
  <podcast:transcript url="ep2.vtt" type="text/vtt" language="en" rel="captions"/>
</item>
<item><title>Ep 1</title><podcast:transcript url="/ep1.vtt" type="text/vtt"/></item>
</channel></rss>`

func feedServer(t *testing.T, routes map[string]string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".xml") || strings.HasSuffix(r.URL.Path, "/feed") {
			w.Header().Set("Content-Type", "application/rss+xml")
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFeedTranscript(t *testing.T) {
	srv := feedServer(t, map[string]string{
		"/show/feed.xml": testFeed,
		"/show/ep2.vtt":  "WEBVTT\n\n00:00:00.000 --> 00:00:02.000\n<v Host>Welcome</v>\n",
		"/ep2.srt":       "1\n00:00:00,000 --> 00:00:02,000\nfrom srt\n",
	})
	// The newest item; its WebVTT outranks the SRT listed first, and a relative URL resolves
	// against the feed.
	r, err := feedTranscript(srv.URL+"/show/feed.xml", false)
	if err != nil || r.Language != "en" || r.IsAuto || !reflect.DeepEqual(r.Segments, []Cue{{0, 2, "Welcome"}}) {
		t.Fatalf("feedTranscript = %+v, %v", r, err)
	}

	// With no WebVTT, SRT; the channel language stands in when the transcript names none.
	srt := strings.Replace(testFeed, `url="ep2.vtt" type="text/vtt" language="en"`, `url="ep2.vtt" type="text/html"`, 1)
	srt = strings.Replace(srt, `type="application/x-subrip" language="en"`, `type="application/srt"`, 1)
	srv2 := feedServer(t, map[string]string{"/feed": srt, "/ep2.srt": "1\n00:00:00,000 --> 00:00:02,000\nfrom srt\n"})
	if r, err := feedTranscript(srv2.URL+"/feed", false); err != nil || r.Language != "en-us" || len(r.Segments) != 1 || r.Segments[0].Text != "from srt" {
		t.Errorf("SRT fallback = %+v, %v", r, err)
	}

	// A relative URL resolves against the feed's final URL after redirects, not the one asked.
	moved := feedServer(t, map[string]string{
		"/rss/feed.xml": testFeed,
		"/rss/ep2.vtt":  "WEBVTT\n\n00:00:00.000 --> 00:00:02.000\nmoved\n",
	})
	hop := httptest.NewServer(http.RedirectHandler(moved.URL+"/rss/feed.xml", http.StatusFound))
	t.Cleanup(hop.Close)
	if r, err := feedTranscript(hop.URL+"/feed", false); err != nil || len(r.Segments) != 1 || r.Segments[0].Text != "moved" {
		t.Errorf("redirected feed = %+v, %v", r, err)
	}

	var f *Fail
	empty := feedServer(t, map[string]string{"/feed": `<rss><channel><title>x</title></channel></rss>`})
	if _, err := feedTranscript(empty.URL+"/feed", false); !errors.As(err, &f) || f.Code != 4 {
		t.Errorf("no episodes err = %v, want exit 4", err)
	}
	bare := feedServer(t, map[string]string{"/feed": `<rss><channel><item><title>x</title><content:encoded><![CDATA[<p>just notes</p>]]></content:encoded></item></channel></rss>`})
	if _, err := feedTranscript(bare.URL+"/feed", false); !errors.As(err, &f) || f.Code != 4 || f.Status != "unavailable" {
		t.Errorf("no transcript err = %v, want exit 4 unavailable", err)
	}
	broken := feedServer(t, map[string]string{"/feed": `<rss><channel><item>`})
	if _, err := feedTranscript(broken.URL+"/feed", false); !errors.As(err, &f) || f.Code != 2 {
		t.Errorf("broken XML err = %v, want exit 2", err)
	}
	if _, err := feedTranscript(srv.URL+"/missing.xml", false); !errors.As(err, &f) || f.Code != 2 {
		t.Errorf("HTTP 404 err = %v, want exit 2", err)
	}
	// Sniffing: a reply that is not XML is no feed, and no error of its own.
	html := feedServer(t, map[string]string{"/watch": "<html></html>"})
	if r, err := feedTranscript(html.URL+"/watch", true); r != nil || err != nil {
		t.Errorf("sniffed HTML = %+v, %v; want nil, nil", r, err)
	}
}

func TestPickFeedTranscript(t *testing.T) {
	type tr = struct{ URL, Type, Language string }
	item := func(ts ...tr) rssItem {
		var it rssItem
		for _, x := range ts {
			it.Transcripts = append(it.Transcripts, struct {
				URL      string `xml:"url,attr"`
				Type     string `xml:"type,attr"`
				Language string `xml:"language,attr"`
			}{x.URL, x.Type, x.Language})
		}
		return it
	}
	cases := []struct {
		name, lang string
		it         rssItem
		want       string
	}{
		{"original-language VTT over a translated VTT listed first", "zh-CN",
			item(tr{"en.vtt", "text/vtt", "en"}, tr{"zh.vtt", "text/vtt", "zh-cn"}), "zh.vtt"},
		{"original-language SRT over a translated VTT", "en",
			item(tr{"es.vtt", "text/vtt", "es"}, tr{"en.srt", "application/srt", "en-US"}), "en.srt"},
		{"no language attribute is the channel's", "en",
			item(tr{"es.vtt", "text/vtt", "es"}, tr{"x.vtt", "text/vtt", ""}), "x.vtt"},
		{"prefix is whole-subtag: enm is not en", "en",
			item(tr{"enm.vtt", "text/vtt", "enm"}, tr{"en.srt", "text/srt", "en"}), "en.srt"},
		{"none in the channel language: first VTT", "fr",
			item(tr{"en.srt", "application/x-subrip", "en"}, tr{"de.vtt", "text/vtt", "de"}), "de.vtt"},
		{"no channel language: first VTT", "",
			item(tr{"en.srt", "application/x-subrip", "en"}, tr{"de.vtt", "text/vtt", "de"}), "de.vtt"},
		{"no readable kind", "en", item(tr{"a.json", "application/json", "en"}), ""},
	}
	for _, c := range cases {
		if got, _, _ := pickFeedTranscript(c.it, c.lang); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseOutline(t *testing.T) {
	encoded := `<p>Show notes intro.</p><ul><li>(00:00) Intro</li><li>(05:30) Scaling laws &amp; data</li><li>[1:02:03] Wrap-up</li></ul>` +
		`<p><strong>Dwarkesh Patel</strong> <em>01:10:00</em></p><p>Today I talk with someone.</p><p>It is great at 10:30 in the morning, says everyone in the long paragraph that runs on well past one hundred characters of text.</p>`
	want := []Cue{
		{0, 330, "Intro"},
		{330, 3723, "Scaling laws & data"},
		{3723, 4200, "Wrap-up"},
		{4200, 4200, "Dwarkesh Patel Today I talk with someone. It is great at 10:30 in the morning, says everyone in the long paragraph that runs on well past one hundred characters of text."},
	}
	if got := parseOutline(encoded); !reflect.DeepEqual(got, want) {
		t.Errorf("parseOutline =\n%+v\nwant\n%+v", got, want)
	}
	// Substack's shape: a table of contents, then the transcript stamped over again.
	twice := `<h2>Timestamps</h2><p>(00:00:00) &#8211; Opening</p><p>(00:22:39) &#8211; Second</p>` +
		`<h2>Transcript</h2><h3>00:00:00 &#8211; Opening</h3><p><strong>Host</strong></p><p>Hi.</p><h3><strong>00:22:39 &#8211; Second</strong></h3><p>Bye.</p>`
	if got, want := parseOutline(twice), []Cue{{0, 1359, "Opening Host Hi."}, {1359, 1359, "Second Bye."}}; !reflect.DeepEqual(got, want) {
		t.Errorf("TOC then transcript = %+v, want %+v", got, want)
	}
	if got := parseOutline(`<p>Recorded 10:30 sharp.</p>`); got != nil {
		t.Errorf("one stamp = %+v, want no outline", got)
	}
}

// id3Tag builds an ID3v2 tag of frames; v4 sizes are syncsafe.
func id3Tag(v4 bool, frames ...[]byte) []byte {
	var body []byte
	for _, f := range frames {
		body = append(body, f...)
	}
	body = append(body, make([]byte, 16)...) // padding
	ver := byte(3)
	if v4 {
		ver = 4
	}
	return append([]byte{'I', 'D', '3', ver, 0, 0, byte(len(body) >> 21 & 0x7f), byte(len(body) >> 14 & 0x7f), byte(len(body) >> 7 & 0x7f), byte(len(body) & 0x7f)}, body...)
}

func mkFrame(v4 bool, id string, body []byte) []byte {
	h := []byte(id + "\x00\x00\x00\x00\x00\x00")
	n := len(body)
	if v4 {
		h[4], h[5], h[6], h[7] = byte(n>>21&0x7f), byte(n>>14&0x7f), byte(n>>7&0x7f), byte(n&0x7f)
	} else {
		binary.BigEndian.PutUint32(h[4:8], uint32(n))
	}
	return append(h, body...)
}

func chap(v4 bool, id string, startMs uint32, title []byte) []byte {
	b := append([]byte(id), 0)
	b = binary.BigEndian.AppendUint32(b, startMs)
	b = binary.BigEndian.AppendUint32(b, startMs+1000)
	b = append(b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff)
	return mkFrame(v4, "CHAP", append(b, mkFrame(v4, "TIT2", title)...))
}

func TestID3Chapters(t *testing.T) {
	utf16le := []byte{1, 0xFF, 0xFE, 'O', 0, 'u', 0, 't', 0, 'r', 0, 'o', 0, 0, 0}
	for _, v4 := range []bool{false, true} {
		tag := id3Tag(v4,
			mkFrame(v4, "TIT2", []byte("\x03Episode")),
			chap(v4, "ch2", 754321, utf16le),
			chap(v4, "ch1", 0, []byte("\x03Intro — 开场\x00")),
			mkFrame(v4, "APIC", make([]byte, 300)), // a cover larger than a 7-bit size byte
			chap(v4, "ch3", 1800000, []byte("\x00Caf\xe9")),
		)
		want := []Chapter{{0, "Intro — 开场"}, {754.321, "Outro"}, {1800, "Café"}}
		if got := id3Chapters(tag); !reflect.DeepEqual(got, want) {
			t.Errorf("v2.%d: %+v, want %+v", map[bool]int{false: 3, true: 4}[v4], got, want)
		}
		// The probe cut the tag after the first chapter: the chapters before the cut survive.
		cut := 10 + len(mkFrame(v4, "TIT2", []byte("\x03Episode"))) + len(chap(v4, "ch2", 754321, utf16le)) + 5
		if got := id3Chapters(tag[:cut]); len(got) != 1 || got[0].Title != "Outro" {
			t.Errorf("cut tag: %+v, want the one whole chapter", got)
		}
	}
	for name, b := range map[string][]byte{
		"no tag":   []byte("\xff\xfb\x90\x00 mp3 frames"),
		"v2.2":     {'I', 'D', '3', 2, 0, 0, 0, 0, 0, 10},
		"short":    []byte("ID3"),
		"no chaps": id3Tag(true, mkFrame(true, "TIT2", []byte("\x03x"))),
	} {
		if got := id3Chapters(b); len(got) != 0 {
			t.Errorf("%s: %+v, want none", name, got)
		}
	}
}

func TestInspectID3Probe(t *testing.T) {
	tag := id3Tag(true, chap(true, "c", 61500, []byte("\x03Part two")))
	var gotRange string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(tag)
	}))
	defer srv.Close()
	head, _, _, err := httpGet(srv.URL+"/ep.mp3", id3ProbeBytes)
	if err != nil || gotRange != "bytes=0-262143" || !reflect.DeepEqual(id3Chapters(head), []Chapter{{61.5, "Part two"}}) {
		t.Errorf("probe = %v, range %q, chapters %+v", err, gotRange, id3Chapters(head))
	}
	if !audioExtRe.MatchString(urlPath(srv.URL+"/ep.MP3?x=1")) || audioExtRe.MatchString(urlPath("https://x/ep.mp3.html")) {
		t.Error("audioExtRe: want .mp3 path matched case-blind, query ignored, other suffix refused")
	}
}
