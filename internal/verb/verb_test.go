package verb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Every test drives the real commands in this checkout's shell/, isolated from the user's own
// state and config the way tests/contract.sh isolates them: nothing is seeded, only moved.
func suite(t *testing.T) *Suite {
	t.Helper()
	// Under the checkout's tmp/, not t.TempDir(): the player's socket lives in $TMPDIR, and
	// macOS caps a unix socket path at 104 bytes, which Go's per-test temp paths overrun —
	// mpv then starts with no socket at all.
	root, err := filepath.Abs("../../tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, "go-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("UT_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("TMPDIR", dir)
	t.Setenv("UT_CONFIG", filepath.Join(dir, "no-config"))
	t.Setenv("UT_HISTORY", "0")
	t.Setenv("UT_ENGINE_DIR", filepath.Join(dir, "no-engines"))
	shell, err := filepath.Abs("../../shell")
	if err != nil {
		t.Fatal(err)
	}
	s, err := LocateIn(shell)
	if err != nil {
		t.Fatal(err)
	}
	if s.TPlay != filepath.Join(shell, "t-play") {
		t.Fatalf("located %q, not the checkout's t-play", s.TPlay)
	}
	return s
}

func TestEngines(t *testing.T) {
	s := suite(t)
	engines, err := s.Engines(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range engines {
		seen[e.Name] = true
		if st, err := os.Stat(e.Bin); err != nil || st.Mode()&0o111 == 0 {
			t.Errorf("%s: %q is not an executable", e.Name, e.Bin)
		}
		if !e.Has("--search") {
			t.Errorf("%s: flags %v do not include --search", e.Name, e.Flags)
		}
	}
	for _, n := range []string{"yt", "bili", "ne"} {
		if !seen[n] {
			t.Errorf("built-in engine %s missing from %v", n, engines)
		}
	}
}

func TestUsageErrorIsTyped(t *testing.T) {
	s := suite(t)
	err := run(context.Background(), []string{s.TPlay, "--engines", "--engine", "yt", "-j"}, nil)
	var ve *Error
	if !errors.As(err, &ve) || ve.Kind() != Usage {
		t.Fatalf("got %v, want a Usage error", err)
	}
	if ve.Error() != "t-play: --engines lists every installed engine — it takes no --engine" {
		t.Errorf("message: %q", ve.Error())
	}
}

func TestNotEffectiveIsTyped(t *testing.T) {
	s := suite(t)
	err := s.Pause(context.Background(), "nosuch")
	var ve *Error
	if !errors.As(err, &ve) || ve.Kind() != NotEffective {
		t.Fatalf("got %v, want a NotEffective error", err)
	}
}

func TestStatusEmpty(t *testing.T) {
	s := suite(t)
	players, err := s.Status(context.Background())
	if err != nil || len(players) != 0 {
		t.Fatalf("%v %v", players, err)
	}
}

// longTrack finds, by a real search, a video long enough that it cannot end mid-test (a live
// stream is not one: its duration is null).
func longTrack(ctx context.Context, t *testing.T, s *Suite) string {
	t.Helper()
	engines, err := s.Engines(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range engines {
		if e.Name != "yt" {
			continue
		}
		res, err := s.Search(ctx, e.Name, "lofi hip hop mix", SearchOpts{N: 10})
		if err != nil {
			t.Fatal(err)
		}
		if res.Engine != "yt" {
			t.Fatalf("search envelope for engine %q", res.Engine)
		}
		for _, r := range res.Results {
			if r.Duration != nil && *r.Duration > 600 {
				return r.URL
			}
		}
		t.Fatalf("no result over 10 minutes in %d", len(res.Results))
	}
	t.Fatal("no yt engine")
	return ""
}

// The live half: a real search, a real detached player, and its watch stream through pause
// and stop. Skipped under -short (it needs the network and mpv).
func TestSearchPlayWatchStop(t *testing.T) {
	if testing.Short() {
		t.Skip("network + mpv")
	}
	s := suite(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	st, err := s.Play(ctx, "yt", longTrack(ctx, t, s), PlayOpts{Mode: "audio", Volume: "0"})
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = s.Stop(context.Background(), st.ID)
		}
	}()
	w, err := s.Watch(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Wait on STATE, not on an event name: every line is the whole record, and a watch that
	// attaches after the sound started says ready:true on its snapshot and never sends "ready".
	await := func(what string, done func(Event) bool) Event {
		t.Helper()
		for ev := range w.Events {
			if ev.ID != st.ID {
				t.Fatalf("event for %q on %q's stream", ev.ID, st.ID)
			}
			if done(ev) {
				return ev
			}
		}
		t.Fatalf("stream closed before %s: %v", what, w.Err())
		return Event{}
	}
	await("ready", func(ev Event) bool { return ev.Ready })
	if err := s.Pause(ctx, st.ID); err != nil {
		t.Fatal(err)
	}
	if ev := await("paused", func(ev Event) bool { return ev.Paused }); ev.Event != "pause" {
		t.Errorf("paused:true first arrived on a %q line", ev.Event)
	}
	if err := s.Stop(ctx, st.ID); err != nil {
		t.Fatal(err)
	}
	stopped = true
	await("end", func(ev Event) bool { return ev.Event == "end" })
	if _, open := <-w.Events; open {
		t.Error("stream still open after end")
	}
	if err := w.Err(); err != nil {
		t.Errorf("clean end reported %v", err)
	}
}

// Close takes the whole watch process group down while the player keeps playing: the TUI
// quits, an adopted player does not.
func TestWatchCloseLeavesPlayer(t *testing.T) {
	if testing.Short() {
		t.Skip("network + mpv")
	}
	s := suite(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	st, err := s.Play(ctx, "yt", longTrack(ctx, t, s), PlayOpts{Mode: "audio", Volume: "0"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background(), st.ID)
	w, err := s.Watch(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ev, ok := <-w.Events; !ok || ev.Event != "snapshot" {
		t.Fatalf("first line: %+v %v", ev, ok)
	}
	w.Close()
	players, err := s.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 1 || players[0].ID != st.ID {
		t.Fatalf("player gone after closing its watch: %+v", players)
	}
}
