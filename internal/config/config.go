// Package config is the suite's configuration chain, read the way every shell entry point
// reads it: flag > environment > the user's file > the shipped file.
// The file format is a frozen contract (ARCH-cli-contract.md「配置面」), so this is a port of
// read_config, not a reinterpretation: KEY=value lines, `#` to end of line, one matching
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

var keyRe = regexp.MustCompile(`^TING_[A-Z0-9_]+$`)

// Names a file may never set, for the reasons read_config states: the config path (a file
// cannot move itself) and the engine dir (it names programs the suite runs). Constants and the
// player's private _TING_* names are outside keyRe, so they need no refusal.
var refused = map[string]bool{"TING_CONFIG": true, "TING_ENGINE_DIR": true}

// Config holds the resolved value of every key any layer set.
type Config struct {
	vals   map[string]string
	pinned map[string]bool // set by the environment: never written back
	// UserPath is the user's file this chain read (or would have): the write-back target.
	UserPath string
}

// Get returns a key's value by either spelling, and whether any layer set it.
func (c *Config) Get(key string) (string, bool) {
	v, ok := c.vals[key]
	return v, ok
}

// Value is Get without the second result.
func (c *Config) Value(key string) string {
	v, _ := c.vals[key]
	return v
}

// Pinned reports whether the environment set this key, which is what keeps it out of the
// write-back.
func (c *Config) Pinned(key string) bool { return c.pinned[key] }

// UserPath resolves the user's file: TING_CONFIG, else $XDG_CONFIG_HOME/ting/config.
func UserPath(getenv func(string) (string, bool)) string {
	if v, ok := getenv("TING_CONFIG"); ok && v != "" {
		return v
	}
	base, _ := getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := getenv("HOME")
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "ting", "config")
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

	// The environment: set-ness, not emptiness, is what counts (`${TING_X+x}`).
	for k, v := range env {
		if keyRe.MatchString(k) {
			c.vals[k], c.pinned[k] = v, true
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
		if _, set := c.vals[key]; !set {
			c.vals[key] = val
		}
	}
	return sc.Err()
}

// ParseLine is one line of read_config: it returns the key and value a line assigns, or
// ok=false for a comment, a blank, a non-assignment, a key outside TING_, or a refused key.
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
