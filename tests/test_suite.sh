#!/usr/bin/env bash
# tests/test_suite.sh — jue end-to-end contract and workflow suite.
#
# Zero mocks: the real binary, a real mpv over its real socket, real yt-dlp against the real
# sites. Each block runs under its own TMPDIR and XDG_STATE_HOME, so it owns its runtime dir,
# its own mpv and its own history (the user's is never touched), and the blocks run side by side;
# the report groups them by the capability plane they prove.
#
# Boundary   A   security boundary  runtime dir 0700 / symlink refusal / absent player writes
#                                   nothing / every verb's usage gate is exit 1 before any work
#            F   token budget       an envelope carries no field, escape or digit beyond the facts
# Ingest     B   ingest contract    inspect chapters, transcript windows and cap, clean text,
#                                   unavailable + inspect fallback (Bilibili & NetEase), LRC
#            K   query resolution   ytsearch1: resolves to the watch URL on every verb; other
#                                   search prefixes are exit 1
#            M   frame perception   0700/0600, audio-only and cover-art refusal, real stream frame,
#                                   stream cache, local HTTP container, outer-box scaling, zero leftovers
# Playback   C   state machine      local + network audio, 200ms stop/pause ergonomics, status,
#                                   every control, stop, kill -9 dead-socket recovery
#            D   concurrency        racing plays (cold & warm) and racing queue adds: one mpv, none lost
#            J   transient queue    add cold / warm, list, next/prev and their ends, clear, play replaces it
#            L   queue ended        one wait for a whole queue; idle and stopped are exit 4
#            O   durable history    play/queue add recorded, newest first, survives stop, --limit,
#                                   0700/0600, cap 500, corrupt lines skipped, clear, a refused
#                                   history never fails a play
# Acoustic   N   ducking            pulse/on/off: non-blocking, fades, re-arm, volume orthogonal,
#                                   floor, foreign input, zero audio-reconfig, lifecycle, old helper,
#                                   AC->Battery unplug auto-quit
#            NW  duck watchdog      the production 30 s watchdog on wall time; its own block so
#                                   its 35 s of paused waiting runs beside N, not after it
# Events     I   event contract     snapshot, edges, eof, stop, timeout, audio_device_changed
# Workflows  E   E-a multimodal chapter study (inspect -> transcript --range -> frame --at)
#                E-b live playhead grounding (play -> pause -> transcript at playhead -> rewind -> resume)
#
# Usage: bash tests/test_suite.sh        exit 0 all green, 1 a failure or a broken suite

set -uo pipefail

REPO=$(cd -P "$(dirname "$0")/.." && pwd -P) || exit 1
pyenv_shims="${PYENV_ROOT:-$HOME/.pyenv}/shims:"
export PATH="${PATH//$pyenv_shims/}"
abort() { echo "suite error: $*" >&2; exit 1; }
for b in go mpv yt-dlp jq python3 perl lsof; do command -v "$b" >/dev/null || abort "$b not on PATH"; done

base=${TMPDIR:-/tmp}
RUN=$(mktemp -d "${base%/}/jue-suite.XXXXXX") || abort "mktemp failed"
export UIDN=$(id -u)
NET_TRIES=3
BLOCK_TIMEOUT=300

# Every mpv this suite starts listens on a socket under $RUN: these PIDs are ours alone.
our_mpv() { pgrep -f "input-ipc-server=$RUN/|--vo-image-outdir=$RUN/"; }
kill_tree() {
    local c; for c in $(pgrep -P "$1"); do kill_tree "$c"; done
    kill "$1" 2>/dev/null || return 0
    sleep 0.5
    kill -0 "$1" 2>/dev/null && kill -9 "$1" 2>/dev/null
    return 0
}
cleanup() {
    local p
    p=$(jobs -p); [[ -n $p ]] && kill $p 2>/dev/null
    p=$(our_mpv); [[ -n $p ]] && kill $p 2>/dev/null
    rm -rf "$RUN"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

BIN="$RUN/jue"
(cd "$REPO" && go build -o "$BIN" ./cmd/jue) || abort "go build failed"

# silence <path> <secs>: 48 kHz stereo 16-bit silence — the shape of a real music track.
le32() { local i; for i in 0 8 16 24; do printf "\\x$(printf %02x $((($1 >> i) & 255)))"; done; }
silence() {
    local n=$((48000 * 4 * $2))
    { printf 'RIFF'; le32 $((36 + n)); printf 'WAVEfmt '; le32 16; printf '\x01\x00\x02\x00'
      le32 48000; le32 192000; printf '\x04\x00\x10\x00data'; le32 "$n"; head -c "$n" /dev/zero; } >"$1"
}
export WAV="$RUN/silence.wav"
silence "$WAV" 60

export JUE_COOKIES_FROM_BROWSER="${JUE_COOKIES_FROM_BROWSER:-none}"

export YT_NN="https://www.youtube.com/watch?v=aircAruvnKk"   # 3Blue1Brown: chapters, en-orig ASR
export YT_RR="https://www.youtube.com/watch?v=dQw4w9WgXcQ"   # human English subtitles
BILI="https://www.bilibili.com/video/BV1xx411c7mD"            # no subtitles at all
NETEASE="https://music.163.com/song?id=1824020871"            # 宇多田ヒカル, timed LRC
DARIO="https://www.youtube.com/watch?v=ugvHCXCOmm4"           # Lex Fridman #452: 5 hours, 40 chapters
NE_PURE="https://music.163.com/song?id=476592630"             # The Dawn, piano: 纯音乐
export QUERY="ytsearch1:Never Gonna Give You Up"

# No markup, entity, cue timing or LRC tag survived, and the whitespace is folded.
CLEAN='all(.segments[]; .text | test("<[^>]+>|&(amp|lt|gt|quot|apos|nbsp|#[0-9]+);|-->|[0-9]{2}:[0-9]{2}:[0-9]{2}[.,][0-9]{3}|\\[[0-9]+:[0-9]+|^ | $|  ") | not)'

# ---- harness ---------------------------------------------------------------------------
RES=/dev/null NOTE= DETAIL= HALTED=
pass() { printf 'ok|%s\n' "$1" >>"$RES"; }
failc() { local d="${2//$'\n'/\\n}"; printf 'FAIL|%s|%s\n' "$1" "$d" >>"$RES"; }

v() { OUT=$("$BIN" "$@" 2>/dev/null); RC=$?; NOTE=; }

transient() { ((RC == 2)) || { ((RC == 4)) && jq -e '.status=="error" and (.error | test("upstream blocked|rate limited") | not)' <<<"$OUT" >/dev/null 2>&1; }; }
net() {
    local n=1
    v "$@"
    while ((n < NET_TRIES)) && transient; do sleep $((2 * n)); n=$((n + 1)); v "$@"; done
    ((n == 1)) || NOTE="attempt $n of $NET_TRIES"
}

fetch() {
    local name=$1; shift
    net "$@"
    printf '%s\n' "$OUT" >"$TMPDIR/$name.out"; echo "$RC" >"$TMPDIR/$name.rc"; printf '%s' "$NOTE" >"$TMPDIR/$name.note"
}
load() { OUT=$(cat "$TMPDIR/$1.out"); RC=$(cat "$TMPDIR/$1.rc"); NOTE=$(cat "$TMPDIR/$1.note"); }

expect() {
    local name=$1${NOTE:+ [$NOTE]}; NOTE=
    if ((RC != $2)); then failc "$name" "exit $RC, want $2: $OUT"; return 1; fi
    if [[ -z $OUT || $OUT == *$'\n'* ]]; then failc "$name" "not one envelope line: $OUT"; return 1; fi
    if ! jq -se 'length == 1' <<<"$OUT" >/dev/null 2>&1; then failc "$name" "not one JSON value: $OUT"; return 1; fi
    if ! jq -e "$3" <<<"$OUT" >/dev/null 2>&1; then failc "$name" "$OUT"; return 1; fi
    pass "$name"
}

check() {
    local name=$1; shift
    if "$@"; then pass "$name"; DETAIL=; return 0; fi
    failc "$name" "${DETAIL:-$OUT}"; DETAIL=; return 1
}

need() { "$@" && return 0; printf 'skip|the rest of this block depends on that step\n' >>"$RES"; HALTED=1; return 1; }

poll() {
    local i; i=$(awk -v s="$1" 'BEGIN { print int(s * 20) }'); shift
    until "$@"; do ((i-- > 0)) || return 1; sleep 0.05; done
}

st() { OUT=$("$BIN" status 2>/dev/null) && jq -e "$1" <<<"$OUT" >/dev/null 2>&1; }
holds_still() {
    local a
    st '.state=="paused"' || return 1
    a=$(jq '.time_pos' <<<"$OUT")
    sleep 0.3
    st ".time_pos==$a" || { DETAIL="time_pos $a, 0.3 s later: $OUT"; return 1; }
}
mpv_of() { pgrep -f "input-ipc-server=$TMPDIR/jue-$UIDN/mpv.sock"; }
one_mpv() { DETAIL="$(mpv_of | tr '\n' ' ')"; [[ $(mpv_of | wc -l) -eq 1 ]]; }
gone() { ! ps -p "$1" >/dev/null; }
no_mpv() { test -z "$(mpv_of)"; }
empty_dir() { DETAIL="$(ls -A "$1" 2>&1)"; [[ -z $(ls -A "$1") ]]; }
no_scratch() { DETAIL="$(ls -A "$1" 2>&1)"; ! ls -A "$1" 2>/dev/null | grep -q scratch-; }
mode_is() { DETAIL="$(stat -f %Lp "$1" 2>&1) $1"; [[ $(stat -f %Lp "$1" 2>/dev/null) == "$2" ]]; }
stopped() {
    v control stop
    expect "control stop: exit 0" 0 '.action=="stop"'
    DETAIL="pid $1: $(ps -o pid=,stat= -p "$1")"
    check "mpv exited and was reaped after stop" poll 3 gone "$1"
}

connected() { lsof -a -p "$1" -U >/dev/null 2>&1; }
subscribe() {
    local f=$1; shift
    "$BIN" events "$@" >"$f" 2>/dev/null &
    SUB=$!
    if ! poll 3 connected "$SUB"; then
        failc "subscribe: jue events $* connected within 3 s" "pid $SUB: $(ps -o pid=,stat= -p "$SUB")"
        return 1
    fi
    sleep 0.1
}
collect() { wait "$SUB"; RC=$?; OUT=$(cat "$1"); NOTE=; }

block() {
    local name=$1; shift
    (
        export TMPDIR="$RUN/$name" XDG_STATE_HOME="$RUN/$name/state"
        mkdir -p "$TMPDIR"
        RES="$RUN/$name.res"
        : >"$RES"
        "$@"
        if [[ -n $HALTED ]]; then local p; p=$(mpv_of); [[ -n $p ]] && kill $p 2>/dev/null; fi
        exit 0
    )
}

# ======================================================================================
# Boundary plane
# ======================================================================================

# ---- A: security boundary and usage gates -------------------------------------------------
block_a() {
    set -f
    local rt="$TMPDIR/jue-$UIDN" want ver a re='^jue [0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'

    want=$(tr -d '[:space:]' <"$REPO/VERSION")
    ver=$("$BIN" --version)
    DETAIL=$ver
    check "--version reports the VERSION file ($want)" test "$ver" = "jue $want"
    DETAIL=$ver
    check "--version is SemVer" eval '[[ $ver =~ $re ]]'

    # No player and no runtime dir: every read or control verb answers, and writes nothing.
    v status
    expect "absent runtime dir: status is idle, exit 0" 0 '.status=="ok" and .state=="idle"'
    v queue list
    expect "absent runtime dir: queue list is empty, pos -1, exit 0" 0 '.status=="ok" and .pos==-1 and .count==0 and .items==[]'
    for a in "control pause" "control stop" "control duck" "control duck off" "queue clear" "events" \
             "events --until queue_ended --timeout 1"; do
        # shellcheck disable=SC2086
        v $a
        expect "absent runtime dir: $a is not_playing, no player running, exit 4" 4 \
            '.status=="not_playing" and .error=="no player running"'
    done
    check "absent player: nothing created a runtime dir" test ! -e "$rt"

    mkdir "$rt" && chmod 0755 "$rt"
    v play "$WAV"
    expect "0755 runtime dir: play refused, exit 2" 2 '.status=="error" and (.error|test("mode 0755, want 0700"))'
    v status
    expect "0755 runtime dir: status refused, exit 2" 2 '.status=="error" and (.error|test("want 0700"))'
    check "0755 runtime dir: nothing written into it" empty_dir "$rt"
    rmdir "$rt"

    mkdir -m 0700 "$TMPDIR/elsewhere" && ln -s "$TMPDIR/elsewhere" "$rt"
    v play "$WAV"
    expect "symlinked runtime dir: play refused, exit 2" 2 '.status=="error" and (.error|test("is a symlink"))'
    v control pause
    expect "symlinked runtime dir: control refused, exit 2" 2 '.status=="error" and (.error|test("is a symlink"))'
    check "symlinked runtime dir: no socket or lock planted in its target" empty_dir "$TMPDIR/elsewhere"
    check "refused plays started no mpv" no_mpv
    rm "$rt"

    # Representative usage gate per verb: detailed flag grammar is unit-tested in cmd/jue;
    # this proves the binary maps Fail{Code:1} to exit 1 + JSON envelope without side effects.
    local -a usage=(
        "inspect"
        "transcript $YT_NN --range 30-10"
        "frame $YT_RR"
        "play --video"
        "queue shuffle"
        "status now"
        "control rewind"
        "control duck sideways"
        "events --until invalid_event"
        "history --limit 0"
    )
    for a in "${usage[@]}"; do
        # shellcheck disable=SC2086
        v $a
        expect "usage: ${a/$YT_NN/<yt>} is exit 1" 1 '.status=="error"'
    done
    v inspect notaurl
    expect "usage: inspect a non-URL is exit 1" 1 '.status=="error" and (.error|test("not a valid URL"))'
    v transcript notaurl
    expect "usage: transcript a non-URL is exit 1" 1 '.status=="error" and (.error|test("not a valid URL"))'
    check "usage gates started no mpv and created no runtime dir" eval 'no_mpv && test ! -e "$rt"'
    check "usage gates and absent players created no history state" test ! -e "$XDG_STATE_HOME"
}

# ---- F: token budget ----------------------------------------------------------------------
bytes() { wc -c <"$TMPDIR/$1.out" | tr -d ' '; }

block_f() {
    fetch long inspect "$DARIO" &
    fetch window transcript "$YT_NN" --range 300-480 &
    fetch past transcript "$YT_NN" --range 999999-1000099 &
    fetch lrcpast transcript "$NETEASE" --range 9999-10099 &
    wait

    local n
    load long
    n=$(bytes long)
    DETAIL="$n bytes: $OUT"
    check "inspect a 40-chapter talk: the whole map in under 2500 bytes" \
        test "$(jq '.chapters|length' <<<"$OUT")" -eq 40 -a "$n" -lt 2500
    expect "inspect: only status, id, title, duration, uploader and {start,title} chapters" 0 \
        '(keys - ["status","id","title","duration","uploader","chapters"]) == []
         and all(.chapters[]; keys == ["start","title"])'
    export BYTES=$n
    expect "inspect: framing beyond the strings is at most 31 bytes a chapter" 0 \
        '($ENV.BYTES|tonumber) - ([.id, .title, .uploader, .chapters[].title] | map(utf8bytelength) | add)
         <= 31 * (.chapters|length) + 100'

    load window
    export BYTES=$(bytes window)
    expect "transcript, a 3-minute window: only status, lang, is_auto and {start,end,text} cues" 0 \
        '(keys - ["status","lang","is_auto","truncated","segments"]) == [] and (has("truncated")|not)
         and (.segments|length) > 0 and all(.segments[]; keys == ["end","start","text"])'
    expect "transcript: framing beyond the words is at most 46 bytes a cue" 0 \
        '($ENV.BYTES|tonumber) - ([.segments[].text|utf8bytelength] | add) <= 46 * (.segments|length) + 80'

    load past
    expect "transcript YouTube past its end: ok, no cues, not truncated, exit 0" 0 \
        '.status=="ok" and .segments==[] and (has("truncated")|not)'
    load lrcpast
    expect "transcript NetEase past its end: ok, no cues, the point cue left out, exit 0" 0 \
        '.status=="ok" and .segments==[] and (has("truncated")|not)'
}

# ======================================================================================
# Ingest plane
# ======================================================================================

# ---- B: ingest contract -------------------------------------------------------------------
block_b() {
    fetch inspect inspect "$YT_NN" &
    fetch full transcript "$YT_NN" &
    fetch wide transcript "$YT_NN" --range 0-99999 &
    fetch unsupported inspect "https://example.com/" &
    fetch human transcript "$YT_RR" --range 18-30 &
    fetch lrc transcript "$NETEASE" --range 20-30 &
    ( fetch bili transcript "$BILI"
      [[ $(cat "$TMPDIR/bili.rc") == 4 ]] && fetch bilimap inspect "$BILI" ) &
    ( fetch pure transcript "$NE_PURE"
      [[ $(cat "$TMPDIR/pure.rc") == 4 ]] && fetch puremap inspect "$NE_PURE" ) &
    wait

    load inspect
    expect "inspect YouTube: id, title, duration, uploader" 0 \
        '.status=="ok" and .id=="aircAruvnKk" and (.title|length>0) and .duration>1000 and (.uploader|length>0)'
    expect "inspect YouTube: chapters non-empty, titled, strictly ascending" 0 \
        '(.chapters|length)>=2 and all(.chapters[]; .title|length>0)
         and ([.chapters[].start] as $s | $s==($s|sort) and ($s|unique|length)==($s|length))'
    expect "inspect YouTube: chapter starts are real offsets, not 0" 0 \
        'any(.chapters[]; .start>0) and any(.chapters[]; .title=="What are neurons?" and .start==162)'

    load full
    expect "transcript without --range: capped at 300 cues, truncated:true" 0 \
        '.truncated==true and (.segments|length)==300'
    load wide
    expect "transcript with a --range wider than the talk: capped at 300 too" 0 \
        '.truncated==true and (.segments|length)==300'
    load unsupported
    expect "inspect a page with no media: Unsupported URL, exit 1" 1 \
        '.status=="error" and (.error|test("Unsupported URL"))'
    load human
    expect "transcript YouTube human track: is_auto false, lang en, clean words" 0 \
        ".is_auto==false and .lang==\"en\" and all(.segments[]; .start<30 and .end>18)
         and ([.segments[].text]|join(\" \")|test(\"strangers to love\")) and ($CLEAN)"
    load lrc
    expect "transcript NetEase: LRC lines in [20,30), no lang, is_auto false, clean text" 0 \
        ".status==\"ok\" and (has(\"lang\")|not) and .is_auto==false and (.segments|length)>0
         and all(.segments[]; .start<30 and .end>20)
         and .segments[0].text==\"初めてのルーブルは\" and .segments[0].start==20.542 and ($CLEAN)"

    load bili
    expect "transcript Bilibili without subtitles: unavailable, exit 4" 4 \
        '.status=="unavailable" and (.error|length>0)'
    load pure
    expect "transcript NetEase instrumental without lyrics: unavailable, exit 4" 4 \
        '.status=="unavailable" and (.error|length>0)'
    for src in bilimap puremap; do
        [[ -e $TMPDIR/$src.out ]] || { failc "inspect fallback ($src)" "transcript was not exit 4"; continue; }
        load "$src"
        expect "inspect fallback answers where transcript is unavailable ($src)" 0 \
            '.status=="ok" and (.title|length>0) and .duration>0 and (.chapters|type)=="array"'
    done

    local rt="$TMPDIR/jue-$UIDN"
    check "transcript left no scratch dir behind" no_scratch "$rt"
    check "transcript created its runtime dir 0700" mode_is "$rt" 700
}

# ---- K: explicit query resolution ---------------------------------------------------------
block_k() {
    local pid a
    fetch kinspect inspect "$QUERY" &
    fetch ktranscript transcript "$QUERY" --range 18-30 &
    fetch kplain inspect "$YT_RR&search_query=ytsearch5:foo" &

    "$BIN" play "$WAV" >/dev/null && "$BIN" control volume 0 >/dev/null
    pid=$(mpv_of)
    net play "$QUERY"
    if expect "play ytsearch1: a query: playing, the envelope gives the watch URL" 0 \
        '.status=="ok" and .state=="playing" and .url==$ENV.YT_RR'; then
        check "status: the query resolved to the watch URL, sounding at volume 0" \
            poll 10 st '.state=="playing" and .url==$ENV.YT_RR and .duration>200 and .volume==0'
        v history last
        expect "history last: the query as given, the watch URL it resolved to, its title" 0 \
            '.item.action=="play" and .item.input==$ENV.QUERY and .item.url==$ENV.YT_RR
             and (.item.title|test("Never Gonna Give You Up")) and .item.duration>200'
        v queue add "ytsearch1:Rick Astley Together Forever"
        expect "queue add a query while playing: queued as the ytdl:// search" 0 \
            '.state=="queued" and .url=="ytdl://ytsearch1:Rick Astley Together Forever" and .pos==1 and .count==2'
        v queue list
        expect "queue list: the sounding query titled, the queued one as given" 0 \
            '(.items[0].title|length>0) and .items[0].current==true and .items[1].url=="ytdl://ytsearch1:Rick Astley Together Forever"'
    fi
    stopped "$pid"
    wait

    load kinspect
    expect "inspect ytsearch1: the first hit, unwrapped: 11-char id, title, duration" 0 \
        '.status=="ok" and .id=="dQw4w9WgXcQ" and (.title|test("Never Gonna Give You Up"))
         and .duration>200 and (has("entries")|not) and (has("_type")|not)'
    load ktranscript
    expect "transcript ytsearch1: the hit's human track, cues in [18,30)" 0 \
        '.status=="ok" and .is_auto==false and .lang=="en" and (.segments|length)>0
         and all(.segments[]; .start<30 and .end>18) and ([.segments[].text]|join(" ")|test("strangers to love"))'
    load kplain
    expect "a watch URL whose query string says search: passes, not a search prefix" 0 \
        '.status=="ok" and .id=="dQw4w9WgXcQ"'

    for a in "inspect ytsearch5:foo" "transcript scsearch:foo" "play scsearch:foo" \
             "queue add ytdl://ytsearch5:foo" "frame ytsearch5:video --at 10"; do
        # shellcheck disable=SC2086
        v $a
        expect "$a is exit 1, unsupported search prefix" 1 '.status=="error" and (.error|test("unsupported search prefix"))'
    done
    check "refused queries started no mpv" no_mpv
}

# ---- M: visual frame perception -----------------------------------------------------------
block_m() {
    local rt="$TMPDIR/jue-$UIDN" shot cdir cjson srv srv_pid port url url2

    v frame --at 10 -- "--fake-mpv-option"
    expect "an option-shaped URL after -- reaches mpv as a file and fails, exit 2" 2 '.status=="error"'

    v frame "$WAV" --at 5
    expect "frame on WAV audio: unavailable, no video stream, exit 4" 4 \
        '.status=="unavailable" and .error=="media contains no video stream"'
    mpv --no-config --really-quiet --frames=1 -o "$TMPDIR/cover.png" "av://lavfi:testsrc=size=64x64" >/dev/null 2>&1
    mpv --no-config --really-quiet -o "$TMPDIR/cover.mp3" "av://lavfi:sine=d=3" \
        --external-file="$TMPDIR/cover.png" --vid=1 --ovc=mjpeg >/dev/null 2>&1
    v frame "$TMPDIR/cover.mp3" --at 1
    expect "frame on an MP3 with embedded cover art: unavailable, exit 4" 4 \
        '.status=="unavailable" and .error=="media contains no video stream"'

    printf 'ftypmp42\x00\x00' >"$TMPDIR/truncated.mp4"
    v frame "$TMPDIR/truncated.mp4" --at 10
    expect "truncated media without duration evidence: exit 2" 2 \
        '.status=="error" and .error=="frame capture failed: stream ended or network error"'
    check "refused and failed frames left no scratch dir" no_scratch "$rt"

    net frame "$YT_RR" --at 99999
    expect "frame past the video's end: unavailable, out of range, exit 4" 4 \
        '.status=="unavailable" and (.error|test("timestamp out of range"))'

    net frame "$YT_RR" --at 73 --width 960 --quality 80
    need expect "frame on a YouTube stream: 960-box jpg, 20-90 KB, exit 0" 0 \
        '.status=="ok" and .at==73 and .duration>0 and (has("actual_at")|not) and (.path|length>0)
         and .width<=960 and .height>0 and .size_bytes>=20000 and .size_bytes<=90000 and .format=="jpg"' || return
    shot=$(jq -r .path <<<"$OUT")
    check "frame file exists, mode 0600" mode_is "$shot" 600
    check "frame scratch dir mode 0700" mode_is "$(dirname "$shot")" 700
    check "frame scratch dir owned by this UID" test "$(stat -f %u "$(dirname "$shot")")" = "$UIDN"

    # Stream cache + local HTTP container inspect/transcript: cold frame admits cache, hot frame
    # reads cached direct URL (pointed at a 640x480 file to prove it was used).
    srv="$TMPDIR/web"
    mkdir -p "$srv"
    mpv --no-config --frames=25 -o "$srv/a.mp4" "av://lavfi:testsrc=size=320x240:duration=1" >/dev/null 2>&1
    mpv --no-config --frames=25 -o "$srv/b.mp4" "av://lavfi:testsrc=size=640x480:duration=1" >/dev/null 2>&1
    python3 -I -c '
import http.server, socketserver, sys, functools
h = functools.partial(http.server.SimpleHTTPRequestHandler, directory=sys.argv[1])
h.func.log_message = lambda *a: None
srv = socketserver.TCPServer(("127.0.0.1", 0), h)
open(sys.argv[2], "w").write(str(srv.server_address[1]))
srv.serve_forever()' "$srv" "$TMPDIR/port" &
    srv_pid=$!
    poll 5 test -s "$TMPDIR/port"
    port=$(cat "$TMPDIR/port")
    url="http://127.0.0.1:$port/a.mp4" url2="http://127.0.0.1:$port/b.mp4"
    v frame "$url" --at 0
    expect "cold frame on a single HTTP stream: ok, cache admitted" 0 '.status=="ok" and .at==0 and (.path|length>0)'
    cdir="$rt/cache"
    cjson=$(ls "$cdir"/*.json 2>/dev/null | head -n 1)
    check "stream cache dir mode 0700" mode_is "$cdir" 700
    check "stream cache entry mode 0600" mode_is "$cjson" 600
    python3 -I -c 'import json, sys; p = sys.argv[1]; d = json.load(open(p)); d["direct_url"] = sys.argv[2]; json.dump(d, open(p, "w"))' \
        "$cjson" "$url2"
    v frame "$url" --at 0 --width 0
    expect "hot frame reads the cached direct URL: 640x480" 0 '.status=="ok" and .width==640 and .height==480'
    v inspect "$url"
    expect "inspect on a local HTTP stream: duration and video track" 0 '.status=="ok" and .duration>0 and (.tracks|length)>=1 and .tracks[0].type=="video"'
    printf '1\n00:00:00,000 --> 00:00:01,000\n你好世界\n' > "$srv/a.zh.srt"
    v transcript "$url"
    expect "transcript on a local HTTP stream: sidecar SRT" 0 '.status=="ok" and .lang=="zh" and .segments[0].text=="你好世界"'
    kill "$srv_pid" 2>/dev/null; wait "$srv_pid" 2>/dev/null

    v frame "av://lavfi:testsrc=size=1080x1920:duration=1" --at 0 --width 960
    expect "portrait source in the 960 box: 540x960" 0 '.status=="ok" and .width==540 and .height==960'
    v frame "av://lavfi:testsrc=size=1280x720:duration=1" --at 0 --width 0
    expect "--width 0 keeps the source size 1280x720" 0 '.status=="ok" and .width==1280 and .height==720'

    net frame "ytsearch1:Rick Astley Never Gonna Give You Up official video" --at 10
    expect "frame on a ytsearch1: query, exit 0" 0 '.status=="ok" and (.path|length>0)'

    check "zero residual mpv in block m" poll 3 eval '! pgrep -f "vo-image-outdir=$TMPDIR|input-ipc-server=$TMPDIR" >/dev/null'
    check "zero residual yt-dlp in block m" poll 3 eval '! pgrep -u "$UIDN" -f "[y]t-dlp.*Rick Astley Never Gonna Give You Up official" >/dev/null'
}

# ======================================================================================
# Playback plane
# ======================================================================================

# ---- C: playback state machine & acoustic ergonomics --------------------------------------
block_c() {
    local rt="$TMPDIR/jue-$UIDN" pid t1

    # Immediate stop right after play reaps mpv within 200 ms.
    v play "$WAV"
    need expect "play local audio from cold: playing, exit 0" 0 '.status=="ok" and .state=="playing" and .url==$ENV.WAV' || return
    check "lazy start created the runtime dir 0700" mode_is "$rt" 700
    check "lazy start: exactly one mpv on the socket" one_mpv
    pid=$(mpv_of)
    v control stop
    expect "control stop the instant play returned: exit 0" 0 '.action=="stop"'
    DETAIL="pid $pid: $(ps -o pid=,stat= -p "$pid")"
    check "stop right after play: mpv exited and was reaped within 200 ms" poll 0.2 gone "$pid"
    v status
    expect "status right after immediate stop: idle" 0 '.state=="idle"'

    v play "$WAV"
    need expect "play local audio again: playing, exit 0" 0 '.state=="playing"' || return
    pid=$(mpv_of)
    v status
    expect "status: playing, url, duration, volume, time_pos" 0 \
        '.state=="playing" and .url==$ENV.WAV and (.duration|floor)==60 and .volume==100 and .time_pos>=0'
    check "status: playhead advances while playing" poll 5 st '.time_pos>0.5'

    v control pause
    expect "control pause: exit 0" 0 '.status=="ok" and .action=="pause"'
    sleep 0.2
    check "local audio: playhead frozen 200 ms after pause returned" holds_still
    t1=$(jq '.time_pos' <<<"$OUT")

    v control resume
    expect "control resume: exit 0" 0 '.status=="ok" and .action=="resume"'
    check "status after resume: playing and moving again" poll 3 st ".state==\"playing\" and .time_pos>$t1"

    v control seek +20
    expect "control seek +20: exit 0" 0 '.action=="seek"'
    check "seek +20 moves the playhead past 20" poll 3 st '.time_pos>=20 and .time_pos<30'
    v control seek 0:40
    expect "control seek 0:40 (absolute): exit 0" 0 '.action=="seek"'
    check "seek 0:40 puts the playhead at 40" poll 3 st '.time_pos>=40 and .time_pos<45'
    v control seek 10
    expect "control seek 10 (absolute): exit 0" 0 '.action=="seek"'
    check "seek 10 moves the playhead back to 10" poll 3 st '.time_pos>=10 and .time_pos<15'
    v control seek -5
    expect "control seek -5: exit 0" 0 '.action=="seek"'
    check "seek -5 moves the playhead back under 10" poll 3 st '.time_pos<10'

    v control volume 35
    expect "control volume 35: exit 0" 0 '.action=="volume"'
    check "status reports volume 35" st '.volume==35'

    # pause belongs to the player, not the track: a play after a pause must still sound.
    "$BIN" control pause >/dev/null
    v play "$WAV" --start 30
    expect "play again with --start 30 while paused: replaces the track, exit 0" 0 '.state=="playing" and .start==30'
    check "replaced track starts at 30 and plays, not paused" poll 3 st '.state=="playing" and .time_pos>30.2'
    DETAIL="was $pid, now $(mpv_of | tr '\n' ' ')"
    check "a warm play reuses the running mpv" test "$(mpv_of)" = "$pid"

    # Network audio, silenced first: volume carries across tracks, and pause freezes playhead within 200 ms.
    "$BIN" control volume 0 >/dev/null
    net play "$YT_NN" --start 165
    if expect "play YouTube audio at 165: exit 0" 0 '.state=="playing" and .url==$ENV.YT_NN and .start==165'; then
        check "status: network track at its start, full duration, volume kept" \
            poll 10 st '.state=="playing" and .url==$ENV.YT_NN and .time_pos>=165.5 and .duration>1000 and .volume==0'
        v control pause
        expect "control pause on network audio: exit 0" 0 '.action=="pause"'
        sleep 0.2
        check "network audio: playhead frozen 200 ms after pause returned" holds_still
    fi

    v play "$TMPDIR/missing.wav"
    expect "play a missing file: load failure reported, exit 4" 4 '.status=="error" and (.error|test("playback"))'
    v status
    expect "status after a failed load: idle" 0 '.state=="idle"'
    v control pause
    expect "control on an idle player: not_playing, player is idle, exit 4" 4 '.status=="not_playing" and .error=="player is idle"'

    stopped "$pid"
    v status
    expect "status after stop: idle" 0 '.state=="idle"'
    v control resume
    expect "control after stop: not_playing, exit 4" 4 '.status=="not_playing"'

    # A player that died hard leaves its socket behind; the next play must not trip on it.
    "$BIN" play "$WAV" >/dev/null
    pid=$(mpv_of)
    kill -9 "$pid"
    poll 3 gone "$pid"
    v status
    expect "after mpv is killed: status idle over the dead socket" 0 '.state=="idle"'
    v play "$WAV"
    expect "after mpv is killed: play starts a fresh one, exit 0" 0 '.state=="playing"'
    check "after mpv is killed: exactly one new mpv" one_mpv
    pid=$(mpv_of)
    "$BIN" control stop >/dev/null
    poll 3 gone "$pid"
}

# ---- D: concurrency -----------------------------------------------------------------------
race() {
    local i bad=
    for ((i = 1; i <= $2; i++)); do
        "$BIN" play "$WAV" --start "$i" >"$TMPDIR/$1-p$i.out" 2>&1 &
        "$BIN" status >"$TMPDIR/$1-s$i.out" 2>&1 &
    done
    wait
    for ((i = 1; i <= $2; i++)); do
        jq -e '.status=="ok" and .state=="playing"' "$TMPDIR/$1-p$i.out" >/dev/null 2>&1 \
            || bad+="play $i: $(cat "$TMPDIR/$1-p$i.out") "
        [[ $(wc -l <"$TMPDIR/$1-s$i.out") -eq 1 ]] && jq -e '.status=="ok"' "$TMPDIR/$1-s$i.out" >/dev/null 2>&1 \
            || bad+="status $i: $(cat "$TMPDIR/$1-s$i.out") "
    done
    DETAIL=$bad
    [[ -z $bad ]]
}

block_d() {
    local pid i bad
    check "6 plays racing from cold: all ok, no crash" race cold 6
    need check "cold race: flock let exactly one mpv start" one_mpv || return
    pid=$(mpv_of)
    check "cold race: the survivor is playing" st '.state=="playing"'
    check "6 plays racing on a warm player: all ok, no crash" race warm 6
    DETAIL="was $pid, now $(mpv_of | tr '\n' ' ')"
    check "warm race: still the same single mpv" test "$(mpv_of)" = "$pid"
    stopped "$pid"

    # Six queue adds racing from cold: one mpv; exactly one started it, five queued; none lost.
    for i in 1 2 3 4 5 6; do ln "$WAV" "$TMPDIR/race$i.wav"; done
    for i in 1 2 3 4 5 6; do "$BIN" queue add "$TMPDIR/race$i.wav" >"$TMPDIR/race$i.out" 2>&1 & done
    wait
    bad=
    for i in 1 2 3 4 5 6; do
        [[ $(wc -l <"$TMPDIR/race$i.out") -eq 1 ]] && jq -e '.status=="ok" and .action=="add"' "$TMPDIR/race$i.out" >/dev/null 2>&1 \
            || bad+="add $i: $(cat "$TMPDIR/race$i.out") "
    done
    DETAIL=$bad
    check "6 queue adds racing from cold: all ok, one envelope each" test -z "$bad"
    need check "cold add race: flock let exactly one mpv start" one_mpv || return
    pid=$(mpv_of)
    DETAIL="$(cat "$TMPDIR"/race*.out)"
    check "cold add race: exactly one add started playback, five queued" \
        test "$(cat "$TMPDIR"/race*.out | jq -s '[.[] | select(.state=="playing")] | length')" -eq 1 \
          -a "$(cat "$TMPDIR"/race*.out | jq -s '[.[] | select(.state=="queued")] | length')" -eq 5
    v queue list
    export RACE="$TMPDIR/race"
    expect "cold add race: the queue holds all six, each once" 0 \
        '.count==6 and ([.items[].url] | sort) == ([range(1;7)] | map($ENV.RACE + "\(.).wav"))'
    v history list --limit 500
    expect "cold add race: history recorded all six adds, none lost to a racing rewrite" 0 \
        '[.items[] | select(.action=="queue_add" and (.url|startswith($ENV.RACE))) | .url] | sort
         == ([range(1;7)] | map($ENV.RACE + "\(.).wav"))'
    stopped "$pid"
}

# ---- J: transient queue -------------------------------------------------------------------
block_j() {
    local pid i
    export WAV1="$TMPDIR/one.wav" WAV2="$TMPDIR/two.wav" WAV3="$TMPDIR/three.wav"
    for i in "$WAV1" "$WAV2" "$WAV3"; do ln "$WAV" "$i"; done

    v queue add "$WAV1"
    need expect "queue add from cold: starts at once, playing, pos 0, count 1" 0 \
        '.status=="ok" and .action=="add" and .state=="playing" and .url==$ENV.WAV1 and .pos==0 and .count==1' || return
    pid=$(mpv_of)
    v queue add "$WAV2"
    expect "queue add while playing: queued behind it, pos 1, count 2" 0 \
        '.action=="add" and .state=="queued" and .url==$ENV.WAV2 and .pos==1 and .count==2'
    v queue list
    expect "queue list: pos 0, count 2, entry 0 current, entry 1 queued" 0 \
        '.pos==0 and .count==2 and .items[0].url==$ENV.WAV1 and .items[0].current==true
         and .items[1].url==$ENV.WAV2 and .items[1].index==1'
    expect "queue list: a queued entry carries no current:false and no title" 0 \
        '(.items[1]|keys) == ["index","url"]'
    check "queue add did not touch the track that sounds" st '.state=="playing" and .url==$ENV.WAV1'

    v control next
    expect "control next: exit 0" 0 '.status=="ok" and .action=="next"'
    check "control next: track two plays" poll 3 st '.state=="playing" and .url==$ENV.WAV2 and .time_pos>=0'
    v control next
    expect "control next past the end: unavailable, end of playlist, exit 4" 4 \
        '.status=="unavailable" and .error=="end of playlist"'
    check "refused next leaves track two playing" st '.state=="playing" and .url==$ENV.WAV2'
    v control prev
    expect "control prev: exit 0" 0 '.status=="ok" and .action=="prev"'
    check "control prev: track one plays again" poll 3 st '.state=="playing" and .url==$ENV.WAV1'
    v control prev
    expect "control prev past the start: unavailable, start of playlist, exit 4" 4 \
        '.status=="unavailable" and .error=="start of playlist"'

    "$BIN" control pause >/dev/null
    check "paused before stepping on" poll 3 st '.state=="paused"'
    v queue add "$WAV3"
    expect "queue add while paused: queued, pos 2, count 3" 0 '.state=="queued" and .pos==2 and .count==3'
    v control next
    expect "control next while paused: exit 0" 0 '.action=="next"'
    check "next while paused: track two plays, unpaused, playhead moving" \
        poll 3 st '.state=="playing" and .url==$ENV.WAV2 and .time_pos>0.3'

    v queue clear
    expect "queue clear: exit 0" 0 '.status=="ok" and .action=="clear"'
    v queue list
    expect "after clear: only the sounding track is left, current" 0 \
        '.count==1 and .pos==0 and .items[0].url==$ENV.WAV2 and .items[0].current==true'
    check "after clear: track two still playing" st '.state=="playing" and .url==$ENV.WAV2'

    "$BIN" queue add "$WAV1" >/dev/null
    v play "$WAV3"
    expect "play over a queue: playing track three, exit 0" 0 '.state=="playing" and .url==$ENV.WAV3'
    v queue list
    expect "play replaced the whole queue: one entry, track three" 0 \
        '.count==1 and .pos==0 and .items[0].url==$ENV.WAV3 and .items[0].current==true'
    DETAIL="was $pid, now $(mpv_of | tr '\n' ' ')"
    check "the queue lived in the one mpv throughout" test "$(mpv_of)" = "$pid"

    stopped "$pid"
    v queue list
    expect "after stop: the queue went with the player, list is empty" 0 '.pos==-1 and .count==0 and .items==[]'
}

# ---- L: queue-ended perception ------------------------------------------------------------
block_l() {
    local s1="$TMPDIR/short1.wav" s2="$TMPDIR/short2.wav" pid
    silence "$s1" 2
    silence "$s2" 2

    v queue add "$s1"
    need expect "queue add short track one from cold: playing" 0 '.state=="playing" and .pos==0' || return
    pid=$(mpv_of)
    v queue add "$s2"
    expect "queue add short track two: queued behind it" 0 '.state=="queued" and .pos==1 and .count==2'
    v events --until queue_ended --timeout 10
    expect "events --until queue_ended: fires once both tracks play out, exit 0" 0 '. == {"event":"queue_ended"}'

    v status
    expect "after queue_ended: the player is idle" 0 '.state=="idle"'
    v queue list
    expect "after queue_ended: queue list is empty, not the played history" 0 '.pos==-1 and .count==0 and .items==[]'
    v events --until queue_ended --timeout 1
    expect "idle player: events --until queue_ended is not_playing, player is idle, exit 4" 4 \
        '.status=="not_playing" and .error=="player is idle"'
    v queue clear
    expect "idle player: queue clear is not_playing, exit 4" 4 '.status=="not_playing" and .error=="player is idle"'
    v control next
    expect "idle player: control next is not_playing, exit 4" 4 '.status=="not_playing"'

    v queue add "$WAV"
    expect "queue add on the idle player: starts afresh at pos 0, count 1" 0 '.state=="playing" and .pos==0 and .count==1'
    subscribe "$TMPDIR/qstop.ev" --until queue_ended --timeout 10
    "$BIN" control stop >/dev/null
    collect "$TMPDIR/qstop.ev"
    expect "stop mid-queue: queue_ended wait ends not_playing, player exited, exit 4" 4 \
        '.status=="not_playing" and .error=="player exited"'
    check "mpv gone after stop" poll 3 gone "$pid"
}

# ---- O: durable history -------------------------------------------------------------------
only_history() { DETAIL="$(ls -A "$1" 2>&1)"; [[ $(ls -A "$1" | tr '\n' ' ') == "history.jsonl history.lock " ]]; }

block_o() {
    local hd="$XDG_STATE_HOME/jue" hf="$XDG_STATE_HOME/jue/history.jsonl" pid
    export WAVA="$TMPDIR/a.wav" WAVB="$TMPDIR/b.wav" T0
    ln "$WAV" "$WAVA"; ln "$WAV" "$WAVB"

    v history
    expect "no history yet: bare history is list, count 0, items [], exit 0" 0 '. == {"status":"ok","count":0,"items":[]}'
    v history last
    expect "no history yet: last is unavailable, history is empty, exit 4" 4 '. == {"status":"unavailable","error":"history is empty"}'
    v history clear
    expect "no history yet: clear is ok, exit 0" 0 '. == {"status":"ok","action":"clear"}'
    check "reading or clearing no history created no state dir" test ! -e "$hd"

    T0=$(date +%s)
    v play "$WAVA"
    need expect "play local audio: playing" 0 '.state=="playing"' || return
    pid=$(mpv_of)
    "$BIN" control volume 0 >/dev/null
    v history last
    expect "history last: the play with its input, url, title, duration and time" 0 \
        '.status=="ok" and .item.action=="play" and .item.input==$ENV.WAVA and .item.url==$ENV.WAVA
         and .item.title=="a.wav" and (.item.duration|floor)==60 and .item.timestamp>=($ENV.T0|tonumber)'
    expect "history last: an item is exactly timestamp, action, input, url, title, duration" 0 \
        '(.item|keys) == ["action","duration","input","timestamp","title","url"]'
    v queue add "$WAVB"
    expect "queue add while playing: queued" 0 '.state=="queued"'
    v history last
    expect "history last: the queued add, no title or duration until it loads" 0 \
        '.item.action=="queue_add" and .item.input==$ENV.WAVB and .item.url==$ENV.WAVB and .item.title=="" and .item.duration==0'
    v history list
    expect "history list: newest first, the add then the play" 0 '.count==2 and [.items[].action]==["queue_add","play"]'
    v history list --limit 1
    expect "history list --limit 1: only the newest" 0 '.count==1 and .items[0].action=="queue_add"'
    check "history dir is 0700" mode_is "$hd" 700
    check "history file is 0600" mode_is "$hf" 600

    v play "$TMPDIR/missing.wav"
    expect "play a missing file: exit 4" 4 '.status=="error"'
    v history
    expect "a play that never sounded is not recorded" 0 '.count==2'
    stopped "$pid"
    v history
    expect "history outlives the player: both entries after stop" 0 '.count==2 and .items[1].url==$ENV.WAVA'

    printf 'not json\n{"action":\n\n' >>"$hf"
    v history
    expect "corrupt lines are skipped: still the two entries, exit 0" 0 '.count==2'

    jq -nc 'range(1;501) | {timestamp:., action:"play", input:"seed\(.)", url:"seed\(.)", title:"", duration:0}' >"$hf"
    v play "$WAVA"
    need expect "play onto a full history: playing" 0 '.state=="playing"' || return
    pid=$(mpv_of)
    v history list --limit 500
    expect "cap: 500 kept, the new play first, seed1 dropped, seed2 the oldest" 0 \
        '.count==500 and .items[0].url==$ENV.WAVA and .items[1].url=="seed500" and .items[499].url=="seed2"'
    DETAIL="$(wc -l <"$hf")"
    check "cap: the file holds 500 lines" test "$(wc -l <"$hf")" -eq 500
    check "the atomic rewrite left no temp file" only_history "$hd"

    v history clear
    expect "history clear: ok, exit 0" 0 '. == {"status":"ok","action":"clear"}'
    v history last
    expect "after clear: last is unavailable, exit 4" 4 '.status=="unavailable" and .error=="history is empty"'

    echo keep >"$TMPDIR/victim"
    ln -s "$TMPDIR/victim" "$hf"
    v history
    expect "symlinked history file: list refused, exit 2" 2 '.status=="error" and (.error|test("is a symlink"))'
    v play "$WAVB"
    expect "symlinked history file: play still plays, exit 0" 0 '.state=="playing"'
    check "symlinked history file: its target untouched" test "$(cat "$TMPDIR/victim")" = keep
    rm "$hf"
    chmod 0755 "$hd"
    v history last
    expect "0755 history dir: refused, exit 2" 2 '.status=="error" and (.error|test("mode 0755, want 0700"))'
    v queue add "$WAVA"
    expect "0755 history dir: queue add still queues, exit 0" 0 '.state=="queued"'
    check "0755 history dir: nothing written into it" test ! -e "$hf"
    chmod 0700 "$hd"
    stopped "$pid"
}

# ======================================================================================
# Acoustic plane
# ======================================================================================

# ---- N: acoustic ducking ------------------------------------------------------------------
mpv_ipc() {
    python3 -I -c '
import json, socket, sys, time
op, arg = sys.argv[2], sys.argv[3]
try:
    s = socket.socket(socket.AF_UNIX); s.settimeout(5); s.connect(sys.argv[1])
    def call(cmd):
        s.sendall((json.dumps({"command": cmd, "request_id": 1}) + "\n").encode())
        end = time.monotonic() + 5; buf = b""
        while True:
            while b"\n" in buf:
                line, buf = buf.split(b"\n", 1)
                if time.monotonic() > end: raise OSError("no reply within 5 s")
                if line:
                    m = json.loads(line.decode())
                    if m.get("request_id") == 1: return m
            left = end - time.monotonic()
            if left <= 0: raise OSError("no reply within 5 s")
            s.settimeout(left)
            chunk = s.recv(65536)
            if not chunk: raise EOFError("mpv disconnected")
            buf += chunk
    if op == "get":
        m = call(["get_property", arg])
        if m["error"] == "success": print(json.dumps(m["data"]))
    elif op == "cmd":
        print(call(json.loads(arg))["error"])
    elif op == "watch":
        s.settimeout(0.05); end = time.monotonic() + float(arg); buf = b""
        print("ready", flush=True)
        while time.monotonic() < end:
            try: buf += s.recv(65536)
            except socket.timeout: pass
        sys.stdout.write(buf.decode())
except (OSError, ValueError) as e:
    print("mpv_ipc:", e, file=sys.stderr); sys.exit(3)
' "$TMPDIR/jue-$UIDN/mpv.sock" "$@" 2>>"$TMPDIR/mpv_ipc.err"
}
now_ms() { perl -MTime::HiRes=clock_gettime,CLOCK_MONOTONIC -e 'printf "%d\n", clock_gettime(CLOCK_MONOTONIC)*1000'; }
elapsed_ms() { echo $(($(now_ms) - T0)); }
at() { local d; d=$(($1 - $(elapsed_ms))); ((d > 0)) && sleep "$(awk -v d="$d" 'BEGIN { print d / 1000 }')"; return 0; }
duck_is() { st "(.duck // null)==$1"; }
gain_is() {
    local g; g=$(mpv_ipc get volume-gain)
    DETAIL="volume-gain ${g:-(no answer: $(tail -n 1 "$TMPDIR/mpv_ipc.err" 2>/dev/null))}"
    [[ -n $g ]] && jq -e ".==$1" <<<"$g" >/dev/null
}
# samples <secs>: single-process sampler polling jue status every 40 ms without shell/jq fork churn.
samples() {
    python3 -I -c '
import json, subprocess, sys, time
end = time.monotonic() + float(sys.argv[2]); out = []
while time.monotonic() < end:
    r = subprocess.run([sys.argv[1], "status"], capture_output=True, text=True)
    if r.returncode == 0 and r.stdout:
        out.append(str(json.loads(r.stdout).get("duck", 100)))
    time.sleep(0.04)
print(" ".join(out))' "$BIN" "$1"
}
ramp_ok() {
    local dir=$1 final=$2; shift 2
    DETAIL="$dir: $*"
    jq -en --arg d "$dir" --argjson f "$final" --argjson s "[$(tr ' ' , <<<"$*")]" '
        ($s | [range(1; length) as $i | if $d == "down" then $s[$i] <= $s[$i-1] else $s[$i] >= $s[$i-1] end] | all)
        and ([$s[] | select(. > 20 and . < 100)] | length >= 3) and $s[-1] == $f' >/dev/null
}

block_n() {
    local rt="$TMPDIR/jue-$UIDN" helper="$TMPDIR/jue-$UIDN/jue_duck.lua" ms m bg
    silence "$TMPDIR/second.wav" 60

    v play "$WAV"
    need expect "play local audio for ducking: ok" 0 '.status=="ok"' || return
    DETAIL="$(ls -l "$helper" 2>&1)"
    check "N1 duck helper in the runtime dir: regular file, 0600, registers its handlers" \
        test -f "$helper" -a ! -L "$helper" -a "$(stat -f %Lp "$helper")" = 600 -a -n "$(grep register_script_message "$helper")"
    DETAIL="$(mpv_ipc get user-data/jue/duck-helper)"
    check "N2 the helper's ready marker reads \"1\"" test "$DETAIL" = '"1"'
    v status
    expect "N3 unducked status has no duck key: 2.1.0's key set" 0 'keys==["duration","state","status","time_pos","url","volume"]'

    T0=$(now_ms)
    v control duck --duration 5 --fade 2000
    ms=$(elapsed_ms)
    expect "N4 duck returns at once, one envelope" 0 '.=={"status":"ok","action":"duck"}'
    DETAIL="took ${ms} ms with --fade 2000"
    check "N4 duck does not wait for its 2 s fade (< 500 ms)" test "$ms" -lt 500
    v status
    expect "N5 right after: the fade has barely begun" 0 '.duck==null or .duck>90'

    "$BIN" control duck off --fade 0 >/dev/null
    v control duck --fade 1000
    check "N6 fade down: monotone, >=3 levels in between, ends at 20" ramp_ok down 20 $(samples 1.2)

    v control duck off --fade 0
    v status
    expect "N7 --fade 0 off: the very next status is unducked" 0 'has("duck")|not'
    v control duck --level 30 --fade 0
    v status
    expect "N7 --fade 0 duck: the very next status reads 30" 0 '.duck==30'
    "$BIN" control duck off --fade 0 >/dev/null
    "$BIN" control duck on --level 20 --fade 2000 >/dev/null
    sleep 0.2
    v control duck on --level 20 --fade 0
    v status
    expect "N7 --fade 0 overrides an in-flight attack to the same target" 0 '.duck==20'
    "$BIN" control duck off --fade 2000 >/dev/null
    sleep 0.2
    v control duck off --fade 0
    v status
    expect "N7 --fade 0 overrides an in-flight release to the same target" 0 'has("duck")|not'

    T0=$(now_ms)
    v control duck --duration 1 --level 30 --fade 100
    check "N8 pulse: down to 30 within 0.5 s" poll 0.5 duck_is 30
    at 800
    check "N8 pulse: still 30 at 0.8 s" duck_is 30
    check "N8 pulse: back up within 2 s (released at 1 s over 200 ms)" poll 1.2 duck_is null

    T0=$(now_ms)
    "$BIN" control duck --duration 1 --level 30 --fade 100 >/dev/null
    at 600
    "$BIN" control duck --duration 1 --level 30 --fade 100 >/dev/null
    at 1300
    check "N9 a second pulse re-arms: still 30 at 1.3 s" duck_is 30
    "$BIN" control duck off --fade 0 >/dev/null

    v control duck on
    expect "N10 duck on: action duck_on" 0 '.=={"status":"ok","action":"duck_on"}'
    sleep 2
    check "N10 duck on holds: 20 two seconds later" duck_is 20

    v control volume 50
    v status
    expect "N11 volume under a duck: volume 50, duck 20" 0 '.volume==50 and .duck==20'
    v control duck off --fade 0
    v status
    expect "N11 after off: volume still 50, no duck" 0 '.volume==50 and (has("duck")|not)'

    "$BIN" control duck on --fade 0 >/dev/null
    v control duck off --fade 1000
    check "N12 fade up: monotone, >=3 levels in between, ends unducked" ramp_ok up 100 $(samples 1.3)

    v control duck off
    expect "N13 off while not ducked: ok, exit 0" 0 '.=={"status":"ok","action":"duck_off"}'
    sleep 0.1
    check "N13 off while not ducked: volume-gain untouched at 0" gain_is 0

    v control duck on --level 0 --fade 0
    v status
    expect "N14 level 0: duck 0" 0 '.duck==0'
    check "N14 level 0: volume-gain at the -96 dB floor" gain_is -96
    "$BIN" control duck off --fade 0 >/dev/null
    check "N14 off: volume-gain back to 0" gain_is 0

    for m in '"duck","nan","5","0","0"' '"duck","inf","5","0","0"' '"duck","1e309","5","0","0"' \
             '"duck","20","nan","0","0"' '"duck","20","5","inf","0"' '"unduck","nan"'; do
        mpv_ipc cmd "[\"script-message-to\",\"jue_duck\",$m]" >/dev/null
    done
    sleep 0.1
    check "N15 non-finite arguments from a foreign sender: volume-gain still 0" gain_is 0
    "$BIN" control duck on --fade 0 >/dev/null
    check "N15 then duck on: 20" duck_is 20
    "$BIN" control duck off --fade 0 >/dev/null
    check "N15 then off: volume-gain exactly 0, not left at the floor" gain_is 0

    mpv_ipc watch 2.5 >"$TMPDIR/n16.ev" &
    bg=$!
    poll 1 grep -q ready "$TMPDIR/n16.ev"
    "$BIN" control duck --fade 300 >/dev/null; sleep 0.4
    "$BIN" control duck off --fade 300 >/dev/null; sleep 0.4
    "$BIN" control duck on --level 0 --fade 0 >/dev/null; sleep 0.1
    "$BIN" control duck off --fade 0 >/dev/null
    wait $bg
    DETAIL="$(grep -c '"audio-reconfig"' "$TMPDIR/n16.ev") audio-reconfig events of $(grep -c . "$TMPDIR/n16.ev") lines"
    check "N16 ducking reconfigures no audio: zero audio-reconfig" \
        eval 'grep -q ready "$TMPDIR/n16.ev" && ! grep -q "\"audio-reconfig\"" "$TMPDIR/n16.ev"'

    "$BIN" queue add "$TMPDIR/second.wav" >/dev/null
    "$BIN" control duck on >/dev/null
    v control next
    expect "N17 next to the second track: ok" 0 '.action=="next"'
    check "N17 across tracks: next keeps the duck" poll 1 duck_is 20
    v control seek 30
    expect "N18 seek 30: ok" 0 '.action=="seek"'
    check "N18 seek keeps the duck" duck_is 20
    "$BIN" control pause >/dev/null
    v control duck on
    expect "N19 duck on while paused: ok" 0 '.action=="duck_on"'
    v control resume
    expect "N19 resume: ok" 0 '.action=="resume"'
    check "N19 resume: still 20" duck_is 20
    v play "$WAV"
    expect "N20 play replacing the queue: ok" 0 '.status=="ok"'
    check "N20 play replacing the queue keeps the duck" duck_is 20
    v control seek 59
    check "N21 played out: the player goes idle" poll 3 st '.state=="idle"'
    v status
    expect "N21 idle status is the bare idle envelope, no duck" 0 '.=={"status":"ok","state":"idle"}'
    v control duck
    expect "N21 duck on an idle player: not_playing, exit 4" 4 '.status=="not_playing"'

    "$BIN" control stop >/dev/null
    poll 3 no_mpv
    v play "$WAV"
    v status
    expect "N22 a new player after stop starts unducked" 0 'has("duck")|not'
    check "N22 a new player after stop: volume-gain 0" gain_is 0

    "$BIN" control stop >/dev/null
    poll 3 no_mpv
    mpv --idle=yes --no-config --no-terminal --no-video --input-ipc-server="$rt/mpv.sock" "$WAV" >/dev/null 2>&1 &
    poll 3 st '.state=="playing"'
    v control duck
    expect "N23 a player without the helper (an older jue's): unavailable, exit 4" 4 \
        '.status=="unavailable" and (.error|test("not loaded"))'
    "$BIN" control stop >/dev/null
    poll 3 no_mpv

    v play "$WAV"
    mpv_ipc cmd '["set_property","user-data/jue/duck-helper","0"]' >/dev/null
    v control duck
    expect "N24 a helper of another protocol: unavailable, exit 4" 4 '.status=="unavailable" and (.error|test("protocol"))'
    check "N24 nothing sent: volume-gain still 0" gain_is 0
    mpv_ipc cmd '["set_property","user-data/jue/duck-helper","1"]' >/dev/null
    v control duck --fade 0
    expect "N24 marker back to 1: duck ok" 0 '.action=="duck"'

    "$BIN" control stop >/dev/null
    poll 3 no_mpv
    echo bait >"$TMPDIR/bait"
    rm -f "$helper" && ln -s "$TMPDIR/bait" "$helper"
    v play "$WAV"
    expect "N25 symlink planted at the helper's path: play still ok" 0 '.status=="ok"'
    DETAIL="$(cat "$TMPDIR/bait"); $(ls -l "$helper")"
    check "N25 the link is replaced, its target untouched, the helper a 0600 file" \
        test "$(cat "$TMPDIR/bait")" = bait -a -f "$helper" -a ! -L "$helper" -a "$(stat -f %Lp "$helper")" = 600

    mpv_ipc cmd '["script-message-to","jue_duck","power-sample","reset"]' >/dev/null
    mpv_ipc cmd '["script-message-to","jue_duck","power-sample","Now drawing from '\''Battery Power'\''"]' >/dev/null
    v status
    expect "N26 initial Battery Power does not stop playback" 0 '.state=="playing"'
    mpv_ipc cmd '["script-message-to","jue_duck","power-sample","Now drawing from '\''AC Power'\''"]' >/dev/null
    mpv_ipc cmd '["script-message-to","jue_duck","power-sample","Now drawing from '\''Battery Power'\''"]' >/dev/null
    check "N26 AC -> Battery transition auto-quits mpv and leaves zero residual player" poll 3 no_mpv
}

# ---- NW: the duck watchdog ----------------------------------------------------------------
block_nw() {
    v play "$WAV"
    need expect "NW0 play local audio: ok" 0 '.status=="ok"' || return
    "$BIN" control pause >/dev/null
    T0=$(now_ms)
    "$BIN" control duck on --fade 0 >/dev/null
    at 3000
    v control duck on --fade 0
    expect "NW1 paused, duck on, and on again 3 s later" 0 '.action=="duck_on"'
    at 31500
    check "NW2 at 31.5 s still 20: the re-arm cancelled the first watchdog, paused or not" duck_is 20
    check "NW3 back up by 34.5 s: the 30 s watchdog fires on wall time while paused" \
        poll "$(awk -v ms="$(elapsed_ms)" 'BEGIN { print (34500 - ms) / 1000 }')" duck_is null
    "$BIN" control stop >/dev/null
    check "NW4 zero residual player in block nw" poll 3 no_mpv
}

# ======================================================================================
# Event plane
# ======================================================================================

# ---- I: event contract --------------------------------------------------------------------
block_i() {
    local short="$TMPDIR/short.wav" first
    silence "$short" 3

    v play "$WAV" --start 10
    need expect "play local audio for event tests: ok" 0 '.status=="ok" and .state=="playing"' || return

    v events --timeout 0.3
    first=$(head -n 1 <<<"$OUT")
    DETAIL=$OUT
    check "stream mode: exit 0 at its timeout" test "$RC" -eq 0
    OUT=$first
    expect "snapshot on connect: state playing, url, duration, volume, no duck key" 0 \
        '.event=="snapshot" and .state=="playing" and .url!=null and .duration!=null and .volume!=null and (has("duck")|not)'

    v events --timeout 1
    DETAIL="$OUT"
    check "steady devices: snapshot first, zero audio_device_changed" \
        test "$(head -n 1 <<<"$OUT" | jq -r .event)" = snapshot -a "$(grep -c audio_device_changed <<<"$OUT")" -eq 0
    v events --until audio_device_changed --timeout 0.3
    expect "until audio_device_changed: a valid event, times out, exit 4" 4 '.status=="timeout"'

    subscribe "$TMPDIR/pause.ev" --until paused --timeout 5
    "$BIN" control pause >/dev/null
    collect "$TMPDIR/pause.ev"
    expect "until paused: exactly the paused edge with its playhead, no snapshot" 0 '.event=="paused" and .time_pos>9'

    subscribe "$TMPDIR/resume.ev" --until resumed --timeout 5
    "$BIN" control resume >/dev/null
    collect "$TMPDIR/resume.ev"
    expect "until resumed: the resumed edge" 0 '.event=="resumed" and .time_pos!=null'

    v play "$short"
    expect "play a 3 s track: ok" 0 '.status=="ok"'
    v events --until track_ended --timeout 6
    expect "natural end: track_ended with reason eof" 0 '.event=="track_ended" and .reason=="eof"'

    poll 3 st '.state=="idle"'
    v events --until audio_device_changed --timeout 0.3
    expect "idle player: until audio_device_changed waits and times out, exit 4" 4 '.status=="timeout"'
    v events --until chapter_changed --timeout 0.2
    expect "until with nothing coming: status timeout, exit 4" 4 '.status=="timeout"'

    v play "$WAV" --start 5
    expect "play before stop: ok" 0 '.status=="ok"'
    subscribe "$TMPDIR/stop.ev" --until track_ended --timeout 5
    "$BIN" control stop >/dev/null
    collect "$TMPDIR/stop.ev"
    expect "stop: track_ended with reason stopped" 0 '.event=="track_ended" and .reason=="stopped"'
    check "zero residual player in block i" poll 3 no_mpv
}

# ======================================================================================
# Agent workflows: multi-verb perception and actuation loops
# ======================================================================================

# ---- E-a: multimodal chapter study (inspect -> transcript --range -> frame --at) -----------
block_ea() {
    local window shot
    net inspect "$YT_NN"
    need expect "workflow a: inspect gives the chapter map" 0 '.status=="ok" and (.chapters|length)>=2' || return
    window=$(jq -r '.chapters as $c | ($c | map(.title) | index("What are neurons?")) as $i
        | "\($c[$i].start)-\($c[$i + 1].start)"' <<<"$OUT")
    DETAIL="window=$window"
    need check "workflow a: chapter located by title gives the window 162-215" test "$window" = "162-215" || return
    net transcript "$YT_NN" --range "$window"
    expect "workflow a: chapter transcript is ASR of the original audio, clean text" 0 \
        ".status==\"ok\" and .is_auto==true and .lang==\"en-orig\" and (.segments|length)>0 and ($CLEAN)"
    expect "workflow a: every cue intersects [162,215), in time order" 0 \
        'all(.segments[]; .start<215 and .end>162 and .end>=.start)
         and ([.segments[].start] as $s | $s==($s|sort))'
    expect "workflow a: the cue straddling the chapter start is kept" 0 'any(.segments[]; .start<162 and .end>162)'
    expect "workflow a: verbatim quote found, timestamped inside the chapter" 0 \
        '([.segments[].text]|join(" ")|test("neural networks are inspired by the brain"))
         and any(.segments[]; (.text|test("inspired by the brain")) and .start>=162 and .start<215)'
    net frame "$YT_NN" --at 165 --width 960
    need expect "workflow a: visual frame captured inside the chapter at 165 s" 0 \
        '.status=="ok" and .at==165 and .width<=960 and (.path|length>0)' || return
    shot=$(jq -r .path <<<"$OUT")
    check "workflow a: chapter frame file exists on disk" test -s "$shot"
}

# ---- E-b: live playback & playhead grounding (play -> pause -> transcript -> rewind) -------
block_eb() {
    local pid head
    "$BIN" play "$WAV" >/dev/null && "$BIN" control volume 0 >/dev/null
    pid=$(mpv_of)
    net play "$NETEASE" --start 22
    need expect "workflow b: play a NetEase song at 22 s, exit 0" 0 '.state=="playing" and .start==22' || return
    need check "workflow b: status shows the song under way past 22 s" \
        poll 10 st '.state=="playing" and (.url|test("music.163.com")) and .time_pos>=22.5 and .duration>0' || return

    v control pause
    need check "workflow b: paused, playhead held" poll 2 holds_still || return
    export POS=$(jq '.time_pos' <<<"$OUT")
    head=$POS
    net transcript "$NETEASE" --range "$(jq -n "$POS | floor - 3")-$(jq -n "$POS | floor + 3")"
    expect "workflow b: the timed lyric line sounding at the playhead" 0 \
        '.status=="ok" and any(.segments[]; .start<=($ENV.POS|tonumber) and .end>($ENV.POS|tonumber) and (.text|length>0))'

    v control seek -2
    expect "workflow b: agent rewinds 2 seconds, exit 0" 0 '.action=="seek"'
    v control resume
    expect "workflow b: agent resumes playback, exit 0" 0 '.action=="resume"'
    check "workflow b: playing again from 2 s back" poll 3 st ".state==\"playing\" and .time_pos>$head-2.5"
    stopped "$pid"
    v status
    expect "workflow b: status idle after stop" 0 '.state=="idle"'
}

# ======================================================================================
# run
# ======================================================================================
PLANES='boundary|Boundary plane
ingest|Ingest plane
playback|Playback plane
acoustic|Acoustic plane
events|Event plane
workflow|Agent workflows'

BLOCKS='a|boundary|A   security boundary and usage gates
f|boundary|F   token budget
b|ingest|B   ingest contract
k|ingest|K   query resolution
m|ingest|M   frame perception
c|playback|C   playback state machine and ergonomics
d|playback|D   concurrency
j|playback|J   transient queue
l|playback|L   queue-ended perception
o|playback|O   durable history
n|acoustic|N   acoustic ducking
nw|acoustic|NW  duck watchdog
i|events|I   event contract
ea|workflow|E-a multimodal chapter study
eb|workflow|E-b live playback and playhead grounding'

START=$SECONDS
ids=() pids=() killed=()
while IFS='|' read -r id _ _; do
    block "$id" "block_$id" &
    ids+=("$id") pids+=($!)
done <<<"$BLOCKS"

while :; do
    live=
    for i in "${!pids[@]}"; do kill -0 "${pids[i]}" 2>/dev/null && live+=" $i"; done
    [[ -z $live ]] && break
    if ((SECONDS - START >= BLOCK_TIMEOUT)); then
        for i in $live; do
            printf 'FAIL|block did not finish within %ss|a hung jue, mpv or socket; its processes were killed\n' \
                "$BLOCK_TIMEOUT" >>"$RUN/${ids[i]}.res"
            kill_tree "${pids[i]}"
            killed[i]=1
            p=$(pgrep -f "input-ipc-server=$RUN/${ids[i]}/|--vo-image-outdir=$RUN/${ids[i]}/"); [[ -n $p ]] && kill $p 2>/dev/null
        done
        break
    fi
    sleep 0.5
done
for i in "${!pids[@]}"; do
    wait "${pids[i]}"; rc=$?
    ((rc == 0)) || [[ -n ${killed[i]:-} ]] ||
        printf 'FAIL|block crashed (exit code %s)|subshell exited non-zero\n' "$rc" >>"$RUN/${ids[i]}.res"
done
wait

PASS=0 FAIL=0
while IFS='|' read -r plane ptitle; do
    printf '\n## %s\n' "$ptitle"
    while IFS='|' read -r id p title; do
        [[ $p == "$plane" ]] || continue
        echo "=== $title ==="
        [[ -s "$RUN/$id.res" ]] || { printf '  \033[31mFAIL\033[0m (block reported nothing)\n'; FAIL=$((FAIL + 1)); continue; }
        while IFS='|' read -r verdict name detail; do
            case $verdict in
            ok) printf '  \033[32mok\033[0m   %s\n' "$name"; PASS=$((PASS + 1)) ;;
            skip) printf '  \033[33m--\033[0m   %s\n' "$name" ;;
            *) printf '  \033[31mFAIL\033[0m %s\n         %s\n' "$name" "$detail"; FAIL=$((FAIL + 1)) ;;
            esac
        done <"$RUN/$id.res"
    done <<<"$BLOCKS"
done <<<"$PLANES"

printf '\n=== cleanup ===\n'
left=$(our_mpv | tr '\n' ' ')
if [[ -z $left ]]; then
    printf '  \033[32mok\033[0m   no mpv left running on any suite socket\n'; PASS=$((PASS + 1))
else
    printf '  \033[31mFAIL\033[0m mpv still running after every block stopped: %s\n' "$left"; FAIL=$((FAIL + 1))
fi
cleanup
trap - EXIT
[[ -e $RUN || -n $(our_mpv) ]] && abort "cleanup left $RUN or an mpv behind"
echo "  scratch dir and players removed"

echo "========================================"
echo "Results: $PASS passed, $FAIL failed ($((SECONDS - START))s)"
echo "========================================"
((FAIL == 0))
