// Command ting is the suite's human face, rebuilt in Go (PLAN-go-tui.md). It is one more
// caller of the CLI contract, level with an agent: every effect is a verb from internal/verb.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/binlecode/ting/internal/config"
	"github.com/binlecode/ting/internal/tui"
	"github.com/binlecode/ting/internal/verb"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
)

func die(code int, format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ting: "+format+"\n", a...)
	os.Exit(code)
}

// installDir is where VERSION and the shipped config live: one level above this binary's
// RESOLVED directory, the rule every shell entry point follows.
func installDir() string {
	exe, err := os.Executable()
	if err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
	}
	return filepath.Dir(filepath.Dir(exe))
}

const usage = `ting — interactive terminal browser for the installed engines (Go build, in progress)

Usage:
  ting [options] [search query]

Options:
  -n NUM         Number of results to fetch (default: TING_SEARCH_RESULTS)
  --engine NAME  Which source to search (default: TING_DEFAULT_ENGINE, else the first installed)
  -f MODE        Playback mode: audio | video | fast (default: TING_PLAY_MODE)
  --volume N     mpv startup volume, 0-100
  -h             This help
  -V             Print the suite version

Keys: ↑/↓ j/k move · Enter play · Space pause/resume · s stop · n new search
      e switch source · q quit (stops a player this session started)
`

type flags struct {
	engine, mode, volume, n string
	query                   []string
}

func parseArgs(args []string) flags {
	var f flags
	need := func(i int, name string) string {
		if i+1 >= len(args) {
			die(1, "%s requires a value", name)
		}
		return args[i+1]
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			f.query = append(f.query, args[i+1:]...)
			return f
		case a == "-h" || a == "--help":
			fmt.Print(usage)
			os.Exit(0)
		case a == "-V" || a == "--version":
			v, err := os.ReadFile(filepath.Join(installDir(), "VERSION"))
			ver := "unknown"
			if err == nil {
				ver = strings.TrimSpace(string(v))
			}
			fmt.Printf("ting %s\n", ver)
			os.Exit(0)
		case a == "--engine":
			f.engine = need(i, a)
			i++
		case strings.HasPrefix(a, "--engine="):
			f.engine = strings.TrimPrefix(a, "--engine=")
		case a == "-f":
			f.mode = need(i, a)
			i++
		case a == "--volume":
			f.volume = need(i, a)
			i++
		case strings.HasPrefix(a, "--volume="):
			f.volume = strings.TrimPrefix(a, "--volume=")
		case a == "-n":
			f.n = need(i, a)
			i++
		case strings.HasPrefix(a, "-") && len(a) > 1:
			die(1, "unknown flag '%s' (see ting -h)", a)
		default:
			f.query = append(f.query, a)
		}
	}
	return f
}

// pick is flag > config: the config chain already folded environment over the two files.
func pick(flag string, cfg *config.Config, key string) string {
	if flag != "" {
		return flag
	}
	return cfg.Value(key)
}

func main() {
	f := parseArgs(os.Args[1:])

	cfg, err := config.Load(os.Environ(), filepath.Join(installDir(), "config"))
	if err != nil {
		var md *config.MissingDefaultsError
		if errors.As(err, &md) {
			die(2, "%s", md.Error())
		}
		die(2, "%v", err)
	}

	mode := pick(f.mode, cfg, "TING_PLAY_MODE")
	switch mode {
	case "audio", "video", "fast", "":
	default:
		die(1, "-f must be one of: audio, video, fast (playback is detached — ascii/viz need a terminal to draw on)")
	}
	volume := pick(f.volume, cfg, "TING_VOLUME")
	if volume != "" {
		if v, err := strconv.Atoi(volume); err != nil || v < 0 || v > 100 {
			die(1, "--volume must be between 0 and 100")
		}
	}
	n := 0
	if s := pick(f.n, cfg, "TING_SEARCH_RESULTS"); s != "" {
		if n, err = strconv.Atoi(s); err != nil || n < 1 {
			die(1, "-n must be a positive integer")
		}
	}
	pageRows, _ := strconv.Atoi(cfg.Value("TING_PAGE_ROWS"))

	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		die(1, "requires a terminal (interactive menu); use %s-search / t-play headless", cfg.Value("TING_DEFAULT_ENGINE"))
	}

	suite, err := verb.Locate()
	if err != nil {
		die(1, "%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	engines, err := suite.Engines(ctx)
	if err != nil {
		die(2, "%v", err)
	}
	if len(engines) == 0 {
		die(1, "no engine found — need a <name>-search and <name>-resolve pair beside t-play, in $TING_ENGINE_DIR, or on PATH")
	}
	names := make([]string, len(engines))
	idx := -1
	want := pick(f.engine, cfg, "TING_DEFAULT_ENGINE")
	for i, e := range engines {
		names[i] = e.Name
		if e.Name == want {
			idx = i
		}
	}
	if idx < 0 {
		if f.engine != "" {
			die(1, "--engine must be one of: %s", strings.Join(names, " "))
		}
		idx = 0
	}

	players, err := suite.Status(ctx)
	if err != nil {
		players = nil
	}

	m := tui.New(ctx, suite, tui.Options{
		Engines:   engines,
		Engine:    idx,
		Query:     strings.Join(f.query, " "),
		Search:    verb.SearchOpts{N: n},
		Play:      verb.PlayOpts{Mode: mode, Quality: cfg.Value("TING_PLAY_QUALITY"), Volume: volume},
		PageRows:  pageRows,
		AdoptFrom: players,
	})
	_, runErr := tea.NewProgram(m).Run()
	m.Close()
	cancel()
	if id := m.SessionPlayer(); id != "" {
		sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = suite.Stop(sctx, id)
		scancel()
	}
	if runErr != nil {
		die(2, "%v", runErr)
	}
}
