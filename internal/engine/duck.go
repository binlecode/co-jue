package engine

import (
	_ "embed"
	"os"
	"path/filepath"
	"strconv"
)

//go:embed jue_duck.lua
var duckHelper []byte

const (
	duckScript   = "jue_duck"                  // mpv's name for the script: the file name less .lua
	duckMarker   = "user-data/jue/duck-helper" // the helper sets it once its handlers are in place
	duckProtocol = "1"                         // the marker value this jue speaks to
	duckWatchdog = 30                          // seconds a duck on holds without a heartbeat or an off
	duckRelease  = 400                         // ms: the watchdog's way back up, and duck off's default
)

// writeDuckHelper puts the helper in dir (already runtimeDir-checked) as a 0600 regular file:
// written to a temp name and renamed over, so a symlink planted at the path is replaced, not followed.
func writeDuckHelper(dir string) (string, error) {
	path := filepath.Join(dir, duckScript+".lua")
	f, err := os.CreateTemp(dir, duckScript+"-*.lua") // CreateTemp makes it 0600
	if err != nil {
		return "", fail(2, "error", "write duck helper: %v", err)
	}
	_, err = f.Write(duckHelper)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
		return "", fail(2, "error", "write duck helper: %v", err)
	}
	return path, nil
}

// DuckRequest is a parsed, validated duck: Mode "pulse", "on" or "off"; Fade in ms, Hold in s.
type DuckRequest struct {
	Mode              string
	Level, Hold, Fade float64
}

// Duck checks the helper's marker, then sends it one script-message-to. A success reply only
// means mpv delivered the message; the marker is what says a helper speaking this protocol is there.
func Duck(req DuckRequest) (*ControlResponse, error) {
	num := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	args := []any{"script-message-to", duckScript}
	action := "duck"
	switch req.Mode {
	case "pulse":
		args = append(args, "duck", num(req.Level), num(req.Hold), num(req.Fade), num(2*req.Fade))
	case "on":
		action = "duck_on"
		args = append(args, "duck", num(req.Level), num(duckWatchdog), num(req.Fade), num(duckRelease))
	case "off":
		action = "duck_off"
		args = append(args, "unduck", num(req.Fade))
	default:
		return nil, fail(1, "error", "unknown duck mode %q", req.Mode)
	}
	c, err := activePlayer()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	var marker string
	ok, err := c.Get(duckMarker, &marker)
	switch {
	case err != nil:
		return nil, fail(4, "not_playing", "player went away: %v", err)
	case !ok:
		return nil, fail(4, "unavailable", "duck helper not loaded (player started by an older jue): stop and play again")
	case marker != duckProtocol:
		return nil, fail(4, "unavailable", "duck helper protocol %q, want %s: stop and play again", marker, duckProtocol)
	}
	r, err := c.Command(args...)
	if err != nil {
		return nil, fail(4, "not_playing", "player went away: %v", err)
	}
	if r.Error != "success" {
		return nil, fail(4, "unavailable", "duck helper refused delivery: %s", r.Error)
	}
	return &ControlResponse{Status: "ok", Action: action}, nil
}
