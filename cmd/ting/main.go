// Command ting is the suite's human face, rebuilt in Go (PLAN-go-tui.md). It is one more
// caller of the CLI contract, level with an agent: every effect is a verb from internal/verb.
package main

import (
	"context"
	_ "embed"
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

// usage is the help: the key table and the environment, stated once, here.
//
//go:embed usage.txt
var usage string

type flags struct {
	engine, mode, volume, n, sort, minDur, maxDur, pageRows, color, theme string
	query                                                                 []string
}

func parseArgs(args []string) flags {
	var f flags
	need := func(i int, name string) string {
		if i+1 >= len(args) {
			die(1, "%s requires a value", name)
		}
		return args[i+1]
	}
	// valued is every flag that takes a value, by its one spelling; --name=value is accepted
	// for the long ones, as the shell entry points accept it.
	valued := map[string]*string{
		"--engine": &f.engine, "-f": &f.mode, "--volume": &f.volume, "-n": &f.n,
		"--sort": &f.sort, "--min-duration": &f.minDur, "--max-duration": &f.maxDur,
		"-p": &f.pageRows, "--color": &f.color, "--theme": &f.theme,
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if dst, ok := valued[a]; ok {
			*dst = need(i, a)
			i++
			continue
		}
		if k, v, ok := strings.Cut(a, "="); ok && strings.HasPrefix(k, "--") {
			if dst, ok := valued[k]; ok {
				*dst = v
				continue
			}
		}
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
		case strings.HasPrefix(a, "-") && len(a) > 1:
			die(1, "unknown flag '%s' (ting flags: -n --min-duration --max-duration --sort -f -p --engine --color --theme --volume); run 'ting -h'", a)
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

// uint parses a non-negative integer the way is_uint does: digits only.
func uint(s string) (int, bool) {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// oneOf reports whether v is one of the words in set.
func oneOf(v string, set ...string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
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

	// The flag gate, in the order the shell entry point runs it; every one exits 1 and names
	// its knob, and all of them come before the TTY gate.
	pageRows, ok := uint(pick(f.pageRows, cfg, "TING_PAGE_ROWS"))
	if !ok || pageRows < 1 {
		die(1, "-p must be a positive integer (TING_PAGE_ROWS)")
	}
	n, ok := uint(pick(f.n, cfg, "TING_SEARCH_RESULTS"))
	if !ok || n < 1 {
		die(1, "-n must be a positive integer (TING_SEARCH_RESULTS)")
	}
	if f.minDur == "" {
		f.minDur = "0"
	}
	if f.maxDur == "" {
		f.maxDur = "0"
	}
	minDur, ok := uint(f.minDur)
	if !ok {
		die(1, "--min-duration must be a non-negative integer")
	}
	maxDur, ok := uint(f.maxDur)
	if !ok {
		die(1, "--max-duration must be a non-negative integer")
	}
	if maxDur != 0 && maxDur <= minDur {
		die(1, "--max-duration must be greater than --min-duration when both are set")
	}
	sort := pick(f.sort, cfg, "TING_SORT_FIELD")
	if !oneOf(sort, "relevance", "view_count", "duration") {
		die(1, "--sort must be one of: relevance, view_count, duration")
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
		die(1, "no engine found — need a ting-engine-<name> file beside ting-play, in $TING_ENGINE_DIR, or on PATH")
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

	mode := pick(f.mode, cfg, "TING_PLAY_MODE")
	if !oneOf(mode, "audio", "video", "fast") {
		die(1, "-f must be one of: audio, video, fast (ascii/viz need a terminal, which detached playback has none of)")
	}
	color := f.color
	if color == "" {
		color = "auto"
	}
	if !oneOf(color, "auto", "always", "never") {
		die(1, "--color must be one of: auto, always, never")
	}
	theme := pick(f.theme, cfg, "TING_THEME")
	if theme == "" {
		theme = "minimal"
	}
	if !oneOf(theme, tui.ThemeNames...) {
		die(1, "--theme must be one of: %s", strings.Join(tui.ThemeNames, ", "))
	}
	if bg := cfg.Value("TING_BG"); bg != "" && !oneOf(bg, "auto", "light", "dark") {
		die(1, "TING_BG must be one of: auto, light, dark")
	}
	volume := pick(f.volume, cfg, "TING_VOLUME")
	if volume != "" {
		if v, ok := uint(volume); !ok || v > 100 {
			die(1, "--volume must be an integer 0-100")
		}
	}
	quality := cfg.Value("TING_PLAY_QUALITY")
	if quality == "" {
		quality = "auto"
	}
	if !oneOf(quality, "auto", "low", "medium", "high") {
		die(1, "TING_PLAY_QUALITY must be one of: auto, low, medium, high")
	}
	loop := cfg.Value("TING_LOOP_MODE")
	if loop == "" {
		loop = "off"
	}
	if !oneOf(loop, "off", "seq", "one") {
		die(1, "TING_LOOP_MODE must be off, seq or one (got '%s')", loop)
	}
	keys := cfg.Value("TING_KEYS")
	if keys == "" {
		keys = "core"
	}
	if !oneOf(keys, "core", "full", "hidden") {
		die(1, "TING_KEYS must be one of: core, full, hidden")
	}
	listMode := cfg.Value("TING_LIST_MODE")
	if listMode == "" {
		listMode = "scroll"
	}
	if !oneOf(listMode, "scroll", "page") {
		die(1, "TING_LIST_MODE must be scroll or page (got '%s')", listMode)
	}
	var rowIndex bool
	switch ri := cfg.Value("TING_ROW_INDEX"); ri {
	case "", "off", "0":
	case "on", "1":
		rowIndex = true
	default:
		die(1, "TING_ROW_INDEX must be on or off (got '%s')", ri)
	}
	resource := cfg.Value("TING_RESOURCE")
	if !oneOf(resource, "0", "1") {
		die(1, "TING_RESOURCE must be 0 or 1")
	}
	batch, ok := uint(cfg.Value("TING_FETCH_BATCH"))
	if !ok || batch < 1 {
		die(1, "TING_FETCH_BATCH must be a positive integer")
	}
	ambig := cfg.Value("TING_AMBIG_WIDE") == "1"
	lang, ok := tui.DetectLang(cfg.Value("TING_LANG"), os.Getenv)
	if !ok {
		die(1, "TING_LANG must be en or zh (got '%s')", cfg.Value("TING_LANG"))
	}

	image := cfg.Value("TING_IMAGE")
	if image == "" {
		image = "auto"
	}
	if !oneOf(image, "auto", "on", "off") {
		die(1, "TING_IMAGE must be one of: auto, on, off (got '%s')", image)
	}
	colors := color == "always" || color == "auto" && os.Getenv("NO_COLOR") == ""
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		die(1, "requires a terminal (interactive menu); use 'ting-play --search' / ting-play headless")
	}

	bg := tui.DetectBackground(cfg.Value("TING_BG"))
	cover := tui.DetectCover(image)
	ct := os.Getenv("COLORTERM")
	players, err := suite.Status(ctx)
	if err != nil {
		players = nil
	}

	m := tui.New(ctx, suite, tui.Options{
		Engines:   engines,
		Engine:    idx,
		Query:     strings.Join(f.query, " "),
		Search:    verb.SearchOpts{N: n, MinDur: minDur, MaxDur: maxDur, Sort: sort},
		Play:      verb.PlayOpts{Mode: mode, Quality: quality, Volume: volume},
		Loop:      loop,
		PageRows:  pageRows,
		Batch:     batch,
		Keys:      keys,
		ListMode:  listMode,
		RowIndex:  rowIndex,
		Resource:  resource == "1",
		Colors:    colors,
		Theme:     theme,
		BG:        bg,
		TrueColor: ct == "truecolor" || ct == "24bit",
		Pinned:    cfg.Pinned,
		Cover:     cover,
		Save:      cfg.WriteBack,
		Lang:      lang,
		ASCII:     tui.DetectASCII(cfg.Value("TING_ASCII"), os.Getenv),
		AmbigWide: ambig,
		AdoptFrom: players,
	})
	_, runErr := tea.NewProgram(m, tea.WithAltScreen()).Run()
	m.Close()
	tui.ClearCover(cover)
	m.Discard()
	if err := m.Flush(); err != nil || m.SaveFailed() {
		fmt.Println("Config: could not be written — this session's preferences were not saved")
	}
	cancel()
	if id := m.SessionPlayer(); id != "" {
		sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := suite.Stop(sctx, id); err != nil {
			fmt.Fprintf(os.Stderr, "ting: the player could not be stopped and is still playing — stop it with: ting-play --stop --id %s\n", id)
		}
		scancel()
	}
	if runErr != nil {
		die(2, "%v", runErr)
	}
	if err := m.Fatal(); err != nil {
		die(2, "%v", err)
	}
}
