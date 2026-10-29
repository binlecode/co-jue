package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFrameParamValidation(t *testing.T) {
	for _, at := range []float64{-1, -0.001, math.NaN(), math.Inf(1), math.Inf(-1), 1e8, 1e9} {
		wantFail(t, func() error { _, err := Frame("https://example.com/v.mp4", at, 960, 80); return err }(), 1, "error", "--at")
	}
	for _, w := range []int{-1, -100, 3841, 10000} {
		wantFail(t, func() error { _, err := Frame("https://example.com/v.mp4", 10, w, 80); return err }(), 1, "error", "width")
	}
	for _, q := range []int{0, -1, 101, 200} {
		wantFail(t, func() error { _, err := Frame("https://example.com/v.mp4", 10, 960, q); return err }(), 1, "error", "quality")
	}
	for _, u := range []string{"bilisearch:bad", "gsearch:query", "ytsearch2:query", "customsearch:foo"} {
		wantFail(t, func() error { _, err := Frame(u, 10, 960, 80); return err }(), 1, "error", "unsupported search prefix")
	}
}

func mkScratch(t *testing.T, root, name, pgid string, age time.Duration) string {
	t.Helper()
	d := filepath.Join(root, name)
	if err := os.Mkdir(d, 0700); err != nil {
		t.Fatal(err)
	}
	if pgid != "" {
		if err := os.WriteFile(filepath.Join(d, "pgid"), []byte(pgid), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if age > 0 {
		ts := time.Now().Add(-age)
		_ = os.Chtimes(d, ts, ts)
	}
	return d
}

func TestSyncOpportunisticGC(t *testing.T) {
	tmp := t.TempDir()
	dead := 99999999
	for syscall.Kill(-dead, 0) == nil {
		dead++
	}
	deadStr := strconv.Itoa(dead)

	expiredDead := mkScratch(t, tmp, "scratch-frame-expired-dead", deadStr, 2*time.Hour)
	kept := []string{
		mkScratch(t, tmp, "scratch-frame-recent", deadStr, 0),
		mkScratch(t, tmp, "scratch-other-expired", deadStr, 2*time.Hour),
		mkScratch(t, tmp, "scratch-frame-corrupt", "not-a-number", 2*time.Hour),
		mkScratch(t, tmp, "scratch-frame-nopgid", "", 2*time.Hour),
		mkScratch(t, tmp, "scratch-frame-alive", strconv.Itoa(syscall.Getpgrp()), 2*time.Hour),
	}

	syncOpportunisticGC(tmp, time.Hour)

	if _, err := os.Stat(expiredDead); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expired dead scratch not deleted: %v", err)
	}
	for _, d := range kept {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("expected %s retained, got %v", filepath.Base(d), err)
		}
	}
}

func TestSyncOpportunisticGCBudgetAndStarvation(t *testing.T) {
	t.Run("30-item cap", func(t *testing.T) {
		tmp := t.TempDir()
		for i := 0; i < 40; i++ {
			mkScratch(t, tmp, fmt.Sprintf("scratch-frame-item-%02d", i), "99999999", 2*time.Hour)
		}
		syncOpportunisticGC(tmp, time.Hour)
		if left, _ := os.ReadDir(tmp); len(left) < 10 {
			t.Errorf("got %d remaining, want >=10 (30-item cap)", len(left))
		}
	})
	t.Run("young or corrupt head does not starve expired tail", func(t *testing.T) {
		for _, mode := range []string{"young", "corrupt"} {
			tmp := t.TempDir()
			for i := 0; i < 40; i++ {
				if mode == "young" {
					mkScratch(t, tmp, fmt.Sprintf("scratch-frame-a-%02d", i), "", 0)
				} else {
					pgid := ""
					if i%2 == 0 {
						pgid = "garbage"
					}
					mkScratch(t, tmp, fmt.Sprintf("scratch-frame-a-%02d", i), pgid, 2*time.Hour)
				}
			}
			for i := 0; i < 5; i++ {
				mkScratch(t, tmp, fmt.Sprintf("scratch-frame-z-%02d", i), "99999999", 2*time.Hour)
			}
			syncOpportunisticGC(tmp, time.Hour)
			left, _ := os.ReadDir(tmp)
			if len(left) != 40 {
				t.Errorf("%s: got %d entries, want 40 head kept and 5 tail reaped", mode, len(left))
			}
		}
	})
}

// stubFrameMPV installs an mpv stub that copies a real JPEG to <outdir>/00000001.jpg,
// then runs tail (shell script text) with $OUT set to the output directory.
func stubFrameMPV(t *testing.T, tail string) {
	t.Helper()
	stubFrameDeps(t)
	bin := filepath.SplitList(os.Getenv("PATH"))[0]
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 64, 48)), nil); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(bin, "src.jpg")
	if err := os.WriteFile(src, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nfor a; do case $a in --vo-image-outdir=*) OUT=${a#--vo-image-outdir=};; esac; done\n" +
		"/bin/cp " + src + " \"$OUT/00000001.jpg\"\n" + tail
	if err := os.WriteFile(filepath.Join(bin, "mpv"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
}

// TestFrameArtifactBeatsVideoNoneText: a title echoed as "Video: none" must not
// turn a frame already on disk into exit 4.
func TestFrameArtifactBeatsVideoNoneText(t *testing.T) {
	stubFrameMPV(t, "echo 'Playing: My Video: none.mkv'\necho ' (+) Video: none'\n")
	res, err := Frame("/tmp/My Video: none.mkv", 0, 0, 80)
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if res.Width != 64 || res.Height != 48 || res.SizeBytes <= 0 {
		t.Errorf("unexpected result %+v", res)
	}
}

// TestFrameNoVideoWithoutArtifact: with no frame on disk, "Video: none" is still exit 4.
func TestFrameNoVideoWithoutArtifact(t *testing.T) {
	stubFrameMPV(t, "/bin/rm -f \"$OUT/00000001.jpg\"\necho ' (+) Video: none'\n")
	_, err := Frame("/tmp/a.mp3", 0, 0, 80)
	var f *Fail
	if !errors.As(err, &f) || f.Code != 4 {
		t.Fatalf("expected exit 4, got %v", err)
	}
}

// TestFramePlaylistStopsAfterFirstEntry: the second entry's "Playing:" kills mpv instead
// of letting it decode the rest of the playlist.
func TestFramePlaylistStopsAfterFirstEntry(t *testing.T) {
	stubFrameMPV(t, "echo 'Playing: a.mkv'\necho 'Playing: b.mkv'\nread _\n")
	start := time.Now()
	res, err := Frame("/tmp/list.m3u", 0, 0, 80)
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("mpv not stopped after the first entry: took %v", time.Since(start))
	}
	if res.Width != 64 {
		t.Errorf("unexpected result %+v", res)
	}
}

// TestFramePlaylistAudioFirstEntryNoFallthrough: a first entry without a frame (audio
// or failed) must not fall through to a later entry's frame: the second "Playing:"
// kills mpv before it can write one.
func TestFramePlaylistAudioFirstEntryNoFallthrough(t *testing.T) {
	stubFrameMPV(t, "/bin/rm -f \"$OUT/00000001.jpg\"\necho 'Playing: a.mp3'\necho 'Playing: b.mkv'\nread _\n/bin/cp \"$OUT/../src.jpg\" \"$OUT/00000001.jpg\"\n")
	start := time.Now()
	_, err := Frame("/tmp/list.m3u", 0, 0, 80)
	if time.Since(start) > 2*time.Second {
		t.Errorf("mpv not stopped at the second entry: took %v", time.Since(start))
	}
	var f *Fail
	if !errors.As(err, &f) || f.Code != 2 {
		t.Fatalf("expected exit 2 with no frame from the first entry, got %v", err)
	}
}

func TestFirstEntryGuardSplitWrites(t *testing.T) {
	g := &firstEntryGuard{}
	g.pgid.Store(99999999)
	// "Playing: " split across writes is still counted once per entry (pgid is bogus).
	_, _ = g.Write([]byte("Play"))
	_, _ = g.Write([]byte("ing: a\nPlaying: b\n"))
	if g.seen != 2 || !bytes.Equal(g.Bytes(), []byte("Playing: a\nPlaying: b\n")) {
		t.Errorf("guard seen=%d buf=%q", g.seen, g.Bytes())
	}
}

// TestFirstEntryGuardLineAnchored: "Playing: " inside a file name is not a new entry.
func TestFirstEntryGuardLineAnchored(t *testing.T) {
	g := &firstEntryGuard{}
	g.pgid.Store(99999999)
	_, _ = g.Write([]byte("Playing: /tmp/Playing: large.y4m\n"))
	if g.seen != 1 {
		t.Errorf("embedded prefix miscounted: seen=%d", g.seen)
	}
	_, _ = g.Write([]byte("Playing: b.mkv\n"))
	if g.seen != 2 {
		t.Errorf("second entry not counted: seen=%d", g.seen)
	}
}

func TestParseDurationFromOutput(t *testing.T) {
	cases := []struct {
		input   string
		wantDur float64
		wantOk  bool
	}{
		{"V: 00:01:13 / 00:03:33 (34%) Cache: 3.9s/411KB", 213, true},
		{"V: 00:00:01 / 00:00:10 (10%)", 10, true},
		{"Duration: 00:01:45.50, start: 0.000000", 105.5, true},
		{"Duration: 12:34", 754, true},
		{"DURATION:42.5\n", 42.5, true},
		{"Playing: DURATION:1.mkv\nDURATION:7\n", 7, true},
		{"Playing: DURATION:1.mkv", 0, false},
		{"No duration here whatsoever", 0, false},
		{"", 0, false},
	}

	for _, c := range cases {
		dur, ok := parseDurationFromOutput(c.input)
		if ok != c.wantOk {
			t.Errorf("parseDurationFromOutput(%q) ok=%v, want %v", c.input, ok, c.wantOk)
		}
		if ok && fmt.Sprintf("%.2f", dur) != fmt.Sprintf("%.2f", c.wantDur) {
			t.Errorf("parseDurationFromOutput(%q) dur=%v, want %v", c.input, dur, c.wantDur)
		}
	}
}

// stubFrameDeps puts a fake mpv and yt-dlp alone on PATH and points TMPDIR at a fresh dir, so
// the reap tests run offline with no real mpv. The fake mpv forks a child and hangs: the
// teardown has a whole process group to kill.
func stubFrameDeps(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	for name, body := range map[string]string{
		"mpv":    "#!/bin/sh\n/bin/sleep 60 &\nwait\n",
		"yt-dlp": "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("TMPDIR", t.TempDir())
}

func TestFrameWritePgidFailureReap(t *testing.T) {
	stubFrameDeps(t)
	orig := writePgidFile
	defer func() { writePgidFile = orig }()

	var capturedPgid int
	var capturedScratch string

	writePgidFile = func(name string, data []byte, perm os.FileMode) error {
		capturedScratch = filepath.Dir(name)
		if n, err := strconv.Atoi(string(data)); err == nil {
			capturedPgid = n
		}
		return errors.New("simulated disk full")
	}

	// The stub mpv passes LookPath and starts; then the pgid write fails.
	_, err := Frame("av://lavfi:testsrc=duration=5", 0, 960, 80)
	var f *Fail
	if !errors.As(err, &f) || f.Code != 2 {
		t.Fatalf("expected Exit 2 on write pgid failure, got %v", err)
	}
	if !strings.Contains(f.Msg, "simulated disk full") {
		t.Fatalf("expected simulated error message in %q", f.Msg)
	}

	// 1. Assert captured PGID was recorded
	if capturedPgid <= 0 {
		t.Fatalf("expected captured PGID > 0, got %d", capturedPgid)
	}

	// 2. Assert the process group has completely died (ESRCH)
	if err := syscall.Kill(-capturedPgid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("expected process group %d to be dead (ESRCH), got err=%v", capturedPgid, err)
	}

	// 3. Assert scratch directory has been synchronously removed
	if capturedScratch != "" {
		if _, err := os.Stat(capturedScratch); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected scratch directory %s to be deleted, got err=%v", capturedScratch, err)
		}
	}
}

func TestFrameTimeoutReap(t *testing.T) {
	stubFrameDeps(t)
	origTimeout := frameTimeout
	defer func() { frameTimeout = origTimeout }()
	frameTimeout = 30 * time.Millisecond // very short timeout to trigger deadline exceeded

	origWrite := writePgidFile
	defer func() { writePgidFile = origWrite }()

	var capturedPgid int
	var capturedScratch string
	writePgidFile = func(name string, data []byte, perm os.FileMode) error {
		capturedScratch = filepath.Dir(name)
		if n, err := strconv.Atoi(string(data)); err == nil {
			capturedPgid = n
		}
		return os.WriteFile(name, data, perm)
	}

	// The stub mpv hangs past the 30ms deadline.
	t0 := time.Now()
	_, err := Frame("https://www.youtube.com/watch?v=dQw4w9WgXcQ", 50, 960, 80)
	elapsed := time.Since(t0)
	var f *Fail
	if !errors.As(err, &f) || f.Code != 2 {
		t.Fatalf("expected Exit 2 on timeout, got %v", err)
	}
	if !strings.Contains(f.Msg, "timeout") {
		t.Fatalf("expected timeout message, got %v", f.Msg)
	}
	// Assert timeout returned boundedly without hanging
	if elapsed > 2*time.Second {
		t.Errorf("expected fast timeout teardown (<2s), took %v", elapsed)
	}

	// Assert captured PGID was recorded
	if capturedPgid <= 0 {
		t.Fatalf("expected captured PGID > 0, got %d", capturedPgid)
	}

	// Assert process group is completely dead (ESRCH)
	if err := syscall.Kill(-capturedPgid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("expected timed-out process group %d to be dead (ESRCH), got err=%v", capturedPgid, err)
	}

	// Assert scratch directory was synchronously deleted
	if capturedScratch != "" {
		if _, err := os.Stat(capturedScratch); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected scratch directory %s to be deleted after timeout, got err=%v", capturedScratch, err)
		}
	}
}

func TestFrameProductionTimeoutBudget(t *testing.T) {
	if frameTimeout != 15*time.Second || frameWaitDelay != 2*time.Second || frameTeardownPoll != 500*time.Millisecond {
		t.Errorf("production frame budget = (%v, %v, %v), want (15s, 2s, 500ms)", frameTimeout, frameWaitDelay, frameTeardownPoll)
	}
}

func TestFrameEnvelopeEnrichment(t *testing.T) {
	// Case 1: Duration present, ActualAt nil -> serializes duration, omits actual_at
	res := &FrameResult{
		Status:    "ok",
		URL:       "https://example.com/video.mp4",
		At:        73.0,
		Duration:  1120.0,
		ActualAt:  nil,
		Path:      "/tmp/frame.jpg",
		Width:     960,
		Height:    540,
		SizeBytes: 42000,
		Format:    "jpg",
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"duration":1120`) {
		t.Errorf("expected JSON to contain duration, got %s", s)
	}
	if strings.Contains(s, "actual_at") {
		t.Errorf("expected actual_at to be omitted when nil, got %s", s)
	}

	// Case 2: ActualAt non-nil (0.0) -> serialized properly
	zero := 0.0
	res2 := &FrameResult{
		Status:   "ok",
		At:       0.0,
		ActualAt: &zero,
	}
	data2, err := json.Marshal(res2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data2), `"actual_at":0`) {
		t.Errorf("expected actual_at:0 to be present, got %s", string(data2))
	}
}

func TestFrameHotFailureFallbackToCold(t *testing.T) {
	isolate(t)
	targetURL := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

	// Install stub mpv: if --no-ytdl is passed (hot attempt), simulate HTTP 403 failure;
	// otherwise (cold fallback), copy JPEG and emit DURATION.
	stubScript := `
has_ytdl_raw=0
for a; do
	if [ "$a" = "--no-ytdl" ]; then
		/bin/rm -f "$OUT/00000001.jpg"
		echo "Failed to open direct stream: Server returned 403"
		exit 2
	fi
	if [ "$a" = "--ytdl-raw-options=extractor-args=youtube:player_client=-tv_samsung" ]; then
		has_ytdl_raw=1
	fi
done
if [ "$has_ytdl_raw" -ne 1 ]; then
	/bin/rm -f "$OUT/00000001.jpg"
	echo "expected --ytdl-raw-options on cold attempt"
	exit 2
fi
echo 'DURATION:120.0'
`
	stubFrameMPV(t, stubScript)

	// Seed stale cache entry into the active isolated test TMPDIR
	if err := putStreamCache(targetURL, "https://invalid.example.test/expired.mp4", 120.0, nil, "TestUA"); err != nil {
		t.Fatalf("putStreamCache failed: %v", err)
	}
	if _, hit := getStreamCache(targetURL); !hit {
		t.Fatal("expected seeded cache to be present")
	}

	res, err := Frame(targetURL, 10, 0, 80)
	if err != nil {
		t.Fatalf("expected successful fallback to cold, got %v", err)
	}
	if res.Width != 64 || res.Height != 48 {
		t.Errorf("unexpected frame dimensions %+v", res)
	}

	// Verify the stale entry was invalidated
	if _, hit := getStreamCache(targetURL); hit {
		t.Error("expected stale cache entry to be deleted after hot failure")
	}
}

func TestFrameHotCorruptJPEGRecoversViaCold(t *testing.T) {
	isolate(t)
	targetURL := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

	// If --no-ytdl is passed (hot attempt), write a corrupt 10-byte truncated JPEG;
	// otherwise (cold fallback), copy real JPEG.
	stubScript := `
for a; do
	if [ "$a" = "--no-ytdl" ]; then
		printf "corruptjpg" > "$OUT/00000001.jpg"
		echo 'DURATION:120.0'
		exit 0
	fi
done
echo 'DURATION:120.0'
`
	stubFrameMPV(t, stubScript)

	// Seed cache entry
	if err := putStreamCache(targetURL, "https://cdn.example.test/stream.mp4", 120.0, nil, "TestUA"); err != nil {
		t.Fatal(err)
	}

	res, err := Frame(targetURL, 10, 0, 80)
	if err != nil {
		t.Fatalf("expected recovery via cold fallback, got %v", err)
	}
	if res.Width != 64 || res.Height != 48 {
		t.Errorf("unexpected frame dimensions %+v", res)
	}

	// Verify corrupted cache entry was invalidated
	if _, hit := getStreamCache(targetURL); hit {
		t.Error("expected corrupt cache entry to be invalidated")
	}
}

func TestFrameLoopbackDirectStreamNoYtdl(t *testing.T) {
	isolate(t)
	targetURL := "http://127.0.0.1:8999/external/movie.mp4"

	// Fail if --no-ytdl is NOT passed on the cold attempt for a loopback HTTP stream.
	stubScript := `
has_no_ytdl=0
for a; do
	if [ "$a" = "--no-ytdl" ]; then
		has_no_ytdl=1
	fi
done
if [ "$has_no_ytdl" -ne 1 ]; then
	/bin/rm -f "$OUT/00000001.jpg"
	echo "expected --no-ytdl on loopback stream"
	exit 2
fi
echo 'DURATION:120.0'
`
	stubFrameMPV(t, stubScript)

	res, err := Frame(targetURL, 10, 0, 80)
	if err != nil {
		t.Fatalf("expected loopback cold attempt to pass --no-ytdl, got %v", err)
	}
	if res.Width != 64 || res.Height != 48 {
		t.Errorf("unexpected frame dimensions %+v", res)
	}
}

