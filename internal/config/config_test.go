package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLine(t *testing.T) {
	cases := []struct {
		line, key, val string
		ok             bool
	}{
		{"TING_X=1", "TING_X", "1", true},
		{"  TING_X =  two words  # note", "TING_X", "two words", true},
		{`TING_X="quoted # not"`, "TING_X", `"quoted`, true}, // # cuts before the quote pair is seen
		{`TING_X="a b"`, "TING_X", "a b", true},
		{`TING_X='a'`, "TING_X", "a", true},
		{`TING_X="a'`, "TING_X", `"a'`, true},
		{"TING_X=~/music", "TING_X", "/home/u/music", true},
		{"TING_X=a~/b", "TING_X", "a~/b", true},
		{"TING_X=$(rm -rf /)", "TING_X", "$(rm -rf /)", true},
		{"TING_X=", "TING_X", "", true},
		{"UT_HISTORY=0", "TING_HISTORY", "0", true},
		{"UT_ENGINE_DIR=/evil", "", "", false},
		{"UT_VERSION=9", "", "", false},
		{"UT_UNKNOWN=1", "", "", false},
		{"TING_LANG=zh", "TING_LANG", "zh", true},
		{"PATH=/evil", "", "", false},
		{"ut_x=1", "", "", false},
		{"TING_ENGINE_DIR=/evil", "", "", false},
		{"TING_CONFIG=/x", "", "", false},
		{"_TING_IPC_SOCK=/x", "", "", false},
		{"YT_IPC_SOCK=/x", "", "", false},
		{"YT_THEME=nord", "TING_THEME", "nord", true},
		{"BILI_UA=x", "TING_BILI_UA", "x", true},
		{"YT_UNKNOWN=1", "", "", false},
		{"# TING_X=1", "", "", false},
		{"just words", "", "", false},
		{"", "", "", false},
		{"TING_X=1\r", "TING_X", "1", true},
	}
	for _, c := range cases {
		k, v, ok := ParseLine(c.line, "/home/u")
		if k != c.key || v != c.val || ok != c.ok {
			t.Errorf("ParseLine(%q) = %q %q %v, want %q %q %v", c.line, k, v, ok, c.key, c.val, c.ok)
		}
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChain(t *testing.T) {
	dir := t.TempDir()
	shipped := filepath.Join(dir, "config")
	user := filepath.Join(dir, "user")
	write(t, shipped, "TING_A=shipped\nTING_B=shipped\nTING_HISTORY=shipped\nTING_D=shipped\nTING_BG=shipped\n")
	write(t, user, "TING_B=user\nTING_B=second-line-loses\nUT_HISTORY=user-old-name\nTING_ENGINE_DIR=/evil\n")
	env := []string{
		"HOME=" + dir,
		"TING_CONFIG=" + user,
		"TING_D=env",
		"YT_BG=old-name",
		"TING_BG=new-name",     // a renamed key's new name wins over its old one
		"YT_SYNC=old-only",     // an old name alone still reads into the new key
		"UT_A=not-an-old-name", // UT_A is on no rename table
	}
	c, err := Load(env, shipped)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"TING_A": "shipped", "TING_B": "user", "TING_HISTORY": "user-old-name",
		"TING_D": "env", "TING_BG": "new-name", "TING_SYNC": "old-only",
	}
	for k, v := range want {
		if got := c.Value(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if _, ok := c.Get("TING_ENGINE_DIR"); ok {
		t.Error("a config file set TING_ENGINE_DIR")
	}
	if !c.Pinned("TING_D") || !c.Pinned("TING_BG") || !c.Pinned("YT_SYNC") || c.Pinned("TING_B") {
		t.Error("pinned should be exactly the environment's keys")
	}
	if c.UserPath != user {
		t.Errorf("UserPath = %q", c.UserPath)
	}
}

func TestUserPathFallsBackToLegacyName(t *testing.T) {
	dir := t.TempDir()
	env := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
	got := UserPath(env(map[string]string{"XDG_CONFIG_HOME": dir}))
	if got != filepath.Join(dir, "uting", "config") {
		t.Errorf("no ting/config: got %q", got)
	}
	os.MkdirAll(filepath.Join(dir, "ting"), 0o755)
	write(t, filepath.Join(dir, "ting", "config"), "")
	if got := UserPath(env(map[string]string{"XDG_CONFIG_HOME": dir})); got != filepath.Join(dir, "ting", "config") {
		t.Errorf("with ting/config: got %q", got)
	}
	if got := UserPath(env(map[string]string{"TING_CONFIG": "/a", "UT_CONFIG": "/b"})); got != "/a" {
		t.Errorf("TING_CONFIG should win: got %q", got)
	}
}

func TestMissingShippedIsItsOwnError(t *testing.T) {
	_, err := Load([]string{"HOME=/nonexistent"}, filepath.Join(t.TempDir(), "absent"))
	if _, ok := err.(*MissingDefaultsError); !ok {
		t.Fatalf("got %v", err)
	}
}

// The real shipped file parses, and a few of its values are what the shell reads.
func TestRealShippedConfig(t *testing.T) {
	c, err := Load([]string{"HOME=/nonexistent", "TING_CONFIG=/nonexistent"}, "../../config")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"TING_DEFAULT_ENGINE", "TING_SEARCH_RESULTS", "TING_PLAY_MODE", "TING_PAGE_ROWS"} {
		if c.Value(k) == "" {
			t.Errorf("%s is empty", k)
		}
	}
}
