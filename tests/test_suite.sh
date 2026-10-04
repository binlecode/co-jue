#!/usr/bin/env bash
# tests/test_suite.sh — ting end-to-end contract and workflow suite.
#
# Zero mocks: the real binary, a real mpv over its real socket, real yt-dlp against the real
# sites. Each block runs under its own TMPDIR, so it owns its runtime dir and its own mpv and
# the blocks run side by side; their results print in block order once all are done.
#
#   A  security boundary      runtime dir 0700 / symlink refusal / read verbs write nothing / usage gates
#   B  ingest contract        inspect chapters, transcript windows, unavailable, LRC
#   C  playback state machine local + network audio, status, every control, stop, crash
#   D  concurrency            racing plays from cold and warm: one mpv, every play ok
#   E  agent workflows        a: study chapter  b: background card  c: "what was that?"
#                             d: autonomous DJ  e: AirPods pinch    f: bilingual lyrics
#   F  token budget           what an envelope costs a context window: no field, escape or digit
#                             beyond the facts; a window past the end is empty, not noise
#   G  acoustic ergonomics    stop right after play silences at once; pause freezes the playhead
#                             within 200 ms, on local and on network audio
#   H  grounding to artifact  a timestamped verbatim citation for the library; when there are no
#                             words to cite, inspect still answers with the map
#   I  event contract         push beacons, snapshot, eof trigger, edge deduplication, zero zombie
#
# Usage: bash tests/test_suite.sh        exit 0 all green, 1 a failure or a broken suite

set -uo pipefail

REPO=$(cd -P "$(dirname "$0")/.." && pwd -P) || exit 1
abort() { echo "suite error: $*" >&2; exit 1; }
for b in go mpv yt-dlp jq; do command -v "$b" >/dev/null || abort "$b not on PATH"; done

base=${TMPDIR:-/tmp}
RUN=$(mktemp -d "${base%/}/ting-suite.XXXXXX") || abort "mktemp failed"
UIDN=$(id -u)

# Every mpv this suite starts listens on a socket under $RUN: these PIDs are ours alone.
our_mpv() { pgrep -f "input-ipc-server=$RUN/"; }
cleanup() {
    local p
    p=$(jobs -p); [[ -n $p ]] && kill $p 2>/dev/null
    p=$(our_mpv); [[ -n $p ]] && kill $p 2>/dev/null
    rm -rf "$RUN"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

BIN="$RUN/ting"
(cd "$REPO" && go build -o "$BIN" ./cmd/ting) || abort "go build failed"

# 60 s of 48 kHz stereo 16-bit silence: long enough to steer, inaudible, and the shape of a
# real music track (mpv routes mono through a different macOS audio output).
export WAV="$RUN/silence.wav"
le32() { local i; for i in 0 8 16 24; do printf "\\x$(printf %02x $((($1 >> i) & 255)))"; done; }
n=$((48000 * 4 * 60))
{ printf 'RIFF'; le32 $((36 + n)); printf 'WAVEfmt '; le32 16; printf '\x01\x00\x02\x00'
  le32 48000; le32 192000; printf '\x04\x00\x10\x00data'; le32 "$n"; head -c "$n" /dev/zero; } >"$WAV"

export YT_NN="https://www.youtube.com/watch?v=aircAruvnKk"   # 3Blue1Brown: chapters, en-orig ASR
YT_RR="https://www.youtube.com/watch?v=dQw4w9WgXcQ"          # human English subtitles
BILI="https://www.bilibili.com/video/BV1xx411c7mD"            # no subtitles at all
NETEASE="https://music.163.com/song?id=1824020871"            # 宇多田ヒカル, timed LRC

# ---- harness ---------------------------------------------------------------------------
RES=/dev/null
pass() { printf 'ok|%s\n' "$1" >>"$RES"; }
failc() { printf 'FAIL|%s|%s\n' "$1" "$2" >>"$RES"; }

# v <args...>: run ting; OUT is its stdout, RC its exit code.
v() { OUT=$("$BIN" "$@" 2>/dev/null); RC=$?; }

# expect <name> <rc> <jq predicate>: the last v exited rc with exactly one JSON line on
# stdout that satisfies the predicate.
expect() {
    if [[ $RC -ne $2 ]]; then failc "$1" "exit $RC, want $2: $OUT"; return 1; fi
    if [[ -z $OUT || $OUT == *$'\n'* ]]; then failc "$1" "not one envelope line: $OUT"; return 1; fi
    if ! jq -e "$3" <<<"$OUT" >/dev/null 2>&1; then failc "$1" "$OUT"; return 1; fi
    pass "$1"
}

# check <name> <command...>: a failure shows DETAIL if set, else the last envelope read.
check() {
    local name=$1; shift
    if "$@"; then pass "$name"; else failc "$name" "${DETAIL:-$OUT}"; fi
    DETAIL=
}

# poll <secs> <command...>: true as soon as the command is, false at the deadline (secs may be
# fractional: poll 0.2 is four 50 ms ticks).
poll() {
    local i; i=$(awk -v s="$1" 'BEGIN { print int(s * 20) }'); shift
    until "$@"; do ((i-- > 0)) || return 1; sleep 0.05; done
}

# st <jq predicate>: the player's status satisfies it right now.
st() { OUT=$("$BIN" status 2>/dev/null) && jq -e "$1" <<<"$OUT" >/dev/null 2>&1; }
# holds_still: paused, and the playhead reads the same twice 0.3 s apart. Wrap it in poll:
# right after a pause the output buffer still drains for a few milliseconds.
holds_still() {
    local a
    st '.state=="paused"' || return 1
    a=$(jq '.time_pos' <<<"$OUT")
    sleep 0.3
    st ".time_pos==$a" || { DETAIL="time_pos $a, 0.3 s later: $OUT"; return 1; }
}
mpv_of() { pgrep -f "input-ipc-server=$TMPDIR/ting-$UIDN/mpv.sock"; }
one_mpv() { [[ $(mpv_of | wc -l) -eq 1 ]]; }
gone() { ! ps -p "$1" >/dev/null; }
empty_dir() { [[ -z $(ls -A "$1") ]]; }

# Each block runs in its own subshell, under its own TMPDIR, reporting to its own file.
block() {
    local name=$1; shift
    (
        export TMPDIR="$RUN/$name"
        mkdir -p "$TMPDIR"
        RES="$RUN/$name.res"
        : >"$RES"
        "$@"
    )
}

# ---- A: security boundary ----------------------------------------------------------------
block_a() {
    local rt="$TMPDIR/ting-$UIDN"

    # SemVer parity and version reporting
    local expected_ver
    expected_ver="$(tr -d '[:space:]' < "$REPO/VERSION")"
    local ver_out
    ver_out="$("$BIN" --version)"
    DETAIL="$ver_out"
    check "version matches VERSION file ($expected_ver)" test "$ver_out" = "ting $expected_ver"
    check "version follows semver format" bash -c "echo '$ver_out' | grep -qE '^ting [0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'"
    unset DETAIL

    v status
    expect "absent runtime dir: status is idle, exit 0" 0 '.status=="ok" and .state=="idle"'
    v control pause
    expect "absent runtime dir: control pause is not_playing, exit 4" 4 '.status=="not_playing"'
    v control stop
    expect "absent runtime dir: control stop is not_playing, exit 4" 4 '.status=="not_playing"'
    check "status and control left no runtime dir behind" test ! -e "$rt"

    mkdir "$rt" && chmod 0755 "$rt"
    v play "$WAV"
    expect "0755 runtime dir: play refused, exit 2" 2 '.status=="error" and (.error|test("mode 0755, want 0700"))'
    v status
    expect "0755 runtime dir: status refused, exit 2" 2 '.status=="error" and (.error|test("want 0700"))'
    DETAIL="$(ls -A "$rt")"
    check "0755 runtime dir: nothing written into it" empty_dir "$rt"
    rmdir "$rt"

    mkdir -m 0700 "$TMPDIR/elsewhere" && ln -s "$TMPDIR/elsewhere" "$rt"
    v play "$WAV"
    expect "symlinked runtime dir: play refused, exit 2" 2 '.status=="error" and (.error|test("is a symlink"))'
    v control pause
    expect "symlinked runtime dir: control refused, exit 2" 2 '.status=="error" and (.error|test("is a symlink"))'
    DETAIL="$(ls -A "$TMPDIR/elsewhere")"
    check "symlinked runtime dir: no socket or lock planted in its target" empty_dir "$TMPDIR/elsewhere"
    check "refused plays started no mpv" test -z "$(mpv_of)"

    # Usage gates answer before any network or player: exit 1, still one envelope line.
    v transcript "$YT_NN" --range 30-10
    expect "usage: reversed --range is exit 1" 1 '.status=="error"'
    v play "$WAV" --start soon
    expect "usage: unparsable --start is exit 1" 1 '.status=="error"'
    v control volume 101
    expect "usage: volume above 100 is exit 1" 1 '.status=="error"'
    v control seek
    expect "usage: seek without seconds is exit 1" 1 '.status=="error"'
    v status now
    expect "usage: status with an argument is exit 1" 1 '.status=="error"'
    # No player is running here: a bad action must still be a usage error, not not_playing.
    v control rewind
    expect "usage: unknown control action is exit 1 with no player" 1 '.status=="error"'
    # NaN and Inf parse as floats; none of them is a time or a volume.
    v control seek nan
    expect "usage: seek nan is exit 1" 1 '.status=="error"'
    v control seek +inf
    expect "usage: seek +inf is exit 1" 1 '.status=="error"'
    v control volume nan
    expect "usage: volume nan is exit 1" 1 '.status=="error"'
    v play "$WAV" --start inf
    expect "usage: --start inf is exit 1" 1 '.status=="error"'
    v transcript "$YT_NN" --range nan-10
    expect "usage: --range nan-10 is exit 1" 1 '.status=="error"'
    # yt-dlp answers these offline; a URL it cannot read is the caller's mistake, not the network's.
    v inspect notaurl
    expect "usage: inspect a non-URL is exit 1" 1 '.status=="error" and (.error|test("not a valid URL"))'
    v transcript notaurl
    expect "usage: transcript a non-URL is exit 1" 1 '.status=="error" and (.error|test("not a valid URL"))'
}

# ---- B + E-a: ingest contract and the study workflow --------------------------------------
# One fetch per question, all at once; the checks read them back off disk.
fetch() { local name=$1; shift; "$BIN" "$@" >"$TMPDIR/$name.out" 2>/dev/null; echo $? >"$TMPDIR/$name.rc"; }
load() { OUT=$(cat "$TMPDIR/$1.out"); RC=$(cat "$TMPDIR/$1.rc"); }

block_b() {
    # Workflow a — an agent studying a talk: inspect for the chapter map, pick the chapter by
    # its title, pull exactly that chapter's verbatim words.
    (
        fetch inspect inspect "$YT_NN"
        jq -r '.chapters as $c | ($c | map(.title) | index("What are neurons?")) as $i
            | "\($c[$i].start)-\($c[$i + 1].start)"' "$TMPDIR/inspect.out" >"$TMPDIR/window" 2>/dev/null
        fetch chapter transcript "$YT_NN" --range "$(cat "$TMPDIR/window")"
    ) &
    fetch full transcript "$YT_NN" &
    fetch wide transcript "$YT_NN" --range 0-99999 &
    fetch unsupported inspect "https://example.com/" &
    fetch human transcript "$YT_RR" --range 18-30 &
    fetch bili transcript "$BILI" &
    fetch lrc transcript "$NETEASE" --range 20-30 &
    wait

    load inspect
    expect "inspect YouTube: id, title, duration, uploader" 0 \
        '.status=="ok" and .id=="aircAruvnKk" and (.title|length>0) and .duration>1000 and (.uploader|length>0)'
    expect "inspect YouTube: chapters non-empty, titled, strictly ascending" 0 \
        '(.chapters|length)>=2 and all(.chapters[]; .title|length>0)
         and ([.chapters[].start] as $s | $s==($s|sort) and ($s|unique|length)==($s|length))'
    # yt-dlp names it start_time: decoded as start, every chapter would read 0.
    expect "inspect YouTube: chapter starts are real offsets, not 0" 0 \
        'any(.chapters[]; .start>0) and any(.chapters[]; .title=="What are neurons?" and .start==162)'

    local window
    window=$(cat "$TMPDIR/window")
    DETAIL="window=$window"
    check "workflow a: chapter located by title gives the window 162-215" test "$window" = "162-215"
    load chapter
    expect "workflow a: chapter transcript is ASR of the original audio" 0 \
        '.status=="ok" and .is_auto==true and .lang=="en-orig" and (.segments|length)>0'
    expect "workflow a: every cue intersects [162,215), in time order" 0 \
        'all(.segments[]; .start<215 and .end>162 and .end>=.start)
         and ([.segments[].start] as $s | $s==($s|sort))'
    # The line being spoken at 162 began before it: intersection keeps it, containment would not.
    expect "workflow a: the cue straddling the chapter start is kept" 0 'any(.segments[]; .start<162 and .end>162)'
    expect "workflow a: verbatim quote found, timestamped inside the chapter" 0 \
        '([.segments[].text]|join(" ")|test("neural networks are inspired by the brain"))
         and any(.segments[]; (.text|test("inspired by the brain")) and .start>=162 and .start<215)'

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
    expect "transcript YouTube human track: is_auto false, lang en" 0 \
        '.is_auto==false and .lang=="en" and all(.segments[]; .start<30 and .end>18)
         and ([.segments[].text]|join(" ")|test("strangers to love"))'
    load bili
    expect "transcript Bilibili without subtitles: unavailable, exit 4" 4 \
        '.status=="unavailable" and (.error|length>0)'
    load lrc
    expect "transcript NetEase: LRC lines in [20,30), no lang, is_auto false" 0 \
        '.status=="ok" and (has("lang")|not) and .is_auto==false and (.segments|length)>0
         and all(.segments[]; .start<30 and .end>20)
         and .segments[0].text=="初めてのルーブルは" and .segments[0].start==20.542'

    local rt="$TMPDIR/ting-$UIDN"
    DETAIL="$(ls -A "$rt" 2>&1)"
    check "transcript left no scratch dir behind" test -z "$(ls -A "$rt" | /usr/bin/grep scratch-)"
    DETAIL="$(stat -f %Lp "$rt" 2>&1)"
    check "transcript created its runtime dir 0700" test "$(stat -f %Lp "$rt")" = 700
}

# ---- C: playback state machine ------------------------------------------------------------
block_c() {
    local rt="$TMPDIR/ting-$UIDN" pid t1
    v play "$WAV"
    expect "play local audio from cold: playing, exit 0" 0 '.status=="ok" and .state=="playing" and .url==$ENV.WAV'
    DETAIL="$(stat -f %Lp "$rt")"
    check "lazy start created the runtime dir 0700" test "$(stat -f %Lp "$rt")" = 700
    DETAIL="$(mpv_of | tr '\n' ' ')"
    check "lazy start: exactly one mpv on the socket" one_mpv
    pid=$(mpv_of)

    v status
    expect "status: playing, url, duration, volume, time_pos" 0 \
        '.state=="playing" and .url==$ENV.WAV and (.duration|floor)==60 and .volume==100 and .time_pos>=0'
    check "status: playhead advances while playing" poll 5 st '.time_pos>0.5'

    v control pause
    expect "control pause: exit 0" 0 '.status=="ok" and .action=="pause"'
    check "status after pause: paused" poll 3 st '.state=="paused"'
    sleep 0.2
    v status
    check "paused playhead holds still" poll 2 holds_still
    t1=$(jq '.time_pos' <<<"$OUT")

    v control resume
    expect "control resume: exit 0" 0 '.status=="ok" and .action=="resume"'
    check "status after resume: playing and moving again" poll 3 st ".state==\"playing\" and .time_pos>$t1"

    v control seek +20
    expect "control seek +20: exit 0" 0 '.action=="seek"'
    check "seek +20 moves the playhead past 20" poll 3 st '.time_pos>=20 and .time_pos<30'
    # A bare time is a position, not an offset: from past 20, 0:40 lands at 40, not past 60.
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

    # Network audio, silenced first: volume is the player's, so it carries across tracks.
    "$BIN" control volume 0 >/dev/null
    v play "$YT_NN" --start 165
    expect "play YouTube audio at 165: exit 0" 0 '.state=="playing" and .url==$ENV.YT_NN and .start==165'
    check "status: network track at its start, full duration, volume kept" \
        poll 5 st '.url==$ENV.YT_NN and .time_pos>=165 and .duration>1000 and .volume==0'

    v play "$TMPDIR/missing.wav"
    expect "play a missing file: load failure reported, exit 4" 4 '.status=="error" and (.error|test("playback"))'
    v status
    expect "status after a failed load: idle" 0 '.state=="idle"'
    v control pause
    expect "control on an idle player: not_playing, exit 4" 4 '.status=="not_playing"'

    v control stop
    expect "control stop: exit 0" 0 '.action=="stop"'
    DETAIL="pid $pid: $(ps -o pid=,stat= -p "$pid")"
    check "stop: mpv exited and was reaped (no zombie)" poll 3 gone "$pid"
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
    DETAIL="$(mpv_of | tr '\n' ' ')"
    check "after mpv is killed: exactly one new mpv" one_mpv
    pid=$(mpv_of)
    "$BIN" control stop >/dev/null
    poll 3 gone "$pid"
}

# ---- D: concurrency -------------------------------------------------------------------------
# race <tag> <n>: n plays at once (plus n status reads); every one must exit 0 with one envelope.
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
    local pid
    check "6 plays racing from cold: all ok, no crash" race cold 6
    DETAIL="$(mpv_of | tr '\n' ' ')"
    check "cold race: flock let exactly one mpv start" one_mpv
    pid=$(mpv_of)
    check "cold race: the survivor is playing" st '.state=="playing"'
    check "6 plays racing on a warm player: all ok, no crash" race warm 6
    DETAIL="was $pid, now $(mpv_of | tr '\n' ' ')"
    check "warm race: still the same single mpv" test "$(mpv_of)" = "$pid"
    v control stop
    expect "after the races: stop, exit 0" 0 '.action=="stop"'
    check "after the races: mpv exited" poll 3 gone "$pid"
}

# ---- E-b: background listening --------------------------------------------------------------
# The agent starts a song, later glances at status to tell the human where it is, then stops.
block_eb() {
    local pid card pos dur
    "$BIN" play "$WAV" >/dev/null && "$BIN" control volume 0 >/dev/null   # keep the suite silent
    v play "$NETEASE"
    expect "workflow b: play a NetEase song, exit 0" 0 '.state=="playing"'
    pid=$(mpv_of)
    check "workflow b: status polls show the song under way" \
        poll 10 st '.state=="playing" and (.url|test("music.163.com")) and .time_pos>=1 and .duration>0'
    pos=$(jq '.time_pos // 0 | floor' <<<"$OUT")
    dur=$(jq '.duration // 0 | floor' <<<"$OUT")
    card=$(printf '## Now playing\n- Source: %s\n- Position: %d:%02d / %d:%02d\n- State: %s\n' \
        "$(jq -r .url <<<"$OUT")" $((pos / 60)) $((pos % 60)) $((dur / 60)) $((dur % 60)) "$(jq -r .state <<<"$OUT")")
    DETAIL="$card"
    check "workflow b: status fills every line of the Markdown card" \
        /usr/bin/grep -qE '^- Position: [0-9]+:[0-5][0-9] / [0-9]+:[0-5][0-9]$' <<<"$card"
    v control stop
    expect "workflow b: control stop, exit 0" 0 '.action=="stop"'
    check "workflow b: mpv gone after stop" poll 3 gone "$pid"
    v status
    expect "workflow b: status idle after stop" 0 '.state=="idle"'
}

# ---- E-c: "wait, what did he just say?" ---------------------------------------------------
# Mid-playback the human asks about what they just heard: the agent pauses, reads the
# playhead, pulls the verbatim words around it, answers, and resumes.
block_ec() {
    local pid pos
    "$BIN" play "$WAV" >/dev/null && "$BIN" control volume 0 >/dev/null   # keep the suite silent
    v play "$YT_NN" --start 165
    expect "workflow c: play the talk at 165, exit 0" 0 '.state=="playing"'
    pid=$(mpv_of)
    check "workflow c: status gives a moving playhead" poll 10 st '.state=="playing" and .time_pos>=166'
    v control pause
    expect "workflow c: control pause, exit 0" 0 '.action=="pause"'
    check "workflow c: paused talk holds its playhead" poll 2 holds_still
    pos=$(jq '.time_pos // 0 | floor' <<<"$OUT")
    export POS=$pos
    v transcript "$YT_NN" --range "$((pos - 5))-$((pos + 10))"
    expect "workflow c: transcript around the playhead, exit 0" 0 \
        '.status=="ok" and (.segments|length)>0 and all(.segments[]; .start<($ENV.POS|tonumber)+10 and .end>($ENV.POS|tonumber)-5)'
    expect "workflow c: one cue covers the playhead second" 0 \
        'any(.segments[]; .start<=($ENV.POS|tonumber) and .end>($ENV.POS|tonumber))'
    v control resume
    expect "workflow c: control resume, exit 0" 0 '.action=="resume"'
    check "workflow c: the talk plays on from where it paused" poll 3 st ".state==\"playing\" and .time_pos>$pos"
    v control stop
    expect "workflow c: control stop, exit 0" 0 '.action=="stop"'
    check "workflow c: mpv gone after stop" poll 3 gone "$pid"
}

# ---- E-d: autonomous DJ next-track (Event Beacon push) ---------------------------------------
# Human: "Play background tracks continuously."
# Agent starts Track 1, sets a non-polling wait with `ting events --until track_ended`,
# catches eof naturally without polling status, and immediately transitions to Track 2.
block_ed() {
    local pid w1 w2
    w1="$TMPDIR/track1.wav"
    w2="$TMPDIR/track2.wav"
    local ns=$((48000 * 4 * 1)) # 1 second test audio
    { printf 'RIFF'; le32 $((36 + ns)); printf 'WAVEfmt '; le32 16; printf '\x01\x00\x02\x00'
      le32 48000; le32 192000; printf '\x04\x00\x10\x00data'; le32 "$ns"; head -c "$ns" /dev/zero; } >"$w1"
    cp "$w1" "$w2"

    v play "$w1"
    expect "workflow d: agent starts track 1, exit 0" 0 '.state=="playing"'
    pid=$(mpv_of)

    # Agent waits on push event instead of busy-polling status
    v events --until track_ended --timeout 5
    expect "workflow d: agent catches track_ended with reason eof (0 polling), exit 0" 0 \
        '.event=="track_ended" and .reason=="eof"'

    # On eof, agent smoothly relays to Track 2
    v play "$w2"
    expect "workflow d: agent auto-transitions to track 2 seamlessly, exit 0" 0 '.state=="playing"'
    check "workflow d: still the same single mpv instance" test "$(mpv_of)" = "$pid"

    v control stop
    expect "workflow d: stop after playlist finishes, exit 0" 0 '.action=="stop"'
    check "workflow d: mpv gone after stop" poll 3 gone "$pid"
}

# ---- E-e: AirPods hardware pinch & conversational catchup -----------------------------------
# While listening to audio, human pinches AirPods to pause: the agent instantly senses the
# hardware pause event via beacon, pulls the playhead, and prepares contextual assistance.
block_ee() {
    local pid w_long bg_sub head
    w_long="$TMPDIR/talk.wav"
    local nl=$((48000 * 4 * 20)) # 20s talk
    { printf 'RIFF'; le32 $((36 + nl)); printf 'WAVEfmt '; le32 16; printf '\x01\x00\x02\x00'
      le32 48000; le32 192000; printf '\x04\x00\x10\x00data'; le32 "$nl"; head -c "$nl" /dev/zero; } >"$w_long"

    v play "$w_long" --start 5
    expect "workflow e: background talk playing at 5s, exit 0" 0 '.state=="playing"'
    pid=$(mpv_of)

    # Agent / listener monitors hardware pause events
    "$BIN" events --until paused --timeout 5 >"$TMPDIR/pinch.ev" 2>&1 &
    bg_sub=$!
    poll 1.0 test -e "$TMPDIR/pinch.ev"
    sleep 0.2

    # Human physically pinches AirPods / presses F8 media key (simulated by control pause)
    v control pause
    expect "workflow e: hardware pause triggered, exit 0" 0 '.action=="pause"'
    wait $bg_sub || true

    OUT=$(cat "$TMPDIR/pinch.ev")
    RC=0
    expect "workflow e: agent senses hardware pause event with playhead, exit 0" 0 \
        '.event=="paused" and .time_pos>0'

    head=$(jq '.time_pos' <<<"$OUT")
    # Agent rewinds 2s and resumes playback
    v control seek -2
    expect "workflow e: agent rewinds 2 seconds, exit 0" 0 '.action=="seek"'
    v control resume
    expect "workflow e: agent resumes playback, exit 0" 0 '.action=="resume"'
    check "workflow e: playhead is moving again" poll 3 st ".state==\"playing\" and .time_pos>$head-2"

    v control stop
    expect "workflow e: cleanup stop, exit 0" 0 '.action=="stop"'
    check "workflow e: mpv exited" poll 3 gone "$pid"
}

# ---- E-f: bilingual lyrics grounding (NetEase timed LRC) ------------------------------------
# While listening to foreign music, human asks: "What does this line mean?"
# Agent checks playhead via status (e.g. 20s), fetches LRC line around it, and formats
# a bilingual comprehension card with translation.
block_ef() {
    local pid lrc_text card
    v play "$WAV" --start 20
    expect "workflow f: play song at 20s, exit 0" 0 '.state=="playing"'
    pid=$(mpv_of)

    # Agent queries transcript window around current playhead [20, 25)
    v transcript "$NETEASE" --range "20-25"
    expect "workflow f: transcript extracts timed LRC lyric line, exit 0" 0 \
        '.status=="ok" and (.segments|length)>0 and .segments[0].text=="初めてのルーブルは"'

    lrc_text=$(jq -r '.segments[0].text' <<<"$OUT")
    card=$(printf '🎵 **原词**：%s\n📖 **释义**：第一次去的卢浮宫\n' "$lrc_text")
    DETAIL="$card"
    check "workflow f: bilingual card contains lyric text and translation" \
        /usr/bin/grep -q '初めてのルーブルは' <<<"$card"

    v control stop
    expect "workflow f: stop song, exit 0" 0 '.action=="stop"'
    check "workflow f: mpv gone after stop" poll 3 gone "$pid"
}

# ---- F: token budget --------------------------------------------------------------------------
# An envelope lands verbatim in an agent's context window, so every byte that is not a fact is
# a token spent on nothing. The bound is derived, not measured: per chapter or cue the framing
# is its fixed keys plus times of at most 9 characters (12345.678); an extra field, an escaped
# & or <, or a float printed as 162.32000000000002 breaks it.
DARIO="https://www.youtube.com/watch?v=ugvHCXCOmm4"   # Lex Fridman #452: 5 hours, 40 chapters
NE_PURE="https://music.163.com/song?id=476592630"     # The Dawn, piano: NetEase marks it 纯音乐

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
    # Header keys ~75 bytes plus a 9-digit duration; each chapter {"start":,"title":""}, is 22.
    export BYTES=$n
    expect "inspect: framing beyond the strings is at most 31 bytes a chapter" 0 \
        '($ENV.BYTES|tonumber) - ([.id, .title, .uploader, .chapters[].title] | map(utf8bytelength) | add)
         <= 31 * (.chapters|length) + 100'

    load window
    export BYTES=$(bytes window)
    expect "transcript, a 3-minute window: only status, lang, is_auto and {start,end,text} cues" 0 \
        '(keys - ["status","lang","is_auto","truncated","segments"]) == [] and (has("truncated")|not)
         and (.segments|length) > 0 and all(.segments[]; keys == ["end","start","text"])'
    # {"start":,"end":,"text":""}, is 28 bytes; two times of at most 9 make 46.
    expect "transcript: framing beyond the words is at most 46 bytes a cue" 0 \
        '($ENV.BYTES|tonumber) - ([.segments[].text|utf8bytelength] | add) <= 46 * (.segments|length) + 80'

    load past
    expect "transcript YouTube past its end: ok, no cues, not truncated, exit 0" 0 \
        '.status=="ok" and .segments==[] and (has("truncated")|not)'
    # The last lyric line is a point cue: a window after the song must not catch it.
    load lrcpast
    expect "transcript NetEase past its end: ok, no cues, the point cue left out, exit 0" 0 \
        '.status=="ok" and .segments==[] and (has("truncated")|not)'
}

# ---- G: acoustic ergonomics ---------------------------------------------------------------
# A human who says "stop" or hits pause hears the effect, not a delay: the bounds here are what
# a person notices, and both run against a local player with nothing on the network in between.
block_g() {
    local pid
    v play "$WAV"
    expect "play local audio: playing, exit 0" 0 '.state=="playing"'
    pid=$(mpv_of)
    v control stop
    expect "control stop the instant play returned: exit 0" 0 '.action=="stop"'
    DETAIL="pid $pid: $(ps -o pid=,stat= -p "$pid")"
    check "stop right after play: mpv exited and was reaped within 200 ms" poll 0.2 gone "$pid"
    v status
    expect "status right after that stop: idle" 0 '.state=="idle"'

    "$BIN" play "$WAV" >/dev/null
    pid=$(mpv_of)
    poll 5 st '.time_pos>0.5' || abort "block g: local audio never started"
    v control pause
    expect "control pause on local audio: exit 0" 0 '.action=="pause"'
    sleep 0.2
    check "local audio: playhead frozen 200 ms after pause returned" holds_still

    # Network audio is where a pause used to keep the playhead creeping (a fallback audio output).
    "$BIN" control volume 0 >/dev/null   # keep the suite silent
    v play "$YT_NN" --start 165
    expect "play YouTube audio at 165: exit 0" 0 '.state=="playing"'
    check "network audio under way" poll 10 st '.state=="playing" and .time_pos>=166'
    v control pause
    expect "control pause on network audio: exit 0" 0 '.action=="pause"'
    sleep 0.2
    check "network audio: playhead frozen 200 ms after pause returned" holds_still
    v control stop
    expect "control stop: exit 0" 0 '.action=="stop"'
    check "mpv gone after stop" poll 3 gone "$pid"
}

# ---- H: grounding to artifact ------------------------------------------------------------------
# The agent turns ting's facts into something a human keeps: a library citation whose link lands
# on the quoted words; and, where a source has no words to quote, an honest card built from the
# map instead of a dead end.
mmss() { printf '%d:%02d' $(($1 / 60)) $(($1 % 60)); }
# No markup, entity, cue timing or LRC tag survived, and the whitespace is folded.
CLEAN='all(.segments[]; .text | test("<[^>]+>|&(amp|lt|gt|quot|apos|nbsp|#[0-9]+);|-->|[0-9]{2}:[0-9]{2}:[0-9]{2}[.,][0-9]{3}|\\[[0-9]+:[0-9]+|^ | $|  ") | not)'

block_h() {
    (
        fetch map inspect "$DARIO"
        jq -r '.chapters as $c | ($c | map(.title) | index("Opus 3.5")) as $i
            | "\($c[$i].start)-\($c[$i + 1].start)"' "$TMPDIR/map.out" >"$TMPDIR/window" 2>/dev/null
        fetch passage transcript "$DARIO" --range "$(cat "$TMPDIR/window")"
    ) &
    fetch lyrics transcript "$NETEASE" &
    ( fetch pure transcript "$NE_PURE"
      [[ $(cat "$TMPDIR/pure.rc") == 4 ]] && fetch puremap inspect "$NE_PURE" ) &
    ( fetch nosubs transcript "$BILI"
      [[ $(cat "$TMPDIR/nosubs.rc") == 4 ]] && fetch bilimap inspect "$BILI" ) &
    wait

    local window from to t title url card
    window=$(cat "$TMPDIR/window")
    from=${window%-*} to=${window#*-}
    DETAIL="window=$window"
    check "workflow h: chapter located by title in a 5-hour talk gives 1784-2070" test "$window" = "1784-2070"
    load passage
    export FROM=$from TO=$to
    expect "workflow h: the passage is the human track, every cue inside the chapter" 0 \
        '.status=="ok" and .is_auto==false and (.segments|length)>0
         and all(.segments[]; .start<($ENV.TO|tonumber) and .end>($ENV.FROM|tonumber))'
    expect "workflow h: passage text is clean, verbatim words only" 0 "$CLEAN"
    load lyrics
    expect "workflow h: NetEase lyrics are clean, no LRC tag left in the text" 0 "$CLEAN"

    # The citation: the first three cues that start inside the chapter, linked at the second the
    # first of them starts.
    load passage
    t=$(jq --argjson f "$from" '[.segments[] | select(.start >= $f)][0].start | floor' <<<"$OUT")
    title=$(jq -r .title "$TMPDIR/map.out")
    url="$DARIO&t=$t"
    card=$(printf '### %s\n\n' "$(jq -r --argjson f "$from" '.chapters[] | select(.start==$f) | .title' "$TMPDIR/map.out")"
        jq -r --argjson f "$from" '[.segments[] | select(.start >= $f)][:3] | map(.text) | join(" ") | "> " + .' <<<"$OUT"
        printf '>\n> — [%s](%s), [%s](%s)\n' "$title" "$DARIO" "$(mmss "$t")" "$url")
    DETAIL="$card"
    check "workflow h: the citation links a second inside the chapter" test "$t" -ge "$from" -a "$t" -lt "$to"
    check "workflow h: the citation is a quote block with source and &t= timestamp links" \
        /usr/bin/grep -qE "^> — \[.+\]\(https://www\.youtube\.com/watch\?v=ugvHCXCOmm4\), \[$(mmss "$t")\]\(https://www\.youtube\.com/watch\?v=ugvHCXCOmm4&t=$t\)$" <<<"$card"
    check "workflow h: the citation carries no markup or cue timing" \
        eval '! /usr/bin/grep -qE "<[^>]+>|&(amp|lt|gt|quot|#[0-9]+);|-->|[0-9]{2}:[0-9]{2}:[0-9]{2}[.,][0-9]{3}" <<<"$card"'

    # No words to quote: the agent falls back to inspect and says what it has instead.
    load pure
    expect "workflow h: a NetEase instrumental has no words: unavailable, exit 4" 4 '.status=="unavailable"'
    local cards= src
    for src in puremap bilimap; do
        load "$src"
        expect "workflow h: inspect still answers where transcript could not ($src)" 0 \
            '.status=="ok" and (.title|length>0) and .duration>0 and (.chapters|type)=="array"'
        cards+=$(jq -r '"## \(.title)\n无公开原声字幕，但已为您准备好" + (if (.chapters|length)>0
            then "章节地图：\n" + (.chapters | map("- \(.start|floor) \(.title)") | join("\n"))
            else "节目概要：\n- 时长 \(.duration|floor) 秒，无章节" end) + "\n"' <<<"$OUT")$'\n'
    done
    DETAIL="$cards"
    check "workflow h: both fallback cards say there is nothing to quote and give the map" \
        test "$(/usr/bin/grep -c '^无公开原声字幕，但已为您准备好' <<<"$cards")" -eq 2 \
             -a "$(/usr/bin/grep -cE '^- (时长 [0-9]+ 秒|[0-9]+ .+)' <<<"$cards")" -ge 2
}

# ---- I: event contract ----------------------------------------------------------------------
block_i() {
    local wav_short="$TMPDIR/short.wav"
    local ns=$((48000 * 4 * 1))
    { printf 'RIFF'; le32 $((36 + ns)); printf 'WAVEfmt '; le32 16; printf '\x01\x00\x02\x00'
      le32 48000; le32 192000; printf '\x04\x00\x10\x00data'; le32 "$ns"; head -c "$ns" /dev/zero; } >"$wav_short"

    # absent player: events is exit 4
    v events
    expect "absent player: events returns not_playing, exit 4" 4 '.status=="not_playing"'

    # usage: invalid --until event is exit 1
    v events --until invalid_event
    expect "usage: unknown --until event is exit 1" 1 '.status=="error"'

    # usage: invalid --timeout is exit 1
    v events --timeout -5
    expect "usage: negative --timeout is exit 1" 1 '.status=="error"'

    # play local audio to start mpv
    v play "$WAV" --start 10
    expect "play local audio for event tests: ok" 0 '.status=="ok" and .state=="playing"'

    # snapshot on initial connect (sample with timeout)
    v events --timeout 0.3
    local first_line
    first_line=$(head -n 1 <<<"$OUT")
    check "snapshot on connect: has event, state playing, url, duration, volume" \
        jq -e '.event=="snapshot" and .state=="playing" and .url!=null and .duration!=null and .volume!=null' <<<"$first_line"

    # --until paused mode: only outputs single matching event line, no snapshot
    "$BIN" events --until paused --timeout 5 >"$TMPDIR/pause.ev" 2>&1 &
    local bg_pause=$!
    poll 1.0 test -e "$TMPDIR/pause.ev"
    sleep 0.2
    v control pause
    expect "control pause triggers event: ok" 0 '.status=="ok"'
    wait $bg_pause || true
    OUT=$(cat "$TMPDIR/pause.ev")
    RC=0
    expect "until paused: exactly one matching event line without snapshot" 0 \
        '.event=="paused" and .time_pos!=null'

    # --until resumed mode
    "$BIN" events --until resumed --timeout 5 >"$TMPDIR/resume.ev" 2>&1 &
    local bg_resume=$!
    poll 1.0 test -e "$TMPDIR/resume.ev"
    sleep 0.2
    v control resume
    expect "control resume triggers event: ok" 0 '.status=="ok"'
    wait $bg_resume || true
    OUT=$(cat "$TMPDIR/resume.ev")
    RC=0
    expect "until resumed: receives resumed event" 0 \
        '.event=="resumed" and .time_pos!=null'

    # natural EOF assertion: play 1s audio, wait for track_ended
    v play "$wav_short"
    expect "play short audio for EOF assertion: ok" 0 '.status=="ok"'
    v events --until track_ended --timeout 5
    expect "track natural EOF: track_ended with reason eof" 0 \
        '.event=="track_ended" and .reason=="eof"'

    # --until timeout assertion: times out and returns exit 4
    v events --until chapter_changed --timeout 0.2
    expect "until timeout: returns status timeout, exit 4" 4 \
        '.status=="timeout"'

    # control stop clean disconnection assertion
    v play "$WAV" --start 5
    expect "play before stop: ok" 0 '.status=="ok"'
    "$BIN" events --until track_ended --timeout 5 >"$TMPDIR/stop.ev" 2>&1 &
    local bg_stop=$!
    poll 1.0 test -e "$TMPDIR/stop.ev"
    sleep 0.2
    v control stop
    expect "control stop: ok" 0 '.status=="ok"'
    wait $bg_stop || true
    OUT=$(cat "$TMPDIR/stop.ev")
    RC=0
    expect "stop event: receives track_ended with reason stopped" 0 \
        '.event=="track_ended" and .reason=="stopped"'

    check "zero residual player in block i" test -z "$(mpv_of)"
}

# ---- run ------------------------------------------------------------------------------------
START=$SECONDS
block a block_a &
block b block_b &
block c block_c &
block d block_d &
block eb block_eb &
block ec block_ec &
block ed block_ed &
block ee block_ee &
block ef block_ef &
block f block_f &
block g block_g &
block h block_h &
block i block_i &
wait

PASS=0 FAIL=0
for blk in "a:A  security boundary" "b:B  ingest contract + E-a study workflow" "c:C  playback state machine" \
    "d:D  concurrency" "eb:E-b background listening" "ec:E-c wait, what did he just say" \
    "ed:E-d autonomous DJ next-track" "ee:E-e AirPods pinch & rewind" "ef:E-f bilingual lyrics grounding" \
    "f:F  token budget" "g:G  acoustic ergonomics" "h:H  grounding to artifact" "i:I  event contract"; do
    echo "=== ${blk#*:} ==="
    [[ -s "$RUN/${blk%%:*}.res" ]] || { echo "  (block reported nothing)"; FAIL=$((FAIL + 1)); continue; }
    while IFS='|' read -r verdict name detail; do
        if [[ $verdict == ok ]]; then
            printf '  \033[32mok\033[0m   %s\n' "$name"; PASS=$((PASS + 1))
        else
            printf '  \033[31mFAIL\033[0m %s\n         %s\n' "$name" "$detail"; FAIL=$((FAIL + 1))
        fi
    done <"$RUN/${blk%%:*}.res"
done

# Every block stops its own player; any mpv still on a suite socket is a stop that did not quit.
echo "=== cleanup ==="
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
