// Package engine is ting-gen-2's whole runtime (PLAN-agentic-media-plane-refactor.md): the
// ingest side squeezes a media URL into small verifiable facts (chapters, verbatim cues), the
// playback side owns one detached mpv behind a Unix socket. Standard library only.
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	cmd := exec.CommandContext(ctx, "yt-dlp", append([]string{"--no-warnings", "--no-progress"}, args...)...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, ytdlpFail(stderr.String(), err)
	}
	return out, nil
}

// ytdlpFail classifies a failed yt-dlp run: a URL it cannot read at all is the caller's
// mistake (exit 1), anything else is the tool or the network (exit 2).
func ytdlpFail(stderr string, err error) *Fail {
	code := 2
	if strings.Contains(stderr, "is not a valid URL") || strings.Contains(stderr, "Unsupported URL") {
		code = 1
	}
	return fail(code, "error", "yt-dlp: %s", lastLine(stderr, err))
}

func lastLine(s string, err error) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return err.Error()
}

// dump is the one metadata extraction both verbs start from; raw is kept so transcript can
// hand it back to yt-dlp with --load-info-json instead of extracting the page twice.
func dump(u string) ([]byte, *rawInfo, error) {
	raw, err := ytdlp("--dump-single-json", "--no-playlist", "--skip-download", "--", u)
	if err != nil {
		return nil, nil, err
	}
	var info rawInfo
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
	var t float64
	for _, p := range strings.Split(strings.TrimSpace(s), ":") {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, fmt.Errorf("bad time %q", s)
		}
		t = t*60 + v
	}
	return t, nil
}

// Transcript returns the verbatim cues of u. With hasRange only the cues that intersect
// [start, end) come back; either way the head is capped at MaxCues.
func Transcript(u string, start, end float64, hasRange bool) (*TranscriptResponse, error) {
	var r *TranscriptResponse
	var err error
	if id := neteaseID(u); id != "" {
		r, err = neteaseLyrics(id)
	} else {
		r, err = ytTranscript(u)
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
		c := Cue{Start: ms(ev.TStartMs), End: ms(ev.TStartMs + ev.DDurationMs), Text: text}
		// A rolling caption repeats the line it is still showing; one cue spans both.
		if n := len(cues); n > 0 && cues[n-1].Text == text && c.Start-cues[n-1].End <= 2 {
			cues[n-1].End = max(cues[n-1].End, c.End)
			continue
		}
		cues = append(cues, c)
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
