package main

// The atomic CLI verbs: the agent-facing face of
// internal/engine. Every outcome is one compact JSON line on stdout and one exit code —
// 0 ok, 1 usage, 2 external tool or network, 4 the effect did not happen.

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/binlecode/co-ting/internal/engine"
)

const verbUsage = `usage: ting inspect <url>
       ting transcript <url> [--range START-END]
       ting play <url> [--start SEC]
       ting queue add <url>|list|clear
       ting control pause|resume|stop|next|prev|seek <+SEC|-SEC|SEC|MM:SS>|volume <0-100>
       ting status
       ting events [--until <EVENT>] [--timeout SEC]`

var verbs = map[string]func([]string) (any, error){
	"inspect":    runInspect,
	"transcript": runTranscript,
	"play":       runPlay,
	"queue":      runQueue,
	"control":    runControl,
	"status":     runStatus,
	"events":     runEvents,
}

func usageErr(msg string) error {
	return &engine.Fail{Code: 1, Status: "error", Msg: msg}
}

// runVerb prints the envelope of verb and returns the exit code.
func runVerb(run func([]string) (any, error), args []string) int {
	out, err := run(args)
	code := 0
	if err != nil {
		f := &engine.Fail{}
		if !errors.As(err, &f) {
			f = &engine.Fail{Code: 2, Status: "error", Msg: err.Error()}
		}
		out, code = struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}{f.Status, f.Msg}, f.Code
		if code == 1 {
			os.Stderr.WriteString(verbUsage + "\n")
		}
	}
	if out != nil || err != nil {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false) // URLs keep their & as is
		enc.Encode(out)
	}
	return code
}

// splitArgs takes one positional argument and the flags named in valued (each with a value).
func splitArgs(args []string, valued ...string) (string, map[string]string, error) {
	var pos []string
	flags := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		known := false
		for _, v := range valued {
			if a == v {
				known = true
			}
		}
		switch {
		case known && i+1 < len(args):
			flags[a] = args[i+1]
			i++
		case known:
			return "", nil, usageErr(a + " requires a value")
		case a == "--":
			pos = append(pos, args[i+1:]...)
			i = len(args)
		case len(a) > 1 && a[0] == '-':
			return "", nil, usageErr("unknown flag " + a)
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) != 1 {
		return "", nil, usageErr("exactly one <url> expected")
	}
	return pos[0], flags, nil
}

func runInspect(args []string) (any, error) {
	u, _, err := splitArgs(args)
	if err != nil {
		return nil, err
	}
	return engine.Inspect(u)
}

func runTranscript(args []string) (any, error) {
	u, flags, err := splitArgs(args, "--range")
	if err != nil {
		return nil, err
	}
	var start, end float64
	rng, has := flags["--range"]
	if has {
		if start, end, err = engine.ParseRange(rng); err != nil {
			return nil, usageErr(err.Error())
		}
	}
	return engine.Transcript(u, start, end, has)
}

func runPlay(args []string) (any, error) {
	u, flags, err := splitArgs(args, "--start")
	if err != nil {
		return nil, err
	}
	var start float64
	if s, ok := flags["--start"]; ok {
		if start, err = engine.ParseTime(s); err != nil {
			return nil, usageErr(err.Error())
		}
	}
	return engine.Play(u, start)
}

func runQueue(args []string) (any, error) {
	if len(args) == 0 {
		return nil, usageErr("queue needs add, list or clear")
	}
	switch rest := args[1:]; args[0] {
	case "add":
		u, _, err := splitArgs(rest)
		if err != nil {
			return nil, err
		}
		return engine.QueueAdd(u)
	case "list", "clear":
		if len(rest) != 0 {
			return nil, usageErr("queue " + args[0] + " takes no arguments")
		}
		if args[0] == "list" {
			return engine.QueueList()
		}
		return engine.QueueClear()
	}
	return nil, usageErr("unknown queue action " + args[0])
}

// controlArgs is how many arguments each control action takes, the action included.
var controlArgs = map[string]int{"pause": 1, "resume": 1, "stop": 1, "next": 1, "prev": 1, "seek": 2, "volume": 2}

func runControl(args []string) (any, error) {
	if len(args) == 0 {
		return nil, usageErr("control needs an action")
	}
	action := args[0]
	want, ok := controlArgs[action]
	if !ok {
		return nil, usageErr("unknown control action " + action)
	}
	if len(args) != want {
		return nil, usageErr("wrong arguments for control " + action)
	}
	var v float64
	switch action {
	case "seek":
		// +N / -N moves from the playhead; a bare time (seconds or mm:ss) is a position.
		arg, mode := args[1], "absolute"
		back := strings.HasPrefix(arg, "-")
		if back || strings.HasPrefix(arg, "+") {
			arg, mode = arg[1:], "relative"
		}
		t, err := engine.ParseTime(arg)
		if err != nil || strings.HasPrefix(arg, "+") || strings.HasPrefix(arg, "-") {
			return nil, usageErr("bad value for seek: " + args[1])
		}
		if back {
			t = -t
		}
		return engine.Control(action, t, mode)
	case "volume":
		var err error
		v, err = strconv.ParseFloat(args[1], 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
			return nil, usageErr("bad value for volume: " + args[1])
		}
	}
	return engine.Control(action, v, "")
}

func runStatus(args []string) (any, error) {
	if len(args) != 0 {
		return nil, usageErr("status takes no arguments")
	}
	return engine.Status()
}

func runEvents(args []string) (any, error) {
	until := ""
	timeout := float64(0)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--until":
			if i+1 >= len(args) {
				return nil, usageErr("--until requires an event name")
			}
			i++
			until = args[i]
			if !engine.ValidUntilEvents[until] {
				return nil, usageErr("unknown --until event: " + until)
			}
		case "--timeout":
			if i+1 >= len(args) {
				return nil, usageErr("--timeout requires seconds")
			}
			i++
			sec, err := strconv.ParseFloat(args[i], 64)
			if err != nil || sec <= 0 || math.IsNaN(sec) || math.IsInf(sec, 0) {
				return nil, usageErr("bad value for --timeout: " + args[i])
			}
			timeout = sec
		default:
			return nil, usageErr("unexpected argument for events: " + args[i])
		}
	}
	if until != "" && timeout == 0 {
		timeout = 600 // default 10m timeout for --until mode
	}
	return engine.Events(until, timeout)
}
