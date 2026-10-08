package engine

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCacheDir0700AndSymlinkRefusal(t *testing.T) {
	isolate(t)
	// Verify cacheDir establishes 0700 mode
	dir, err := cacheDir()
	if err != nil {
		t.Fatalf("cacheDir failed: %v", err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		t.Fatalf("stat cache dir: %v", err)
	}
	if !fi.IsDir() || fi.Mode().Perm() != 0700 {
		t.Errorf("expected cache dir 0700, got %v", fi.Mode().Perm())
	}

	// Symlink refusal test: plant a symlink at cache dir path
	rDir, err := runtimeDir(true)
	if err != nil {
		t.Fatal(err)
	}
	cPath := filepath.Join(rDir, "cache")
	_ = os.RemoveAll(cPath)

	fakeTarget := filepath.Join(rDir, "cache-target")
	_ = os.Mkdir(fakeTarget, 0700)
	defer os.RemoveAll(fakeTarget)
	if err := os.Symlink(fakeTarget, cPath); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(cPath)

	_, err = cacheDir()
	if err == nil {
		t.Fatal("expected symlink cache dir to be refused")
	}
	var f *Fail
	if !errors.As(err, &f) || f.Code != 2 || !strings.Contains(f.Msg, "symlink") {
		t.Errorf("expected exit 2 symlink refusal, got %v", err)
	}
}

func TestStreamCachePutGetExpire(t *testing.T) {
	isolate(t)
	target := "https://example.com/lecture.mp4"
	direct := "https://cdn.example.com/stream-1234.mp4"
	headers := map[string]string{"Referer": "https://example.com"}
	ua := "Audit-Test-UserAgent/1.0"

	// Put cache with UserAgent
	if err := putStreamCache(target, direct, 1120.0, headers, ua); err != nil {
		t.Fatalf("putStreamCache failed: %v", err)
	}

	// Get cache: hit
	entry, hit := getStreamCache(target)
	if !hit || entry == nil {
		t.Fatal("expected cache hit")
	}
	if entry.DirectURL != direct {
		t.Errorf("expected direct URL %q, got %q", direct, entry.DirectURL)
	}
	if entry.Duration != 1120.0 {
		t.Errorf("expected duration 1120.0, got %v", entry.Duration)
	}
	if entry.HTTPHeaders["Referer"] != "https://example.com" {
		t.Errorf("expected Referer header preserved, got %v", entry.HTTPHeaders)
	}
	if entry.UserAgent != ua {
		t.Errorf("expected UserAgent preserved, got %q", entry.UserAgent)
	}

	// Verify file permissions 0600
	_, jsonPath, _, err := cacheEntryPaths(target)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(jsonPath)
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Errorf("expected cache entry mode 0600, got %v", fi.Mode().Perm())
	}

	// Expiration test: rewrite json with expired time
	entry.ExpiresAt = time.Now().Add(-time.Hour)
	expData, _ := json.Marshal(entry)
	_ = os.WriteFile(jsonPath, expData, 0600)
	if _, hit := getStreamCache(target); hit {
		t.Error("expected expired entry to result in cache miss")
	}

	// Re-put fresh entry for subsequent tests
	_ = putStreamCache(target, direct, 1120.0, headers, ua)

	// TOCTOU delete: wrong fingerprint does not delete
	deleteStreamCache(target, "wrong-fingerprint")
	if _, hit := getStreamCache(target); !hit {
		t.Error("expected entry to be preserved when fingerprint mismatches")
	}

	// Correct fingerprint deletes
	currentEntry, _ := getStreamCache(target)
	deleteStreamCache(target, currentEntry.Fingerprint)
	if _, hit := getStreamCache(target); hit {
		t.Error("expected entry to be deleted when fingerprint matches")
	}
}

func TestParseStreamURLPositiveEvidence(t *testing.T) {
	// Case 1: Valid single mp4 stream with valid duration, clean Referer and UserAgent
	validOut := `DURATION:120.500000
FILE_FORMAT:mov,mp4,m4a,3gp,3g2,mj2
STREAM_URL:https://rr1.googlevideo.com/videoplayback?id=123
HTTP_HEADERS:Referer: https://youtube.com/
USER_AGENT:Custom-Client/2.0
LAVF_OPTS:
`
	dURL, headers, ua, ok := parseStreamURLFromOutput(validOut)
	if !ok || dURL != "https://rr1.googlevideo.com/videoplayback?id=123" {
		t.Errorf("expected valid stream parse, got ok=%v, url=%q", ok, dURL)
	}
	if headers["Referer"] != "https://youtube.com/" {
		t.Errorf("expected Referer header parsed, got %v", headers)
	}
	if ua != "Custom-Client/2.0" {
		t.Errorf("expected User-Agent parsed, got %q", ua)
	}

	// Case 2: EDL format rejected
	edlOut := `DURATION:120.500000
FILE_FORMAT:mov,mp4,m4a,3gp,3g2,mj2
STREAM_URL:edl://https://cdn.example.com/part1;https://cdn.example.com/part2
HTTP_HEADERS:
USER_AGENT:
LAVF_OPTS:
`
	if _, _, _, ok := parseStreamURLFromOutput(edlOut); ok {
		t.Error("expected EDL stream to be rejected")
	}

	// Case 3: HLS/DASH manifest format rejected
	hlsOut := `DURATION:120.500000
FILE_FORMAT:hls,applehttp
STREAM_URL:https://cdn.example.com/playlist.m3u8
HTTP_HEADERS:
USER_AGENT:
LAVF_OPTS:
`
	if _, _, _, ok := parseStreamURLFromOutput(hlsOut); ok {
		t.Error("expected HLS format to be rejected")
	}

	// Case 4: Dynamic cookie injection rejected
	cookieOut := `DURATION:120.500000
FILE_FORMAT:matroska,webm
STREAM_URL:https://cdn.example.com/video.webm
HTTP_HEADERS:
USER_AGENT:
LAVF_OPTS:cookies=auth_token=abcdef
`
	if _, _, _, ok := parseStreamURLFromOutput(cookieOut); ok {
		t.Error("expected cookie binding to be rejected")
	}

	// Case 5: Ambiguous comma-separated headers rejected
	ambiguousHeaderOut := `DURATION:120.500000
FILE_FORMAT:mov,mp4,m4a,3gp,3g2,mj2
STREAM_URL:https://cdn.example.com/video.mp4
HTTP_HEADERS:X-Header-1: val1,X-Header-2: val2
USER_AGENT:
LAVF_OPTS:
`
	if _, _, _, ok := parseStreamURLFromOutput(ambiguousHeaderOut); ok {
		t.Error("expected ambiguous comma-separated headers to be rejected")
	}

	// Case 6: Headers containing cookie injected via stream-lavf-o rejected
	headerCookieOut := `DURATION:120.500000
FILE_FORMAT:mov,mp4,m4a,3gp,3g2,mj2
STREAM_URL:https://cdn.example.com/video.mp4
HTTP_HEADERS:
USER_AGENT:
LAVF_OPTS:headers=Cookie: session=xyz
`
	if _, _, _, ok := parseStreamURLFromOutput(headerCookieOut); ok {
		t.Error("expected headers=Cookie: lavf opts to be rejected")
	}

	// Case 7: Any non-empty lavf opts (e.g. http_proxy) rejected to prevent lost options
	proxyOut := `DURATION:120.500000
FILE_FORMAT:mov,mp4,m4a,3gp,3g2,mj2
STREAM_URL:https://cdn.example.com/video.mp4
HTTP_HEADERS:
USER_AGENT:
LAVF_OPTS:http_proxy=http://127.0.0.1:3128
`
	if _, _, _, ok := parseStreamURLFromOutput(proxyOut); ok {
		t.Error("expected non-empty lavf opts (http_proxy) to be rejected")
	}
}

func TestOpportunisticCacheGC(t *testing.T) {
	isolate(t)
	cDir := t.TempDir()
	_ = os.Chmod(cDir, 0700)

	oldTime := time.Now().Add(-2 * time.Hour)
	recentTime := time.Now()

	// 1. Expired entry
	expJson := filepath.Join(cDir, "expired.json")
	_ = os.WriteFile(expJson, []byte("{}"), 0600)
	_ = os.Chtimes(expJson, oldTime, oldTime)

	// 2. Fresh entry
	freshJson := filepath.Join(cDir, "fresh.json")
	_ = os.WriteFile(freshJson, []byte("{}"), 0600)
	_ = os.Chtimes(freshJson, recentTime, recentTime)

	// Run GC
	opportunisticCacheGC(cDir, time.Hour, time.Now().Add(50*time.Millisecond))

	if _, err := os.Stat(expJson); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected expired cache file to be removed, got err=%v", err)
	}
	if _, err := os.Stat(freshJson); err != nil {
		t.Errorf("expected fresh cache file to be retained, got err=%v", err)
	}
}
