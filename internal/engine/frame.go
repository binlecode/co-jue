package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// FrameResult is the single-line JSON envelope for the frame verb.
// actual_at is explicitly *float64 (nil serializes to JSON null) to avoid false precision.
type FrameResult struct {
	Status    string   `json:"status"`
	URL       string   `json:"url"`
	At        float64  `json:"at"`
	ActualAt  *float64 `json:"actual_at"` // nil (serializes to JSON null)
	Path      string   `json:"path"`
	Width     int      `json:"width"`
	Height    int      `json:"height"`
	SizeBytes int64    `json:"size_bytes"`
	Format    string   `json:"format"`
}

// durationMsgRe parses machine-readable duration from --term-playing-msg.
var durationMsgRe = regexp.MustCompile(`(?m)^DURATION:([0-9]+(?:\.[0-9]+)?)$`)

// durationFallbackRe parses media duration from standard ffmpeg/demuxer output lines.
var durationFallbackRe = regexp.MustCompile(`(?:Duration:\s*|/\s*)(\d{1,2}:\d{2}:\d{2}(?:\.\d+)?|\d{1,2}:\d{2}(?:\.\d+)?)`)

var (
	// writePgidFile is the pgid identifier file writer, swappable for testing failure injection.
	writePgidFile = os.WriteFile

	// Production time budget parameters, swappable for testing.
	frameTimeout      = 15 * time.Second
	frameWaitDelay    = 2 * time.Second
	frameTeardownPoll = 500 * time.Millisecond
)

func parseDurationFromOutput(out string) (float64, bool) {
	if m := durationMsgRe.FindStringSubmatch(out); len(m) >= 2 {
		if dur, err := strconv.ParseFloat(m[1], 64); err == nil && dur > 0 {
			return dur, true
		}
	}
	matches := durationFallbackRe.FindStringSubmatch(out)
	if len(matches) < 2 {
		return 0, false
	}
	dur, err := ParseTime(matches[1])
	if err != nil || dur <= 0 {
		return 0, false
	}
	return dur, true
}

// syncOpportunisticGC scans runtimeDir for expired scratch-frame-* directories
// created over ttl ago whose process group has completely terminated.
// It is strictly bounded: at most 30 liveness probes and 50ms execution time.
// Only entries that reach the process-group probe spend the budget; young, foreign or
// pgid-less/corrupt entries are skipped without counting (and retained), so a stable head
// of such directories cannot starve the reapable ones behind it.
func syncOpportunisticGC(runtimeDir string, ttl time.Duration) {
	deadline := time.Now().Add(50 * time.Millisecond)
	f, err := os.Open(runtimeDir)
	if err != nil {
		return
	}
	defer f.Close()

	entries, err := f.ReadDir(-1)
	if err != nil {
		return
	}

	checked := 0
	for _, entry := range entries {
		if checked >= 30 || time.Now().After(deadline) {
			break
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "scratch-frame-") {
			continue
		}
		dirPath := filepath.Join(runtimeDir, name)
		info, err := entry.Info()
		if err != nil || !info.IsDir() {
			continue
		}
		if time.Since(info.ModTime()) <= ttl {
			continue
		}

		// Strictly verify pgid file attributes: regular file, not a symlink, exactly mode 0600
		pgidFile := filepath.Join(dirPath, "pgid")
		fi, err := os.Lstat(pgidFile)
		if err != nil || !fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm() != 0600 {
			continue
		}
		data, err := os.ReadFile(pgidFile)
		if err != nil {
			continue
		}
		pgidStr := strings.TrimSpace(string(data))
		dirPgid, err := strconv.Atoi(pgidStr)
		if err != nil || dirPgid <= 0 {
			continue
		}
		checked++

		// Verify that the whole process group is dead: must receive syscall.ESRCH
		if err := syscall.Kill(-dirPgid, 0); errors.Is(err, syscall.ESRCH) {
			_ = os.RemoveAll(dirPath)
		}
	}
}

// Frame captures a single visual frame from rawURL at seconds at, scaled to width
// with JPEG compression quality.
func Frame(rawURL string, at float64, width int, quality int) (*FrameResult, error) {
	// 1. Parameter validation: at >= 0 && at < 1e8, no NaN/Inf; width 0-3840; quality 1-100
	if math.IsNaN(at) || math.IsInf(at, 0) || at < 0 || at >= 1e8 {
		return nil, fail(1, "error", "invalid --at value: %v", at)
	}
	if width < 0 || width > 3840 {
		return nil, fail(1, "error", "width %d out of range (0-3840)", width)
	}
	if quality < 1 || quality > 100 {
		return nil, fail(1, "error", "quality %d out of range (1-100)", quality)
	}

	// 2. Explicit search prefix normalization (unsupported prefixes return exit 1)
	targetURL, err := normalizeForMPV(rawURL)
	if err != nil {
		return nil, err
	}

	// 3. Dependency checks: LookPath for mpv and yt-dlp, missing returns exit 2
	if _, err := exec.LookPath("mpv"); err != nil {
		return nil, fail(2, "error", "mpv not found on PATH")
	}
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return nil, fail(2, "error", "yt-dlp not found on PATH (mpv resolves streams through it)")
	}

	// 4. Scratch sandbox & opportunistic GC
	rDir, err := runtimeDir(true)
	if err != nil {
		return nil, err
	}
	syncOpportunisticGC(rDir, time.Hour)

	scratch, err := os.MkdirTemp(rDir, "scratch-frame-")
	if err != nil {
		return nil, fail(2, "error", "create scratch dir: %v", err)
	}
	if err := os.Chmod(scratch, 0700); err != nil {
		_ = os.RemoveAll(scratch)
		return nil, fail(2, "error", "chmod scratch dir: %v", err)
	}

	// 5. One-shot mpv argument assembly
	args := []string{
		"--no-config", "--no-audio", "--no-sub", "--audio-display=no",
		"--frames=1",
		"--loop-playlist=no",
		fmt.Sprintf("--start=%f", at),
		"--hr-seek=yes",
		`--ytdl-format=bv*[height<=720]/b[height<=720]`,
		"--demuxer-max-bytes=8MiB",
		"--demuxer-readahead-secs=0",
		"--vo=image",
		"--vo-image-format=jpg",
		fmt.Sprintf("--vo-image-jpeg-quality=%d", quality),
		fmt.Sprintf("--vo-image-outdir=%s", scratch),
		"--term-playing-msg=DURATION:${=duration}",
	}
	if width > 0 {
		vfFilter := fmt.Sprintf("lavfi=[scale=w='min(%d,iw)':h='min(%d,ih)':force_original_aspect_ratio=decrease,scale=w='trunc(iw/2)*2':h='trunc(ih/2)*2']", width, width)
		args = append(args, "--vf="+vfFilter)
	}
	args = append(args, "--", targetURL)

	// 6. Subprocess setup with isolated process group. mpv's own pgid keeps the terminal's
	// Ctrl-C from reaching it, so SIGINT/SIGTERM on jue cancel ctx and Cancel kills the group.
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(sigCtx, frameTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "mpv", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = frameWaitDelay
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

	stdout := &firstEntryGuard{}
	var stderr bytes.Buffer
	cmd.Stdout = stdout
	cmd.Stderr = &stderr

	var pgid int
	var success bool

	// Unified teardown: broadcast SIGKILL, poll ESRCH up to frameTeardownPoll, remove scratch only if !success && dead
	defer func() {
		if pgid > 0 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			var dead bool
			deadline := time.Now().Add(frameTeardownPoll)
			for time.Now().Before(deadline) {
				if err := syscall.Kill(-pgid, 0); errors.Is(err, syscall.ESRCH) {
					dead = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !success && dead {
				_ = os.RemoveAll(scratch)
			}
		}
	}()

	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(scratch)
		return nil, fail(2, "error", "failed to start mpv: %v", err)
	}
	pgid = cmd.Process.Pid
	stdout.pgid.Store(int64(pgid))
	if err := writePgidFile(filepath.Join(scratch, "pgid"), []byte(strconv.Itoa(pgid)), 0600); err != nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = cmd.Wait() // 显式回收直接子进程，避免僵尸进程阻塞 ESRCH 探测
		return nil, fail(2, "error", "write pgid file: %v", err)
	}

	waitErr := cmd.Wait()
	out := append(stdout.Bytes(), stderr.Bytes()...)
	outStr := string(out)

	// 7. Interrupt / timeout check: Exit 2
	if sigCtx.Err() != nil {
		return nil, fail(2, "error", "interrupted")
	}
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fail(2, "error", "stream fetch timeout (15s)")
	}

	// 8. Positive evidence: media contains no video stream (Exit 4). A frame already on disk
	// wins over log text: a title or file name may itself contain "Video: none".
	srcPath := filepath.Join(scratch, "00000001.jpg")
	if !nonEmptyFile(srcPath) && (strings.Contains(outStr, "No video or audio streams selected") || strings.Contains(outStr, "Video: none")) {
		return nil, fail(4, "unavailable", "media contains no video stream")
	}

	// 9. Duration gate: if reliable duration is known and at >= duration, request is out of range (Exit 4)
	dur, hasDur := parseDurationFromOutput(outStr)
	if hasDur && at >= dur {
		return nil, fail(4, "unavailable", "timestamp out of range: requested %.2fs >= duration %.2fs", at, dur)
	}

	// 10. Artifact processing: check for 00000001.jpg
	if nonEmptyFile(srcPath) {
		targetPath := filepath.Join(scratch, fmt.Sprintf("frame_%.0fs.jpg", at))
		if err := os.Rename(srcPath, targetPath); err != nil {
			return nil, fail(2, "error", "failed to rename frame: %v", err)
		}
		if err := os.Chmod(targetPath, 0600); err != nil {
			return nil, fail(2, "error", "failed to chmod frame: %v", err)
		}
		f, err := os.Open(targetPath)
		if err != nil {
			return nil, fail(2, "error", "failed to open frame: %v", err)
		}
		cfg, _, err := image.DecodeConfig(f)
		_ = f.Close()
		if err != nil {
			return nil, fail(2, "error", "failed to decode frame header: %v", err)
		}
		fi, err := os.Stat(targetPath)
		if err != nil {
			return nil, fail(2, "error", "failed to stat frame: %v", err)
		}

		success = true
		return &FrameResult{
			Status:    "ok",
			URL:       rawURL,
			At:        at,
			ActualAt:  nil,
			Path:      targetPath,
			Width:     cfg.Width,
			Height:    cfg.Height,
			SizeBytes: fi.Size(),
			Format:    "jpg",
		}, nil
	}

	// 11. No image generated and not clearly out of range: conservative Exit 2 for stream EOF or network error.
	_ = waitErr
	return nil, fail(2, "error", "frame capture failed: stream ended or network error")
}

// nonEmptyFile reports whether path is a regular file holding at least one byte.
func nonEmptyFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// firstEntryGuard buffers mpv's stdout and stops a playlist after its first entry:
// --frames=1 is per file, so mpv would go on to decode every entry. mpv prints
// "Playing: " as each entry starts; the second such line kills the process group
// whether or not the first entry produced a frame, so an audio-only or failed first
// entry never falls through to a later one.
type firstEntryGuard struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	seen int
	pgid atomic.Int64
}

func (g *firstEntryGuard) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.buf.Write(p)
	if n := countLinePrefix(g.buf.Bytes(), []byte("Playing: ")); n > g.seen {
		g.seen = n
		if pgid := g.pgid.Load(); pgid > 0 && n > 1 {
			_ = syscall.Kill(-int(pgid), syscall.SIGKILL)
		}
	}
	return len(p), nil
}

// countLinePrefix counts lines of b that begin with prefix, so a prefix embedded
// mid-line (e.g. in a file name) is not mistaken for a new entry.
func countLinePrefix(b, prefix []byte) int {
	n := bytes.Count(b, append([]byte{'\n'}, prefix...))
	if bytes.HasPrefix(b, prefix) {
		n++
	}
	return n
}

func (g *firstEntryGuard) Bytes() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.buf.Bytes()
}
