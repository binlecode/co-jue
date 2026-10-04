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

// launch is the one lazy start, and only play calls it. The flock spans dial-or-spawn, so two
// plays racing from a cold start yield one mpv: the loser waits, then dials the winner's.
func launch() (*IPCClient, error) {
	dir, err := runtimeDir(true)
	if err != nil {
		return nil, err
	}
	sock := filepath.Join(dir, "mpv.sock")
	lock, err := os.OpenFile(filepath.Join(dir, "ting.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fail(2, "error", "open lock: %v", err)
	}
	defer lock.Close() // closing the descriptor releases the flock
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return nil, fail(2, "error", "flock: %v", err)
	}

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

type PlayResponse struct {
	Status string  `json:"status"`
	State  string  `json:"state"`
	URL    string  `json:"url"`
	Start  float64 `json:"start,omitempty"`
}

// Play replaces whatever is playing with u and returns once mpv has loaded it — the loadfile
// reply alone only means the entry was queued, not that anything will sound.
func Play(u string, start float64) (*PlayResponse, error) {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return nil, fail(2, "error", "yt-dlp not found on PATH (mpv resolves streams through it)")
	}
	c, err := launch()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	args := []any{"loadfile", u, "replace", -1}
	if start > 0 {
		args = append(args, fmt.Sprintf("start=%g", start))
	}
	r, err := c.Command(args...)
	if err != nil {
		return nil, fail(2, "error", "mpv ipc: %v", err)
	}
	var data struct {
		ID int64 `json:"playlist_entry_id"`
	}
	if r.Error != "success" || json.Unmarshal(r.Data, &data) != nil || data.ID == 0 {
		return nil, fail(4, "error", "mpv refused loadfile: %s", r.Error)
	}
	// pause is the player's, not the track's: after a control pause the new track would load
	// silent. Cleared after loadfile so the old track never sounds again on the way out.
	if r, err := c.Command("set_property", "pause", false); err != nil || r.Error != "success" {
		return nil, fail(2, "error", "mpv ipc: unpause: %v", errOr(err, r))
	}
	if err := c.WaitForPlaybackSuccess(data.ID, 30*time.Second); err != nil {
		return nil, fail(4, "error", "%v", err)
	}
	return &PlayResponse{Status: "ok", State: "playing", URL: u, Start: start}, nil
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

func Status() (*StatusResponse, error) {
	idle := &StatusResponse{Status: "ok", State: "idle"}
	c, err := Connect()
	if err != nil || c == nil {
		return idle, err
	}
	defer c.Close()
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

type ControlResponse struct {
	Status string `json:"status"`
	Action string `json:"action"`
}

// Control applies one action to the running player. Nothing running, or a player still idle,
// is exit 4: there is nothing for the action to act on. seekMode is "relative" or "absolute"
// and only read by seek.
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
	if r.Error != "success" {
		return nil, fail(4, "error", "mpv: %s", r.Error)
	}
	return &ControlResponse{Status: "ok", Action: action}, nil
}
