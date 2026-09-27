// Package verb is the only place the Go TUI runs a process. It builds an argv for one of the
// suite's commands, runs it, decodes the one-line envelope, and maps the exit code onto a
// typed error. Every key handler goes through it, which makes it where the layering rule is
// enforced (PLAN-go-tui.md「切分原则」): nothing here, and so nothing in the binary, names mpv,
// yt-dlp, a socket path or a site. Engine names pass through as opaque strings, and every
// engine verb is asked of ting-play, which forwards it to the engine.
package verb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Kind is the exit-code class (ARCH-cli-contract.md「退出码、TTY、依赖」).
type Kind int

const (
	Usage        Kind = iota // 1: the argv was wrong
	External                 // 2+: a missing tool or a propagated engine/player failure
	NotEffective             // 4: a well-formed call that did not take effect
)

// Error is a verb that exited non-zero. Status and Reason come from its envelope when it
// printed one; Stderr is what it said to a person.
type Error struct {
	Argv   []string
	Code   int
	Status string
	Reason string
	Stderr string
}

func (e *Error) Kind() Kind {
	switch {
	case e.Code == 1:
		return Usage
	case e.Code == 4:
		return NotEffective
	default:
		return External
	}
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
		msg = msg[i+1:]
	}
	msg = strings.TrimPrefix(msg, "Error: ")
	switch {
	case msg != "":
	case e.Reason != "":
		msg = e.Reason
	case e.Status != "":
		msg = e.Status
	default:
		msg = fmt.Sprintf("exit %d", e.Code)
	}
	return fmt.Sprintf("%s: %s", filepath.Base(e.Argv[0]), msg)
}

// Suite is where the three suite commands the TUI calls by name live.
type Suite struct {
	TPlay     string
	TPlaylist string // "" when absent: the contract degrades, it does not fail
	THistory  string
}

// Locate finds the suite the way every shell entry point finds its siblings: beside this
// binary's RESOLVED path, then on PATH. Only ting-play is required.
func Locate() (*Suite, error) {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	dir := ""
	if err == nil {
		dir = filepath.Dir(exe)
	}
	return LocateIn(dir)
}

// LocateIn is Locate with the sibling directory given.
func LocateIn(dir string) (*Suite, error) {
	find := func(name string) string {
		if dir != "" {
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
				return p
			}
		}
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
		return ""
	}
	s := &Suite{TPlay: find("ting-play"), TPlaylist: find("ting-playlist"), THistory: find("ting-history")}
	if s.TPlay == "" {
		return nil, errors.New("cannot locate ting-play beside this binary or on PATH")
	}
	return s, nil
}

// run executes argv with no stdin and decodes the LAST stdout line into out (every verb
// prints one line; the last is the envelope even if something chatted before it). A non-zero
// exit is an *Error, with the envelope's status/reason when there is one.
func run(ctx context.Context, argv []string, out any) error {
	return runIn(ctx, argv, nil, out)
}

// runIn is run with stdin: the queue verbs read their items there.
func runIn(ctx context.Context, argv []string, stdin []byte, out any) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	line := lastLine(stdout.Bytes())
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		ve := &Error{Argv: argv, Code: exitErr.ExitCode(), Stderr: stderr.String()}
		var env struct {
			Status string  `json:"status"`
			Reason *string `json:"reason"`
		}
		if json.Unmarshal(line, &env) == nil {
			ve.Status = env.Status
			if env.Reason != nil {
				ve.Reason = *env.Reason
			}
		}
		return ve
	}
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(argv[0]), err)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(line, out); err != nil {
		return fmt.Errorf("%s: unreadable envelope: %w", filepath.Base(argv[0]), err)
	}
	return nil
}

func lastLine(b []byte) []byte {
	b = bytes.TrimRight(b, "\n")
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		return b[i+1:]
	}
	return b
}

// Engine is one installed source: its opaque name, the file ting-play runs for it (for
// diagnosis, not for running), and the engine flags it accepts through ting-play.
type Engine struct {
	Name  string   `json:"name"`
	Bin   string   `json:"bin"`
	Flags []string `json:"flags"`
}

// Has reports whether the engine accepts one flag, e.g. "--transcript".
func (e Engine) Has(flag string) bool {
	for _, f := range e.Flags {
		if f == flag {
			return true
		}
	}
	return false
}

// Engines asks ting-play for the registry. An empty list is an answer, not an error.
func (s *Suite) Engines(ctx context.Context) ([]Engine, error) {
	var env struct {
		Engines []Engine `json:"engines"`
	}
	if err := run(ctx, []string{s.TPlay, "--engines", "-j"}, &env); err != nil {
		return nil, err
	}
	return env.Engines, nil
}

// Result is one search row: the fields the TUI draws, not the whole envelope.
type Result struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	Channel     string   `json:"channel"`
	Duration    *float64 `json:"duration"`
	DurationFmt *string  `json:"duration_fmt"`
	ViewCount   *int64   `json:"view_count"`
	LiveStatus  string   `json:"live_status"`
	Description string   `json:"description"`
	Kind        string   `json:"kind"`
	Access      string   `json:"access"`
	Thumbnail   string   `json:"thumbnail"`
}

// SearchResult is the search envelope.
type SearchResult struct {
	Status  string   `json:"status"`
	Engine  string   `json:"engine"`
	Query   string   `json:"query"`
	Count   int      `json:"count"`
	Results []Result `json:"results"`
}

// SearchOpts are the flags of ting-play --search; zero values are left to the engine's defaults.
type SearchOpts struct {
	N      int
	MinDur int
	MaxDur int
	Sort   string
}

// Search asks one engine, through ting-play.
func (s *Suite) Search(ctx context.Context, engine, query string, o SearchOpts) (*SearchResult, error) {
	argv := []string{s.TPlay, "--search", "--engine", engine, "-j"}
	if o.N > 0 {
		argv = append(argv, "-n", fmt.Sprint(o.N))
	}
	if o.MinDur > 0 {
		argv = append(argv, "--min-duration", fmt.Sprint(o.MinDur))
	}
	if o.MaxDur > 0 {
		argv = append(argv, "--max-duration", fmt.Sprint(o.MaxDur))
	}
	if o.Sort != "" {
		argv = append(argv, "--sort", o.Sort)
	}
	argv = append(argv, "--", query)
	var r SearchResult
	if err := run(ctx, argv, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// PlayOpts are the launch flags; zero values are left to ting-play's own defaults.
type PlayOpts struct {
	Mode    string
	Quality string
	Volume  string // "" = mpv's own
	Loop    string // off | one; "" = ting-play's own
}

func (o PlayOpts) argv() []string {
	var a []string
	if o.Mode != "" {
		a = append(a, "-f", o.Mode)
	}
	if o.Quality != "" {
		a = append(a, "--quality", o.Quality)
	}
	if o.Volume != "" {
		a = append(a, "--volume", o.Volume)
	}
	if o.Loop != "" {
		a = append(a, "--loop", o.Loop)
	}
	return a
}

// Started is the -d -j envelope, less the two private paths it also carries.
type Started struct {
	ID    string  `json:"id"`
	PID   int     `json:"pid"`
	URL   string  `json:"url"`
	Mode  string  `json:"mode"`
	Title *string `json:"title"`
}

// Play starts a detached player.
func (s *Suite) Play(ctx context.Context, engine, url string, o PlayOpts) (*Started, error) {
	argv := append([]string{s.TPlay, "-d", "-j", "--engine", engine}, o.argv()...)
	argv = append(argv, "--", url)
	var st Started
	if err := run(ctx, argv, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// QueueItem is one entry of a queue: engine per item, so one queue can mix sources.
type QueueItem struct {
	Engine   string   `json:"engine"`
	URL      string   `json:"url"`
	Title    string   `json:"title,omitempty"`
	Duration *float64 `json:"duration,omitempty"`
}

// PlayQueue starts a detached player on a queue: the first item plays, the rest wait.
func (s *Suite) PlayQueue(ctx context.Context, items []QueueItem, o PlayOpts) (*Started, error) {
	in, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	argv := append([]string{s.TPlay, "-d", "-j"}, o.argv()...)
	argv = append(argv, "--queue", "-")
	var st Started
	if err := runIn(ctx, argv, in, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Enqueue appends items to a running player's queue. owner is this process's PID, which
// keeps a copy --undo can put back.
func (s *Suite) Enqueue(ctx context.Context, id string, items []QueueItem, owner int) error {
	in, err := json.Marshal(items)
	if err != nil {
		return err
	}
	argv := []string{s.TPlay, "--enqueue", "-", "--id", id, "-j"}
	if owner > 0 {
		argv = append(argv, "--owner", fmt.Sprint(owner))
	}
	return runIn(ctx, argv, in, nil)
}

// Next skips to the next queued track; NotEffective when there is none.
func (s *Suite) Next(ctx context.Context, id string) error {
	return run(ctx, []string{s.TPlay, "--next", "--id", id, "-j"}, nil)
}

// SetVolume sets a detached player's live volume, 0-100.
func (s *Suite) SetVolume(ctx context.Context, id string, v int) error {
	return run(ctx, []string{s.TPlay, "--set-volume", fmt.Sprint(v), "--id", id, "-j"}, nil)
}

// Seek moves the playhead by delta seconds; the sign is always spelled.
func (s *Suite) Seek(ctx context.Context, id string, delta int) error {
	return run(ctx, []string{s.TPlay, "--seek", fmt.Sprintf("%+d", delta), "--id", id, "-j"}, nil)
}

// SetLoop turns repeat on (one) or off while a player runs.
func (s *Suite) SetLoop(ctx context.Context, id, loop string) error {
	return run(ctx, []string{s.TPlay, "--set-loop", loop, "--id", id, "-j"}, nil)
}

// Auth is an engine's cookie decision.
type Auth struct {
	Auth           string `json:"auth"`
	CookieBrowser  string `json:"cookie_browser"`
	CookieReadable *bool  `json:"cookie_readable"`
}

// Auth asks one engine, through ting-play.
func (s *Suite) Auth(ctx context.Context, engine string) (*Auth, error) {
	var a Auth
	if err := run(ctx, []string{s.TPlay, "--auth", "--engine", engine, "-j"}, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// Stop stops one player.
func (s *Suite) Stop(ctx context.Context, id string) error {
	return run(ctx, []string{s.TPlay, "--stop", "-j", "--id", id}, nil)
}

// Pause and Resume toggle a player.
func (s *Suite) Pause(ctx context.Context, id string) error {
	return run(ctx, []string{s.TPlay, "--pause", "-j", "--id", id}, nil)
}

func (s *Suite) Resume(ctx context.Context, id string) error {
	return run(ctx, []string{s.TPlay, "--resume", "-j", "--id", id}, nil)
}

// Player is one live record from --status, less its private paths.
type Player struct {
	ID       string   `json:"id"`
	PID      int      `json:"pid"`
	URL      string   `json:"url"`
	Engine   string   `json:"engine"`
	Mode     string   `json:"mode"`
	Title    *string  `json:"title"`
	Paused   *bool    `json:"paused"`
	Position *float64 `json:"position"`
	Duration *float64 `json:"duration"`
	Volume   *float64 `json:"volume"`
}

// Status lists the live players.
func (s *Suite) Status(ctx context.Context) ([]Player, error) {
	var env struct {
		Players []Player `json:"players"`
	}
	if err := run(ctx, []string{s.TPlay, "--status", "-j"}, &env); err != nil {
		return nil, err
	}
	return env.Players, nil
}
