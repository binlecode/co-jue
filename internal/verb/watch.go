package verb

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"sync"
	"syscall"
)

// Event is one --watch line: the whole player record plus why the line was sent. Every line is
// complete, so a reader keeps only the latest one.
type Event struct {
	Event    string   `json:"event"`
	ID       string   `json:"id"`
	URL      string   `json:"url"`
	Engine   string   `json:"engine"`
	Mode     string   `json:"mode"`
	Title    *string  `json:"title"`
	Paused   bool     `json:"paused"`
	Ready    bool     `json:"ready"`
	Position *float64 `json:"position"`
	Duration *float64 `json:"duration"`
	Volume   *float64 `json:"volume"`
	Loop     string   `json:"loop"`
	CPU      *float64 `json:"cpu"`
	Mem      *float64 `json:"mem"`
	Ended    *string  `json:"ended"`     // transitioning: finished | interrupted | failed
	ExitCode *int     `json:"exit_code"` // end
	Reason   *string  `json:"reason"`    // end
	Media    *Media   `json:"media"`
	Queue    *struct {
		Pos int `json:"pos"`
		Len int `json:"len"`
	} `json:"queue"`
}

// Media is what the player is decoding; every member null until mpv knows it.
type Media struct {
	VideoCodec   *string  `json:"video_codec"`
	Width        *int     `json:"width"`
	Height       *int     `json:"height"`
	FPS          *float64 `json:"fps"`
	VideoBitrate *int     `json:"video_bitrate"`
	AudioCodec   *string  `json:"audio_codec"`
	AudioBitrate *int     `json:"audio_bitrate"`
	SampleRate   *int     `json:"sample_rate"`
	Channels     *string  `json:"channels"`
}

// Watcher owns one `ting-play --watch` process. Events closes after the `end` line, or when the
// process dies; Err then says why when it was not a clean end.
type Watcher struct {
	Events <-chan Event
	cmd    *exec.Cmd
	stop   chan struct{} // closed by Close: a sender blocked on a reader that left lets go
	once   sync.Once
	done   chan struct{}
	err    error
}

// Watch follows one player until it ends. The process runs in its own process group so Close
// takes its netcat and jq with it: the TUI owns this child, and a reader that stops reading
// while the player is paused would otherwise leave it waiting for the next event.
func (s *Suite) Watch(ctx context.Context, id string) (*Watcher, error) {
	argv := []string{s.TPlay, "--watch", "-j", "--id", id}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &tailBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	ch := make(chan Event, 16)
	w := &Watcher{Events: ch, cmd: cmd, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(w.done)
		defer close(ch)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			var ev Event
			if json.Unmarshal(sc.Bytes(), &ev) != nil {
				continue
			}
			select {
			case ch <- ev:
			case <-w.stop:
			case <-ctx.Done():
				w.kill()
			}
		}
		if werr := cmd.Wait(); werr != nil {
			if ee, ok := werr.(*exec.ExitError); ok && ee.ExitCode() > 0 {
				w.err = &Error{Argv: argv, Code: ee.ExitCode(), Stderr: stderr.String()}
			}
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			w.kill()
		case <-w.done:
		}
	}()
	return w, nil
}

func (w *Watcher) kill() {
	if w.cmd.Process != nil {
		_ = syscall.Kill(-w.cmd.Process.Pid, syscall.SIGTERM)
	}
}

// Close stops the watch (not the player) and waits for the process to go. Safe to call
// whether or not anyone is still reading Events.
func (w *Watcher) Close() {
	w.once.Do(func() { close(w.stop) })
	w.kill()
	<-w.done
}

// Err is why the stream ended early; nil after a clean `end`.
func (w *Watcher) Err() error {
	<-w.done
	return w.err
}

// tailBuffer keeps the last few KB of stderr: enough for the one line that says why.
type tailBuffer struct{ b []byte }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 4096 {
		t.b = t.b[len(t.b)-4096:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(t.b) }
