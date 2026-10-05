package engine

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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
	if _, err := Control("pause", 0, ""); err == nil {
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

// fakeMPV listens where Connect dials and answers like mpv: reply(cmd) gives the lines to send
// back for each command (the reply and any events), and every command is kept in order. The
// runtime dir sits under /tmp: a t.TempDir path overflows the 104-byte socket address.
type fakeMPV struct {
	mu   sync.Mutex
	cmds [][]any
}

func startFakeMPV(t *testing.T, reply func(cmd []any, id int64) []string) *fakeMPV {
	t.Helper()
	base, err := os.MkdirTemp("/tmp", "ting-t")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	t.Setenv("TMPDIR", base)
	dir, err := runtimeDir(true)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "mpv.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f := &fakeMPV{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				for {
					line, err := r.ReadBytes('\n')
					if err != nil {
						return
					}
					var req struct {
						Command   []any `json:"command"`
						RequestID int64 `json:"request_id"`
					}
					json.Unmarshal(line, &req)
					f.mu.Lock()
					f.cmds = append(f.cmds, req.Command)
					f.mu.Unlock()
					for _, l := range reply(req.Command, req.RequestID) {
						conn.Write([]byte(l + "\n"))
					}
				}
			}()
		}
	}()
	return f
}

// sent is every command but the property reads, as compact JSON.
func (f *fakeMPV) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.cmds {
		if c[0] != "get_property" {
			b, _ := json.Marshal(c)
			out = append(out, string(b))
		}
	}
	return out
}

func answer(id int64, data any) string {
	b, _ := json.Marshal(map[string]any{"request_id": id, "error": "success", "data": data})
	return string(b)
}

func unavailable(id int64) string {
	return fmt.Sprintf(`{"request_id":%d,"error":"property unavailable","data":null}`, id)
}

// props answers get_property from m (absent is unavailable) and every other command with success.
func props(m map[string]any) func([]any, int64) []string {
	return func(cmd []any, id int64) []string {
		if cmd[0] != "get_property" {
			return []string{answer(id, nil)}
		}
		if v, ok := m[cmd[1].(string)]; ok {
			return []string{answer(id, v)}
		}
		return []string{unavailable(id)}
	}
}

func TestStatusStates(t *testing.T) {
	cases := []struct {
		name  string
		props map[string]any
		want  string
	}{
		{"playhead moving", map[string]any{"idle-active": false, "pause": false, "time-pos": 3.5}, "playing"},
		// Between loadfile and the first frame mpv has no time-pos: nothing sounds yet.
		{"no playhead yet", map[string]any{"idle-active": false, "pause": false}, "loading"},
		{"paused before the first frame", map[string]any{"idle-active": false, "pause": true}, "paused"},
		{"paused", map[string]any{"idle-active": false, "pause": true, "time-pos": 3.5}, "paused"},
		{"idle", map[string]any{"idle-active": true}, "idle"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			startFakeMPV(t, props(c.props))
			r, err := Status()
			if err != nil || r.State != c.want {
				t.Errorf("Status = %+v, %v; want state %q", r, err, c.want)
			}
		})
	}
}

func TestControlSeekModes(t *testing.T) {
	for _, mode := range []string{"relative", "absolute"} {
		f := startFakeMPV(t, props(map[string]any{"idle-active": false}))
		if _, err := Control("seek", -15, mode); err != nil {
			t.Fatalf("Control(seek, %s) = %v", mode, err)
		}
		want := fmt.Sprintf(`["seek",-15,%q]`, mode)
		if got := f.sent(); len(got) != 1 || got[0] != want {
			t.Errorf("sent %v, want [%s]", got, want)
		}
	}
}

func TestControlBadActionIsUsage(t *testing.T) {
	// With no player and with one: a bad action is the caller's mistake either way, and
	// reaches no player.
	isolate(t)
	for _, live := range []bool{false, true} {
		var f *fakeMPV
		if live {
			f = startFakeMPV(t, props(map[string]any{"idle-active": false}))
		}
		for _, c := range []struct{ action, mode string }{{"rewind", ""}, {"seek", ""}, {"seek", "sideways"}} {
			var fl *Fail
			if _, err := Control(c.action, 1, c.mode); !errors.As(err, &fl) || fl.Code != 1 {
				t.Errorf("live=%v Control(%q, %q) err = %v, want exit 1", live, c.action, c.mode, err)
			}
		}
		if f != nil {
			f.mu.Lock()
			if len(f.cmds) != 0 {
				t.Errorf("bad actions sent %v to the player", f.cmds)
			}
			f.mu.Unlock()
		}
	}
}

func TestPlayUnpausesTheNewTrack(t *testing.T) {
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "yt-dlp"), []byte("#!/bin/sh\n"), 0755)
	t.Setenv("PATH", bin)
	// The player was paused by control pause: the next play must still sound.
	f := startFakeMPV(t, func(cmd []any, id int64) []string {
		switch cmd[0] {
		case "get_property":
			return []string{answer(id, false)}
		case "loadfile":
			return []string{answer(id, map[string]any{"playlist_entry_id": 2}),
				`{"event":"start-file","playlist_entry_id":2}`, `{"event":"file-loaded"}`}
		}
		return []string{answer(id, nil)}
	})
	if _, err := Play("x.wav", 30); err != nil {
		t.Fatalf("Play = %v", err)
	}
	want := []string{`["loadfile","x.wav","replace",-1,"start=30"]`, `["set_property","pause",false]`}
	if got := f.sent(); !reflect.DeepEqual(got, want) {
		t.Errorf("sent %v, want %v", got, want)
	}
}

// fakeYtdlp puts a do-nothing yt-dlp on PATH: play and queue add only check it is there.
func fakeYtdlp(t *testing.T) {
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "yt-dlp"), []byte("#!/bin/sh\n"), 0755)
	t.Setenv("PATH", bin)
}

func wantFail(t *testing.T, err error, code int, status, msg string) {
	t.Helper()
	var f *Fail
	if !errors.As(err, &f) || f.Code != code || f.Status != status || !strings.Contains(f.Msg, msg) {
		t.Errorf("err = %v, want exit %d %s %q", err, code, status, msg)
	}
}

// queueMPV is a stateful fake playlist: idle until a loadfile, which (when idle) starts the
// entry with start-file and file-loaded; playlist-clear keeps only the current entry.
func queueMPV() func([]any, int64) []string {
	var mu sync.Mutex
	var entries []int64
	pos, next := -1, int64(0)
	return func(cmd []any, id int64) []string {
		mu.Lock()
		defer mu.Unlock()
		switch cmd[0] {
		case "get_property":
			switch cmd[1] {
			case "idle-active":
				return []string{answer(id, pos < 0)}
			case "playlist-pos":
				return []string{answer(id, pos)}
			case "playlist-count":
				return []string{answer(id, len(entries))}
			case "path":
				return []string{answer(id, "https://www.youtube.com/watch?v=dQw4w9WgXcQ")}
			}
			return []string{unavailable(id)}
		case "playlist-clear":
			if pos < 0 {
				entries = nil
			} else {
				entries, pos = entries[pos:pos+1], 0
			}
		case "loadfile":
			next++
			entries = append(entries, next)
			if pos < 0 {
				pos = len(entries) - 1
				return []string{answer(id, map[string]any{"playlist_entry_id": next}),
					ev("start-file", next, ""), ev("file-loaded", 0, "")}
			}
			return []string{answer(id, map[string]any{"playlist_entry_id": next})}
		}
		return []string{answer(id, nil)}
	}
}

func TestQueueAddIdleStartsAndWaits(t *testing.T) {
	fakeYtdlp(t)
	f := startFakeMPV(t, queueMPV())
	r, err := QueueAdd("ytsearch1:rick")
	if err != nil {
		t.Fatalf("QueueAdd = %v", err)
	}
	want := QueueAddResponse{Status: "ok", Action: "add", State: "playing",
		URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", Pos: 0, Count: 1}
	if *r != want {
		t.Errorf("QueueAdd = %+v, want %+v", *r, want)
	}
	// History of an idle player is dropped first; the search reaches mpv behind ytdl://.
	sent := []string{`["playlist-clear"]`, `["loadfile","ytdl://ytsearch1:rick","append-play"]`, `["set_property","pause",false]`}
	if got := f.sent(); !reflect.DeepEqual(got, sent) {
		t.Errorf("sent %v, want %v", got, sent)
	}
}

func TestQueueAddPlayingQueues(t *testing.T) {
	fakeYtdlp(t)
	get := props(map[string]any{"idle-active": false, "playlist-pos": 0, "playlist-count": 2})
	f := startFakeMPV(t, func(cmd []any, id int64) []string {
		if cmd[0] == "loadfile" {
			return []string{answer(id, map[string]any{"playlist_entry_id": 2})}
		}
		return get(cmd, id)
	})
	r, err := QueueAdd("ytdl://ytsearch1:七里香")
	if err != nil {
		t.Fatalf("QueueAdd = %v", err)
	}
	want := QueueAddResponse{Status: "ok", Action: "add", State: "queued", URL: "ytdl://ytsearch1:七里香", Pos: 1, Count: 2}
	if *r != want {
		t.Errorf("QueueAdd = %+v, want %+v", *r, want)
	}
	if got := f.sent(); len(got) != 1 || got[0] != `["loadfile","ytdl://ytsearch1:七里香","append-play"]` {
		t.Errorf("sent %v, want the append alone (no clear, no unpause, no wait)", got)
	}
}

func TestQueueAddRacesShareOnePlaylist(t *testing.T) {
	fakeYtdlp(t)
	f := startFakeMPV(t, queueMPV())
	const n = 6
	var wg sync.WaitGroup
	results := make([]*QueueAddResponse, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := QueueAdd(fmt.Sprintf("/tmp/t%d.wav", i))
			if err != nil {
				t.Errorf("QueueAdd %d = %v", i, err)
			}
			results[i] = r
		}(i)
	}
	wg.Wait()
	playing, seen := 0, map[int]bool{}
	for _, r := range results {
		if r == nil {
			continue
		}
		if r.State == "playing" {
			playing++
		}
		seen[r.Pos] = true
	}
	// Under the lock exactly one add finds the player idle; the rest see its entry and queue.
	if playing != 1 || len(seen) != n {
		t.Errorf("results %+v: want one playing and %d distinct positions", results, n)
	}
	clears := 0
	for _, c := range f.sent() {
		if c == `["playlist-clear"]` {
			clears++
		}
	}
	if clears != 1 {
		t.Errorf("%d playlist-clear sent, want 1: a later add wiped an earlier one", clears)
	}
}

func TestQueueAddUnsupportedSearchIsUsage(t *testing.T) {
	_, dir := isolate(t)
	fakeYtdlp(t)
	for _, u := range []string{"scsearch:foo", "ytsearch5:foo"} {
		_, err := QueueAdd(u)
		wantFail(t, err, 1, "error", "unsupported search prefix")
		_, err = Play(u, 0)
		wantFail(t, err, 1, "error", "unsupported search prefix")
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused search created %s (err %v)", dir, err)
	}
}

func TestPlayReportsResolvedURL(t *testing.T) {
	fakeYtdlp(t)
	f := startFakeMPV(t, queueMPV())
	r, err := Play("ytsearch1:rick", 0)
	if err != nil || r.URL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Errorf("Play = %+v, %v; want the URL the search resolved to", r, err)
	}
	if got := f.sent(); len(got) == 0 || got[0] != `["loadfile","ytdl://ytsearch1:rick","replace",-1]` {
		t.Errorf("sent %v, want the search behind ytdl://", got)
	}
}

func TestQueueListIdleIsEmptyAndCreatesNothing(t *testing.T) {
	_, dir := isolate(t)
	r, err := QueueList()
	if err != nil || r.Pos != -1 || r.Count != 0 || r.Items == nil || len(r.Items) != 0 {
		t.Errorf("QueueList = %+v, %v; want pos -1, empty items", r, err)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("queue list created %s (err %v)", dir, err)
	}
	// A running but idle player still holds what it played: that is not a queue.
	startFakeMPV(t, props(map[string]any{"idle-active": true, "playlist-pos": -1,
		"playlist": []map[string]any{{"filename": "/old.wav", "id": 1}}}))
	if r, err := QueueList(); err != nil || r.Count != 0 || r.Pos != -1 {
		t.Errorf("idle QueueList = %+v, %v; want the empty queue", r, err)
	}
}

func TestQueueListEnvelope(t *testing.T) {
	startFakeMPV(t, props(map[string]any{"playlist-pos": 0, "playlist": []map[string]any{
		{"filename": "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "title": "晴天", "current": true, "playing": true, "id": 3},
		{"filename": "ytdl://ytsearch1:七里香", "id": 2},
	}}))
	r, err := QueueList()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	want := `{"status":"ok","pos":0,"count":2,"items":[{"index":0,"url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ","title":"晴天","current":true},{"index":1,"url":"ytdl://ytsearch1:七里香"}]}`
	if string(b) != want {
		t.Errorf("QueueList = %s\nwant          %s", b, want)
	}
}

func TestQueueClear(t *testing.T) {
	isolate(t)
	_, err := QueueClear()
	wantFail(t, err, 4, "not_playing", "no player running")

	startFakeMPV(t, props(map[string]any{"idle-active": true, "playlist-pos": -1}))
	_, err = QueueClear()
	wantFail(t, err, 4, "not_playing", "player is idle")

	f := startFakeMPV(t, props(map[string]any{"idle-active": false, "playlist-pos": 0}))
	if r, err := QueueClear(); err != nil || r.Action != "clear" {
		t.Errorf("QueueClear = %+v, %v", r, err)
	}
	if got := f.sent(); len(got) != 1 || got[0] != `["playlist-clear"]` {
		t.Errorf("sent %v, want [playlist-clear]", got)
	}
}

func TestControlNextPrev(t *testing.T) {
	for _, action := range []string{"next", "prev"} {
		f := startFakeMPV(t, func(cmd []any, id int64) []string {
			switch cmd[0] {
			case "get_property":
				return []string{answer(id, false)}
			case "playlist-next", "playlist-prev":
				return []string{answer(id, nil), ev("end-file", 1, `"reason":"stop"`), ev("start-file", 2, ""), ev("file-loaded", 0, "")}
			}
			return []string{answer(id, nil)}
		})
		if r, err := Control(action, 0, ""); err != nil || r.Action != action {
			t.Errorf("Control(%s) = %+v, %v", action, r, err)
		}
		want := []string{fmt.Sprintf(`["playlist-%s","weak"]`, action), `["set_property","pause",false]`}
		if got := f.sent(); !reflect.DeepEqual(got, want) {
			t.Errorf("sent %v, want %v", got, want)
		}
	}
}

func TestControlNextPrevPastTheEnds(t *testing.T) {
	for action, msg := range map[string]string{"next": "end of playlist", "prev": "start of playlist"} {
		f := startFakeMPV(t, func(cmd []any, id int64) []string {
			if cmd[0] == "get_property" {
				return []string{answer(id, false)}
			}
			return []string{fmt.Sprintf(`{"request_id":%d,"error":"error running command"}`, id)}
		})
		_, err := Control(action, 0, "")
		wantFail(t, err, 4, "unavailable", msg)
		if got := f.sent(); len(got) != 1 {
			t.Errorf("%s sent %v, want the step alone (no unpause past the end)", action, got)
		}
	}
}
