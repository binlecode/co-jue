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
		{"UT_X=1", "UT_X", "1", true},
		{"  UT_X =  two words  # note", "UT_X", "two words", true},
		{`UT_X="quoted # not"`, "UT_X", `"quoted`, true}, // # cuts before the quote pair is seen
		{`UT_X="a b"`, "UT_X", "a b", true},
		{`UT_X='a'`, "UT_X", "a", true},
		{`UT_X="a'`, "UT_X", `"a'`, true},
		{"UT_X=~/music", "UT_X", "/home/u/music", true},
		{"UT_X=a~/b", "UT_X", "a~/b", true},
		{"UT_X=$(rm -rf /)", "UT_X", "$(rm -rf /)", true},
		{"UT_X=", "UT_X", "", true},
		{"TING_X=1", "TING_X", "1", true},
		{"TING_LANG=zh", "TING_LANG", "zh", true},
		{"PATH=/evil", "", "", false},
		{"ut_x=1", "", "", false},
		{"UT_ENGINE_DIR=/evil", "", "", false},
		{"TING_CONFIG=/x", "", "", false},
		{"_TING_IPC_SOCK=/x", "", "", false},
		{"YT_IPC_SOCK=/x", "", "", false},
		{"YT_THEME=nord", "TING_THEME", "nord", true},
		{"BILI_UA=x", "TING_BILI_UA", "x", true},
		{"YT_UNKNOWN=1", "", "", false},
		{"# UT_X=1", "", "", false},
		{"just words", "", "", false},
		{"", "", "", false},
		{"UT_X=1\r", "UT_X", "1", true},
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
	write(t, shipped, "UT_A=shipped\nUT_B=shipped\nUT_C=shipped\nUT_D=shipped\nTING_BG=shipped\n")
	write(t, user, "UT_B=user\nUT_B=second-line-loses\nTING_C=user-new-name\nUT_ENGINE_DIR=/evil\n")
	env := []string{
		"HOME=" + dir,
		"UT_CONFIG=" + user,
		"UT_D=env",
		"YT_BG=old-name",
		"TING_BG=new-name",       // a renamed key's new name wins over its old one
		"YT_SYNC=old-only",       // an old name alone still reads into the new key
		"TING_A=not-on-the-list", // TING_A is not a renamed knob in the environment
	}
	c, err := Load(env, shipped)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"UT_A": "shipped", "UT_B": "user", "TING_B": "user", "UT_C": "user-new-name",
		"UT_D": "env", "TING_BG": "new-name", "TING_SYNC": "old-only",
	}
	for k, v := range want {
		if got := c.Value(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if _, ok := c.Get("UT_ENGINE_DIR"); ok {
		t.Error("a config file set UT_ENGINE_DIR")
	}
	if !c.Pinned("UT_D") || !c.Pinned("TING_BG") || !c.Pinned("YT_SYNC") || c.Pinned("UT_B") {
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
	c, err := Load([]string{"HOME=/nonexistent", "UT_CONFIG=/nonexistent"}, "../../config")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"UT_DEFAULT_ENGINE", "UT_SEARCH_RESULTS", "UT_PLAY_MODE", "UT_PAGE_ROWS"} {
		if c.Value(k) == "" {
			t.Errorf("%s is empty", k)
		}
	}
}
