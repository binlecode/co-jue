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
		{"TING_VERSION=9", "", "", false},
		{"TING_LANG=zh", "TING_LANG", "zh", true},
		{"PATH=/evil", "", "", false},
		{"ting_x=1", "", "", false},
		{"TING_ENGINE_DIR=/evil", "", "", false},
		{"TING_CONFIG=/x", "", "", false},
		{"TING_IPC_SOCK=/x", "", "", false},
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
	write(t, user, "TING_B=user\nTING_B=second-line-loses\nTING_HISTORY=user\nTING_ENGINE_DIR=/evil\n")
	env := []string{
		"HOME=" + dir,
		"TING_CONFIG=" + user,
		"TING_D=env",
		"TING_BG=env",
	}
	c, err := Load(env, shipped)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"TING_A": "shipped", "TING_B": "user", "TING_HISTORY": "user",
		"TING_D": "env", "TING_BG": "env",
	}
	for k, v := range want {
		if got := c.Value(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if _, ok := c.Get("TING_ENGINE_DIR"); ok {
		t.Error("a config file set TING_ENGINE_DIR")
	}
	if !c.Pinned("TING_D") || !c.Pinned("TING_BG") || c.Pinned("TING_B") {
		t.Error("pinned should be exactly the environment's keys")
	}
	if c.UserPath != user {
		t.Errorf("UserPath = %q", c.UserPath)
	}
}

func TestUserPath(t *testing.T) {
	env := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
	if got := UserPath(env(map[string]string{"XDG_CONFIG_HOME": "/x"})); got != "/x/ting/config" {
		t.Errorf("XDG default: got %q", got)
	}
	if got := UserPath(env(map[string]string{"HOME": "/h"})); got != "/h/.config/ting/config" {
		t.Errorf("HOME default: got %q", got)
	}
	if got := UserPath(env(map[string]string{"TING_CONFIG": "/a", "XDG_CONFIG_HOME": "/x"})); got != "/a" {
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
