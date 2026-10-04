package main

// The gen-2 verbs (PLAN-agentic-media-plane-refactor.md): the agent-facing face of
// internal/engine. Every outcome is one compact JSON line on stdout and one exit code —
// 0 ok, 1 usage, 2 external tool or network, 4 the effect did not happen.

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"

	"github.com/binlecode/ting/internal/engine"
)

const verbUsage = `usage: ting inspect <url>
       ting transcript <url> [--range START-END]
       ting play <url> [--start SEC]
       ting control pause|resume|stop|seek <[+-]SEC>|volume <0-100>
       ting status`

var verbs = map[string]func([]string) (any, error){
	"inspect":    runInspect,
	"transcript": runTranscript,
	"play":       runPlay,
	"control":    runControl,
	"status":     runStatus,
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
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false) // URLs keep their & as is
	enc.Encode(out)
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

func runControl(args []string) (any, error) {
	if len(args) == 0 {
		return nil, usageErr("control needs an action")
	}
	action, want := args[0], 1
	if action == "seek" || action == "volume" {
		want = 2
	}
	if len(args) != want {
		return nil, usageErr("wrong arguments for control " + action)
	}
	var v float64
	if want == 2 {
		var err error
		v, err = strconv.ParseFloat(args[1], 64)
		if err != nil || (action == "volume" && (v < 0 || v > 100)) {
			return nil, usageErr("bad value for " + action + ": " + args[1])
		}
	}
	return engine.Control(action, v)
}

func runStatus(args []string) (any, error) {
	if len(args) != 0 {
		return nil, usageErr("status takes no arguments")
	}
	return engine.Status()
}
