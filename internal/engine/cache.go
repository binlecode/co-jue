package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// StreamCacheEntry stores the resolved direct stream URL and its freshness metadata.
type StreamCacheEntry struct {
	URL         string            `json:"url"`
	DirectURL   string            `json:"direct_url"`
	Duration    float64           `json:"duration,omitempty"`
	HTTPHeaders map[string]string `json:"http_headers,omitempty"`
	UserAgent   string            `json:"user_agent,omitempty"`
	Fingerprint string            `json:"fingerprint"`
	CreatedAt   time.Time         `json:"created_at"`
	ExpiresAt   time.Time         `json:"expires_at"`
}

const (
	streamCacheTTL   = 10 * time.Minute
	maxCacheFileSize = 64 * 1024 // 64KB bound on single cache entry
	numLockShards    = 32        // strictly bounded, stable identity lock pool
)

func cacheDir() (string, error) {
	rDir, err := runtimeDir(true)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(rDir, "cache")
	if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", fail(2, "error", "create cache dir %s: %v", dir, err)
	}

	fi, err := os.Lstat(dir)
	if err != nil {
		return "", fail(2, "error", "stat cache dir: %v", err)
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

func streamURLHash(targetURL string) string {
	sum := sha256.Sum256([]byte(targetURL))
	return hex.EncodeToString(sum[:])
}

func shardLockPath(cDir, hash string) string {
	var shardIdx uint64
	if len(hash) >= 2 {
		shardIdx, _ = strconv.ParseUint(hash[:2], 16, 8)
	}
	return filepath.Join(cDir, fmt.Sprintf("shard_%02x.lock", shardIdx%numLockShards))
}

func cacheEntryPaths(targetURL string) (dir string, jsonPath string, lockPath string, err error) {
	dir, err = cacheDir()
	if err != nil {
		return "", "", "", err
	}
	h := streamURLHash(targetURL)
	return dir, filepath.Join(dir, h+".json"), shardLockPath(dir, h), nil
}

func computeFingerprint(directURL string, createdAt time.Time) string {
	data := fmt.Sprintf("%s|%d", directURL, createdAt.UnixNano())
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// withEntryLock executes fn under an exclusive non-blocking lock for targetURL.
// The lock files have permanent identity and bounded count (32 shards) and are NEVER unlinked,
// completely eliminating flock unlink/inode-reuse races and unbounded accumulation.
func withEntryLock(lockPath string, fn func() error) (executed bool, err error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return false, nil
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return false, nil // lock contention: skip optional maintenance
	}
	defer func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}()

	return true, fn()
}

func getStreamCache(targetURL string) (*StreamCacheEntry, bool) {
	_, jsonPath, _, err := cacheEntryPaths(targetURL)
	if err != nil {
		return nil, false
	}

	fi, err := os.Lstat(jsonPath)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm() != 0600 || fi.Size() > maxCacheFileSize {
		return nil, false
	}
	st, _ := fi.Sys().(*syscall.Stat_t)
	if st == nil || st.Uid != uint32(os.Getuid()) {
		return nil, false
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, false
	}

	var entry StreamCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}

	if time.Now().After(entry.ExpiresAt) || entry.DirectURL == "" {
		return nil, false
	}
	return &entry, true
}

func putStreamCache(targetURL, directURL string, duration float64, headers map[string]string, userAgent string) error {
	dir, jsonPath, lockPath, err := cacheEntryPaths(targetURL)
	if err != nil {
		return err
	}

	now := time.Now()
	entry := StreamCacheEntry{
		URL:         targetURL,
		DirectURL:   directURL,
		Duration:    duration,
		HTTPHeaders: headers,
		UserAgent:   userAgent,
		Fingerprint: computeFingerprint(directURL, now),
		CreatedAt:   now,
		ExpiresAt:   now.Add(streamCacheTTL),
	}

	payload, err := json.Marshal(&entry)
	if err != nil {
		return err
	}

	_, _ = withEntryLock(lockPath, func() error {
		tmpFile, err := os.CreateTemp(dir, "cache-tmp-*.json")
		if err != nil {
			return err
		}
		tmpPath := tmpFile.Name()
		_ = os.Chmod(tmpPath, 0600)

		if _, err := tmpFile.Write(payload); err != nil {
			tmpFile.Close()
			_ = os.Remove(tmpPath)
			return err
		}
		if err := tmpFile.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}

		if err := os.Rename(tmpPath, jsonPath); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
		return os.Chmod(jsonPath, 0600)
	})
	return nil
}

func deleteStreamCache(targetURL string, failedFingerprint string) {
	_, jsonPath, lockPath, err := cacheEntryPaths(targetURL)
	if err != nil {
		return
	}

	_, _ = withEntryLock(lockPath, func() error {
		data, err := os.ReadFile(jsonPath)
		if err != nil {
			return nil
		}
		var entry StreamCacheEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil
		}
		// Strict TOCTOU guard: only unlink if fingerprint exactly matches failed attempt
		if entry.Fingerprint == failedFingerprint {
			_ = os.Remove(jsonPath)
		}
		return nil
	})
}

func opportunisticCacheGC(cDir string, ttl time.Duration, deadline time.Time) {
	cf, err := os.Open(cDir)
	if err != nil {
		return
	}
	defer cf.Close()

	checked := 0
	for {
		if checked >= 30 || time.Now().After(deadline) {
			break
		}
		entries, err := cf.ReadDir(30)
		if err != nil || len(entries) == 0 {
			break
		}
		for _, ce := range entries {
			if checked >= 30 || time.Now().After(deadline) {
				break
			}
			if !strings.HasSuffix(ce.Name(), ".json") {
				continue // skip permanent lock shards and non-json entries
			}
			cPath := filepath.Join(cDir, ce.Name())
			cinfo, err := ce.Info()
			if err != nil || time.Since(cinfo.ModTime()) <= ttl {
				continue // young entries skipped without counting, preventing starvation
			}
			checked++

			baseHex := strings.TrimSuffix(ce.Name(), ".json")
			lockPath := shardLockPath(cDir, baseHex)

			_, _ = withEntryLock(lockPath, func() error {
				if fi, err := os.Lstat(cPath); err == nil && time.Since(fi.ModTime()) > ttl {
					_ = os.Remove(cPath)
				}
				return nil
			})
		}
	}
}

var (
	cacheStreamURLRe = regexp.MustCompile(`(?m)^STREAM_URL:([^\n\r]+)$`)
	cacheFormatRe    = regexp.MustCompile(`(?m)^FILE_FORMAT:([^\n\r]+)$`)
	cacheHeadersRe   = regexp.MustCompile(`(?m)^HTTP_HEADERS:([^\n\r]*)$`)
	cacheUserAgentRe = regexp.MustCompile(`(?m)^USER_AGENT:([^\n\r]*)$`)
	cacheLavfOptsRe  = regexp.MustCompile(`(?m)^LAVF_OPTS:([^\n\r]*)$`)
)

// Allowed single media container formats (strictly non-manifest, non-segmented)
var allowedSingleMediaFormats = map[string]bool{
	"mov,mp4,m4a,3gp,3g2,mj2": true,
	"matroska,webm":           true,
	"webm":                    true,
	"mp4":                     true,
	"m4a":                     true,
	"ogg":                     true,
	"flv":                     true,
}

// parseStreamURLFromOutput inspects mpv's standard output and extracts direct stream
// credentials only when all positive proof invariants are strictly satisfied.
func parseStreamURLFromOutput(outStr string) (directURL string, headers map[string]string, userAgent string, ok bool) {
	// 1. Evidence 1: Positive non-zero duration from machine tag
	mDur := durationMsgRe.FindStringSubmatch(outStr)
	if len(mDur) < 2 {
		return "", nil, "", false
	}
	dur, err := strconv.ParseFloat(mDur[1], 64)
	if err != nil || dur <= 0 || math.IsNaN(dur) || math.IsInf(dur, 0) {
		return "", nil, "", false
	}

	// 2. Evidence 2: Positive container format whitelist (strictly reject hls, applehttp, dash)
	mFmt := cacheFormatRe.FindStringSubmatch(outStr)
	if len(mFmt) < 2 {
		return "", nil, "", false
	}
	fileFmt := strings.TrimSpace(mFmt[1])
	if !allowedSingleMediaFormats[fileFmt] || strings.Contains(fileFmt, "hls") || strings.Contains(fileFmt, "dash") {
		return "", nil, "", false
	}

	// 3. Evidence 3: Stream URL protocol (must be http/https and strictly NOT edl://)
	mURL := cacheStreamURLRe.FindStringSubmatch(outStr)
	if len(mURL) < 2 {
		return "", nil, "", false
	}
	sURL := strings.TrimSpace(mURL[1])
	if !strings.HasPrefix(sURL, "http://") && !strings.HasPrefix(sURL, "https://") {
		return "", nil, "", false
	}
	if strings.HasPrefix(sURL, "edl://") || strings.Contains(sURL, ".m3u8") || strings.Contains(sURL, ".mpd") {
		return "", nil, "", false
	}

	// 4. Evidence 4: Authentication & Stream lavf options inspection (must be completely empty)
	mLavf := cacheLavfOptsRe.FindStringSubmatch(outStr)
	if len(mLavf) < 2 {
		return "", nil, "", false // missing LAVF_OPTS confirmation
	}
	lavfOpts := strings.TrimSpace(mLavf[1])
	if lavfOpts != "" {
		return "", nil, "", false // contains unmodeled stream options (cookies, proxies, etc.); refuse caching
	}

	// 5. Header extraction & unambiguous boundary verification
	mH := cacheHeadersRe.FindStringSubmatch(outStr)
	if len(mH) < 2 {
		return "", nil, "", false // missing HTTP_HEADERS confirmation
	}
	headers = map[string]string{}
	hStr := strings.TrimSpace(mH[1])
	if hStr != "" {
		// Only admit single unambiguous standard Referer (Referer: https://...) with no internal commas.
		if strings.HasPrefix(hStr, "Referer: ") && !strings.Contains(hStr[9:], ",") {
			headers["Referer"] = strings.TrimSpace(hStr[9:])
		} else {
			return "", nil, "", false // compound or ambiguous comma-separated headers: reject caching
		}
	}

	// 6. User-Agent extraction
	mUA := cacheUserAgentRe.FindStringSubmatch(outStr)
	ua := ""
	if len(mUA) >= 2 {
		ua = strings.TrimSpace(mUA[1])
	}

	return sURL, headers, ua, true
}
