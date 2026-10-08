package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
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
// actual_at is explicitly *float64 (omitempty when nil) to avoid false precision.
// duration carries the total media duration in seconds when detected.
type FrameResult struct {
	Status    string   `json:"status"`
	URL       string   `json:"url"`
	At        float64  `json:"at"`
	Duration  float64  `json:"duration,omitempty"`
	ActualAt  *float64 `json:"actual_at,omitempty"`
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

	// Production time budget parameters, strictly verified by invariants.
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
func syncOpportunisticGC(runtimeDir string, ttl time.Duration) {
	deadline := time.Now().Add(50 * time.Millisecond)
	f, err := os.Open(runtimeDir)
	if err != nil {
		return
	}
	defer f.Close()

	checked := 0
	for {
		if checked >= 30 || time.Now().After(deadline) {
			break
		}
		entries, err := f.ReadDir(30)
		if err != nil || len(entries) == 0 {
			break
		}
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

			if err := syscall.Kill(-dirPgid, 0); errors.Is(err, syscall.ESRCH) {
				_ = os.RemoveAll(dirPath)
			}
		}
	}

	// Also opportunistically clean expired cache files
	opportunisticCacheGC(filepath.Join(runtimeDir, "cache"), ttl, deadline)
}

// executeOneShotMPV launches a one-shot mpv subprocess under an isolated process group.
// It manages the process lifecycle, reaping and termination, but does NOT remove scratch.
func executeOneShotMPV(parentCtx context.Context, deadline time.Time, args []string, scratch string) (outStr string, waitErr error, dead bool, timedOut bool, err error) {
	ctx, cancel := context.WithDeadline(parentCtx, deadline)
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
	defer func() {
		if pgid > 0 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			deadline := time.Now().Add(frameTeardownPoll)
			for time.Now().Before(deadline) {
				if err := syscall.Kill(-pgid, 0); errors.Is(err, syscall.ESRCH) {
					dead = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	if err := cmd.Start(); err != nil {
		return "", nil, true, false, fail(2, "error", "failed to start mpv: %v", err)
	}
	pgid = cmd.Process.Pid
	stdout.pgid.Store(int64(pgid))
	if err := writePgidFile(filepath.Join(scratch, "pgid"), []byte(strconv.Itoa(pgid)), 0600); err != nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = cmd.Wait()
		return "", nil, false, false, fail(2, "error", "write pgid file: %v", err)
	}

	waitErr = cmd.Wait()
	out := append(stdout.Bytes(), stderr.Bytes()...)
	outStr = string(out)

	if ctx.Err() == context.DeadlineExceeded {
		return outStr, waitErr, dead, true, nil
	}
	return outStr, waitErr, dead, false, nil
}

// Frame captures a single visual frame from rawURL at seconds at, scaled to width
// with JPEG compression quality.
func Frame(rawURL string, at float64, width int, quality int) (*FrameResult, error) {
	// Call-level absolute deadlines
	callDeadline := time.Now().Add(17500 * time.Millisecond)
	workDeadline := callDeadline.Add(-200 * time.Millisecond)

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

	createScratch := func() (string, error) {
		s, err := os.MkdirTemp(rDir, "scratch-frame-")
		if err != nil {
			return "", fail(2, "error", "create scratch dir: %v", err)
		}
		if err := os.Chmod(s, 0700); err != nil {
			_ = os.RemoveAll(s)
			return "", fail(2, "error", "chmod scratch dir: %v", err)
		}
		return s, nil
	}

	vfFilter := ""
	if width > 0 {
		vfFilter = fmt.Sprintf("lavfi=[scale=w='min(%d,iw)':h='min(%d,ih)':force_original_aspect_ratio=decrease,scale=w='trunc(iw/2)*2':h='trunc(ih/2)*2']", width, width)
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	buildArgs := func(targetOutDir, mediaTarget string, noYtdl bool, headers map[string]string, ua string) []string {
		a := []string{
			"--no-config", "--no-audio", "--no-sub", "--audio-display=no",
			"--frames=1",
			"--loop-playlist=no",
			fmt.Sprintf("--start=%f", at),
			"--hr-seek=yes",
			"--demuxer-max-bytes=8MiB",
			"--demuxer-readahead-secs=0",
			"--vo=image",
			"--vo-image-format=jpg",
			fmt.Sprintf("--vo-image-jpeg-quality=%d", quality),
			fmt.Sprintf("--vo-image-outdir=%s", targetOutDir),
			"--term-playing-msg=DURATION:${=duration}\nSTREAM_URL:${stream-open-filename}\nFILE_FORMAT:${file-format}\nHTTP_HEADERS:${file-local-options/http-header-fields}\nUSER_AGENT:${file-local-options/user-agent}\nLAVF_OPTS:${file-local-options/stream-lavf-o}",
		}
		if noYtdl {
			a = append(a, "--no-ytdl")
			if headers != nil {
				if ref := headers["Referer"]; ref != "" {
					a = append(a, "--http-header-fields=Referer: "+ref)
				}
			}
			if ua != "" {
				a = append(a, fmt.Sprintf("--user-agent=%s", ua))
			}
		} else {
			a = append(a, `--ytdl-format=bv*[height<=720]/b[height<=720]`)
		}
		if vfFilter != "" {
			a = append(a, "--vf="+vfFilter)
		}
		a = append(a, "--", mediaTarget)
		return a
	}

	deliverArtifact := func(targetOutDir string, dur float64) (*FrameResult, error) {
		if time.Until(callDeadline) <= 0 {
			return nil, fail(2, "error", "delivery deadline exceeded")
		}
		srcPath := filepath.Join(targetOutDir, "00000001.jpg")
		if !nonEmptyFile(srcPath) {
			return nil, nil
		}
		targetPath := filepath.Join(targetOutDir, fmt.Sprintf("frame_%.0fs.jpg", at))
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
		img, err := jpeg.Decode(f)
		_ = f.Close()
		if err != nil {
			return nil, fail(2, "error", "failed to decode complete frame: %v", err)
		}
		fi, err := os.Stat(targetPath)
		if err != nil {
			return nil, fail(2, "error", "failed to stat frame: %v", err)
		}
		if time.Until(callDeadline) <= 0 {
			return nil, fail(2, "error", "delivery deadline exceeded")
		}

		b := img.Bounds()
		return &FrameResult{
			Status:    "ok",
			URL:       rawURL,
			At:        at,
			Duration:  dur,
			ActualAt:  nil,
			Path:      targetPath,
			Width:     b.Dx(),
			Height:    b.Dy(),
			SizeBytes: fi.Size(),
			Format:    "jpg",
		}, nil
	}

	// 5. Check cache for direct stream URL (network URLs only)
	isNetwork := strings.HasPrefix(targetURL, "http://") || strings.HasPrefix(targetURL, "https://") || strings.HasPrefix(targetURL, "ytdl://")
	var entry *StreamCacheEntry
	var hit bool
	if isNetwork {
		entry, hit = getStreamCache(targetURL)
	}
	if hit && entry != nil {
		// Zero-duration safe range gate
		if entry.Duration > 0 && at >= entry.Duration {
			return nil, fail(4, "unavailable", "timestamp out of range: requested %.2fs >= duration %.2fs", at, entry.Duration)
		}

		// Hot attempt budget: min(2s, remaining)
		rem := time.Until(workDeadline) - 2500*time.Millisecond
		if rem > 0 {
			hotScratch, sErr := createScratch()
			if sErr != nil {
				return nil, sErr
			}

			hotTimeout := 2 * time.Second
			if rem < hotTimeout {
				hotTimeout = rem
			}
			hotDeadline := time.Now().Add(hotTimeout)
			teardownReserve := frameWaitDelay + frameTeardownPoll
			if hotDeadline.After(workDeadline.Add(-teardownReserve)) {
				hotDeadline = workDeadline.Add(-teardownReserve)
			}
			hotArgs := buildArgs(hotScratch, entry.DirectURL, true, entry.HTTPHeaders, entry.UserAgent)
			outStr, waitErr, hotDead, timedOut, launchErr := executeOneShotMPV(sigCtx, hotDeadline, hotArgs, hotScratch)
			if launchErr != nil {
				if hotDead {
					_ = os.RemoveAll(hotScratch)
				}
				return nil, launchErr
			}

			dur, _ := parseDurationFromOutput(outStr)
			if dur <= 0 {
				dur = entry.Duration
			}

			// Do NOT deliver artifact if hot process timed out or crashed (prevents corrupt partial JPEG)
			if !timedOut && waitErr == nil {
				res, err := deliverArtifact(hotScratch, dur)
				if err == nil && res != nil {
					return res, nil // Hot hit success!
				}
				// Deliver artifact failed (corrupt JPEG or delivery timeout): invalidate stale cache and fallback to cold
				deleteStreamCache(targetURL, entry.Fingerprint)
			}

			// Clean up failed hot scratch strictly if process group completely terminated
			if hotDead {
				_ = os.RemoveAll(hotScratch)
			}

			// Artifact absent: check if this was a definitive business error
			if strings.Contains(outStr, "No video or audio streams selected") || strings.Contains(outStr, "Video: none") {
				return nil, fail(4, "unavailable", "media contains no video stream")
			}
			if dur > 0 && at >= dur {
				return nil, fail(4, "unavailable", "timestamp out of range: requested %.2fs >= duration %.2fs", at, dur)
			}

			// If hot attempt failed due to network / 403 / timedOut, safely invalidate and fallback to cold
			if timedOut || strings.Contains(outStr, "403") || strings.Contains(outStr, "Failed to open") || strings.Contains(outStr, "Server returned 403") {
				deleteStreamCache(targetURL, entry.Fingerprint)
			}
		}
	}

	// 6. Cold attempt with independent fresh scratch directory
	remWork := time.Until(workDeadline) - 2500*time.Millisecond
	if remWork <= 0 {
		return nil, fail(2, "error", "work deadline exceeded")
	}
	coldTimeout := frameTimeout
	if remWork < coldTimeout {
		coldTimeout = remWork
	}
	coldDeadline := time.Now().Add(coldTimeout)
	teardownReserve := frameWaitDelay + frameTeardownPoll
	if coldDeadline.After(workDeadline.Add(-teardownReserve)) {
		coldDeadline = workDeadline.Add(-teardownReserve)
	}

	coldScratch, sErr := createScratch()
	if sErr != nil {
		return nil, sErr
	}

	coldArgs := buildArgs(coldScratch, targetURL, false, nil, "")
	outStr, waitErr, coldDead, timedOut, launchErr := executeOneShotMPV(sigCtx, coldDeadline, coldArgs, coldScratch)
	if launchErr != nil {
		if coldDead {
			_ = os.RemoveAll(coldScratch)
		}
		return nil, launchErr
	}
	if sigCtx.Err() != nil {
		if coldDead {
			_ = os.RemoveAll(coldScratch)
		}
		return nil, fail(2, "error", "interrupted")
	}
	if timedOut {
		if coldDead {
			_ = os.RemoveAll(coldScratch)
		}
		return nil, fail(2, "error", "stream fetch timeout")
	}

	// Positive evidence: media contains no video stream (Exit 4)
	srcPath := filepath.Join(coldScratch, "00000001.jpg")
	if !nonEmptyFile(srcPath) && (strings.Contains(outStr, "No video or audio streams selected") || strings.Contains(outStr, "Video: none")) {
		if coldDead {
			_ = os.RemoveAll(coldScratch)
		}
		return nil, fail(4, "unavailable", "media contains no video stream")
	}

	// Duration gate
	dur, hasDur := parseDurationFromOutput(outStr)
	if hasDur && at >= dur {
		if coldDead {
			_ = os.RemoveAll(coldScratch)
		}
		return nil, fail(4, "unavailable", "timestamp out of range: requested %.2fs >= duration %.2fs", at, dur)
	}

	// Artifact delivery
	res, err := deliverArtifact(coldScratch, dur)
	if err != nil {
		if coldDead {
			_ = os.RemoveAll(coldScratch)
		}
		return nil, err
	}
	if res != nil {
		// Populate cache if positive single-stream proof is satisfied and budget allows
		if time.Until(callDeadline) > 0 {
			if dURL, headers, ua, ok := parseStreamURLFromOutput(outStr); ok {
				_ = putStreamCache(targetURL, dURL, dur, headers, ua)
			}
		}
		if time.Until(callDeadline) <= 0 {
			if coldDead {
				_ = os.RemoveAll(coldScratch)
			}
			return nil, fail(2, "error", "delivery deadline exceeded")
		}
		return res, nil
	}

	_ = waitErr
	if coldDead {
		_ = os.RemoveAll(coldScratch)
	}
	return nil, fail(2, "error", "frame capture failed: stream ended or network error")
}

func nonEmptyFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

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
