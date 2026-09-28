package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/binlecode/ting/internal/verb"
)

// row is one line of the list: what the renderer, the details block and the filter read.
type row struct {
	ID, Title, URL, Engine, Channel, Live, Desc, Thumb, Access string
	Duration                                                   *float64
	Views                                                      *int64
	N                                                          int    // queue index, or a part/chapter ordinal; -1 = none
	Rail                                                       string // a chapter's span, drawn instead of the clock
	Sec                                                        int    // a chapter's start; -1 = not a chapter
}

func rowsFromSearch(res *verb.SearchResult) []row {
	out := make([]row, 0, len(res.Results))
	for _, r := range res.Results {
		eng := res.Engine
		ch := oneline(r.Channel)
		if ch == "" {
			ch = "?"
		}
		out = append(out, row{ID: r.ID, Title: clean(r.Title), URL: r.URL, Engine: eng,
			Channel: ch, Live: r.LiveStatus, Desc: clean(r.Description), Access: r.Access,
			Thumb: r.Thumbnail, Duration: r.Duration, Views: r.ViewCount, N: -1, Sec: -1})
	}
	return out
}

// rowsFromItems reads any list-shaped envelope. Channel and view count are absent there on
// purpose (a stored item keeps neither; they expire), so they render as absent. ordinal
// numbers the rows 1..n for a parts list, whose details say "part k/total".
func rowsFromItems(l *verb.ItemList, ordinal bool) []row {
	out := make([]row, 0, len(l.Items))
	for i, it := range l.Items {
		t := it.URL
		if it.Title != nil && *it.Title != "" {
			t = *it.Title
		}
		r := row{Title: clean(t), URL: it.URL, Engine: it.Engine, Desc: clean(it.Description),
			Thumb: it.Thumbnail, Duration: it.Duration, N: -1, Sec: -1}
		if r.Engine == "" {
			r.Engine = l.Engine
		}
		if it.ID != nil {
			r.ID = *it.ID
		}
		switch {
		case it.Index != nil:
			r.N = *it.Index
		case ordinal:
			r.N = i + 1
		}
		out = append(out, r)
	}
	return out
}

// engines is the source segment of a mixed list: its engines, sorted, joined.
func engines(rows []row) string {
	var seen []string
	for _, r := range rows {
		dup := false
		for _, s := range seen {
			dup = dup || s == r.Engine
		}
		if !dup && r.Engine != "" {
			seen = append(seen, r.Engine)
		}
	}
	if len(seen) == 0 {
		return "?"
	}
	sort.Strings(seen)
	return strings.Join(seen, "+")
}

// fmtDurLong is the wire's duration_fmt shape, 01h:02m:03s.
func fmtDurLong(s int) string {
	return fmt.Sprintf("%02dh:%02dm:%02ds", s/3600, s/60%60, s%60)
}

func oneline(s string) string { return strings.Join(strings.Fields(s), " ") }

// clean drops the pictographs a title carries — emoji, arrows, dingbats, the presentation
// selectors and the joiner — so every glyph left on a row has a width the table can be
// trusted with; then it folds whitespace as oneline does.
func clean(s string) string {
	return oneline(strings.Map(func(r rune) rune {
		switch {
		case r >= 0x1F000 && r <= 0x1FAFF, r >= 0x2190 && r <= 0x21FF, r >= 0x2300 && r <= 0x27BF,
			r >= 0x2B00 && r <= 0x2BFF, r >= 0xFE00 && r <= 0xFE0F, r == 0x200D, r == 0x2122, r == 0x2139:
			return -1
		}
		return r
	}, s))
}

func (r row) isLive() bool { return r.Live == "is_live" }

// shortDur is the rail's clock: h:mm:ss, or m:ss under an hour, --:-- when there is none.
func shortDur(sec *float64) string {
	if sec == nil || *sec < 0 {
		return "--:--"
	}
	return clock(int(*sec))
}

func clock(s int) string {
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// fmtSec is the banner's zero-padded clock.
func fmtSec(sec float64) string {
	s := int(sec)
	if s < 0 {
		return "--:--"
	}
	if s >= 3600 {
		return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

func commas(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// rail is what a row prints at its right edge.
func (r row) rail() string {
	if r.Rail != "" {
		return r.Rail
	}
	if r.isLive() {
		return "LIVE"
	}
	tail := shortDur(r.Duration)
	switch r.Access {
	case "preview":
		return "30s " + tail
	case "paywalled":
		return "VIP " + tail
	}
	return tail
}

// matches is the live filter's test: every whitespace-separated token, case-insensitively,
// somewhere in the row's title, channel, clock, view count or rail.
func (r row) matches(tokens []string) bool {
	hay := r.Title + " " + r.Channel + " " + shortDur(r.Duration)
	if r.Views != nil {
		hay += " " + fmt.Sprint(*r.Views)
	}
	hay += " " + r.rail()
	hay = strings.ToLower(hay)
	for _, t := range tokens {
		if !strings.Contains(hay, strings.ToLower(t)) {
			return false
		}
	}
	return true
}

// handleKey is a handle with its fragment and any t= offset dropped: two rows are the same
// media when these agree, whatever offset either carries.
func handleKey(u string) string {
	if i := strings.IndexByte(u, '#'); i >= 0 {
		u = u[:i]
	}
	base, q, ok := strings.Cut(u, "?")
	if !ok {
		return base
	}
	var keep []string
	for _, seg := range strings.Split(q, "&") {
		if seg == "" || strings.HasPrefix(seg, "t=") {
			continue
		}
		keep = append(keep, seg)
	}
	if len(keep) == 0 {
		return base
	}
	return base + "?" + strings.Join(keep, "&")
}

func fmtBitrate(n *int) string {
	if n == nil || *n <= 0 {
		return ""
	}
	k := (*n + 500) / 1000
	if k >= 1000 {
		return fmt.Sprintf("%d.%d Mbps", k/1000, k/100%10)
	}
	return fmt.Sprintf("%d kbps", k)
}

func fmtHz(n *int) string {
	if n == nil || *n <= 0 {
		return ""
	}
	if f := *n % 1000 / 100; f != 0 {
		return fmt.Sprintf("%d.%d kHz", *n/1000, f)
	}
	return fmt.Sprintf("%d kHz", *n/1000)
}

// mediaLine is what the player is decoding, video half then audio half.
func mediaLine(m *verb.Media, sep string) string {
	if m == nil {
		return ""
	}
	var v, a string
	if m.VideoCodec != nil {
		v = *m.VideoCodec
		if m.Width != nil && m.Height != nil {
			v += fmt.Sprintf(" %dx%d", *m.Width, *m.Height)
		}
		if m.FPS != nil {
			v += " " + strings.TrimSuffix(fmt.Sprintf("%.2f", *m.FPS), ".00") + "fps"
		}
		if br := fmtBitrate(m.VideoBitrate); br != "" {
			v += " " + br
		}
	}
	if m.AudioCodec != nil {
		a = *m.AudioCodec
		if br := fmtBitrate(m.AudioBitrate); br != "" {
			a += " " + br
		}
		if hz := fmtHz(m.SampleRate); hz != "" {
			a += " " + hz
		}
		if m.Channels != nil {
			a += " " + *m.Channels
		}
	}
	if v != "" && a != "" {
		return v + " " + sep + " " + a
	}
	return v + a
}
