// Package config is the suite's configuration chain, read the way every shell entry point
// reads it: flag > environment > the user's file > the shipped file.
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

var keyRe = regexp.MustCompile(`^TING_[A-Z0-9_]+$`)

// Names a file may never set, for the reasons ut_read_config states: the config path (a file
// cannot move itself) and the engine dir (it names programs the suite runs). Constants and the
// player's private _TING_* names are outside keyRe, so they need no refusal.
var refused = map[string]bool{"TING_CONFIG": true, "TING_ENGINE_DIR": true}

// renamedOrder is CFG_RENAMED, the OLD:NEW list every shell entry point carries: a key that
// was renamed or folded into another, and the key that replaced it. The old name still reads,
// one version, in the environment (while the new name is unset) and in a file. In the shell's
// order, so when two old names disagree the same one wins.
var renamedOrder = strings.Fields("UT_STATE_DIR:TING_STATE_DIR UT_ENGINE_DIR:TING_ENGINE_DIR UT_HISTORY:TING_HISTORY " +
	"UT_DEFAULT_ENGINE:TING_DEFAULT_ENGINE UT_SEARCH_RESULTS:TING_SEARCH_RESULTS " +
	"UT_COOKIE_BROWSER:TING_COOKIE_BROWSER UT_SORT_FIELD:TING_SORT_FIELD " +
	"UT_PLAY_MODE:TING_PLAY_MODE UT_VOLUME:TING_VOLUME UT_PLAY_QUALITY:TING_PLAY_QUALITY " +
	"UT_VIZ_STYLE:TING_VIZ_STYLE UT_VIZ_COLOR:TING_VIZ_COLOR UT_PAGE_ROWS:TING_PAGE_ROWS " +
	"UT_FETCH_BATCH:TING_FETCH_BATCH UT_LOOP_MODE:TING_LOOP_MODE UT_RESOURCE:TING_RESOURCE " +
	"UT_KEYS:TING_KEYS UT_LIST_MODE:TING_LIST_MODE UT_ROW_INDEX:TING_ROW_INDEX " +
	"UT_IMAGE:TING_IMAGE UT_START_RESULTS:TING_SEARCH_RESULTS " +
	"YT_COOKIE_BROWSER:TING_COOKIE_BROWSER BILI_COOKIE_BROWSER:TING_COOKIE_BROWSER " +
	"NE_COOKIE_BROWSER:TING_COOKIE_BROWSER YT_THEME:TING_THEME YT_LANG:TING_LANG " +
	"YT_ASCII:TING_ASCII YT_ASCII_VO:TING_ASCII_VO YT_MPV_INPUT_CONF:TING_MPV_INPUT_CONF " +
	"YT_BG:TING_BG YT_SYNC:TING_SYNC YT_AMBIG_WIDE:TING_AMBIG_WIDE " +
	"YT_AUDIO_FORMAT:TING_YT_AUDIO_FORMAT YT_VIDEO_FORMAT:TING_YT_VIDEO_FORMAT " +
	"YT_VIDEO_FORMAT_FAST:TING_YT_VIDEO_FORMAT_FAST YT_SUB_LANG_CHAIN:TING_YT_SUB_LANG_CHAIN " +
	"BILI_AUDIO_FORMAT:TING_BILI_AUDIO_FORMAT BILI_VIDEO_FORMAT:TING_BILI_VIDEO_FORMAT " +
	"BILI_VIDEO_FORMAT_FAST:TING_BILI_VIDEO_FORMAT_FAST BILI_UA:TING_BILI_UA " +
	"BILI_BUVID:TING_BILI_BUVID NE_AUDIO_FORMAT:TING_NE_AUDIO_FORMAT NE_UA:TING_NE_UA " +
	"NE_INCLUDE_VIP:TING_NE_INCLUDE_VIP")

// renamed is renamedOrder as a map, old name to new.
var renamed = func() map[string]string {
	m := map[string]string{}
	for _, p := range renamedOrder {
		i := strings.IndexByte(p, ':')
		m[p[:i]] = p[i+1:]
	}
	return m
}()

// Config holds the resolved value of every key any layer set, under its TING_ name.
type Config struct {
	vals   map[string]string
	pinned map[string]bool // set by the environment: never written back
	// UserPath is the user's file this chain read (or would have): the write-back target.
	UserPath string
}

// canon folds an old name onto the key that replaced it, so every spelling looks up the same
// slot.
func canon(key string) string {
	if k, ok := renamed[key]; ok {
		return k
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

// UserPath resolves the user's file: TING_CONFIG, then the old UT_CONFIG, then
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

	// The environment: set-ness, not emptiness, is what counts (`${TING_X+x}`). An old name
	// reads only while its new key is still unset, so the new name wins.
	for k, v := range env {
		if keyRe.MatchString(k) {
			c.vals[k], c.pinned[k] = v, true
		}
	}
	for _, p := range renamedOrder {
		old, k := p[:strings.IndexByte(p, ':')], p[strings.IndexByte(p, ':')+1:]
		if v, ok := env[old]; ok && !c.pinned[k] {
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
		if _, set := c.vals[canon(key)]; !set {
			c.vals[canon(key)] = val
		}
	}
	return sc.Err()
}

// ParseLine is one line of ut_read_config: it returns the key and value a line assigns, or
// ok=false for a comment, a blank, a non-assignment, a key outside TING_, or a refused key. An old name on CFG_RENAMED comes back as the key that replaced it.
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
	if k, ok := renamed[key]; ok {
		key = k
	}
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
