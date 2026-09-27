// Package config is the suite's configuration chain, read the way every shell entry point
// reads it: flag > environment (TING_* before UT_*) > the user's file > the shipped file.
// The file format is a frozen contract (ARCH-cli-contract.md「配置面」), so this is a port of
// ut_read_config, not a reinterpretation: KEY=value lines, `#` to end of line, one matching
// quote pair stripped, a leading ~/ expanded, the first assignment of a key wins.
//
// Flags are the caller's to apply on top; this package answers the other three layers.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var keyRe = regexp.MustCompile(`^(TING|UT|YT|BILI|NE)_[A-Z0-9_]+$`)

// Names a file may never set, for the reasons ut_read_config states: the two config paths (a
// file cannot move itself), the version (a data file, not a setting), the engine dir (it names
// programs the suite runs), and the four the player sets for its own detached child.
var refused = map[string]bool{
	"TING_CONFIG": true, "TING_DEFAULTS": true, "TING_ENGINE_DIR": true, "TING_VERSION": true,
	"UT_CONFIG": true, "UT_DEFAULTS": true, "UT_ENGINE_DIR": true, "UT_VERSION": true,
	"YT_IPC_SOCK": true, "YT_DETACHED": true, "YT_PLAYER_ID": true, "YT_DETACHED_LOG": true,
}

// envRenamed is the `for _v in …` list duplicated in every shell entry point: the knobs whose
// TING_ environment spelling is honoured. A file honours TING_ for every key.
var envRenamed = strings.Fields("STATE_DIR ENGINE_DIR HISTORY THEME VOLUME LANG IMAGE KEYS " +
	"RESOURCE ASCII SYNC PLAY_QUALITY VIZ_STYLE VIZ_COLOR DEFAULT_ENGINE")

// merged maps a key that was folded into another onto the key that replaced it: the old
// name still reads, one version, in the environment and in a file.
var merged = map[string]string{
	"UT_START_RESULTS":    "UT_SEARCH_RESULTS",
	"YT_COOKIE_BROWSER":   "UT_COOKIE_BROWSER",
	"BILI_COOKIE_BROWSER": "UT_COOKIE_BROWSER",
	"NE_COOKIE_BROWSER":   "UT_COOKIE_BROWSER",
}

// Config holds the resolved value of every key any layer set. TING_X and UT_X are one key,
// stored under its UT_ spelling.
type Config struct {
	vals   map[string]string
	pinned map[string]bool // set by the environment: never written back
	// UserPath is the user's file this chain read (or would have): the write-back target.
	UserPath string
}

// canon folds the new-name spelling onto the old one, and a merged key onto the key that
// replaced it, so every spelling looks up the same slot.
func canon(key string) string {
	if k, ok := merged[key]; ok {
		return k
	}
	if strings.HasPrefix(key, "TING_") {
		return "UT_" + strings.TrimPrefix(key, "TING_")
	}
	return key
}

// Get returns a key's value by either spelling, and whether any layer set it.
func (c *Config) Get(key string) (string, bool) {
	v, ok := c.vals[canon(key)]
	return v, ok
}

// Value is Get without the second result.
func (c *Config) Value(key string) string {
	v, _ := c.vals[canon(key)]
	return v
}

// Pinned reports whether the environment set this key, which is what keeps it out of the
// write-back.
func (c *Config) Pinned(key string) bool { return c.pinned[canon(key)] }

// UserPath resolves the user's file: TING_CONFIG, then UT_CONFIG, then
// $XDG_CONFIG_HOME/ting/config when it is readable, else the pre-rename uting/config.
func UserPath(getenv func(string) (string, bool)) string {
	if v, ok := getenv("TING_CONFIG"); ok && v != "" {
		return v
	}
	if v, ok := getenv("UT_CONFIG"); ok && v != "" {
		return v
	}
	base, _ := getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := getenv("HOME")
		base = filepath.Join(home, ".config")
	}
	if f, err := os.Open(filepath.Join(base, "ting", "config")); err == nil {
		f.Close()
		return filepath.Join(base, "ting", "config")
	}
	return filepath.Join(base, "uting", "config")
}

// MissingDefaultsError is a broken install: the shipped file declares every default in the
// suite and is not optional, so a caller exits 2 on it, as every shell entry point does.
type MissingDefaultsError struct{ Path string }

func (e *MissingDefaultsError) Error() string {
	return fmt.Sprintf("cannot read the shipped defaults at %s — that file declares every default value in the suite and is not optional", e.Path)
}

// Load resolves the chain. environ is os.Environ()-shaped; shipped is <install>/config.
func Load(environ []string, shipped string) (*Config, error) {
	env := map[string]string{}
	for _, kv := range environ {
		if i := strings.IndexByte(kv, '='); i > 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}
	getenv := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	c := &Config{vals: map[string]string{}, pinned: map[string]bool{}, UserPath: UserPath(getenv)}

	// The environment: set-ness, not emptiness, is what counts (`${TING_X+x}`). Only the
	// renamed knobs have a TING_ spelling in the environment — the list every entry point
	// carries verbatim — and there the new name wins over the old one.
	for k, v := range env {
		if keyRe.MatchString(k) && !strings.HasPrefix(k, "TING_") && merged[k] == "" {
			c.vals[k], c.pinned[k] = v, true
		}
	}
	// In the shell's order, so when two old names disagree the same one wins.
	for _, old := range []string{"UT_START_RESULTS", "YT_COOKIE_BROWSER", "BILI_COOKIE_BROWSER", "NE_COOKIE_BROWSER"} {
		k := merged[old]
		if v, ok := env[old]; ok && !c.pinned[k] {
			c.vals[k], c.pinned[k] = v, true
		}
	}
	for _, n := range envRenamed {
		if v, ok := env["TING_"+n]; ok {
			c.vals["UT_"+n], c.pinned["UT_"+n] = v, true
		}
	}

	home := env["HOME"]
	if err := c.read(c.UserPath, home); err != nil && !os.IsNotExist(err) && !os.IsPermission(err) {
		return nil, err
	}
	if err := c.read(shipped, home); err != nil {
		return nil, &MissingDefaultsError{Path: shipped}
	}
	return c, nil
}

func (c *Config) read(path, home string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := ParseLine(sc.Text(), home)
		if !ok {
			continue
		}
		if _, set := c.vals[canon(key)]; !set {
			c.vals[canon(key)] = val
		}
	}
	return sc.Err()
}

// ParseLine is one line of ut_read_config: it returns the key and value a line assigns, or
// ok=false for a comment, a blank, a non-assignment, a key outside the five prefixes, or a
// refused key.
func ParseLine(line, home string) (key, val string, ok bool) {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimFunc(line, isSpace)
	i := strings.IndexByte(line, '=')
	if line == "" || i < 0 {
		return "", "", false
	}
	key = strings.TrimRightFunc(line[:i], isSpace)
	val = strings.TrimLeftFunc(line[i+1:], isSpace)
	if !keyRe.MatchString(key) || refused[key] {
		return "", "", false
	}
	if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
		val = val[1 : len(val)-1]
	}
	if strings.HasPrefix(val, "~/") {
		val = home + "/" + val[2:]
	}
	return key, val, true
}

// isSpace is [[:space:]] in the C locale, which is what the shell loop strips.
func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f'
}
