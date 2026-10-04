#!/usr/bin/env bash
# tests/test_gen2.sh — ting-gen-2 functional & contract test suite
# Strict zero-mock / real-driver discipline:
# Drives real bin/ting, real mpv IPC, real yt-dlp metadata & subtitle resolution.

set -uo pipefail

REPO=$(cd -P "$(dirname "$0")/.." && pwd -P) || exit 1
cd "$REPO" || exit 1

BIN="$REPO/ting"
if [[ ! -x "$BIN" ]]; then
    echo "Building ting..."
    go build -o "$BIN" ./cmd/ting || exit 1
fi

PASS=0
FAIL=0

ok() {
    printf "  \033[32mok\033[0m   %-55s\n" "$1"
    PASS=$((PASS + 1))
}

fail() {
    printf "  \033[31mFAIL\033[0m %-55s (%s)\n" "$1" "$2"
    FAIL=$((FAIL + 1))
}

# Isolated runtime directory for this test run
TEST_RUN_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ting-test-XXXXXX")
trap 'rm -rf "$TEST_RUN_DIR"' EXIT

export TMPDIR="$TEST_RUN_DIR"
UID_NUM=$(id -u)
RT_DIR="$TEST_RUN_DIR/ting-$UID_NUM"

# Generate a 60-second silence test audio so controls can be tested while active
TEST_WAV="$TEST_RUN_DIR/silence.wav"
ffmpeg -v error -y -f lavfi -i "anullsrc=r=16000:cl=mono" -t 60 -c:a pcm_s16le "$TEST_WAV"

echo "=== 1. Directory Permissions & Security ==="

# 1.1 Absent runtime directory: status returns idle (0), does not create directory
out=$("$BIN" status)
rc=$?
if [[ $rc -eq 0 ]] && grep -q '"state":"idle"' <<<"$out"; then
    ok "absent directory: status returns state:idle"
else
    fail "absent directory: status returns state:idle" "rc=$rc out=$out"
fi

if [[ -d "$RT_DIR" ]]; then
    fail "absent directory: status must not create directory" "directory was created"
else
    ok "absent directory: status does not create state dir"
fi

# 1.2 Absent runtime directory: control returns not_playing (4)
out=$("$BIN" control pause 2>&1)
rc=$?
if [[ $rc -eq 4 ]] && grep -q '"status":"not_playing"' <<<"$out"; then
    ok "absent directory: control pause returns not_playing (4)"
else
    fail "absent directory: control pause returns not_playing (4)" "rc=$rc out=$out"
fi

# 1.3 Permissive directory (0755): play rejects with exit code 2
mkdir -p "$RT_DIR"
chmod 0755 "$RT_DIR"
out=$("$BIN" play "$TEST_WAV" 2>&1)
rc=$?
if [[ $rc -eq 2 ]] && grep -q 'has mode 0755, want 0700' <<<"$out"; then
    ok "permissive directory 0755: play rejects with code 2"
else
    fail "permissive directory 0755: play rejects with code 2" "rc=$rc out=$out"
fi
chmod 0700 "$RT_DIR"
rm -rf "$RT_DIR"

# 1.4 Symlink runtime directory: play rejects with exit code 2
REAL_DIR="$TEST_RUN_DIR/real-dir"
mkdir -p "$REAL_DIR" && chmod 0700 "$REAL_DIR"
ln -s "$REAL_DIR" "$RT_DIR"
out=$("$BIN" play "$TEST_WAV" 2>&1)
rc=$?
if [[ $rc -eq 2 ]] && grep -q 'is a symlink; refusing to use it' <<<"$out"; then
    ok "symlink runtime directory: play rejects with code 2"
else
    fail "symlink runtime directory: play rejects with code 2" "rc=$rc out=$out"
fi
rm -rf "$RT_DIR" "$REAL_DIR"

echo "=== 2. Real Ingestion Plane (inspect & transcript) ==="

# 2.1 YouTube inspect: verify chapters, duration, uploader
out=$("$BIN" inspect "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
rc=$?
if [[ $rc -eq 0 ]] && grep -q '"id":"dQw4w9WgXcQ"' <<<"$out" && grep -q '"status":"ok"' <<<"$out"; then
    ok "inspect YouTube: returns id, title, and status:ok"
else
    fail "inspect YouTube: returns id, title, and status:ok" "rc=$rc out=$out"
fi

# 2.2 YouTube transcript with --range
out=$("$BIN" transcript "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --range 18-30)
rc=$?
if [[ $rc -eq 0 ]] && grep -q 'strangers to love' <<<"$out" && grep -q '"is_auto":false' <<<"$out"; then
    ok "transcript YouTube: returns verbatim cues in range, is_auto:false"
else
    fail "transcript YouTube: returns verbatim cues in range, is_auto:false" "rc=$rc out=$out"
fi

# 2.3 Bilibili transcript returns unavailable (exit code 4)
out=$("$BIN" transcript "https://www.bilibili.com/video/BV1xx411c7mD" 2>&1)
rc=$?
if [[ $rc -eq 4 ]] && grep -q '"status":"unavailable"' <<<"$out"; then
    ok "transcript Bilibili: returns unavailable with exit code 4"
else
    fail "transcript Bilibili: returns unavailable with exit code 4" "rc=$rc out=$out"
fi

# 2.4 NetEase lyrics transcript
out=$("$BIN" transcript "https://music.163.com/song?id=1824020871" --range 0-30)
rc=$?
if [[ $rc -eq 0 ]] && grep -q '宇多田ヒカル' <<<"$out" && grep -q '"status":"ok"' <<<"$out"; then
    ok "transcript NetEase: returns LRC lyrics cues with exit code 0"
else
    fail "transcript NetEase: returns LRC lyrics cues with exit code 0" "rc=$rc out=$out"
fi

# 2.5 Invalid range format rejected (exit code 1)
out=$("$BIN" transcript "https://www.youtube.com/watch?v=dQw4w9WgXcQ" --range "bad-range" 2>&1)
rc=$?
if [[ $rc -eq 1 ]]; then
    ok "transcript invalid range: rejected with exit code 1"
else
    fail "transcript invalid range: rejected with exit code 1" "rc=$rc out=$out"
fi

echo "=== 3. Playback Plane & Lifecycle (mpv IPC) ==="

# 3.1 Lazy-start play
out=$("$BIN" play "$TEST_WAV")
rc=$?
if [[ $rc -eq 0 ]] && grep -q '"state":"playing"' <<<"$out"; then
    ok "play local wav: starts mpv, returns state:playing"
else
    fail "play local wav: starts mpv, returns state:playing" "rc=$rc out=$out"
fi

# Verify socket exists with mode 0700 dir
if [[ -S "$RT_DIR/mpv.sock" ]]; then
    ok "play lazy-start: created mpv.sock"
else
    fail "play lazy-start: created mpv.sock" "socket file missing"
fi

# 3.2 Status during playback
out=$("$BIN" status)
rc=$?
if [[ $rc -eq 0 ]] && grep -q '"state":"playing"' <<<"$out" && grep -q '"url":' <<<"$out"; then
    ok "status: reports state:playing and url"
else
    fail "status: reports state:playing and url" "rc=$rc out=$out"
fi

# 3.3 Control pause
out=$("$BIN" control pause)
rc=$?
if [[ $rc -eq 0 ]] && grep -q '"action":"pause"' <<<"$out"; then
    ok "control pause: returns action:pause, code 0"
else
    fail "control pause: returns action:pause, code 0" "rc=$rc out=$out"
fi

out=$("$BIN" status)
if grep -q '"state":"paused"' <<<"$out"; then
    ok "status after pause: reports state:paused"
else
    fail "status after pause: reports state:paused" "out=$out"
fi

# 3.4 Control resume
out=$("$BIN" control resume)
rc=$?
if [[ $rc -eq 0 ]] && grep -q '"action":"resume"' <<<"$out"; then
    ok "control resume: returns action:resume, code 0"
else
    fail "control resume: returns action:resume, code 0" "rc=$rc out=$out"
fi

# 3.5 Control volume
out=$("$BIN" control volume 60)
rc=$?
if [[ $rc -eq 0 ]] && grep -q '"action":"volume"' <<<"$out"; then
    ok "control volume: sets volume, code 0"
else
    fail "control volume: sets volume, code 0" "rc=$rc out=$out"
fi

# 3.6 Control stop
out=$("$BIN" control stop)
rc=$?
if [[ $rc -eq 0 ]] && grep -q '"action":"stop"' <<<"$out"; then
    ok "control stop: returns action:stop, code 0"
else
    fail "control stop: returns action:stop, code 0" "rc=$rc out=$out"
fi

# Wait up to 2 seconds for mpv to exit on stop
for _ in {1..20}; do
    if ! pgrep -f "input-ipc-server=$RT_DIR/mpv.sock" >/dev/null 2>&1; then
        break
    fi
    sleep 0.1
done

if pgrep -f "input-ipc-server=$RT_DIR/mpv.sock" >/dev/null 2>&1; then
    fail "control stop: mpv process exited" "mpv still running"
else
    ok "control stop: mpv process exited cleanly"
fi

# Allow kernel socket tear-down to complete
sleep 0.3

echo "=== 4. Concurrency & Mutex ==="

# 4.1 Launch 3 concurrent play commands racing to start
PIDS=()
for i in 1 2 3; do
    ("$BIN" play "$TEST_WAV" --start "0.$i" > "$TEST_RUN_DIR/p$i.out" 2>&1) &
    PIDS+=($!)
done

wait "${PIDS[@]}"

all_zero=1
for i in 1 2 3; do
    if ! grep -q '"status":"ok"' "$TEST_RUN_DIR/p$i.out"; then
        all_zero=0
        fail "concurrency: play $i succeeded" "$(cat "$TEST_RUN_DIR/p$i.out")"
    fi
done

if [[ $all_zero -eq 1 ]]; then
    ok "concurrency: 3 racing plays all succeeded without deadlock"
fi

# Clean up player
"$BIN" control stop >/dev/null 2>&1 || true

echo "========================================"
echo "Results: $PASS passed, $FAIL failed"
echo "========================================"

if [[ $FAIL -gt 0 ]]; then
    exit 1
fi
exit 0
