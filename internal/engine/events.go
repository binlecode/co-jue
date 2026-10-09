package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"
)

type SnapshotEvent struct {
	Event    string   `json:"event"`
	State    string   `json:"state"`
	URL      string   `json:"url,omitempty"`
	TimePos  *float64 `json:"time_pos,omitempty"`
	Duration *float64 `json:"duration,omitempty"`
	Volume   *float64 `json:"volume,omitempty"`
	Duck     *int     `json:"duck,omitempty"` // as in StatusResponse
}

type TrackStartedEvent struct {
	Event    string   `json:"event"`
	URL      string   `json:"url,omitempty"`
	Duration *float64 `json:"duration,omitempty"`
}

type TrackEndedEvent struct {
	Event    string   `json:"event"`
	URL      string   `json:"url,omitempty"`
	Reason   string   `json:"reason"`
	Duration *float64 `json:"duration,omitempty"`
	Error    string   `json:"error,omitempty"`
}

type PauseEvent struct {
	Event   string   `json:"event"` // "paused" or "resumed"
	TimePos *float64 `json:"time_pos,omitempty"`
}

type ChapterChangedEvent struct {
	Event        string   `json:"event"`
	ChapterIndex int      `json:"chapter_index"`
	Title        string   `json:"title,omitempty"`
	Start        *float64 `json:"start,omitempty"`
}

// QueueEndedEvent is the queue played out: the last entry ended on its own and the player
// went idle. Skipping or replacing past the end is the caller's own doing and not reported.
type QueueEndedEvent struct {
	Event string `json:"event"`
}

type AudioDevice struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// AudioDeviceChangedEvent is the set of audio outputs the system offers changing: a headset
// plugged or unplugged, AirPods connecting or going.
type AudioDeviceChangedEvent struct {
	Event string `json:"event"` // "audio_device_changed"
	// AudioDevice is mpv's audio-device: the output selector it was configured with ("auto"
	// under --no-config), not the device sound is routed to — mpv 0.41 exposes no such property.
	AudioDevice string        `json:"audio_device"`
	Devices     []AudioDevice `json:"devices"` // sorted by name; [] never null
}

// deviceList decodes an audio-device-list notification into its sorted form and fingerprint:
// one topology enumerated in another order is the same print, and null is []. ok is false
// when the notification carries no list (data absent or not an array).
func deviceList(raw map[string]any) (devs []AudioDevice, fp string, ok bool) {
	data, has := raw["data"]
	if _, isList := data.([]any); !has || data != nil && !isList {
		return nil, "", false
	}
	b, _ := json.Marshal(data)
	if json.Unmarshal(b, &devs) != nil {
		return nil, "", false
	}
	if devs == nil {
		devs = []AudioDevice{}
	}
	sort.Slice(devs, func(i, j int) bool {
		if devs[i].Name != devs[j].Name {
			return devs[i].Name < devs[j].Name
		}
		return devs[i].Description < devs[j].Description
	})
	b, _ = json.Marshal(devs)
	return devs, string(b), true
}

var ValidUntilEvents = map[string]bool{
	"track_started":   true,
	"track_ended":     true,
	"paused":          true,
	"resumed":         true,
	"chapter_changed": true,
	"queue_ended":     true,
	// Device topology is the system's, not the queue's: an idle player still waits for it.
	"audio_device_changed": true,
}

func eventType(ev any) string {
	switch v := ev.(type) {
	case SnapshotEvent:
		return v.Event
	case TrackStartedEvent:
		return v.Event
	case TrackEndedEvent:
		return v.Event
	case PauseEvent:
		return v.Event
	case ChapterChangedEvent:
		return v.Event
	case QueueEndedEvent:
		return v.Event
	case AudioDeviceChangedEvent:
		return v.Event
	}
	return ""
}

func Events(until string, timeoutSec float64) (any, error) {
	c, err := Connect()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, fail(4, "not_playing", "no player running")
	}
	defer c.Close()
	return eventsOn(c, until, timeoutSec, os.Stdout)
}

func eventsOn(c *IPCClient, until string, timeoutSec float64, out io.Writer) (any, error) {
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	c.Command("observe_property", 1, "pause")
	c.Command("observe_property", 2, "chapter")
	c.Command("observe_property", 3, "idle-active")
	// The device list's baseline is id 4's first valid notification, not a Get: a Get's reply
	// and the subscription's initial value land in either order, and a change between them
	// would be lost or invented.
	c.Command("observe_property", 4, "audio-device-list")
	devicePrint, haveDevices := "", false

	st, err := StatusOn(c)
	if err != nil {
		return nil, err
	}

	lastIdle := st.State == "idle"
	if lastIdle && until == "queue_ended" {
		return nil, fail(4, "not_playing", "player is idle") // no queue to end: it would never come
	}
	lastPause := (st.State == "paused")
	lastChapter := -2
	curURL, curDur := st.URL, st.Duration
	trackActive := (st.State == "playing" || st.State == "paused" || st.State == "loading")
	trackEndedEmitted := false
	endedOnItsOwn := false // the last entry ended at eof or error, not skipped or replaced

	var matched any
	emit := func(ev any) bool {
		if until != "" {
			if eventType(ev) == until {
				matched = ev
				return true
			}
			return false
		}
		return enc.Encode(ev) != nil
	}

	if until == "" {
		snap := SnapshotEvent{
			Event: "snapshot", State: st.State, URL: st.URL,
			TimePos: st.TimePos, Duration: st.Duration, Volume: st.Volume, Duck: st.Duck,
		}
		if emit(snap) {
			return matched, nil
		}
	}

	var deadline time.Time
	if timeoutSec > 0 {
		deadline = time.Now().Add(time.Duration(timeoutSec * float64(time.Second)))
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGPIPE)
	defer signal.Stop(sigCh)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-sigCh:
			c.Close()
		case <-done:
		}
	}()

	for {
		if !deadline.IsZero() && time.Now().After(deadline) {
			if until != "" {
				return nil, fail(4, "timeout", "events --until %s timed out after %vs", until, timeoutSec)
			}
			return nil, nil
		}

		raw, err := c.NextEvent(deadline)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				if until != "" {
					return nil, fail(4, "timeout", "events --until %s timed out after %vs", until, timeoutSec)
				}
				return nil, nil
			}
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.ECONNRESET) {
				if trackActive && !trackEndedEmitted {
					if emit(TrackEndedEvent{Event: "track_ended", URL: curURL, Reason: "stopped", Duration: curDur}) {
						return matched, nil
					}
				}
				if until != "" {
					return nil, fail(4, "not_playing", "player exited")
				}
				return nil, nil
			}
			return nil, fail(2, "error", "%v", err)
		}

		switch raw["event"] {
		case "start-file":
			// path is already the new entry's here: a track that fails to load must not be
			// reported under the previous one's URL.
			curURL, curDur, endedOnItsOwn = "", nil, false
			c.Get("path", &curURL)

		case "file-loaded":
			c.Get("path", &curURL)
			c.Get("duration", &curDur)
			trackActive, trackEndedEmitted = true, false
			lastChapter = -2 // reset chapter tracker for new track
			if emit(TrackStartedEvent{Event: "track_started", URL: curURL, Duration: curDur}) {
				return matched, nil
			}

		case "end-file":
			reason, _ := raw["reason"].(string)
			if reason == "redirect" {
				continue
			}
			mapped := "error"
			switch reason {
			case "eof", "error":
				mapped = reason
			case "stop":
				mapped = "replaced"
			case "quit":
				mapped = "stopped"
			}
			fileErr, _ := raw["file_error"].(string)
			trackActive, trackEndedEmitted = false, true
			endedOnItsOwn = mapped == "eof" || mapped == "error"
			if emit(TrackEndedEvent{Event: "track_ended", URL: curURL, Reason: mapped, Duration: curDur, Error: fileErr}) {
				return matched, nil
			}

		case "property-change":
			switch raw["name"] {
			case "pause":
				newPause, ok := raw["data"].(bool)
				if !ok || newPause == lastPause {
					continue
				}
				lastPause = newPause
				name := "resumed"
				if newPause {
					name = "paused"
				}
				var timePos *float64
				var pos float64
				if ok, _ := c.Get("time-pos", &pos); ok {
					timePos = &pos
				}
				if emit(PauseEvent{Event: name, TimePos: timePos}) {
					return matched, nil
				}

			case "idle-active":
				isIdle, ok := raw["data"].(bool)
				if !ok || isIdle == lastIdle {
					continue
				}
				lastIdle = isIdle
				if isIdle && endedOnItsOwn && emit(QueueEndedEvent{Event: "queue_ended"}) {
					return matched, nil
				}

			case "audio-device-list":
				devs, fp, ok := deviceList(raw)
				if !ok || fp == devicePrint && haveDevices {
					continue
				}
				first := !haveDevices
				devicePrint, haveDevices = fp, true
				if first {
					continue
				}
				ev := AudioDeviceChangedEvent{Event: "audio_device_changed", Devices: devs}
				c.Get("audio-device", &ev.AudioDevice)
				if emit(ev) {
					return matched, nil
				}

			case "chapter":
				val, ok := raw["data"].(float64)
				if !ok || val < 0 || int(val) == lastChapter {
					continue
				}
				lastChapter = int(val)
				var title string
				c.Get(fmt.Sprintf("chapter-list/%d/title", lastChapter), &title)
				var start *float64
				var s float64
				if ok, _ := c.Get(fmt.Sprintf("chapter-list/%d/time", lastChapter), &s); ok {
					start = &s
				}
				if emit(ChapterChangedEvent{Event: "chapter_changed", ChapterIndex: lastChapter, Title: title, Start: start}) {
					return matched, nil
				}
			}
		}
	}
}
