package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate points os.TempDir at a fresh directory and returns where runtimeDir will look.
func isolate(t *testing.T) (base, dir string) {
	t.Helper()
	base = t.TempDir()
	t.Setenv("TMPDIR", base)
	return base, filepath.Join(base, fmt.Sprintf("ting-%d", os.Getuid()))
}

func wantRefused(t *testing.T, err error, msg string) {
	t.Helper()
	var f *Fail
	if !errors.As(err, &f) || f.Code != 2 || !strings.Contains(f.Msg, msg) {
		t.Errorf("err = %v, want exit-2 refusal mentioning %q", err, msg)
	}
}

func TestRuntimeDirCreates0700(t *testing.T) {
	_, want := isolate(t)
	got, err := runtimeDir(true)
	if err != nil || got != want {
		t.Fatalf("runtimeDir(true) = %q, %v; want %q", got, err, want)
	}
	fi, _ := os.Lstat(want)
	if !fi.IsDir() || fi.Mode().Perm() != 0700 {
		t.Errorf("created %v, want a 0700 directory", fi.Mode())
	}
	// A second caller finds it and accepts it.
	if got, err := runtimeDir(true); err != nil || got != want {
		t.Errorf("runtimeDir on an existing 0700 dir = %q, %v", got, err)
	}
}

func TestRuntimeDirAbsentIsNotCreated(t *testing.T) {
	_, dir := isolate(t)
	if _, err := runtimeDir(false); err != errNoDir {
		t.Errorf("err = %v, want errNoDir", err)
	}
	// status and control read through Connect: nothing running, and nothing left behind.
	if c, err := Connect(); c != nil || err != nil {
		t.Errorf("Connect = %v, %v; want nil, nil", c, err)
	}
	if r, err := Status(); err != nil || r.State != "idle" {
		t.Errorf("Status = %+v, %v; want idle", r, err)
	}
	if _, err := Control("pause", 0); err == nil {
		t.Error("Control with no player succeeded")
	} else if f := (*Fail)(nil); !errors.As(err, &f) || f.Code != 4 || f.Status != "not_playing" {
		t.Errorf("Control err = %v, want exit 4 not_playing", err)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("read-only verbs created %s (err %v)", dir, err)
	}
}

func TestRuntimeDirRefusesLooseMode(t *testing.T) {
	for _, mode := range []os.FileMode{0755, 0750, 0711, 0777} {
		_, dir := isolate(t)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		os.Chmod(dir, mode) // set after Mkdir: the umask would mask it
		for _, create := range []bool{true, false} {
			_, err := runtimeDir(create)
			wantRefused(t, err, "want 0700")
		}
	}
}

func TestRuntimeDirRefusesSymlink(t *testing.T) {
	base, dir := isolate(t)
	// The target itself would pass every check: only the link is wrong.
	target := filepath.Join(base, "elsewhere")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, dir); err != nil {
		t.Fatal(err)
	}
	for _, create := range []bool{true, false} {
		_, err := runtimeDir(create)
		wantRefused(t, err, "is a symlink")
	}
	if _, err := scratchDir(); err == nil {
		t.Error("scratchDir wrote through a symlinked runtime dir")
	}
	if left, _ := os.ReadDir(target); len(left) != 0 {
		t.Errorf("symlink target got %d entries, want none", len(left))
	}
}

func TestRuntimeDirRefusesFile(t *testing.T) {
	_, dir := isolate(t)
	if err := os.WriteFile(dir, nil, 0700); err != nil {
		t.Fatal(err)
	}
	_, err := runtimeDir(true)
	wantRefused(t, err, "not a directory")
}

func TestScratchDirIsInsideRuntimeDir(t *testing.T) {
	_, dir := isolate(t)
	s, err := scratchDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(s) != dir || !strings.HasPrefix(filepath.Base(s), "scratch-") {
		t.Errorf("scratchDir = %q, want a scratch-* entry of %q", s, dir)
	}
	if fi, _ := os.Lstat(s); fi.Mode().Perm() != 0700 {
		t.Errorf("scratch dir mode %v, want 0700", fi.Mode().Perm())
	}
}
