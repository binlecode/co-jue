package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestMain points XDG_STATE_HOME at a scratch directory for the whole package: every Play and
// QueueAdd a test drives records history, and none may land in the real ~/.local/state.
func TestMain(m *testing.M) {
	state, err := os.MkdirTemp("", "jue-state")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", state)
	code := m.Run()
	os.RemoveAll(state)
	os.Exit(code)
}

// isolateState gives the test its own XDG_STATE_HOME and returns where history lives.
func isolateState(t *testing.T) (state, dir string) {
	t.Helper()
	state = t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	return state, filepath.Join(state, "jue")
}

func entry(i int) HistoryEntry {
	return HistoryEntry{Timestamp: int64(1000 + i), Action: "play", Input: fmt.Sprintf("in%d", i), URL: fmt.Sprintf("u%d", i)}
}

func TestRecordAndList(t *testing.T) {
	_, dir := isolateState(t)
	for i := 1; i <= 3; i++ {
		if err := recordHistory(entry(i)); err != nil {
			t.Fatal(err)
		}
	}
	r, err := HistoryList(20)
	if err != nil || r.Count != 3 || len(r.Items) != 3 {
		t.Fatalf("HistoryList = %+v, %v; want 3 items", r, err)
	}
	for i, want := range []string{"u3", "u2", "u1"} { // newest first
		if r.Items[i].URL != want {
			t.Errorf("item %d = %q, want %q", i, r.Items[i].URL, want)
		}
	}
	if r, _ := HistoryList(2); r.Count != 2 || r.Items[0].URL != "u3" || r.Items[1].URL != "u2" {
		t.Errorf("HistoryList(2) = %+v, want the newest two", r)
	}
	// The envelope: fixed keys, title and duration present when unknown. The CLI prints it with
	// HTML escaping off; the file is written the same way, so a URL's & stays an &.
	recordHistory(HistoryEntry{Timestamp: 1, Action: "queue_add", Input: "ytsearch1:a&b", URL: "https://x/?a=1&b=2"})
	r, _ = HistoryList(1)
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(r)
	b := strings.TrimSpace(buf.String())
	want := `{"status":"ok","count":1,"items":[{"timestamp":1,"action":"queue_add","input":"ytsearch1:a&b","url":"https://x/?a=1&b=2","title":"","duration":0}]}`
	if string(b) != want {
		t.Errorf("HistoryList = %s\nwant          %s", b, want)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "history.jsonl")); !strings.Contains(string(raw), `"url":"https://x/?a=1&b=2"`) {
		t.Errorf("history.jsonl escapes &: %s", raw)
	}
}

func TestHistoryLast(t *testing.T) {
	isolateState(t)
	_, err := HistoryLast()
	wantFail(t, err, 4, "unavailable", "history is empty")
	recordHistory(entry(1))
	recordHistory(entry(2))
	if r, err := HistoryLast(); err != nil || r.Item != entry(2) {
		t.Errorf("HistoryLast = %+v, %v; want %+v", r, err, entry(2))
	}
}

func TestHistoryClear(t *testing.T) {
	_, dir := isolateState(t)
	// Nothing recorded yet: clear, list and last answer, and create nothing.
	if r, err := HistoryClear(); err != nil || r.Action != "clear" {
		t.Errorf("HistoryClear on no history = %+v, %v", r, err)
	}
	if r, err := HistoryList(20); err != nil || r.Count != 0 || r.Items == nil {
		t.Errorf("HistoryList on no history = %+v, %v; want count 0, items []", r, err)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("read-only history verbs created %s (err %v)", dir, err)
	}
	recordHistory(entry(1))
	if _, err := HistoryClear(); err != nil {
		t.Fatal(err)
	}
	if r, _ := HistoryList(20); r.Count != 0 {
		t.Errorf("after clear: %+v, want empty", r)
	}
	_, err := HistoryLast()
	wantFail(t, err, 4, "unavailable", "history is empty")
	recordHistory(entry(2))
	if r, _ := HistoryLast(); r.Item != entry(2) {
		t.Errorf("record after clear: last = %+v", r.Item)
	}
}

func TestHistoryCapTrimming(t *testing.T) {
	_, dir := isolateState(t)
	os.MkdirAll(dir, 0700)
	seed := make([]HistoryEntry, HistoryCap)
	for i := range seed {
		seed[i] = entry(i)
	}
	if err := writeHistory(dir, seed); err != nil {
		t.Fatal(err)
	}
	if err := recordHistory(entry(HistoryCap)); err != nil {
		t.Fatal(err)
	}
	all, _ := readHistory(dir)
	if len(all) != HistoryCap || all[0] != entry(1) || all[HistoryCap-1] != entry(HistoryCap) {
		t.Errorf("after one past the cap: %d entries, first %+v, last %+v; want %d, entry 1 .. entry %d",
			len(all), all[0], all[len(all)-1], HistoryCap, HistoryCap)
	}
	// The trim is a rename over: no temp file is left behind.
	left, _ := os.ReadDir(dir)
	for _, e := range left {
		if e.Name() != "history.jsonl" && e.Name() != "history.lock" {
			t.Errorf("trim left %s behind", e.Name())
		}
	}
}

func TestHistoryConcurrentRecords(t *testing.T) {
	isolateState(t)
	const n = 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := recordHistory(entry(i)); err != nil {
				t.Errorf("record %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if r, _ := HistoryList(HistoryCap); r.Count != n {
		t.Errorf("%d racing records left %d entries: one rewrote over another", n, r.Count)
	}
}

func TestHistoryCorruptLines(t *testing.T) {
	_, dir := isolateState(t)
	os.MkdirAll(dir, 0700)
	a, _ := json.Marshal(entry(1))
	b, _ := json.Marshal(entry(2))
	body := "not json\n" + string(a) + "\n{\"timestamp\":\"x\"}\n{}\n\n{\"action\":\"play\",\"url\":\"half\n" +
		strings.Repeat("z", 2<<20) + "\n" + string(b) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "history.jsonl"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := HistoryList(20)
	if err != nil || r.Count != 2 || r.Items[0] != entry(2) || r.Items[1] != entry(1) {
		t.Fatalf("HistoryList over corrupt/oversized lines = %+v, %v; want entries 2, 1", r, err)
	}
	// The next record rewrites the file clean: the good entries and the new one.
	if err := recordHistory(entry(3)); err != nil {
		t.Fatal(err)
	}
	if r, _ := HistoryList(20); r.Count != 3 || r.Items[0] != entry(3) || r.Items[1] != entry(2) || r.Items[2] != entry(1) {
		t.Errorf("after a record over corrupt lines: %+v", r)
	}
}

func TestHistorySecurityGates(t *testing.T) {
	t.Run("creates 0700 dir and 0600 file", func(t *testing.T) {
		_, dir := isolateState(t)
		if err := recordHistory(entry(1)); err != nil {
			t.Fatal(err)
		}
		if fi, _ := os.Lstat(dir); !fi.IsDir() || fi.Mode().Perm() != 0700 {
			t.Errorf("history dir %v, want a 0700 directory", fi.Mode())
		}
		for _, name := range []string{"history.jsonl", "history.lock"} {
			if fi, _ := os.Lstat(filepath.Join(dir, name)); !fi.Mode().IsRegular() || fi.Mode().Perm() != 0600 {
				t.Errorf("%s %v, want a 0600 regular file", name, fi.Mode())
			}
		}
	})
	t.Run("relative XDG_STATE_HOME falls back to HOME", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_STATE_HOME", "relative/state")
		if err := recordHistory(entry(1)); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(home, ".local", "state", "jue", "history.jsonl")); err != nil {
			t.Errorf("history not under ~/.local/state: %v", err)
		}
	})
	t.Run("loose dir mode refused", func(t *testing.T) {
		_, dir := isolateState(t)
		os.Mkdir(dir, 0700)
		os.Chmod(dir, 0755)
		wantRefused(t, recordHistory(entry(1)), "want 0700")
		_, err := HistoryList(20)
		wantRefused(t, err, "want 0700")
		_, err = HistoryClear()
		wantRefused(t, err, "want 0700")
	})
	t.Run("symlinked dir refused", func(t *testing.T) {
		state, dir := isolateState(t)
		target := filepath.Join(state, "elsewhere")
		os.Mkdir(target, 0700)
		os.Symlink(target, dir)
		wantRefused(t, recordHistory(entry(1)), "is a symlink")
		_, err := HistoryLast()
		wantRefused(t, err, "is a symlink")
		if left, _ := os.ReadDir(target); len(left) != 0 {
			t.Errorf("symlink target got %d entries, want none", len(left))
		}
	})
	t.Run("symlinked file refused, target untouched", func(t *testing.T) {
		state, dir := isolateState(t)
		os.Mkdir(dir, 0700)
		target := filepath.Join(state, "victim")
		os.WriteFile(target, []byte("keep\n"), 0600)
		os.Symlink(target, filepath.Join(dir, "history.jsonl"))
		wantRefused(t, recordHistory(entry(1)), "is a symlink")
		_, err := HistoryList(20)
		wantRefused(t, err, "is a symlink")
		if b, _ := os.ReadFile(target); string(b) != "keep\n" {
			t.Errorf("symlink target rewritten: %q", b)
		}
	})
	t.Run("loose file mode refused", func(t *testing.T) {
		_, dir := isolateState(t)
		os.Mkdir(dir, 0700)
		path := filepath.Join(dir, "history.jsonl")
		os.WriteFile(path, nil, 0600)
		os.Chmod(path, 0644)
		_, err := HistoryList(20)
		wantRefused(t, err, "want 0600")
		wantRefused(t, recordHistory(entry(1)), "want 0600")
	})
}

// A play and a queue add record what they did; a track queued behind another has no title yet.
func TestPlayAndQueueAddRecordHistory(t *testing.T) {
	isolateState(t)
	fakeYtdlp(t)
	get := props(map[string]any{"idle-active": false, "playlist-pos": -1,
		"path": "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "media-title": "Never Gonna Give You Up", "duration": 213.1})
	startFakeMPV(t, func(cmd []any, id int64) []string {
		if cmd[0] == "loadfile" {
			return []string{answer(id, map[string]any{"playlist_entry_id": 1}), ev("start-file", 1, ""), ev("file-loaded", 0, "")}
		}
		return get(cmd, id)
	})
	if _, err := Play("ytsearch1:rick", 0); err != nil {
		t.Fatal(err)
	}
	r, err := HistoryLast()
	want := HistoryEntry{Action: "play", Input: "ytsearch1:rick", URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		Title: "Never Gonna Give You Up", Duration: 213.1}
	if err != nil || r.Item.Timestamp == 0 {
		t.Fatalf("HistoryLast = %+v, %v", r, err)
	}
	if r.Item.Timestamp = 0; r.Item != want {
		t.Errorf("play recorded %+v, want %+v", r.Item, want)
	}

	isolateState(t)
	get = props(map[string]any{"idle-active": false, "playlist-pos": 0, "playlist-count": 2, "media-title": "playing, not queued"})
	startFakeMPV(t, func(cmd []any, id int64) []string {
		if cmd[0] == "loadfile" {
			return []string{answer(id, map[string]any{"playlist_entry_id": 2})}
		}
		return get(cmd, id)
	})
	if _, err := QueueAdd("ytsearch1:七里香"); err != nil {
		t.Fatal(err)
	}
	r, _ = HistoryLast()
	want = HistoryEntry{Action: "queue_add", Input: "ytsearch1:七里香", URL: "ytdl://ytsearch1:七里香"}
	if r.Item.Timestamp = 0; r.Item != want {
		t.Errorf("queued add recorded %+v, want %+v", r.Item, want)
	}
}

// A history that cannot be written never fails the play that sounds.
func TestHistoryFailureDoesNotFailPlay(t *testing.T) {
	state, dir := isolateState(t)
	target := filepath.Join(state, "elsewhere")
	os.Mkdir(target, 0700)
	os.Symlink(target, dir)
	fakeYtdlp(t)
	startFakeMPV(t, queueMPV())
	if r, err := Play("x.wav", 0); err != nil || r.State != "playing" {
		t.Errorf("Play with a refused history = %+v, %v; want playing", r, err)
	}
	if left, _ := os.ReadDir(target); len(left) != 0 {
		t.Errorf("history wrote through a symlinked dir: %d entries", len(left))
	}
}

func TestHistoryFIFORefused(t *testing.T) {
	_, dir := isolateState(t)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "history.jsonl")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- recordHistory(entry(1)) }()
	select {
	case err := <-done:
		wantFail(t, err, 2, "error", "not a regular file")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("recordHistory hung on FIFO: O_NONBLOCK missing")
	}
}

func TestHistoryMetadataIdentitySeparation(t *testing.T) {
	isolateState(t)
	fakeYtdlp(t)
	get := props(map[string]any{"idle-active": false, "media-title": "Song B", "duration": 222.0})
	startFakeMPV(t, func(cmd []any, id int64) []string {
		if cmd[0] == "loadfile" {
			return []string{answer(id, map[string]any{"playlist_entry_id": 1}), ev("start-file", 1, ""), ev("file-loaded", 0, "")}
		}
		if cmd[0] == "get_property" && cmd[1] == "playlist" {
			return []string{answer(id, []map[string]any{{"id": 1, "filename": "song-a.wav"}}),
				ev("end-file", 1, "stop"), ev("start-file", 2, ""), ev("file-loaded", 0, "")}
		}
		return get(cmd, id)
	})
	if _, err := Play("song-a.wav", 0); err != nil {
		t.Fatal(err)
	}
	r, err := HistoryLast()
	if err != nil {
		t.Fatal(err)
	}
	if r.Item.Title == "Song B" || r.Item.Duration == 222 {
		t.Errorf("recorded another entry's metadata: %+v", r.Item)
	}
}
