package engine

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// HistoryCap is how many entries history.jsonl keeps; a record past it drops the oldest.
const HistoryCap = 500

// HistoryEntry is one line of history.jsonl: a play or queue add that succeeded. Input is
// what the caller passed (a URL or a ytsearch1: query), URL what mpv resolved it to. Title
// and Duration are known only for an entry jue waited on until it loaded: a track queued
// behind another has "" and 0.
type HistoryEntry struct {
	Timestamp int64   `json:"timestamp"`
	Action    string  `json:"action"`
	Input     string  `json:"input"`
	URL       string  `json:"url"`
	Title     string  `json:"title"`
	Duration  float64 `json:"duration"`
}

// historyDir is $XDG_STATE_HOME/jue/ (~/.local/state/jue/ when unset or relative, as the XDG
// spec says), held to runtimeDir's rules: a real directory, this uid's, mode 0700. With
// create false an absent directory is errNoDir: reading or clearing history creates nothing.
func historyDir(create bool) (string, error) {
	state := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(state) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fail(2, "error", "history: %v", err)
		}
		state = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(state, "jue")
	if create {
		if err := os.MkdirAll(state, 0700); err != nil {
			return "", fail(2, "error", "create %s: %v", state, err)
		}
	}
	return privateDir(dir, create)
}

// readHistory is the entries of the history file in dir, oldest first. Absent is empty; a
// line that does not parse is skipped. The file must be a regular 0600 file of this uid:
// O_NOFOLLOW refuses a symlink planted at the path, O_NONBLOCK refuses blocking on FIFOs.
func readHistory(dir string) ([]HistoryEntry, error) {
	path := filepath.Join(dir, "history.jsonl")
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, nil
	case errors.Is(err, syscall.ELOOP):
		return nil, fail(2, "error", "%s is a symlink; refusing to use it", path)
	case err != nil:
		return nil, fail(2, "error", "history: %v", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fail(2, "error", "history: %v", err)
	}
	st, _ := fi.Sys().(*syscall.Stat_t)
	switch {
	case !fi.Mode().IsRegular():
		return nil, fail(2, "error", "%s is not a regular file", path)
	case st == nil || st.Uid != uint32(os.Getuid()):
		return nil, fail(2, "error", "%s is not owned by uid %d", path, os.Getuid())
	case fi.Mode().Perm() != 0600:
		return nil, fail(2, "error", "%s has mode %04o, want 0600", path, fi.Mode().Perm())
	}
	var out []HistoryEntry
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && len(line) <= 1<<20 {
			var e HistoryEntry
			if json.Unmarshal(line, &e) == nil && e.Action != "" {
				out = append(out, e)
			}
		}
		if err != nil {
			break
		}
	}
	return out, nil
}

// withHistoryLock runs fn on the history dir under history.lock, so concurrent records (six
// racing queue adds) each land and none rewrites over another. The lock is tried for at most a
// second: a record must never hold up playback.
func withHistoryLock(dir string, fn func() error) error {
	lock, err := os.OpenFile(filepath.Join(dir, "history.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return fail(2, "error", "open history lock: %v", err)
	}
	defer lock.Close() // closing the descriptor releases the flock
	for deadline := time.Now().Add(time.Second); ; time.Sleep(10 * time.Millisecond) {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			return fail(2, "error", "history lock: %v", err)
		}
	}
	return fn()
}

// writeHistory replaces the history file with entries: written to a temp name (0600) and
// renamed over, so a reader sees the old file or the new one, never half of either, and a
// symlink at the path is replaced, not followed.
func writeHistory(dir string, entries []HistoryEntry) error {
	f, err := os.CreateTemp(dir, "history-*.jsonl")
	if err != nil {
		return fail(2, "error", "write history: %v", err)
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, e := range entries {
		if err == nil {
			err = enc.Encode(e)
		}
	}
	if err == nil {
		err = w.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(dir, "history.jsonl"))
	}
	if err != nil {
		os.Remove(f.Name())
		return fail(2, "error", "write history: %v", err)
	}
	return nil
}

// recordHistory appends e, keeping the newest HistoryCap entries. A history file that is
// refused (a symlink, a loose mode) is left as it is: the record is dropped, not forced.
func recordHistory(e HistoryEntry) error {
	dir, err := historyDir(true)
	if err != nil {
		return err
	}
	return withHistoryLock(dir, func() error {
		entries, err := readHistory(dir)
		if err != nil {
			return err
		}
		entries = append(entries, e)
		if len(entries) > HistoryCap {
			entries = entries[len(entries)-HistoryCap:]
		}
		return writeHistory(dir, entries)
	})
}

// record is the best-effort history write of a play or queue add that succeeded: c is the
// connection it waited on, nil when it waited on nothing (a track queued behind another), and
// title and duration are read only when the loaded entry is still its own. A failure is
// dropped — history must never turn a track that sounds into an error.
func record(action, input, u string, c *IPCClient) {
	e := HistoryEntry{Timestamp: time.Now().Unix(), Action: action, Input: input, URL: u}
	if c != nil && c.CurrentLoadedID() > 0 {
		var title string
		var duration float64
		c.Get("media-title", &title)
		c.Get("duration", &duration)
		if c.CurrentLoadedID() > 0 {
			e.Title = title
			e.Duration = duration
		}
	}
	recordHistory(e)
}

type HistoryListResponse struct {
	Status string         `json:"status"`
	Count  int            `json:"count"`
	Items  []HistoryEntry `json:"items"`
}

// HistoryList is the newest limit entries, newest first. No history dir yet is the empty list,
// and like status it creates nothing.
func HistoryList(limit int) (*HistoryListResponse, error) {
	entries, err := loadHistory()
	if err != nil {
		return nil, err
	}
	r := &HistoryListResponse{Status: "ok", Items: []HistoryEntry{}}
	for i := len(entries) - 1; i >= 0 && len(r.Items) < limit; i-- {
		r.Items = append(r.Items, entries[i])
	}
	r.Count = len(r.Items)
	return r, nil
}

type HistoryLastResponse struct {
	Status string       `json:"status"`
	Item   HistoryEntry `json:"item"`
}

// HistoryLast is the newest entry; none is exit 4 unavailable.
func HistoryLast() (*HistoryLastResponse, error) {
	entries, err := loadHistory()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fail(4, "unavailable", "history is empty")
	}
	return &HistoryLastResponse{Status: "ok", Item: entries[len(entries)-1]}, nil
}

// HistoryClear removes the history file under the lock, so a record racing it lands after, not
// half-before. No history dir yet is already clear, and nothing is created.
func HistoryClear() (*ControlResponse, error) {
	dir, err := historyDir(false)
	if err == errNoDir {
		return &ControlResponse{Status: "ok", Action: "clear"}, nil
	}
	if err != nil {
		return nil, err
	}
	err = withHistoryLock(dir, func() error {
		if err := os.Remove(filepath.Join(dir, "history.jsonl")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fail(2, "error", "clear history: %v", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ControlResponse{Status: "ok", Action: "clear"}, nil
}

func loadHistory() ([]HistoryEntry, error) {
	dir, err := historyDir(false)
	if err == errNoDir {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return readHistory(dir)
}
