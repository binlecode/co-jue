package tui

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // YouTube's hq720.jpg is served as image/webp
	"golang.org/x/sys/unix"
)

// The cover box: the details block's right column, 24 cells by 7 rows, drawn only while the
// list keeps 4 rows and the text keeps 50 columns — otherwise no cover at all, rather than a
// smaller one with a second layout to keep aligned.
const (
	coverCols     = 24
	coverRows     = 7
	coverMinText  = 50
	coverMinRows  = 4
	coverID       = 99
	coverMaxBytes = 8 << 20
)

// Cover is what the terminal can draw: on, and its cell size in pixels (asked, not assumed:
// Ghostty reports 16x36, and a box transcoded for a guessed 8x16 draws a quarter the size).
type Cover struct {
	On     bool
	CW, CH int
}

// DetectCover resolves TING_IMAGE before the UI owns the terminal. auto is off under tmux (no
// portable passthrough), on for the terminals known to speak the kitty protocol, else asked.
func DetectCover(mode string) Cover {
	c := Cover{CW: 8, CH: 16}
	switch mode {
	case "off":
		return c
	case "on":
		c.On = true
	default:
		if os.Getenv("TMUX") != "" || os.Getenv("TERM") == "dumb" {
			return c
		}
		t, tp := os.Getenv("TERM"), strings.ToLower(os.Getenv("TERM_PROGRAM"))
		c.On = strings.Contains(t, "kitty") || strings.Contains(t, "ghostty") ||
			tp == "ghostty" || tp == "wezterm" || tp == "kitty"
		if !c.On {
			// The query rides ahead of a DA1, which every terminal answers: a terminal that
			// does not speak the protocol answers only the DA1, at once, and nothing of either
			// reply is left for the key reader.
			r := ttyQuery("\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\\x1b[c", "c")
			c.On = strings.Contains(r, ";OK")
			if !c.On {
				return c
			}
		}
	}
	// CSI 16 t answers 6;height;width t.
	r := ttyQuery("\x1b[16t\x1b[c", "c")
	if i := strings.Index(r, "\x1b[6;"); i >= 0 {
		f := strings.SplitN(strings.TrimSuffix(strings.SplitN(r[i+4:], "t", 2)[0], "t"), ";", 2)
		if len(f) == 2 {
			h, e1 := strconv.Atoi(f[0])
			w, e2 := strconv.Atoi(f[1])
			if e1 == nil && e2 == nil && h > 0 && w > 0 {
				c.CH, c.CW = h, w
			}
		}
	}
	return c
}

// ttyQuery writes q to the terminal and reads the reply until one ending in end arrives, in
// raw mode, each byte waited on with select() for at most a second.
func ttyQuery(q, end string) string {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(uintptr(fd))
	if err != nil {
		return ""
	}
	defer term.Restore(uintptr(fd), old)
	if _, err := os.Stdout.WriteString(q); err != nil {
		return ""
	}
	var out []byte
	b := make([]byte, 1)
	deadline := time.Now().Add(2 * time.Second)
	for len(out) < 256 && time.Now().Before(deadline) {
		var set unix.FdSet
		set.Set(fd)
		tv := unix.NsecToTimeval(int64(time.Second))
		n, err := unix.Select(fd+1, &set, nil, nil, &tv)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n == 0 {
			break
		}
		if k, err := os.Stdin.Read(b); err != nil || k == 0 {
			break
		}
		out = append(out, b[0])
		// A DA1 reply is ESC [ ? … c; stop at the first one.
		if strings.HasSuffix(string(out), end) && strings.Contains(string(out), "\x1b[?") {
			break
		}
	}
	return string(out)
}

// coverImg is one cover ready to emit: its PNG, base64, and how many columns it will take.
type coverImg struct {
	b64  string
	cols int
}

type coverMsg struct {
	url string
	img *coverImg
}

// coverState is the session's covers: one fetch in flight at most (held j flies past nine
// rows nobody looks at), the ones decoded, and the ones not gettable — tried once, never again.
type coverState struct {
	on       bool
	cw, ch   int
	done     map[string]*coverImg
	failed   map[string]bool
	inFlight string
}

func (c *coverState) fetch(ctx context.Context, url string) tea.Cmd {
	cw, ch := c.cw, c.ch
	c.inFlight = url
	return func() tea.Msg { return coverMsg{url: url, img: loadCover(ctx, url, cw, ch)} }
}

// coverClient offers only the classic key exchanges. Go's default adds the post-quantum
// X25519MLKEM768 share, whose larger ClientHello a CDN in front of one engine's covers never
// answers (measured: the handshake times out every time; with X25519/P-256 the same cover
// arrives in 1.7 s, as it does for curl).
var coverClient = &http.Client{Transport: &http.Transport{
	Proxy:           http.ProxyFromEnvironment,
	TLSClientConfig: &tls.Config{CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256}},
}}

// loadCover is one cover or none: a row whose thumbnail does not arrive or does not decode
// simply has no cover, and is not asked again.
func loadCover(ctx context.Context, url string, cw, ch int) *coverImg {
	b, err := fetchCover(ctx, url)
	if err != nil {
		return nil
	}
	img, _ := fitCover(b, cw, ch)
	return img
}

func fetchCover(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := coverClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, coverMaxBytes))
}

// fitCover decodes a thumbnail and fits it into the box (never stretched: a square album
// cover stays square), as a PNG the terminal decodes itself.
func fitCover(b []byte, cw, ch int) (*coverImg, error) {
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	bw, bh := coverCols*cw, coverRows*ch
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if sw == 0 || sh == 0 {
		return nil, fmt.Errorf("empty image")
	}
	w, h := bw, sh*bw/sw
	if h > bh {
		w, h = sw*bh/sh, bh
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(w, 1), max(h, 1)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return &coverImg{b64: base64.StdEncoding.EncodeToString(buf.Bytes()), cols: (w + cw - 1) / cw}, nil
}

// kittyPut is the cover as kitty graphics: chunks of 4096, the last one carrying m=0. It
// gives only r and lets the terminal work out the columns; C=1 leaves the cursor where it
// was (a=T would push it past the image and shove the frame up a line), q=2 keeps the
// terminal silent (any reply would arrive as keypresses).
func kittyPut(b64 string) string {
	var sb strings.Builder
	for i := 0; i < len(b64); i += 4096 {
		j := min(i+4096, len(b64))
		more := 1
		if j == len(b64) {
			more = 0
		}
		if i == 0 {
			fmt.Fprintf(&sb, "\x1b_Ga=T,f=100,i=%d,r=%d,q=2,C=1,m=%d;%s\x1b\\", coverID, coverRows, more, b64[i:j])
		} else {
			fmt.Fprintf(&sb, "\x1b_Gm=%d;%s\x1b\\", more, b64[i:j])
		}
	}
	return sb.String()
}

// kittyDel deletes this program's placement by id — never d=a, which would take every image
// on the screen, a multiplexer neighbour's included. d=I also frees the data, for the exit.
func kittyDel(free bool) string {
	d := "i"
	if free {
		d = "I"
	}
	return fmt.Sprintf("\x1b_Ga=d,d=%s,i=%d,q=2\x1b\\", d, coverID)
}

// ClearCover is the exit's delete, written after the UI has let go of the terminal.
func ClearCover(c Cover) {
	if c.On {
		os.Stdout.WriteString(kittyDel(true))
	}
}
