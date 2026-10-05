package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// runtimeDir is $TMPDIR/ting-<uid>/, the only place a socket, lock or scratch file lives.
// The socket in it is a remote control for this user's audio, so before anything is opened
// there the directory must be a real directory (not a symlink planted by someone else),
// owned by this uid and mode exactly 0700. With create false an absent directory is
// errNoDir: status and control never create state.
func runtimeDir(create bool) (string, error) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("ting-%d", os.Getuid()))
	if create {
		if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", fail(2, "error", "create %s: %v", dir, err)
		}
	}
	fi, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", errNoDir
	}
	if err != nil {
		return "", fail(2, "error", "%v", err)
	}
	st, _ := fi.Sys().(*syscall.Stat_t)
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return "", fail(2, "error", "%s is a symlink; refusing to use it", dir)
	case !fi.IsDir():
		return "", fail(2, "error", "%s is not a directory", dir)
	case st == nil || st.Uid != uint32(os.Getuid()):
		return "", fail(2, "error", "%s is not owned by uid %d", dir, os.Getuid())
	case fi.Mode().Perm() != 0700:
		return "", fail(2, "error", "%s has mode %04o, want 0700", dir, fi.Mode().Perm())
	}
	return dir, nil
}

var errNoDir = errors.New("runtime directory absent")

func scratchDir() (string, error) {
	dir, err := runtimeDir(true)
	if err != nil {
		return "", err
	}
	d, err := os.MkdirTemp(dir, "scratch-")
	if err != nil {
		return "", fail(2, "error", "%v", err)
	}
	return d, nil
}

func dialSocket(path string) (*IPCClient, error) {
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return nil, err
	}
	return newIPCClient(c), nil
}

// Connect reaches the running player and never starts one: (nil, nil) means nothing is
// running — no directory, no socket, or a socket nobody answers on.
func Connect() (*IPCClient, error) {
	dir, err := runtimeDir(false)
	if err == errNoDir {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c, err := dialSocket(filepath.Join(dir, "mpv.sock"))
	if err != nil {
		return nil, nil
	}
	return c, nil
}

// flockAction runs fn on the one player, starting it if need be, all under ting.lock: the
// lock spans dial-or-spawn and fn's commands, so two plays or queue adds racing from a cold
// start yield one mpv and neither sees the other's half-done playlist. The lock is released
// on return; an entryID > 0 is a track still loading, for the caller to wait on outside the
// lock with the connection it is handed (and closes).
func flockAction(dir string, fn func(c *IPCClient) (entryID int64, err error)) (*IPCClient, int64, error) {
	lock, err := os.OpenFile(filepath.Join(dir, "ting.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, 0, fail(2, "error", "open lock: %v", err)
	}
	defer lock.Close() // closing the descriptor releases the flock
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return nil, 0, fail(2, "error", "flock: %v", err)
	}
	c, err := launch(dir)
	if err != nil {
		return nil, 0, err
	}
	id, err := fn(c)
	if err != nil {
		c.Close()
		return nil, 0, err
	}
	return c, id, nil
}

// launch is the one lazy start, called only under flockAction's lock: it dials the running
// player or spawns one.
func launch(dir string) (*IPCClient, error) {
	sock := filepath.Join(dir, "mpv.sock")
	if c, err := dialSocket(sock); err == nil {
		for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
			var isIdle bool
			if _, err := c.Get("idle-active", &isIdle); err == nil {
				return c, nil
			}
		}
		c.Close()
	}
	os.Remove(sock) // a socket nobody answers on is a dead player's leftover
	mpv, err := exec.LookPath("mpv")
	if err != nil {
		return nil, fail(2, "error", "mpv not found on PATH")
	}
	cmd := exec.Command(mpv,
		"--no-config", "--no-terminal",
		"--idle=yes", // idle until a loadfile; stays idle between tracks; quits on control stop
		"--no-video",
		"--input-media-keys=yes", // headset and keyboard play/pause reach this player
		"--audio-format=s16",     // forces CoreAudio format; prevents fallback to unpausable avfoundation on macOS
		"--demuxer-max-bytes=32MiB",
		"--demuxer-max-back-bytes=16MiB",
		"--input-ipc-server="+sock,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // outlives the calling terminal
	if err := cmd.Start(); err != nil {
		return nil, fail(2, "error", "start mpv: %v", err)
	}
	cmd.Process.Release()

	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(25 * time.Millisecond) {
		if c, err := dialSocket(sock); err == nil {
			return c, nil
		}
	}
	return nil, fail(2, "error", "mpv socket not ready after 3s")
}

// normalizeForMPV is u as mpv takes it: a search only reaches mpv's ytdl hook behind the
// ytdl:// scheme (bare, mpv reads it as a file path). Anything else passes as it is.
func normalizeForMPV(u string) (string, error) {
	bare, search, err := searchQuery(u)
	if err != nil || !search {
		return u, err
	}
	return "ytdl://" + bare, nil
}

// prepare is the shared front of play and queue add: u normalized for mpv, yt-dlp present
// (mpv resolves streams through it) and the runtime dir in place.
func prepare(u string) (string, string, error) {
	u, err := normalizeForMPV(u)
	if err != nil {
		return "", "", err
	}
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return "", "", fail(2, "error", "yt-dlp not found on PATH (mpv resolves streams through it)")
	}
	dir, err := runtimeDir(true)
	return u, dir, err
}

// loadfile sends one loadfile and returns the playlist entry id mpv gave it.
func loadfile(c *IPCClient, args ...any) (int64, error) {
	r, err := c.Command(append([]any{"loadfile"}, args...)...)
	if err != nil {
		return 0, fail(2, "error", "mpv ipc: %v", err)
	}
	var data struct {
		ID int64 `json:"playlist_entry_id"`
	}
	if r.Error != "success" || json.Unmarshal(r.Data, &data) != nil || data.ID == 0 {
		return 0, fail(4, "error", "mpv refused loadfile: %s", r.Error)
	}
	return data.ID, nil
}

// unpause clears the player's pause: it is the player's, not the track's, so after a control
// pause the next track would load silent.
func unpause(c *IPCClient) error {
	if r, err := c.Command("set_property", "pause", false); err != nil || r.Error != "success" {
		return fail(2, "error", "mpv ipc: unpause: %v", errOr(err, r))
	}
	return nil
}

// waitSounding waits outside the lock for entry id (-1: whatever starts next) to load, then
// reads the URL it resolved to — a search or a short link comes back as the watch URL.
func waitSounding(c *IPCClient, id int64, u string) (string, error) {
	if err := c.WaitForPlaybackSuccess(id, 30*time.Second); err != nil {
		return "", fail(4, "error", "%v", err)
	}
	var path string
	if ok, _ := c.Get("path", &path); ok && path != "" {
		u = path
	}
	return u, nil
}

type PlayResponse struct {
	Status string  `json:"status"`
	State  string  `json:"state"`
	URL    string  `json:"url"`
	Start  float64 `json:"start,omitempty"`
}

// Play replaces whatever is playing (the whole queue) with u and returns once mpv has loaded
// it — the loadfile reply alone only means the entry was queued, not that anything will sound.
func Play(u string, start float64) (*PlayResponse, error) {
	u, dir, err := prepare(u)
	if err != nil {
		return nil, err
	}
	c, id, err := flockAction(dir, func(c *IPCClient) (int64, error) {
		args := []any{u, "replace", -1}
		if start > 0 {
			args = append(args, fmt.Sprintf("start=%g", start))
		}
		id, err := loadfile(c, args...)
		if err != nil {
			return 0, err
		}
		// Cleared after loadfile so the old track never sounds again on the way out.
		return id, unpause(c)
	})
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if u, err = waitSounding(c, id, u); err != nil {
		return nil, err
	}
	return &PlayResponse{Status: "ok", State: "playing", URL: u, Start: start}, nil
}

type QueueAddResponse struct {
	Status string `json:"status"`
	Action string `json:"action"`
	State  string `json:"state"`
	URL    string `json:"url"`
	Pos    int    `json:"pos"`
	Count  int    `json:"count"`
}

// QueueAdd appends u to mpv's in-memory playlist. A player that is idle (or not yet running)
// starts u at once and returns when it sounds, as play does; a playing one queues u behind
// what it has and returns at once. Idle is playlist-pos -1, read under the lock: idle-active
// lags a loadfile, so a second add racing the first would see it still idle.
func QueueAdd(u string) (*QueueAddResponse, error) {
	u, dir, err := prepare(u)
	if err != nil {
		return nil, err
	}
	r := &QueueAddResponse{Status: "ok", Action: "add", URL: u}
	c, id, err := flockAction(dir, func(c *IPCClient) (int64, error) {
		pos := -1
		if _, err := c.Get("playlist-pos", &pos); err != nil {
			return 0, fail(2, "error", "mpv ipc: %v", err)
		}
		if pos >= 0 {
			if _, err := loadfile(c, u, "append-play"); err != nil {
				return 0, err
			}
			if _, err := c.Get("playlist-count", &r.Count); err != nil {
				return 0, fail(2, "error", "mpv ipc: %v", err)
			}
			r.State, r.Pos = "queued", r.Count-1
			return 0, nil
		}
		// An idle mpv keeps the tracks it has played: drop them so the queue starts at u.
		if rr, err := c.Command("playlist-clear"); err != nil || rr.Error != "success" {
			return 0, fail(2, "error", "mpv ipc: playlist-clear: %v", errOr(err, rr))
		}
		id, err := loadfile(c, u, "append-play")
		if err != nil {
			return 0, err
		}
		r.State, r.Pos, r.Count = "playing", 0, 1
		return id, unpause(c)
	})
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if id > 0 {
		if r.URL, err = waitSounding(c, id, u); err != nil {
			return nil, err
		}
	}
	return r, nil
}

type QueueItem struct {
	Index   int    `json:"index"`
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`   // mpv knows it only once the entry has loaded
	Current bool   `json:"current,omitempty"` // only the playing entry says so
}

type QueueListResponse struct {
	Status string      `json:"status"`
	Pos    int         `json:"pos"`
	Count  int         `json:"count"`
	Items  []QueueItem `json:"items"`
}

// QueueList reads mpv's playlist. Nothing running, or an idle player (whose playlist is only
// what it has already played), is the empty queue, and like status it creates no state.
func QueueList() (*QueueListResponse, error) {
	empty := &QueueListResponse{Status: "ok", Pos: -1, Items: []QueueItem{}}
	c, err := Connect()
	if err != nil || c == nil {
		return empty, err
	}
	defer c.Close()
	pos := -1
	if _, err := c.Get("playlist-pos", &pos); err != nil || pos < 0 {
		return empty, nil
	}
	var pl []struct {
		Filename string `json:"filename"`
		Title    string `json:"title"`
		Current  bool   `json:"current"`
	}
	if _, err := c.Get("playlist", &pl); err != nil {
		return empty, nil // hung up mid-read: the player is exiting
	}
	r := &QueueListResponse{Status: "ok", Pos: pos, Count: len(pl), Items: []QueueItem{}}
	for i, e := range pl {
		r.Items = append(r.Items, QueueItem{Index: i, URL: e.Filename, Title: e.Title, Current: e.Current})
	}
	return r, nil
}

// QueueClear drops every queued entry; the one sounding plays on.
func QueueClear() (*ControlResponse, error) {
	c, err := activePlayer()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	r, err := c.Command("playlist-clear")
	if err != nil {
		return nil, fail(4, "not_playing", "player went away: %v", err)
	}
	if r.Error != "success" {
		return nil, fail(4, "error", "mpv: %s", r.Error)
	}
	return &ControlResponse{Status: "ok", Action: "clear"}, nil
}

// activePlayer connects to a player that has something loaded: nothing running, or a player
// still idle, is exit 4 — there is nothing for the action to act on.
func activePlayer() (*IPCClient, error) {
	c, err := Connect()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, fail(4, "not_playing", "no player running")
	}
	pos := -1
	if _, err := c.Get("playlist-pos", &pos); err != nil || pos < 0 {
		c.Close()
		return nil, fail(4, "not_playing", "player is idle")
	}
	return c, nil
}

func errOr(err error, r *IPCResponse) any {
	if err != nil {
		return err
	}
	return r.Error
}

type StatusResponse struct {
	Status   string   `json:"status"`
	State    string   `json:"state"`
	URL      string   `json:"url,omitempty"`
	TimePos  *float64 `json:"time_pos,omitempty"`
	Duration *float64 `json:"duration,omitempty"`
	Volume   *float64 `json:"volume,omitempty"`
}

func StatusOn(c *IPCClient) (*StatusResponse, error) {
	idle := &StatusResponse{Status: "ok", State: "idle"}
	if c == nil {
		return idle, nil
	}
	var isIdle, paused bool
	if _, err := c.Get("idle-active", &isIdle); err != nil || isIdle {
		return idle, nil // a player that hung up mid-read is exiting: idle is the truth
	}
	r := &StatusResponse{Status: "ok", State: "playing"}
	c.Get("pause", &paused)
	if paused {
		r.State = "paused"
	}
	c.Get("path", &r.URL)
	for name, dst := range map[string]**float64{"time-pos": &r.TimePos, "duration": &r.Duration, "volume": &r.Volume} {
		var v float64
		if ok, _ := c.Get(name, &v); ok {
			*dst = &v
		}
	}
	// Between loadfile and the first decoded frame there is no playhead yet: nothing sounds.
	if r.State == "playing" && r.TimePos == nil {
		r.State = "loading"
	}
	return r, nil
}

func Status() (*StatusResponse, error) {
	c, err := Connect()
	if err != nil || c == nil {
		return &StatusResponse{Status: "ok", State: "idle"}, err
	}
	defer c.Close()
	return StatusOn(c)
}

type ControlResponse struct {
	Status string `json:"status"`
	Action string `json:"action"`
}

// Control applies one action to the running player. Nothing running, or a player still idle,
// is exit 4: there is nothing for the action to act on. seekMode is "relative" or "absolute"
// and only read by seek. next and prev step through the queue and return once the new entry
// sounds; past either end is exit 4 unavailable.
func Control(action string, value float64, seekMode string) (*ControlResponse, error) {
	var args []any
	switch action {
	case "stop":
		args = []any{"quit"}
	case "pause", "resume":
		args = []any{"set_property", "pause", action == "pause"}
	case "seek":
		if seekMode != "relative" && seekMode != "absolute" {
			return nil, fail(1, "error", "unknown seek mode %q", seekMode)
		}
		args = []any{"seek", value, seekMode}
	case "volume":
		args = []any{"set_property", "volume", value}
	case "next", "prev":
		// weak: past either end of the queue mpv refuses instead of stopping the player.
		args = []any{"playlist-" + action, "weak"}
	default:
		return nil, fail(1, "error", "unknown control action %q", action)
	}
	c, err := Connect()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, fail(4, "not_playing", "no player running")
	}
	defer c.Close()
	if action != "stop" {
		var isIdle bool
		if _, err := c.Get("idle-active", &isIdle); err != nil || isIdle {
			return nil, fail(4, "not_playing", "player is idle")
		}
	}
	r, err := c.Command(args...)
	if err != nil {
		return nil, fail(4, "not_playing", "player went away: %v", err)
	}
	switch {
	case r.Error != "success" && action == "next":
		return nil, fail(4, "unavailable", "end of playlist")
	case r.Error != "success" && action == "prev":
		return nil, fail(4, "unavailable", "start of playlist")
	case r.Error != "success":
		return nil, fail(4, "error", "mpv: %s", r.Error)
	case action == "next" || action == "prev":
		if err := unpause(c); err != nil {
			return nil, err
		}
		if err := c.WaitForPlaybackSuccess(-1, 30*time.Second); err != nil {
			return nil, fail(4, "error", "%v", err)
		}
	}
	return &ControlResponse{Status: "ok", Action: action}, nil
}
