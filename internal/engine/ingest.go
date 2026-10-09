// Package engine is jue's runtime: the
// ingest side squeezes a media URL into small verifiable facts (chapters, verbatim cues), the
// playback side owns one detached mpv behind a Unix socket. Standard library only.
package engine

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
)

// Fail is every error a verb returns: the envelope status it prints and the exit code it
// leaves (1 usage, 2 external tool or network, 4 the asked-for effect did not happen).
type Fail struct {
	Code   int
	Status string
	Msg    string
}

func (f *Fail) Error() string { return f.Msg }

func fail(code int, status, format string, a ...any) *Fail {
	return &Fail{Code: code, Status: status, Msg: fmt.Sprintf(format, a...)}
}

type Chapter struct {
	Start float64 `json:"start"`
	Title string  `json:"title"`
}

type InspectResponse struct {
	Status   string    `json:"status"`
	ID       string    `json:"id,omitempty"`
	Title    string    `json:"title,omitempty"`
	Duration float64   `json:"duration,omitempty"`
	Uploader string    `json:"uploader,omitempty"`
	Chapters []Chapter `json:"chapters"`
}

type Cue struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

type TranscriptResponse struct {
	Status    string `json:"status"`
	Language  string `json:"lang,omitempty"`
	IsAuto    bool   `json:"is_auto"`
	Truncated bool   `json:"truncated,omitempty"`
	Segments  []Cue  `json:"segments"`
}

// rawInfo is yt-dlp's record, parsed apart from the envelope: a chapter's start arrives as
// start_time, and decoding it straight into Chapter would read every one as 0.
type rawInfo struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Duration float64 `json:"duration"`
	Uploader string  `json:"uploader"`
	Language string  `json:"language"`
	Chapters []struct {
		StartTime float64 `json:"start_time"`
		Title     string  `json:"title"`
	} `json:"chapters"`
	Subtitles map[string]json.RawMessage `json:"subtitles"`
	AutoCaps  map[string]json.RawMessage `json:"automatic_captions"`
	Type      string                     `json:"_type"`
	Entries   []json.RawMessage          `json:"entries"`
}

// MaxCues caps every transcript, windowed or not: past it the caller gets the head and
// truncated:true, the hint to come back with a narrower window.
const MaxCues = 300

func ytdlp(args ...string) ([]byte, error) {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return nil, fail(2, "error", "yt-dlp not found on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	base := append([]string{"--no-warnings", "--no-progress"}, cookieArgs()...)
	cmd := exec.CommandContext(ctx, "yt-dlp", append(base, args...)...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, ytdlpFail(stderr.String(), err)
	}
	return out, nil
}

// cookieArgs lends yt-dlp the cookies of a logged-in browser, read in place: jue never writes a
// cookies file. JUE_COOKIES_FROM_BROWSER names the browser in yt-dlp's own syntax
// (chrome, firefox, "chrome:Profile 1"); unset means chrome, none or off lends nothing.
func cookieArgs() []string {
	b := strings.TrimSpace(os.Getenv("JUE_COOKIES_FROM_BROWSER"))
	switch strings.ToLower(b) {
	case "none", "off":
		return nil
	case "":
		b = "chrome"
	}
	return []string{"--cookies-from-browser", b}
}

// ytdlpFail classifies a failed yt-dlp run into deterministic exit codes:
//   - 1 (usage): invalid URL or unknown cookies browser.
//   - 4 (unavailable): media does not exist, removed, or private.
//   - 4 (error): upstream rate-limit (429), bot verification, or geo-block (terminal, no retry).
//   - 2 (error): transient transport/socket failures or local cookie storage faults (retryable).
func ytdlpFail(stderr string, err error) *Fail {
	line := lastLine(stderr, err)
	lower := strings.ToLower(stderr)

	// 1. Caller mistakes (Exit 1)
	switch {
	case strings.Contains(stderr, "is not a valid URL") || strings.Contains(stderr, "Unsupported URL"):
		return fail(1, "error", "yt-dlp: %s", line)
	case strings.Contains(stderr, "unsupported browser specified for cookies"):
		return fail(1, "error", "yt-dlp: %s (check JUE_COOKIES_FROM_BROWSER)", line)
	}

	// 2. Local cookie storage faults (Exit 2)
	if strings.Contains(stderr, "failed to load cookies") || strings.Contains(stderr, "cookies database") ||
		strings.Contains(stderr, "database is locked") {
		return fail(2, "error", "yt-dlp: cannot read browser cookies: %s (set JUE_COOKIES_FROM_BROWSER to another browser, or none)", line)
	}

	// 3. Upstream media does not exist / unavailable (Exit 4, unavailable)
	if strings.Contains(lower, "video is unavailable") ||
		strings.Contains(lower, "video unavailable") ||
		strings.Contains(lower, "private video") ||
		strings.Contains(lower, "has been removed") ||
		strings.Contains(lower, "copyright claim") {
		return fail(4, "unavailable", "yt-dlp: %s", line)
	}

	// 4. Upstream anti-scraping / rate limits / access restrictions (Exit 4, error, non-retryable)
	if strings.Contains(lower, "http error 429") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "confirm you're not a bot") ||
		strings.Contains(lower, "confirm you are not a bot") ||
		strings.Contains(lower, "sign in to view this video") ||
		strings.Contains(lower, "geo-restricted") ||
		strings.Contains(lower, "available in your country") {
		return fail(4, "error", "yt-dlp: upstream blocked or rate limited: %s", line)
	}

	// 5. Raw transport errors or unhandled tool failures (Exit 2, retryable)
	return fail(2, "error", "yt-dlp: %s", line)
}

// lastLine is the last ERROR line of a yt-dlp run, so a Python traceback printed after it
// does not stand in for the reason; failing that, its last line.
func lastLine(s string, err error) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); strings.HasPrefix(l, "ERROR:") || strings.HasPrefix(l, "yt-dlp: error:") {
			return l
		}
	}
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return err.Error()
}

// searchPrefixRe is anchored at the head: a search word further in (youtube.com/results?
// search_query=) is a plain URL, not a search.
var searchPrefixRe = regexp.MustCompile(`^[a-z0-9]*search[a-z0-9]*:`)

// searchQuery strips mpv's ytdl:// and reports whether what is left is a yt-dlp search. Only
// ytsearch1: and ytsearch: (both one result) are searches jue runs; any other search prefix
// is a usage error, not a path to guess at.
func searchQuery(u string) (string, bool, error) {
	bare := strings.TrimPrefix(u, "ytdl://")
	switch p := searchPrefixRe.FindString(bare); p {
	case "":
		return bare, false, nil
	case "ytsearch1:", "ytsearch:":
		return bare, true, nil
	default:
		return "", false, fail(1, "error", "unsupported search prefix %q (only ytsearch1: and ytsearch:)", p)
	}
}

// normalizeForYtdlp is u as yt-dlp takes it: it refuses the ytdl:// scheme mpv needs.
func normalizeForYtdlp(u string) (string, error) {
	bare, _, err := searchQuery(u)
	return bare, err
}

// dump is the one metadata extraction both verbs start from; raw is kept so transcript can
// hand it back to yt-dlp with --load-info-json instead of extracting the page twice.
func dump(u string) ([]byte, *rawInfo, error) {
	u, err := normalizeForYtdlp(u)
	if err != nil {
		return nil, nil, err
	}
	raw, err := ytdlp("--dump-single-json", "--no-playlist", "--skip-download", "--", u)
	if err != nil {
		return nil, nil, err
	}
	return decodeDump(u, raw)
}

// decodeDump parses yt-dlp's record of u. A search answers with a playlist shell around its
// one hit: the hit's own bytes replace the shell, so transcript's --load-info-json reads the
// video and not the shell. A playlist URL is left as it is — only a search is unwrapped.
func decodeDump(u string, raw []byte) ([]byte, *rawInfo, error) {
	var info rawInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, nil, fail(2, "error", "yt-dlp: unreadable JSON: %v", err)
	}
	if !searchPrefixRe.MatchString(u) || info.Type != "playlist" {
		return raw, &info, nil
	}
	if len(info.Entries) == 0 {
		return nil, nil, fail(4, "unavailable", "no search results for %s", u)
	}
	raw, info = info.Entries[0], rawInfo{}
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, nil, fail(2, "error", "yt-dlp: unreadable JSON: %v", err)
	}
	return raw, &info, nil
}

func Inspect(u string) (*InspectResponse, error) {
	_, info, err := dump(u)
	if err != nil {
		return nil, err
	}
	r := &InspectResponse{Status: "ok", ID: info.ID, Title: info.Title, Duration: info.Duration,
		Uploader: info.Uploader, Chapters: []Chapter{}}
	for _, c := range info.Chapters {
		r.Chapters = append(r.Chapters, Chapter{Start: c.StartTime, Title: c.Title})
	}
	// A bare podcast MP3 keeps its chapters in the ID3v2 tag yt-dlp does not read. Best
	// effort: a probe that fails leaves what yt-dlp found.
	if len(r.Chapters) == 0 && isHTTP(u) && audioExtRe.MatchString(urlPath(u)) {
		if head, _, _, err := httpGet(u, id3ProbeBytes); err == nil {
			r.Chapters = append(r.Chapters, id3Chapters(head)...)
		}
	}
	return r, nil
}

// ParseRange reads "600-900" or "10:00-15:00" (any [h:]m:s on either side).
func ParseRange(s string) (float64, float64, error) {
	a, b, ok := strings.Cut(s, "-")
	start, err1 := ParseTime(a)
	end, err2 := ParseTime(b)
	if !ok || err1 != nil || err2 != nil || end <= start {
		return 0, 0, fmt.Errorf("bad range %q (want START-END, e.g. 600-900 or 10:00-15:00)", s)
	}
	return start, end, nil
}

// ParseTime reads seconds ("845.5") or colon form ("14:05", "1:02:03").
func ParseTime(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("bad time %q", s)
	}
	var t float64
	for _, p := range strings.Split(s, ":") {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, fmt.Errorf("bad time %q", s)
		}
		t = t*60 + v
		if t > 1e8 || math.IsNaN(t) || math.IsInf(t, 0) {
			return 0, fmt.Errorf("bad time %q", s)
		}
	}
	return t, nil
}

// Transcript returns the verbatim cues of u. With hasRange only the cues that intersect
// [start, end) come back; either way the head is capped at MaxCues.
func Transcript(u string, start, end float64, hasRange bool) (*TranscriptResponse, error) {
	u, err := normalizeForYtdlp(u) // ytdl:// would hide a netease host from neteaseID
	if err != nil {
		return nil, err
	}
	var r *TranscriptResponse
	var f *Fail
	switch {
	case neteaseID(u) != "":
		r, err = neteaseLyrics(neteaseID(u))
	case isFeedURL(u):
		r, err = feedTranscript(u, false)
	default:
		r, err = ytTranscript(u)
		// A feed whose URL does not say so: yt-dlp finds no subtitles, the server says XML.
		if errors.As(err, &f) && (f.Code == 1 || f.Code == 4) && isHTTP(u) {
			if fr, ferr := feedTranscript(u, true); fr != nil || ferr != nil {
				r, err = fr, ferr
			}
		}
	}
	if err != nil {
		return nil, err
	}
	r.Segments, r.Truncated = clip(r.Segments, start, end, hasRange)
	return r, nil
}

// clip keeps the cues intersecting [start, end) when hasRange, then caps them at MaxCues: a
// window wide enough to hold the whole talk must not slip past the cap.
func clip(cues []Cue, start, end float64, hasRange bool) ([]Cue, bool) {
	if hasRange {
		kept := []Cue{}
		for _, c := range cues {
			// A point cue (the last lyric line has no successor to end it) counts when it
			// starts inside the window.
			if c.Start < end && (c.End > start || c.Start >= start) {
				kept = append(kept, c)
			}
		}
		cues = kept
	}
	if len(cues) > MaxCues {
		return cues[:MaxCues], true
	}
	return cues, false
}

func ytTranscript(u string) (*TranscriptResponse, error) {
	raw, info, err := dump(u)
	if err != nil {
		return nil, err
	}
	lang, auto := pickTrack(info)
	if lang == "" {
		return nil, fail(4, "unavailable", "no original-language subtitles for %s", u)
	}
	// transcript calls scratchDir(), which depends on and creates runtimeDir(true) (0700),
	// storing info.json (0600) and downloaded subtitles. defer os.RemoveAll cleans it up
	// synchronously upon return, leaving zero durable disk footprint.
	dir, err := scratchDir()
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	infoFile := filepath.Join(dir, "info.json")
	if err := os.WriteFile(infoFile, raw, 0600); err != nil {
		return nil, fail(2, "error", "%v", err)
	}
	write := "--write-subs"
	if auto {
		write = "--write-auto-subs"
	}
	// One exact track: a wildcard like en.* fans out to every machine translation YouTube
	// offers, which both pollutes the evidence and earns a 429.
	if _, err := ytdlp("--load-info-json", infoFile, "--skip-download", write, "--sub-langs", lang,
		"--sub-format", "json3", "-o", filepath.Join(dir, "sub.%(ext)s")); err != nil {
		return nil, err
	}
	body, err := os.ReadFile(filepath.Join(dir, "sub."+lang+".json3"))
	if err != nil {
		return nil, fail(2, "error", "yt-dlp wrote no %s json3 track", lang)
	}
	cues, err := parseJSON3(body)
	if err != nil {
		return nil, err
	}
	if len(cues) == 0 {
		return nil, fail(4, "unavailable", "subtitle track %s is empty", lang)
	}
	return &TranscriptResponse{Status: "ok", Language: lang, IsAuto: auto, Segments: cues}, nil
}

// pickTrack is the language chain. Human subtitles in the media's own language first; then
// YouTube's ASR of the original audio (the -orig track); then plain ASR in that language. A
// human track in ANOTHER language is a translation and is only taken when the media's
// language is unknown. Never a machine-translated auto track.
func pickTrack(info *rawInfo) (string, bool) {
	usable := func(k string) bool { return k != "live_chat" && k != "danmaku" }
	keys := func(m map[string]json.RawMessage) []string {
		var ks []string
		for k := range m {
			if usable(k) {
				ks = append(ks, k)
			}
		}
		sort.Strings(ks)
		return ks
	}
	manual, auto, lang := keys(info.Subtitles), keys(info.AutoCaps), info.Language
	if lang != "" {
		for _, k := range manual {
			if k == lang || strings.HasPrefix(k, lang+"-") {
				return k, false
			}
		}
	}
	for _, k := range auto {
		if strings.HasSuffix(k, "-orig") {
			return k, true
		}
	}
	for _, k := range auto {
		if lang != "" && k == lang {
			return k, true
		}
	}
	if lang != "" || len(manual) == 0 {
		return "", false
	}
	// Language unknown (old uploads carry none): English, then Chinese, then the first in
	// sort order, so the same video always yields the same track.
	for _, pre := range []string{"en", "zh"} {
		for _, k := range manual {
			if k == pre || strings.HasPrefix(k, pre+"-") {
				return k, false
			}
		}
	}
	return manual[0], false
}

func parseJSON3(body []byte) ([]Cue, error) {
	var doc struct {
		Events []struct {
			TStartMs    float64 `json:"tStartMs"`
			DDurationMs float64 `json:"dDurationMs"`
			Segs        []struct {
				UTF8 string `json:"utf8"`
			} `json:"segs"`
		} `json:"events"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fail(2, "error", "unreadable json3 track: %v", err)
	}
	var cues []Cue
	for _, ev := range doc.Events {
		var b strings.Builder
		for _, s := range ev.Segs {
			b.WriteString(s.UTF8)
		}
		text := cleanText(b.String())
		if text == "" {
			continue
		}
		cues = appendCue(cues, Cue{Start: ms(ev.TStartMs), End: ms(ev.TStartMs + ev.DDurationMs), Text: text})
	}
	// ASR windows overlap the next line by seconds; a cue ends where the next one begins, so a
	// time window selects what was being said in it and not its neighbours too.
	for i := 0; i+1 < len(cues); i++ {
		if cues[i].End > cues[i+1].Start && cues[i+1].Start >= cues[i].Start {
			cues[i].End = cues[i+1].Start
		}
	}
	return cues, nil
}

// appendCue adds c, unless it is a rolling caption repeating the line still showing: then one
// cue spans both.
func appendCue(cues []Cue, c Cue) []Cue {
	if n := len(cues); n > 0 && cues[n-1].Text == c.Text && c.Start-cues[n-1].End <= 2 {
		cues[n-1].End = max(cues[n-1].End, c.End)
		return cues
	}
	return append(cues, c)
}

func ms(v float64) float64 { return math.Round(v) / 1000 }

// cleanText drops control and format characters (newlines, zero-width marks) and folds runs
// of whitespace to one space.
func cleanText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

var (
	neteasePathRe = regexp.MustCompile(`/song/(\d+)$`)
	digitsRe      = regexp.MustCompile(`^\d+$`)
	lrcTagRe      = regexp.MustCompile(`^\[(\d+):(\d+(?:[.:]\d+)?)\]`)
)

// neteaseID is the song id of a music.163.com song URL, "" for anything else — a playlist or
// album carries an id= too, and is not a song. yt-dlp's netease extractor fetches no lyrics,
// so this one site is read over plain HTTP.
func neteaseID(u string) string {
	p, err := url.Parse(u)
	if err != nil || !strings.HasSuffix(p.Hostname(), "music.163.com") {
		return ""
	}
	// The web player routes in the fragment: music.163.com/#/song?id=N.
	route, query := p.Path, p.RawQuery
	if strings.HasPrefix(p.Fragment, "/") {
		route, query, _ = strings.Cut(p.Fragment, "?")
	}
	route = strings.TrimSuffix(route, "/")
	if m := neteasePathRe.FindStringSubmatch(route); m != nil {
		return m[1]
	}
	if q, _ := url.ParseQuery(query); strings.HasSuffix(route, "/song") && digitsRe.MatchString(q.Get("id")) {
		return q.Get("id")
	}
	return ""
}

func neteaseLyrics(id string) (*TranscriptResponse, error) {
	req, _ := http.NewRequest("GET", "https://music.163.com/api/song/lyric?id="+id+"&lv=-1&kv=-1&tv=-1", nil)
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("Cookie", "os=pc")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, fail(2, "error", "netease lyric request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var doc struct {
		Code    int  `json:"code"`
		NoLyric bool `json:"nolyric"`
		Lrc     struct {
			Lyric string `json:"lyric"`
		} `json:"lrc"`
	}
	if resp.StatusCode != 200 || json.Unmarshal(body, &doc) != nil || doc.Code != 200 {
		return nil, fail(2, "error", "netease lyric endpoint answered HTTP %d", resp.StatusCode)
	}
	// An instrumental says so in one of two ways: the flag, or a site-written placeholder line.
	if doc.NoLyric || strings.Contains(doc.Lrc.Lyric, "纯音乐，请欣赏") {
		return nil, fail(4, "unavailable", "no lyrics for netease song %s", id)
	}
	cues := parseLRC(doc.Lrc.Lyric)
	if len(cues) == 0 {
		return nil, fail(4, "unavailable", "no lyrics for netease song %s", id)
	}
	// The endpoint tags no language and the catalogue is not one language: lang stays empty.
	// Lyrics are contributed by people, so is_auto is false.
	return &TranscriptResponse{Status: "ok", Segments: cues}, nil
}

// parseLRC turns [mm:ss.xx] lines into cues; a line may carry several tags. A cue ends where
// the next starts, capped at 6s across a long instrumental gap; the last one is a point.
func parseLRC(lrc string) []Cue {
	var cues []Cue
	for _, line := range strings.Split(lrc, "\n") {
		line = strings.TrimSpace(line)
		var starts []float64
		for {
			m := lrcTagRe.FindStringSubmatch(line)
			if m == nil {
				break
			}
			mm, _ := strconv.ParseFloat(m[1], 64)
			ss, _ := strconv.ParseFloat(strings.Replace(m[2], ":", ".", 1), 64)
			starts = append(starts, math.Round((mm*60+ss)*1000)/1000)
			line = line[len(m[0]):]
		}
		// An empty tagged line is kept until the ends are set: it is where the line before ends.
		for _, s := range starts {
			cues = append(cues, Cue{Start: s, End: s, Text: cleanText(line)})
		}
	}
	sort.SliceStable(cues, func(i, j int) bool { return cues[i].Start < cues[j].Start })
	var out []Cue
	for i, c := range cues {
		if i+1 < len(cues) {
			gap := cues[i+1].Start - c.Start
			if gap > 8 {
				gap = 6
			}
			c.End = math.Round((c.Start+gap)*1000) / 1000
		}
		if c.Text != "" {
			out = append(out, c)
		}
	}
	return out
}

// Cue-file scanner states. A WebVTT or SRT file is blocks parted by blank lines; a line is
// read in exactly one of these.
const (
	StateHeader = iota // skipping the WEBVTT header block
	StateSkip          // skipping a NOTE, STYLE or REGION block: every line up to the blank one
	StateTime          // between cues: an identifier or SRT index, then the timing line
	StateText          // inside a cue: payload lines up to the blank line
	StateFlush         // the cue is whole: clean it and emit it
)

// parseVTT reads a WebVTT file into cues.
func parseVTT(body []byte) ([]Cue, error) {
	body = bytes.TrimPrefix(body, []byte("\uFEFF"))
	if !bytes.HasPrefix(body, []byte("WEBVTT")) {
		return nil, fail(2, "error", "not a WebVTT file (no WEBVTT header)")
	}
	return parseCueFile(body, StateHeader)
}

// parseSRT reads a SubRip file into cues.
func parseSRT(body []byte) ([]Cue, error) {
	return parseCueFile(bytes.TrimPrefix(body, []byte("\uFEFF")), StateTime)
}

// parseCueFile is the one line scanner both formats share: they differ only in that WebVTT
// opens on a header block and SRT straight on its first cue.
func parseCueFile(body []byte, state int) ([]Cue, error) {
	var cues []Cue
	var cur Cue
	var text []string
	timed := false
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	for i := 0; i <= len(lines); i++ {
		line, eof := "", i == len(lines)
		if !eof {
			line = strings.TrimSpace(strings.TrimSuffix(lines[i], "\r"))
		}
		blank := line == ""
		switch state {
		case StateSkip:
			// A NOTE may hold anything, a timing-shaped line included: none of it is a cue.
			if blank {
				state = StateTime
			}
			continue
		case StateHeader:
			if blank {
				state = StateTime
			}
			if _, _, ok := parseTiming(line); !ok {
				continue
			}
			// A header with no blank line after it runs straight into the first cue.
			fallthrough
		case StateTime:
			if s, e, ok := parseTiming(line); ok {
				cur, text, timed, state = Cue{Start: s, End: e}, nil, true, StateText
			} else if line == "NOTE" || strings.HasPrefix(line, "NOTE ") || strings.HasPrefix(line, "NOTE\t") || line == "STYLE" || line == "REGION" {
				state = StateSkip
			}
			continue
		case StateText:
			// A timing line with no blank line before it still starts the next cue.
			if s, e, ok := parseTiming(line); ok {
				cues = appendCue(cues, flushCue(cur, text))
				cur, text = Cue{Start: s, End: e}, nil
				continue
			}
			if !blank && !eof {
				text = append(text, line)
				continue
			}
			state = StateFlush
		}
		// StateFlush
		cues = appendCue(cues, flushCue(cur, text))
		state = StateTime
	}
	if !timed && strings.TrimSpace(string(body)) != "" && !bytes.HasPrefix(body, []byte("WEBVTT")) {
		return nil, fail(2, "error", "no cue timings in subtitle file")
	}
	out := cues[:0]
	for _, c := range cues {
		if c.Text != "" {
			out = append(out, c)
		}
	}
	return out, nil
}

// flushCue joins a cue's payload lines with one space and cleans them.
func flushCue(c Cue, lines []string) Cue {
	c.Text = cleanText(html.UnescapeString(stripTags(strings.Join(lines, " "))))
	return c
}

// stripTags drops markup in one pass: WebVTT voice <v Speaker>, class <c.x>, karaoke
// <00:01.200> and HTML <b>, </font>. Only a '<' opening a tag name (optional '/' then a
// letter) or a karaoke stamp (1-2 digits then ':') starts a tag; "x < 10 and y > 5" and a
// '<' that never closes stay text.
func stripTags(s string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i:], '>')
		if j < 0 {
			break
		}
		if !isTagStart(s[i+1:]) {
			b.WriteString(s[:i+1])
			s = s[i+1:]
			continue
		}
		b.WriteString(s[:i])
		s = s[i+j+1:]
	}
	b.WriteString(s)
	return b.String()
}

// isTagStart reports whether s, the text right after a '<', opens a tag: `/?[a-zA-Z]` or `[0-9]{1,2}:`.
func isTagStart(s string) bool {
	if name := strings.TrimPrefix(s, "/"); name != "" {
		if c := name[0] | 0x20; c >= 'a' && c <= 'z' {
			return true
		}
	}
	n := 0
	for n < len(s) && n < 3 && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return (n == 1 || n == 2) && n < len(s) && s[n] == ':'
}

// parseTiming reads "00:01:02.500 --> 00:01:04,000 align:start": either side [HH:]MM:SS
// with a dot or a comma before the milliseconds; WebVTT cue settings after the end are ignored.
func parseTiming(line string) (float64, float64, bool) {
	a, b, ok := strings.Cut(line, "-->")
	if !ok {
		return 0, 0, false
	}
	f := strings.Fields(b)
	if len(f) == 0 {
		return 0, 0, false
	}
	start, ok1 := parseStamp(a)
	end, ok2 := parseStamp(f[0])
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return start, max(start, end), true
}

func parseStamp(s string) (float64, bool) {
	s = strings.Replace(strings.TrimSpace(s), ",", ".", 1)
	if n := strings.Count(s, ":"); n < 1 || n > 2 {
		return 0, false
	}
	t, err := ParseTime(s)
	if err != nil {
		return 0, false
	}
	return math.Round(t*1000) / 1000, true
}

func isHTTP(u string) bool {
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
}

func urlPath(u string) string {
	if p, err := url.Parse(u); err == nil {
		return strings.ToLower(p.Path)
	}
	return ""
}

// isFeedURL says a URL names a podcast feed by its shape: a .xml or .rss path, a feed or rss
// path segment (/feed, /feed/podcast, /12345/rss), or a feeds. host (Transistor, Simplecast,
// Megaphone serve feeds bare).
func isFeedURL(u string) bool {
	p, err := url.Parse(u)
	if err != nil || !isHTTP(u) {
		return false
	}
	path := strings.ToLower(p.Path)
	if strings.HasSuffix(path, ".xml") || strings.HasSuffix(path, ".rss") || strings.HasPrefix(p.Hostname(), "feeds.") {
		return true
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "feed" || seg == "rss" {
			return true
		}
	}
	return false
}

// httpGet fetches u, at most limit bytes of it; limit is also sent as a Range so a server
// that honours it sends no more. Anything but 200 or 206 is a failed fetch. It also returns
// the URL the body came from after redirects, the base for any relative link inside it.
func httpGet(u string, limit int64) ([]byte, string, string, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, "", "", fail(1, "error", "bad URL %q", u)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	if limit < maxFetchBytes {
		req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", limit-1))
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, "", "", fail(2, "error", "fetch %s: %v", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		return nil, "", "", fail(2, "error", "fetch %s: HTTP %d", u, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, "", "", fail(2, "error", "fetch %s: %v", u, err)
	}
	return body, resp.Header.Get("Content-Type"), resp.Request.URL.String(), nil
}

const maxFetchBytes = 16 << 20

// rssFeed is the slice of an RSS 2.0 feed transcript reads. encoding/xml matches local names,
// so podcast:transcript and content:encoded bind whatever prefix the feed declares.
type rssFeed struct {
	Channel struct {
		Language string    `xml:"language"`
		Items    []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Transcripts []struct {
		URL      string `xml:"url,attr"`
		Type     string `xml:"type,attr"`
		Language string `xml:"language,attr"`
	} `xml:"transcript"`
	Encoded string `xml:"encoded"`
}

// feedTranscript reads the newest episode of a podcast feed: its <podcast:transcript> in
// WebVTT or SRT, else the timestamped outline in its <content:encoded>. With sniff the URL
// is only maybe a feed: a reply that is not XML returns nil, nil and leaves the caller's error.
func feedTranscript(u string, sniff bool) (*TranscriptResponse, error) {
	body, ctype, final, err := httpGet(u, maxFetchBytes)
	if sniff && (err != nil || !strings.Contains(ctype, "xml")) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fail(2, "error", "unreadable RSS feed %s: %v", u, err)
	}
	if len(feed.Channel.Items) == 0 {
		return nil, fail(4, "unavailable", "no episodes in feed %s", u)
	}
	item := feed.Channel.Items[0]
	if tu, typ, lang := pickFeedTranscript(item, feed.Channel.Language); tu != "" {
		if base, err := url.Parse(final); err == nil {
			if ref, err := base.Parse(tu); err == nil {
				tu = ref.String()
			}
		}
		sub, _, _, err := httpGet(tu, maxFetchBytes)
		if err != nil {
			return nil, err
		}
		parse := parseSRT
		if typ == "text/vtt" {
			parse = parseVTT
		}
		cues, err := parse(sub)
		if err != nil {
			return nil, err
		}
		if len(cues) == 0 {
			return nil, fail(4, "unavailable", "transcript of %q is empty", item.Title)
		}
		return &TranscriptResponse{Status: "ok", Language: cmp(lang, feed.Channel.Language), Segments: cues}, nil
	}
	if cues := parseOutline(item.Encoded); len(cues) > 0 {
		return &TranscriptResponse{Status: "ok", Language: feed.Channel.Language, Segments: cues}, nil
	}
	return nil, fail(4, "unavailable", "no transcript for %q in feed %s", item.Title, u)
}

func cmp(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// pickFeedTranscript prefers a transcript in the channel's language, the original wording, over
// any translation: a WebVTT one, else an SRT one (no language attribute means the channel's,
// per Podcasting 2.0); only with none in that language does it take
// the first WebVTT, else the first SRT. The JSON and HTML kinds carry no format jue reads.
func pickFeedTranscript(item rssItem, channelLang string) (string, string, string) {
	base := strings.ToLower(strings.TrimSpace(channelLang))
	if i := strings.IndexAny(base, "-_"); i >= 0 {
		base = base[:i]
	}
	for _, matchLang := range []bool{true, false} {
		if matchLang && base == "" {
			continue
		}
		for _, want := range []string{"text/vtt", "application/x-subrip"} {
			for _, t := range item.Transcripts {
				typ := strings.ToLower(strings.TrimSpace(t.Type))
				if typ == "application/srt" || typ == "text/srt" {
					typ = "application/x-subrip"
				}
				if typ != want || t.URL == "" {
					continue
				}
				if matchLang {
					l := strings.ToLower(strings.TrimSpace(t.Language))
					if l != "" && l != base && !strings.HasPrefix(l, base+"-") && !strings.HasPrefix(l, base+"_") {
						continue
					}
				}
				return t.URL, typ, t.Language
			}
		}
	}
	return "", "", ""
}

var (
	outlineBreakRe = regexp.MustCompile(`(?i)<br\s*/?>|</(p|li|div|h[1-6]|blockquote)>`)
	outlineStampRe = regexp.MustCompile(`[(\[]?\b(\d{1,2}:\d{2}(?::\d{2})?)\b[)\]]?`)
)

// parseOutline reads the timestamps show notes and Substack transcripts carry in
// content:encoded: a short line led or closed by [HH:]MM:SS ("(00:12:30) Scaling laws",
// "Dwarkesh Patel 00:01:05") starts a cue; the lines up to the next such line are its text.
// A cue ends where the next begins, the last is a point. Fewer than two stamps is no outline.
func parseOutline(encoded string) []Cue {
	text := html.UnescapeString(stripTags(outlineBreakRe.ReplaceAllString(encoded, "\n")))
	var cues []Cue
	for _, line := range strings.Split(text, "\n") {
		line = cleanText(line)
		if line == "" {
			continue
		}
		if loc := outlineStampRe.FindStringSubmatchIndex(line); loc != nil {
			rest := strings.Trim(line[:loc[0]]+" "+line[loc[1]:], " -–—:|·")
			if (loc[0] == 0 || loc[1] == len(line)) && len([]rune(rest)) <= 100 {
				if t, ok := parseStamp(line[loc[2]:loc[3]]); ok {
					// The clock running back means what came before was a table of contents and
					// the transcript it outlines starts over: the fuller pass wins.
					if n := len(cues); n > 0 && t < cues[n-1].Start {
						cues = cues[:0]
					}
					cues = append(cues, Cue{Start: t, End: t, Text: cleanText(rest)})
					continue
				}
			}
		}
		if n := len(cues); n > 0 {
			cues[n-1].Text = strings.TrimSpace(cues[n-1].Text + " " + line)
		}
	}
	if len(cues) < 2 {
		return nil
	}
	out := cues[:0]
	for i, c := range cues {
		if i+1 < len(cues) && cues[i+1].Start > c.Start {
			c.End = cues[i+1].Start
		}
		if c.Text != "" {
			out = append(out, c)
		}
	}
	return out
}

// id3ProbeBytes is how much of an audio file Inspect reads for its ID3v2 tag: chapters sit
// in the tag at the head, and the head is all that is fetched.
const id3ProbeBytes = 256 << 10

var audioExtRe = regexp.MustCompile(`\.(mp3|m4a|aac)$`)

// id3Chapters decodes the CHAP frames of the ID3v2.3/2.4 tag heading b, titled by their TIT2
// sub-frame, in start order. A tag cut off by the probe yields the chapters before the cut.
func id3Chapters(b []byte) []Chapter {
	if len(b) < 10 || string(b[:3]) != "ID3" || (b[3] != 3 && b[3] != 4) || b[5]&0x80 != 0 {
		return nil
	}
	v4 := b[3] == 4
	end := min(len(b), 10+syncsafe(b[6:10]))
	pos := 10
	if b[5]&0x40 != 0 && end >= 14 { // extended header
		n := int(binary.BigEndian.Uint32(b[10:14]))
		if v4 {
			pos += syncsafe(b[10:14])
		} else {
			pos += 4 + n
		}
	}
	var chs []Chapter
	for _, f := range id3Frames(b, pos, end, v4) {
		if f.id != "CHAP" {
			continue
		}
		body := f.body
		i := bytes.IndexByte(body, 0)
		if i < 0 || len(body) < i+17 {
			continue
		}
		start := binary.BigEndian.Uint32(body[i+1:])
		var title string
		for _, sub := range id3Frames(body, i+17, len(body), v4) {
			if sub.id == "TIT2" {
				title = id3Text(sub.body)
			}
		}
		chs = append(chs, Chapter{Start: float64(start) / 1000, Title: title})
	}
	sort.SliceStable(chs, func(i, j int) bool { return chs[i].Start < chs[j].Start })
	return chs
}

type id3Frame struct {
	id   string
	body []byte
}

// id3Frames walks the frames in b[pos:end] up to the padding or the first frame that runs
// past end.
func id3Frames(b []byte, pos, end int, v4 bool) []id3Frame {
	var fs []id3Frame
	for pos+10 <= end && b[pos] != 0 {
		n := int(binary.BigEndian.Uint32(b[pos+4 : pos+8]))
		if v4 {
			n = syncsafe(b[pos+4 : pos+8])
		}
		if n < 0 || pos+10+n > end {
			break
		}
		fs = append(fs, id3Frame{string(b[pos : pos+4]), b[pos+10 : pos+10+n]})
		pos += 10 + n
	}
	return fs
}

func syncsafe(b []byte) int {
	return int(b[0]&0x7f)<<21 | int(b[1]&0x7f)<<14 | int(b[2]&0x7f)<<7 | int(b[3]&0x7f)
}

// id3Text decodes a text frame: an encoding byte (0 Latin-1, 1 UTF-16 with BOM, 2 UTF-16BE,
// 3 UTF-8), then the string.
func id3Text(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	enc, b := b[0], b[1:]
	var s string
	switch enc {
	case 1, 2:
		big := enc == 2
		if len(b) >= 2 && (b[0] == 0xFE && b[1] == 0xFF || b[0] == 0xFF && b[1] == 0xFE) {
			big, b = b[0] == 0xFE, b[2:]
		}
		u := make([]uint16, len(b)/2)
		for i := range u {
			if big {
				u[i] = binary.BigEndian.Uint16(b[2*i:])
			} else {
				u[i] = binary.LittleEndian.Uint16(b[2*i:])
			}
		}
		s = string(utf16.Decode(u))
	case 3:
		s = string(b)
	default:
		r := make([]rune, len(b))
		for i, c := range b {
			r[i] = rune(c)
		}
		s = string(r)
	}
	return cleanText(strings.TrimRight(s, "\x00"))
}
