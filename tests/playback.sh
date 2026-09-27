#!/usr/bin/env bash
# Real detached playback: the one surface whose bugs are PROCESSES, not output. Everything
# here starts real mpv players — silent (--volume 0), in a state dir of their own, and the run
# does not pass until `pgrep` comes back empty: a leaked mpv is the failure this file exists
# to catch.
#
# No gate. It used to sit behind YT_TEST_LIFECYCLE=1 because starting players meant starting
# them ON TOP OF the user's — --stop --all reached whatever they were listening to. The state
# dir below removes that, and what is left is a run that needs the network — a reason to run it
# when the player changed, not a reason for an env var to guard it.
#
# It also owns two claims contract.sh's offline half can never reach, both for the same
# reason — they need players that are really running: that a mutation with no `--id` and two
# players answers `status:"ambiguous"` (4 alone does not separate it from an idle call), and
# ARCH-cli-contract.md「调用面」's `ting-playlist --show … -j | ting-play -d --queue -`, run
# verbatim — contract.sh proves that envelope reaches the gate, this is where it launches.
#
# Cost: no figure is kept here — a total quoted in a header goes stale the first time a check
# is added, and the run that measures it is the one that should say it. What is stable is
# WHERE the time goes: live engine resolves (ting-play's run_mpv records the measured median
# between tracks) and the listening-log section playing a 19-second track out to its own end
# rather than seeking there — that section cannot seek, because `duration` is null on a live
# stream and a check must not go green or red on whether the track was streaming that
# afternoon. Neither is reducible without a stand-in, and this
# file has none. What IS reducible is waiting on one player while another could be starting,
# so the single-handle players launch back to back and are polled by --id afterwards.
#
# EVERY WAIT HERE IS A BOUNDED POLL, on a 0.25s tick. There is exactly one fixed sleep left and
# it is not a wait — it is SETUP, and it says so where it sits. A fixed guess costs twice: it is
# too long on a fast machine and red on a merely slow one, and a red that is not a bug still
# costs somebody an investigation.
#
# And it carries the one claim that needs a SECOND source: that the player applies the
# http_headers an engine hands it. See the Bilibili section for why only that site can show it.
#
# It also owns the LIVE READ (--status off the mpv socket), for the same reason: the peer is
# real mpv or it is nothing. The suite keeps no stand-in for a component, so a claim about
# talking to mpv can only be made where mpv is running — which is here, and only here. That
# now includes `media` — what is actually decoding — which exists in no record and in no
# engine envelope at all, so this file is the only place it can be proved to exist.
#
# Portability: bash 3.2. Needs jq for the envelopes; no tmux and no terminal — every
# assertion here is an exit code or a field out of a real envelope.
#
# Usage:  tests/playback.sh
# Exit:   0 = every check held, 1 = at least one failed

set -uo pipefail
REPO=$(cd -P "$(dirname "$0")/.." && pwd -P) || exit 1
cd "$REPO" || exit 1

# ---- a state dir of this file's own -------------------------------------------------
# Why, once, for all three files under tests/: contract.sh's header. Here it earns
# the sharpest form of the same sentence — every --stop --all below would reach the player the
# user is listening to, and every orphan count would be a count of THEIR mpv.
#
# EVERY INHERITED CONFIG NAME IS DROPPED FIRST, before this file sets any of its own. Each
# one is a config key the loader reads from the environment ahead of any file, so a developer's
# exported TING_DEFAULT_ENGINE or TING_HISTORY=0 beats the empty config below and steers every
# check without appearing anywhere in the run. A sweep over
# the whole namespace rather than a list, because a list is only as complete as the day it was
# written. The checks that prove a particular name works set it themselves, one command at a
# time. `compgen -v` rather than parsing `env`: it lists NAMES, so a value with a newline in it
# cannot forge a line that looks like one.
for _v in $(compgen -v); do
    case "$_v" in TING_*) unset "$_v" 2>/dev/null ;; esac
done
unset _v
TING_TEST_TMP=$(mktemp -d "${TMPDIR:-/tmp}/ting-playback.XXXXXX") || exit 1
export TMPDIR="$TING_TEST_TMP"
STATE_DIR="$TMPDIR/ting-$(id -u)"

# And the USER-LEVEL store, for the same reason one line up but a longer-lived consequence:
# a detached player writes a row to the listening log for every track it finishes, so without
# this every run of this file would append a dozen tracks nobody listened to into the user's
# real history — and unlike a player, a log is not something --stop takes back.
export TING_STATE_DIR="$TING_TEST_TMP/state"

# AND THE USER'S OWN CONFIG FILE, which is the third thing this run must not read — the one
# that was missed. Every default this file leans on comes from the four-level chain, and the
# user's own file wins over the shipped one: a `TING_DEFAULT_ENGINE=bili` written there by
# `ting`'s own e key (it writes that key back) sends every YouTube handle below to
# `ting-engine-bili`, which refuses the host, and 23 checks go red saying nothing whatsoever about
# the player. That is not hypothetical — it is what this file did on the machine that added
# the paragraph, and the failures named sockets and volumes rather than the config that
# caused them. An EMPTY file, not an unset variable: unset means "read ~/.config/ting/config"
# and empty means "the shipped defaults, and nothing a person happened to prefer".
# contract.sh has isolated this from the start; this file had not. The environment half of the
# same chain was swept at the top of this block.
: >"$TING_TEST_TMP/config"
export TING_CONFIG="$TING_TEST_TMP/config"

# Two long, stable tracks. Silent at --volume 0; the point is the process, not the audio.
U1=${YT_TEST_URL1:-https://www.youtube.com/watch?v=n61ULEU7CO0}
U2=${YT_TEST_URL2:-https://www.youtube.com/watch?v=8S0FDjFBj8o}
# The second engine's handle: an old-format BV with 76M views, picked to outlive the rig.
BV=${YT_TEST_BILI:-BV1fx411N7bU}

pass=0; fail=0; FAILED=""
ok()  { pass=$((pass + 1)); printf '  ok    %s\n' "$1"; }
bad() { fail=$((fail + 1)); FAILED="${FAILED}    ${1}"$'\n'; printf '  FAIL  %s\n' "$1"; }
report() { if [ "$2" = "$3" ]; then ok "$1 ($3)"; else bad "$1: want $2, got $3"; fi; }

# A detached launch returns the moment the child is forked -- that is the point of it -- so the
# mpv IPC socket does not exist yet. Any check that MUTATES a player has to wait for the socket
# or it asserts on `ipc_failed`/exit 4, which is the CORRECT answer to "talk to a player that is
# not listening". Poll (bounded), never sleep a fixed guess: mpv's start is network-bound here
# and has ranged from under a second to fifteen. Returns 1 on timeout so the caller can say so.
wait_for_sock() {
    local sock=$1 i
    for i in $(seq 1 240); do
        [ -S "$sock" ] && return 0
        sleep 0.25
    done
    return 1
}

# wait_live <id> <field>  — poll --status until one LIVE field reports something real, then
# echo it; 1 on timeout, with whatever it last saw. The fields this waits on come off the mpv
# socket, not the record, so until mpv is decoding they are legitimately null — null there is
# an honest READING (ARCH-player.md「运行时 IPC」), and what must not happen is null forever. Same rule as
# wait_for_sock: bounded poll, never a fixed sleep, because the wait is network-bound.
#
# By FIELD rather than one loop per field: position and duration arrive at the same moment for
# the same reason, and the duration site below is a queue changing tracks, where reading once
# races the child killing one mpv and starting the next.
#
# `field` is a DOTTED PATH, so a nested live field (media.audio_codec) polls through the same
# loop as a flat one. getpath, not .[$f], for exactly that: the media object arrives when the
# decoder does, which is the same race every other field here is waiting out, and a second
# poller written to walk one level deeper would be the same bounded-poll rule stated twice.
wait_live() {
    local id=$1 field=$2 v="" i
    for i in $(seq 1 160); do
        v=$(shell/ting-play --status -j 2>/dev/null \
            | jq -r --arg i "$id" --arg f "$field" \
                '.players[]|select(.id==$i)|getpath($f|split("."))//empty' 2>/dev/null)
        case "$v" in "" | null | 0) ;; *) printf '%s' "$v"; return 0 ;; esac
        sleep 0.25
    done
    printf '%s' "$v"
    return 1
}

# no_orphans <label>  — every mpv this file started is gone. Scoped to this run's own socket
# dir: a bare `mpv .*--input-ipc-server` counts the user's players too, so on any machine
# where ting is actually used the orphan check was a coin toss.
#
# The wait is INSIDE the helper, not a `sleep` at each call site: a process is reaped when it
# is reaped, both callers want the same answer, and one poll in one place cannot drift from
# itself. It reports the LAST count it saw, which is what keeps this able to fail — the shape
# wait_live already uses, where a timeout still hands back its final reading.
no_orphans() {
    local n i
    for i in $(seq 1 80); do
        n=$(pgrep -f "mpv .*--input-ipc-server=$STATE_DIR" 2>/dev/null | wc -l | tr -d ' ')
        [ "${n:-0}" = "0" ] && break
        sleep 0.25
    done
    report "$1" 0 "${n:-0}"
}

# Always stop everything, however this exits — a leaked player outlives the shell.
cleanup() {
    shell/ting-play --stop --all -j >/dev/null 2>&1
    rm -rf "$TING_TEST_TMP"
    return 0
}
trap cleanup EXIT INT TERM

shell/ting-play --stop --all -j >/dev/null 2>&1      # start from a clean slate

echo "── detach returns BEFORE mpv is up ────────────────────────────────"
# The envelope is the handle; if this waited for the player there would be nothing detached
# about it. A slow return has meant the title updater holding the captured pipe.
t0=$(date +%s)
o1=$(shell/ting-play -d -j --volume 0 -- "$U1" 2>/dev/null)
t1=$(date +%s)
report "detach envelope" 0 \
    "$(printf '%s' "$o1" | jq -e '.id and .pid and .sock' >/dev/null 2>&1; echo $?)"
if [ $((t1 - t0)) -le 3 ]; then ok "detach returned in $((t1 - t0))s (<= 3)"
else bad "detach took $((t1 - t0))s — is something holding the pipe?"; fi

id1=$(printf '%s' "$o1" | jq -r '.id // empty')
o2=$(shell/ting-play -d -j --volume 0 -- "$U2" 2>/dev/null)
id2=$(printf '%s' "$o2" | jq -r '.id // empty')

echo "── two players: the POPULATED envelope, one compact line ──────────"
report "--status one line" 1 "$(shell/ting-play --status -j | wc -l | tr -d ' ')"
report "--status sees 2"   2 "$(shell/ting-play --status -j | jq '.players | length')"

echo "── a selector-less mutation on 2 players is ambiguous -> exit 4 ───"
report "--set-volume no --id" 4 "$(shell/ting-play --set-volume 40 -j >/dev/null 2>&1; echo $?)"
# The exit code alone leaves the third row of ARCH-cli-contract.md「调用面」's 1-vs-4 table
# unproved: 4 is also what an IDLE mutation answers, so a caller told only "4" cannot tell
# "nothing to act on" from "say which one". `status` is the field that separates them, and it
# has no check anywhere else — contract.sh reaches the not_playing side and can never reach
# this one, ambiguity needing two live players.
# Captured FIRST, never piped straight from the command: under `set -o pipefail` the pipeline
# carries ting-play's own 4 and jq's verdict is thrown away — this line read FAIL/4 that way on
# its first run, against correct behaviour. Same trap contract.sh's jq_ok exists to close.
amb=$(shell/ting-play --set-volume 40 -j 2>/dev/null)
report "…and says ambiguous"  0 "$(printf '%s' "$amb" | jq -e '.status=="ambiguous" and .reason=="multiple_players" and (.players|length)==2' >/dev/null 2>&1; echo $?)"
# --stop takes the same ambiguity rule (ARCH-cli-contract.md「数据契约」): with 2 players and no
# selector it must refuse with 4 AND stop nothing — a --stop that guessed would kill the
# wrong listener's audio, which no exit code repairs.
report "--stop no --id"       4 "$(shell/ting-play --stop -j >/dev/null 2>&1; echo $?)"
report "ambiguous --stop stopped nothing" 2 "$(shell/ting-play --status -j | jq '.players | length')"
# Ambiguity is decided before any IPC, so the check above needs no player listening. The
# targeted ones below do -- wait for player 1's socket first (see wait_for_sock).
sock1=$(printf '%s' "$o1" | jq -r '.sock // empty')
wait_for_sock "$sock1" || bad "player 1's IPC socket never appeared -- the checks below are moot"
# The socket the line above just proved live, read back out of --status. A caller that never
# saw the -d envelope has no other way to learn it: ting adopting a background player at
# startup is the real one, and the alternative -- rebuilding "$STATE_DIR/mpv-$id.sock" in a
# second script -- is the duplication the envelope exists to prevent (ARCH-player.md「运行时 IPC」).
# Asserted as the SAME path rather than as "a socket exists", because a --status that answered
# with a plausible path it built itself would pass the weaker check and still be the bug.
st_sock=$(shell/ting-play --status -j | jq -r --arg i "$id1" '.players[]|select(.id==$i)|.sock // ""')
report "--status hands back the live socket" "$sock1" "$st_sock"
st_log=$(shell/ting-play --status -j | jq -r --arg i "$id1" '.players[]|select(.id==$i)|.log // ""')
report "…and a log path that is really there" 1 \
    "$([ -n "$st_log" ] && [ -f "$st_log" ] && echo 1 || echo 0)"
report "--set-volume --id"    0 "$(shell/ting-play --set-volume 40 --id "$id1" -j >/dev/null 2>&1; echo $?)"
# Only the targeted player moved: a mutation that leaks across players is the bug --id exists for.
report "only the target moved" "40" \
    "$(shell/ting-play --status -j | jq -r --arg i "$id1" '.players[]|select(.id==$i)|.volume')"

echo "── the live read: a real socket, a real peer, null is not false ───"
# The four live fields cost a round trip to mpv itself. Nothing in this repo may stand in for
# that peer, so the read is proved HERE, against the real one — never in contract.sh against
# something written to imitate it.
#
# Poll for the first reading rather than sleeping a guess (see wait_live).
if pos=$(wait_live "$id1" position); then
    ok "position came off the socket (${pos}s), not off the record"
else
    bad "player 1 never reported a position — the live read is unproved"
fi
# false is an ANSWER; null is "the question could not be asked" (ARCH-player.md「运行时 IPC」). A playing player that
# reported paused:null would make every consumer's readiness probe read a fabrication, and a
# playing player that reported it as anything but false would make --pause unobservable.
report "live paused is false, not null" 0 \
    "$(shell/ting-play --status -j | jq -e --arg i "$id1" \
        '.players[]|select(.id==$i)|.paused==false' >/dev/null 2>&1; echo $?)"
report "live duration is a number" 0 \
    "$(shell/ting-play --status -j | jq -e --arg i "$id1" \
        '.players[]|select(.id==$i)|.duration|type=="number"' >/dev/null 2>&1; echo $?)"
# The backfill's third field. `title` and `format` were already patched in from the resolve
# envelope; `selected` joins them, and it is the one of the three that no OFFLINE check can
# reach — the record is born with selected:null at launch, and only a real resolve of a real
# handle ever replaces it. A player record still reporting null here is a backfill that
# dropped the key, which is exactly what the launch-time default looks like.
report "the record carries selected" 0 \
    "$(shell/ting-play --status -j | jq -e --arg i "$id1" \
        '.players[]|select(.id==$i)|.selected|type=="string"' >/dev/null 2>&1; echo $?)"
# `media` — what is ACTUALLY DECODING, and the only place in the suite it can be proved: the
# facts come off the mpv socket, so an implementation without a real peer has nothing to
# report and this suite keeps no stand-in for one.
#
# THE DISCRIMINATING INPUT IS THE PLAYER ITSELF, playing audio. Two plausible wrong
# implementations are separated here without touching a tracked file:
#
#   * "copy what the engine said" — the record already carries `selected`, and on this play
#     it is a yt-dlp format string like "251 - audio only (medium)". A codec name cannot
#     contain a space, so the pattern below goes red the moment audio_codec is that string
#     rather than the decoder's own answer;
#   * "an absent number is zero" — this is an AUDIO play, so video_codec/width/height do not
#     apply, and the contract is that they are null. A width of 0 claims a video track one
#     pixel narrower than none; null says the question does not apply. Same distinction the
#     `paused` check above rests on, one field family over.
#
# audio_bitrate is deliberately NOT asserted: mpv computes it from recently decoded packets
# and reports it unavailable for the first seconds of a track and on some streams
# indefinitely (measured 2026-09-04, mpv 0.41). A check on it would go red on a demuxer's
# timing rather than on a bug, and a red that is not a bug still costs somebody a look.
if acodec=$(wait_live "$id1" media.audio_codec); then
    ok "media.audio_codec came off the socket ($acodec), not out of the record"
else
    bad "player 1 never reported a decoding codec — the media read is unproved"
fi
report "audio_codec is a codec name, not the engine's format string" 0 \
    "$(shell/ting-play --status -j | jq -e --arg i "$id1" \
        '.players[]|select(.id==$i)|.media.audio_codec|test("^[a-z0-9_.+-]+$")' >/dev/null 2>&1; echo $?)"
report "sample_rate is a number the engine never sent" 0 \
    "$(shell/ting-play --status -j | jq -e --arg i "$id1" \
        '.players[]|select(.id==$i)|.media.sample_rate|type=="number" and .>0' >/dev/null 2>&1; echo $?)"
report "an audio play reports video_codec/width null, not 0" 0 \
    "$(shell/ting-play --status -j | jq -e --arg i "$id1" \
        '.players[]|select(.id==$i)|.media|.video_codec==null and .width==null' >/dev/null 2>&1; echo $?)"
# The whole object is always present with all nine members, absent value or not: a caller
# that has to tell a missing KEY from a null VALUE is a caller we made work for nothing.
report "media carries all nine keys" 0 \
    "$(shell/ting-play --status -j | jq -e --arg i "$id1" \
        '.players[]|select(.id==$i)|.media|keys_unsorted|sort ==
         ["audio_bitrate","audio_codec","channels","fps","height","sample_rate","video_bitrate","video_codec","width"]' \
        >/dev/null 2>&1; echo $?)"
echo "── the playback verbs: the envelope reports what mpv answered ───"
# --pause / --resume / --seek / --seek-to over the same one-shot socket as --set-volume.
# contract.sh owns the idle half (no player → 4, an unsigned --seek → 1); what only a real
# player can show is that the verb MOVED something and that the number in the envelope came
# back off mpv rather than out of the caller's own arithmetic.
#
# Pause FIRST and seek while paused: a playing time-pos advances on its own, so any assertion
# about where a seek landed would be racing the decoder — and a check that depends on how
# fast this machine decodes is the timing assertion CLAUDE.md forbids. Paused, the playhead
# holds still and every claim below is about behaviour.
report "--pause reads back paused"  "true" \
    "$(shell/ting-play --pause --id "$id1" -j | jq -r '.paused')"
report "…and --status agrees"         "true" \
    "$(shell/ting-play --status -j | jq -r --arg i "$id1" '.players[]|select(.id==$i)|.paused')"
# The order is load-bearing: seek FORWARD first, then home. Doing it the other way round
# leaves a --seek-to that secretly seeks RELATIVE looking correct — a relative 0 from a
# playhead near the start also lands near the start. Proved by breaking it exactly that way
# and watching this pass; it only goes red once the absolute seek has somewhere to come back
# from. Each claim is anchored to the position BEFORE it, so a verb that does nothing at all
# cannot ride on where the decoder happened to be.
before=$(shell/ting-play --status -j | jq -r --arg i "$id1" '.players[]|select(.id==$i)|.position')
# A RELATIVE seek lands on a keyframe, so the claim is "forward by about that much", never an
# exact 30 — asserting the exact number would be asserting mpv's keyframe interval. What is
# proved here is that the sign was honoured and that the number is READ, not computed.
p1=$(shell/ting-play --seek +30 --id "$id1" -j | jq -r '.position')
# Each operand is judged SEPARATELY. Concatenating them ("$before$p1") let an empty baseline
# through whenever p1 was a number — and an empty `before` is 0 to bash 3.2 arithmetic, so
# `p1 >= before + 20` became `p1 >= 20` and this passed with no baseline at all.
case "$before" in
    "" | null) bad "--seek +30 had no baseline position to compare against" ;;
    *) case "$p1" in
        "" | null) bad "--seek +30 reported no position — the read-back is unproved" ;;
        *) if [ "$p1" -ge $((before + 20)) ]; then ok "--seek +30 moved the playhead forward (${before}s → ${p1}s)"
           else bad "--seek +30 left the playhead at ${p1}s (was ${before}s)"; fi ;;
       esac ;;
esac
# An ABSOLUTE seek is exact (mpv hr-seeks it), so coming back from ~30s means the start
# itself, not a keyframe in its neighbourhood.
p0=$(shell/ting-play --seek-to 0 --id "$id1" -j | jq -r '.position')
case "$p0" in
    "" | null) bad "--seek-to 0 reported no position — the read-back is unproved" ;;
    *) if [ "$p0" -le 2 ]; then ok "--seek-to 0 came back to the start (${p1}s → ${p0}s)"
       else bad "--seek-to 0 landed at ${p0}s, which is not the start"; fi ;;
esac
report "--resume reads back running" "false" \
    "$(shell/ting-play --resume --id "$id1" -j | jq -r '.paused')"
report "…and --status agrees"         "false" \
    "$(shell/ting-play --status -j | jq -r --arg i "$id1" '.players[]|select(.id==$i)|.paused')"
# THE SAME ROUND TRIP WITH NOTHING BUT THE STOCK nc. contract.sh proves the client is
# accepted; only a real mpv socket proves BSD `nc -U -w1` actually carries a command and brings
# the answer back. PATH holds jq and the system dirs, so no ncat can stand in for it. Skipped
# where the premise is absent (no BSD nc with -U, or an ncat in the system dirs).
NC_ONLY="$TING_TEST_TMP/nc-only"
mkdir -p "$NC_ONLY"
ln -sf "$(command -v jq)" "$NC_ONLY/jq"
_nch=""
[ -x /usr/bin/nc ] && _nch=$(/usr/bin/nc -h 2>&1 || true)
if [[ "$_nch" =~ [[:space:]]-U[[:space:]] ]] &&
    ! env "PATH=/usr/bin:/bin" command -v ncat >/dev/null 2>&1; then
    report "stock nc: --pause reads back paused" "true" \
        "$(env "PATH=$NC_ONLY:/usr/bin:/bin" shell/ting-play --pause --id "$id1" -j | jq -r '.paused')"
    report "stock nc: --resume reads back running" "false" \
        "$(env "PATH=$NC_ONLY:/usr/bin:/bin" shell/ting-play --resume --id "$id1" -j | jq -r '.paused')"
else
    echo "  skip  (no BSD nc with -U in /usr/bin, or an ncat beside it — the stock-client round trip cannot be tested here)"
fi

# NOT checked here: the `head -n <count>` pipe close in live_props (ARCH-player.md「运行时 IPC」).
# Tried and pulled: against the real peer it cannot go red — swap the `head` for a bare `cat`
# and the same read still measures 0.04s, so the assertion passes whatever the code does. The
# 1.11s that guard appeared to save was measured against a SCRIPTED peer, and this suite keeps
# none. The guard stays in the player as a defence; it does not get a green tick pretending
# this file proved it. Do not re-add it as a timing assertion.
# The degradation an agent must be able to tell from a reading — and it is produced by doing
# it, not by imitating it: the socket of a REALLY running player is really removed, which is
# what a crashed mpv or a half-cleaned state dir leaves behind. Live fields go null and volume
# falls back to the record, which --set-volume patched to 40 above.
rm -f "$sock1"
report "socketless player: nulls, volume off the record" 0 \
    "$(shell/ting-play --status -j | jq -e --arg i "$id1" \
        '.players[]|select(.id==$i)|.paused==null and .position==null and .duration==null and .volume==40' \
        >/dev/null 2>&1; echo $?)"

echo "── three more players, launched back to back ─────────────────────"
# Each of the next three sections owns one player of its own, and every one of them spends its
# time on the network — a resolve, then time-to-first-byte. Launched one after another and
# waited on one after another, those costs ADD; launched together they overlap, and nothing is
# lost, because every claim below selects its player by --id and a detached launch returns
# before mpv is up anyway. The waits stay per player, in the section that makes the claim.
#
# THE HANDLE FOR THE THIRD ENGINE IS SEARCHED FOR, NOT WRITTEN DOWN, and on that site it is not
# a style choice. Roughly 43% of the NetEase catalogue is VIP-only, so a pinned song id is a
# check that goes red the day someone else's licence changes. `ne-search` filters to
# access:"full" by default, so its first row is by construction a track this account can really
# play — which makes "take row one" both the cheapest handle and the correct one. `lofi` is the
# query because it measured 98% playable; the row itself is asserted, so a query that stops
# returning one is a red with a name on it rather than a mystery below.
NE_ROW=$(shell/ting-play --search --engine ne -j -n 5 -- lofi 2>/dev/null | jq -r '.results[0].url // empty')
o3=$(shell/ting-play -d -j --volume 0 --engine bili -- "$BV" 2>/dev/null)
o6=""
[ -n "$NE_ROW" ] && o6=$(shell/ting-play -d -j --volume 0 --engine ne -- "$NE_ROW" 2>/dev/null)
# ONE player for three launch options, because none of them can mask another: the tier and -f
# choose WHICH stream, --start chooses where in it mpv opens, and --loop only lands in the
# record until the track ends — which a six-hour stream opened at 600s does not do during this
# run. A player that comes up at the offset has proved the tier and -f did not break the stream
# as well; that is why the separate quality player could go.
o4=$(shell/ting-play -d -j --volume 0 --quality low -f audio --start 600 --loop one -- "$U1" 2>/dev/null)

echo "── a second engine: the envelope's http_headers reach mpv ─────────"
# The only check in the suite whose SITE makes a dropped header block observable. contract.sh
# asserts http_headers is PRESENT in the resolve envelope; nothing else there asserts that
# ting-play forwards it into mpv. This site is what makes the difference visible: its CDN answers
# 403 to a bare stream URL and 206 to the same URL carrying the envelope's Referer
# (docs/ARCHITECTURE.md「调用栈」, measured). So a player that dropped the header block would still
# play YouTube, and every other check in this file would stay green, while bytes never flowed
# from here. Position leaving zero IS the proof that they did. The NetEase player below carries
# headers too, but its CDN has not been shown to refuse a bare URL, so its green is a pipeline
# claim and not a header one.
report "bili detach envelope" 0 \
    "$(printf '%s' "$o3" | jq -e '.id and .pid and .sock' >/dev/null 2>&1; echo $?)"
id3=$(printf '%s' "$o3" | jq -r '.id // empty')
sock3=$(printf '%s' "$o3" | jq -r '.sock // empty')
# A record is a CALL: {engine, url} is the argv that produced it, so a caller reading --status
# can re-issue what is playing instead of guessing which engine owns the handle. This player is
# a discriminating input — launched under --engine, against a suite default of yt — so a field
# filled from the default, or missing, answers `yt` here.
report "the record names the engine that resolved it" "bili" \
    "$(shell/ting-play --status -j | jq -r --arg i "$id3" '.players[] | select(.id == $i) | .engine // "null"')"
if wait_for_sock "$sock3"; then
    # `position` is read live off the socket, not from the record, and time-to-first-byte is
    # network-bound — so it is the same bounded poll as player 1's (wait_live).
    if pos=$(wait_live "$id3" position); then
        ok "bili audio flowed (position ${pos}s) — the envelope's headers reached mpv"
    else
        bad "bili position never left 0 — did http_headers reach mpv? (CDN 403s without the Referer)"
    fi
else
    bad "the bili player's IPC socket never appeared — the header claim is untested"
fi

echo "── a third engine: a search row plays, end to end ─────────────────"
# THE WHOLE PIPELINE ON ONE ENGINE, with nothing pinned: a real `ne-search` answered above, its
# first row's url went straight to `ting-play -d --engine ne`, and the playhead moves. Three
# envelopes have to agree for that to happen — search's row, resolve's stream_urls/http_headers,
# and the player's record — and this is the only place all three are the same engine's and all
# three are real.
if [ -z "$NE_ROW" ]; then
    bad "ne-search returned no playable row — the third engine's pipeline is untested"
else
    report "ne detach envelope" 0 \
        "$(printf '%s' "$o6" | jq -e '.id and .pid and .sock' >/dev/null 2>&1; echo $?)"
    id6=$(printf '%s' "$o6" | jq -r '.id // empty')
    sock6=$(printf '%s' "$o6" | jq -r '.sock // empty')
    report "the record names the engine that resolved it" "ne" \
        "$(shell/ting-play --status -j | jq -r --arg i "$id6" '.players[] | select(.id == $i) | .engine // "null"')"
    if wait_for_sock "$sock6"; then
        if pos=$(wait_live "$id6" position); then
            ok "ne audio flowed (position ${pos}s) — search row to playhead, one engine"
        else
            bad "ne position never left 0 — did the resolve envelope's stream url and headers reach mpv?"
        fi
    else
        bad "the ne player's IPC socket never appeared — the third engine's pipeline is untested"
    fi
fi

echo "── the quality tier, the start offset and the loop mode, one player ─"
# --quality low must stack with -f and reach the engine without breaking format selection — a
# detached player that comes up and reports a position proves the tier did not break the
# stream. auto (the default) sends no sort at all.
#
# The start offset is the claim only a real mpv can settle: contract.sh proves the engine READS
# a timestamp and the gate refuses a bad one, and neither says the number ever reached a
# decoder. Read back off the same live socket everything else here uses.
#
# N is 600, not 42, and that is the whole design of this check. An implementation that drops
# --start reports a single-digit position — two orders of magnitude away — so the window can
# be generous without ever going green on the failure it exists to catch. At 42 the honest
# window would be a couple of seconds wide (a cold, unwarmed decoder reports its first
# position anywhere in that range), and a window that wide DOES admit "started from zero" on
# a small N. Widening a tolerance until the check passes is how a check stops being able to
# fail; moving N is how the same tolerance stops mattering.
#
# The lower bound is 595 rather than 600 because mpv lands on the keyframe at or before the
# target — measured 599 for this stream. Asserting 600 exactly would be asserting the
# keyframe interval of whatever U1 resolves to today.
report "quality + start-offset detach envelope" 0 \
    "$(printf '%s' "$o4" | jq -e '.id and .pid and .sock' >/dev/null 2>&1; echo $?)"
id4=$(printf '%s' "$o4" | jq -r '.id // empty')
sock4=$(printf '%s' "$o4" | jq -r '.sock // empty')
# The LAUNCH half of the loop field: --loop rides a detach the way -f and --quality do, and the
# record it lands in is what the child re-reads before every track. Asserted on the RECORD and
# not on mpv, because the repeat pair in the queue section proves what the value does; what is
# unproved without this line is that a launch can carry it at all. The parent writes the
# record, so this needs no socket.
report "a launch records its loop mode" "one" \
    "$(shell/ting-play --status -j | jq -r --arg i "$id4" '.players[]|select(.id==$i)|.loop // empty')"
if wait_for_sock "$sock4"; then
    if pos=$(wait_live "$id4" position); then
        ok "quality audio flowed (position ${pos}s) — the tier reached the engine"
        if [ "$pos" -ge 595 ] && [ "$pos" -le 605 ]; then
            ok "--start 600 opened at ${pos}s — mpv got the offset"
        else
            bad "--start 600 opened at ${pos}s, which is not where it was told to"
        fi
    else
        bad "the quality + start-offset player never reported a position — did --quality break the stream?"
    fi
else
    bad "the quality + start-offset player's IPC socket never appeared"
fi

echo "── stop is targeted, then idempotent, and leaks nothing ───────────"
report "--stop --id"       0 "$(shell/ting-play --stop --id "$id1" -j >/dev/null 2>&1; echo $?)"
report "--stop --all"      0 "$(shell/ting-play --stop --all -j >/dev/null 2>&1; echo $?)"
report "--stop --all again" 0 "$(shell/ting-play --stop --all -j >/dev/null 2>&1; echo $?)"
report "no players left"   0 "$(shell/ting-play --status -j | jq -e '.players==[]' >/dev/null 2>&1; echo $?)"

no_orphans "no orphan mpv"

echo "── a queue is a player consuming a playlist ───────────────────────"
# Everything above played ONE handle. This section plays a LIST, and it is here rather than
# in contract.sh for the reason that file states about itself: proving a queue ADVANCES
# needs a real engine round trip and a real mpv reaching the end of a track, and nothing may
# stand in for either. A mock engine answering av://lavfi:sine would skip the JIT resolve —
# the very thing most likely to break between two tracks — and be green for it.
#
# One player, so no --id is needed anywhere below (exactly-one is the zero-friction case).
shell/ting-play --stop --all -j >/dev/null 2>&1
# The list is STORED first and read back out, so what launches the player is
# ARCH-cli-contract.md「调用面」's second pipeline run verbatim:
#     ting-playlist --show chill -j | ting-play -d --queue -
# It used to be a `jq -nc` array inlined here, which proved the array arm and left the arm the
# doc actually advertises unexecuted — and that arm is the one carrying the claim: a stored
# record IS a call, so the two commands need no jq mapping between them. contract.sh proves
# the same envelope reaches the gate offline (4, no player); this is the half where it really
# starts one. TING_STATE_DIR is this file's own (top of file), so the list is disposable.
jq -nc --arg a "$U1" --arg b "$U2" '[{engine:"yt",url:$a},{engine:"yt",url:$b}]' |
    shell/ting-playlist --add chill -j >/dev/null 2>&1
report "the list stored 2" 2 "$(shell/ting-playlist --show chill -j 2>/dev/null | jq -r '.count // 0')"
qout=$(shell/ting-playlist --show chill -j 2>/dev/null | shell/ting-play -d --queue - -j --volume 0 2>/dev/null)
report "--queue - launches" 0 "$(printf '%s' "$qout" | jq -e '.status=="started" and .id' >/dev/null 2>&1; echo $?)"
# The queue is visible from the moment the player exists, not once the first track decodes:
# a caller that asks what is queued must not have to wait for mpv. `upcoming` is asserted
# beside `next` because it is the READ half of the queue (ting's card draws it): the two
# describe the same tail, so a projection that let them disagree about what comes next is
# the failure this check exists to catch. `duration` is the field `next` does not carry —
# the whole reason the list exists — so it is asserted as PRESENT rather than as a number
# (these items were queued from urls alone, and an absent duration is null, not zero).
report "--status carries the queue" 0 \
    "$(shell/ting-play --status -j | jq -e '.players[0].queue
        | .pos==0 and .len==2 and (.next.url|type=="string")
          and (.upcoming|length)==1 and .upcoming[0].url==.next.url
          and (.upcoming[0]|has("duration"))' >/dev/null 2>&1; echo $?)"
qsock=$(printf '%s' "$qout" | jq -r '.sock // empty')
wait_for_sock "$qsock" || bad "the queued player's socket never appeared — the checks below are moot"

# --enqueue lands on a RUNNING player, and the envelope reports the queue it wrote.
report "--enqueue appends" 0 \
    "$(jq -nc --arg a "$U1" '[{engine:"yt",url:$a}]' | shell/ting-play --enqueue - -j 2>/dev/null \
        | jq -e '.status=="ok" and .added==1 and .queue.len==3' >/dev/null 2>&1; echo $?)"
# Concurrency is DRIVEN, not argued (the rule ting-playlist's eight concurrent --add checks
# already follow): six writers, six items, no lost update. With lock_queue_state stubbed to
# fail this loop leaves fewer — watched, so the check is known to be able to fail.
for i in 1 2 3 4 5 6; do
    jq -nc --arg a "$U2" '[{engine:"yt",url:$a}]' | shell/ting-play --enqueue - -j >/dev/null 2>&1 &
done
wait
report "6 concurrent --enqueue all land (3+6)" 9 \
    "$(shell/ting-play --status -j | jq -r '.players[0].queue.len')"
# Nine queued items is the discriminating input for the CAP: a projection that dumped the
# whole tail would answer 8 here and grow with every --enqueue, which is the thing --status
# --all must not do. `len` above already proved the total is still told honestly.
report "upcoming is capped, len is not" 5 \
    "$(shell/ting-play --status -j | jq -r '.players[0].queue.upcoming | length')"

# --next: the POSITION moves in the parent, so the envelope reports a queue it read. Then the
# player follows — a bounded poll, because what is being proved is that it DID follow, not
# how fast it resolved (a duration assertion against a live site is the timing check
# CLAUDE.md forbids).
report "--next advances the position" 1 \
    "$(shell/ting-play --next -j 2>/dev/null | jq -r '.queue.pos')"
i=0
while [ $i -lt 240 ]; do
    u=$(shell/ting-play --status -j | jq -r '.players[0].url // empty')
    [ "$u" = "$U2" ] && break
    sleep 0.25; i=$((i + 1))
done
report "the record follows the track" "$U2" \
    "$(shell/ting-play --status -j | jq -r '.players[0].url // empty')"

# The one a queue exists for: a track ENDING on its own starts the next. Seek to just before
# the end rather than waiting out a six-hour stream, then poll for the position to move —
# the child has to notice mpv exited, advance under its own lock, resolve the next handle and
# start a new mpv, and none of that is driven from here.
# WAIT for the duration, never read it once. `duration` comes off the socket while the poll
# above only proved the RECORD advanced — at that moment the child may have just killed track
# one's mpv and still be resolving track two, so a single read has come back empty and turned
# this red on a correct player (measured). Worse than the red: it skipped the arm below, so
# the one claim a queue exists for went unproved while the score dropped by only 1.
qid=$(printf '%s' "$qout" | jq -r '.id // empty')
if dur=$(wait_live "$qid" duration); then
    shell/ting-play --seek-to $((dur - 4)) -j >/dev/null 2>&1
    i=0
    while [ $i -lt 360 ]; do
        [ "$(shell/ting-play --status -j | jq -r '.players[0].queue.pos // empty')" = "2" ] && break
        sleep 0.25; i=$((i + 1))
    done
    report "a track ending advances the queue" "2" \
        "$(shell/ting-play --status -j | jq -r '.players[0].queue.pos // empty')"
else
    bad "the queued player never reported a duration in 40s — cannot drive it to a track end"
fi

# ── REPEAT: the track that does not end ────────────────────────────────────────────────
# Driven on the queue player deliberately, because the claim IS about the boundary: a track
# reaching its end under repeat must start over INSTEAD of handing the queue on. Reading
# mpv's loop-file back over the socket would only prove a property was set; this proves what
# the property DOES, and it uses the same seek the advance above uses rather than waiting out
# a real ending.
# THE ORDER IS THE POINT: wait for the track to be UP before pressing the key. That is the
# real gesture (music is playing, the user turns repeat on) and it is also the only order
# that proves anything — between two tracks there is no socket, so the mode would reach mpv
# only at the next launch and this block would be timing a resolve instead of a loop.
if dur=$(wait_live "$qid" duration); then
    qpos=$(shell/ting-play --status -j | jq -r '.players[0].queue.pos // empty')
    report "--set-loop reports the mode it set" "one" \
        "$(shell/ting-play --set-loop one -j 2>/dev/null | jq -r '.loop // empty')"
    report "…and --status agrees" "one" \
        "$(shell/ting-play --status -j | jq -r '.players[0].loop // empty')"
    shell/ting-play --seek-to $((dur - 4)) -j >/dev/null 2>&1
    # Polled, never timed: how long mpv takes to loop a file is not this suite's subject.
    # The playhead coming back to somewhere near the START of the same track is the whole
    # observable, and `dur - 10` is a floor no seek in this block ever lands above.
    # Either outcome ends the poll: the playhead coming home (repeat worked) or the queue
    # moving (it did not). Waiting only for the first would spend forty seconds arriving at
    # the same red the second reports immediately.
    i=0
    while [ $i -lt 160 ]; do
        p=$(shell/ting-play --status -j | jq -r '.players[0].position // empty')
        [ "$(shell/ting-play --status -j | jq -r '.players[0].queue.pos // empty')" != "$qpos" ] && break
        case "$p" in "" | null) ;; *) [ "$p" -lt $((dur - 10)) ] && break ;; esac
        sleep 0.25; i=$((i + 1))
    done
    report "a repeating track wraps to its own start" 1 \
        "$(p=$(shell/ting-play --status -j | jq -r '.players[0].position // 999999')
           [ "$p" -lt $((dur - 10)) ] && echo 1 || echo 0)"
    report "…and the queue did not advance" "$qpos" \
        "$(shell/ting-play --status -j | jq -r '.players[0].queue.pos // empty')"
    # THE OTHER HALF OF THE PAIR, and the reason the wrap above is not just a queue being
    # slow: turn repeat off and drive the SAME track to the SAME end. Now it must hand on.
    # It also proves the child re-reads the record between tracks — the value it was launched
    # with is `off`, so a child holding that would pass the wrap check above by accident and
    # this one either way; only the pair separates them.
    shell/ting-play --set-loop off -j >/dev/null 2>&1
    shell/ting-play --seek-to $((dur - 4)) -j >/dev/null 2>&1
    i=0
    while [ $i -lt 360 ]; do
        [ "$(shell/ting-play --status -j | jq -r '.players[0].queue.pos // empty')" != "$qpos" ] && break
        sleep 0.25; i=$((i + 1))
    done
    report "repeat off: the same end advances the queue" "$((qpos + 1))" \
        "$(shell/ting-play --status -j | jq -r '.players[0].queue.pos // empty')"
else
    bad "the repeating player never reported a duration in 40s — the repeat claims are unproved"
fi

# --stop takes the whole QUEUE down. The child traps BOTH signals stop_group sends it, and
# the INT half is not belt-and-braces: bash only sets SIGINT to SIG_IGN in an async child when
# job control is OFF, and detach_play launches under `set -m`, so an untrapped INT here is a
# plain kill landing while the TERM handler runs (measured — see stop_group). The signals are
# repeated on every tick of the escalation because the engine call between two tracks spawns
# processes that never saw the first one. --stop does not return until that escalation has
# ended and the record is removed (do_stop), so the empty record is asserted immediately:
# polling for it would be a wait that could never fail.
shell/ting-play --next -j >/dev/null 2>&1
# NOT a wait, and so not a poll: this sleep is SETUP. It puts the --stop below in the middle
# of the between-tracks resolve, which is the race being driven; without it the stop lands
# before the child has spawned anything and the check passes without touching the claim.
sleep 0.5
report "--stop ends the queue" 0 "$(shell/ting-play --stop --all -j >/dev/null 2>&1; echo $?)"
report "no players after a queue" 0 "$(shell/ting-play --status -j | jq -e '.players==[]' >/dev/null 2>&1; echo $?)"
no_orphans "no orphan mpv after a queue"

# ── editing a queue that is being consumed ─────────────────────────────────────────────
# A fresh player, deliberately, rather than editing one of the queues above: those were left
# at positions these checks would have to guess at, and an index is the whole subject here.
#
# WHAT THIS SECTION IS FOR. The five edit verbs are the only part of the CLI that can destroy
# something a user cannot get back — a queue dies with its player, so a track removed by a
# stale index is gone from everywhere. Both guards are driven against a REAL running player,
# because both are decided under the lock against the position of the moment, and a position
# only moves when something is really playing.
shell/ting-play --stop --all -j >/dev/null 2>&1
eout=$(jq -nc --arg a "$U1" --arg b "$U2" \
    '[{engine:"yt",url:$a},{engine:"yt",url:$b},{engine:"yt",url:$a},{engine:"yt",url:$b}]' |
    shell/ting-play -d --queue - -j --volume 0 2>/dev/null)
report "the edit player launched" 0 \
    "$(printf '%s' "$eout" | jq -e '.status=="started" and .id' >/dev/null 2>&1; echo $?)"
wait_for_sock "$(printf '%s' "$eout" | jq -r '.sock // empty')" ||
    bad "the edit player's socket never appeared — the checks below are moot"

# --queue-show is the READ half, and the one thing --status cannot do: its tail is capped at
# five, because it projects every live player at once. Four items is under that cap, so what
# this pair proves is not the length but the AGREEMENT — the same pos and len --status
# reports, plus the per-item index the write verbs take.
report "--queue-show reports every item" 0 \
    "$(shell/ting-play --queue-show -j | jq -e '.status=="ok" and .len==4 and .pos==0
        and (.items|length)==4 and (.items|map(.index))==[0,1,2,3]
        and (.items[0]|has("engine") and has("url"))' >/dev/null 2>&1; echo $?)"
report "…and it agrees with --status" 0 \
    "$(a=$(shell/ting-play --queue-show -j | jq -c '{pos,len}')
       b=$(shell/ting-play --status -j | jq -c '{pos:.players[0].queue.pos,len:.players[0].queue.len}')
       [ "$a" = "$b" ] && echo 0 || echo 1)"

# THE PLAYING TRACK IS NOT A QUEUE EDIT. Index 0 is what pos names, and the pin is CORRECT
# here — so a range check that ran after the identity check, or not at all, would let this
# through and take the song out from under mpv.
eu0=$(shell/ting-play --queue-show -j | jq -r '.items[0].url')
report "removing the playing track is 4" 4 \
    "$(shell/ting-play --queue-rm 0 --expect-url "$eu0" -j >/dev/null 2>&1; echo $?)"
report "…and it says queue_range" "queue_range" \
    "$(shell/ting-play --queue-rm 0 --expect-url "$eu0" -j 2>/dev/null | jq -r '.reason // empty')"
report "…past the end is queue_range too" "queue_range" \
    "$(shell/ting-play --queue-rm 9 --expect-url "$eu0" -j 2>/dev/null | jq -r '.reason // empty')"

# THE CENTRAL CLAIM OF THE DESIGN, and an exit code alone does not prove it: a refused write
# must write NOTHING. The length either side of the refusal is the assertion — a queue_stale
# that had already rewritten the file would report the same 4.
#
# The pin is a url that is REAL but belongs to another index; a made-up string would also be
# caught by a check that merely tested for non-empty.
ebefore=$(shell/ting-play --queue-show -j | jq -r '.len')
eu1=$(shell/ting-play --queue-show -j | jq -r '.items[1].url')
report "a stale pin is 4" 4 \
    "$(shell/ting-play --queue-rm 2 --expect-url "$eu1" -j >/dev/null 2>&1; echo $?)"
report "…and it says queue_stale" "queue_stale" \
    "$(shell/ting-play --queue-rm 2 --expect-url "$eu1" -j 2>/dev/null | jq -r '.reason // empty')"
report "…and the queue is UNCHANGED" "$ebefore" \
    "$(shell/ting-play --queue-show -j | jq -r '.len')"

# The move, both directions, read back off the file rather than believed from the envelope.
# 3 -> 1 and 1 -> 3 is a round trip, so the second half asserts the queue came HOME: a --to
# that landed one slot out in one direction would not survive being undone.
# The WHOLE order either side, never one index: the queue is A,B,A,B, so "the track that was
# at 3 is now at 1" is true before the move as well and would pass against a verb that did
# nothing at all. [A,B,A,B] -> move 3 to 1 -> [A,B,B,A] is the discriminating shape.
eorder=$(shell/ting-play --queue-show -j | jq -c '[.items[].url]')
eu3=$(shell/ting-play --queue-show -j | jq -r '.items[3].url')
ewant=$(shell/ting-play --queue-show -j | jq -c '[.items[0].url,.items[1].url,.items[3].url,.items[2].url]')
report "--queue-mv moves it up" 0 \
    "$(shell/ting-play --queue-mv 3 --to 1 --expect-url "$eu3" -j >/dev/null 2>&1; echo $?)"
report "…and the whole order moved with it" "$ewant" \
    "$(shell/ting-play --queue-show -j | jq -c '[.items[].url]')"
report "…and the length did not change" "$ebefore" \
    "$(shell/ting-play --queue-show -j | jq -r '.len')"
# Undoing it is the other direction, and the order coming HOME is what says a --to landed on
# exactly the slot asked for: one slot out in either direction does not survive a round trip.
report "--queue-mv moves it back down" 0 \
    "$(shell/ting-play --queue-mv 1 --to 3 --expect-url "$eu3" -j >/dev/null 2>&1; echo $?)"
report "…and the order came home" "$eorder" \
    "$(shell/ting-play --queue-show -j | jq -c '[.items[].url]')"
# The destination is held to the SAME bounds as the index, for the same reason: slot 0 is the
# track being heard. A --to checked only for being a number would accept this.
report "--to cannot aim at the playing slot" "queue_range" \
    "$(shell/ting-play --queue-mv 3 --to 0 --expect-url "$eu3" -j 2>/dev/null | jq -r '.reason // empty')"

# The removal that is supposed to work, proved by what is LEFT rather than by an exit code:
# the item goes, the ones after it close up, and every other url stays where it was. The
# envelope is captured from the ONE call that does it — re-running the verb to read it would
# remove a second track.
eu2=$(shell/ting-play --queue-show -j | jq -r '.items[2].url')
ekeep=$(shell/ting-play --queue-show -j | jq -c '[.items[0].url,.items[1].url,.items[3].url]')
erm=$(shell/ting-play --queue-rm 2 --expect-url "$eu2" -j 2>/dev/null)
report "--queue-rm removes it" 0 \
    "$(printf '%s' "$erm" | jq -e '.status=="ok" and .removed.index==2' >/dev/null 2>&1; echo $?)"
report "…and names the url it removed" "$eu2" \
    "$(printf '%s' "$erm" | jq -r '.removed.url // empty')"
# The whole queue_snapshot object, not a shortened copy of it: --enqueue and --next already
# publish {pos,len,next,upcoming} under this key, and a same-named object with fewer fields is
# how a contract drifts without anyone deciding to change it.
report "…and carries the whole queue object" 0 \
    "$(printf '%s' "$erm" | jq -e '.queue|has("pos") and has("len") and has("next") and has("upcoming")' >/dev/null 2>&1; echo $?)"
report "…and the rest closed up in order" "$ekeep" \
    "$(shell/ting-play --queue-show -j | jq -c '[.items[].url]')"

# --queue-jump is the one verb that touches the RUNNING player: it moves the track to pos+1
# and advances pos onto it in a single locked write, then signals the child. Both halves are
# asserted — the file, and then the player following it — because the write alone would be a
# queue claiming to have moved on while mpv plays the old track.
ejump=$(shell/ting-play --queue-show -j | jq -r '.items[2].url')
report "--queue-jump is ok" 0 \
    "$(shell/ting-play --queue-jump 2 --expect-url "$ejump" -j >/dev/null 2>&1; echo $?)"
report "…pos moved to 1" "1" \
    "$(shell/ting-play --queue-show -j | jq -r '.pos')"
report "…and that track is the one at pos" "$ejump" \
    "$(shell/ting-play --queue-show -j | jq -r '.items[.pos].url')"
i=0
while [ $i -lt 240 ]; do
    [ "$(shell/ting-play --status -j | jq -r '.players[0].url // empty')" = "$ejump" ] && break
    sleep 0.25; i=$((i + 1))
done
report "…and the PLAYER followed the jump" "$ejump" \
    "$(shell/ting-play --status -j | jq -r '.players[0].url // empty')"

# UNDO, on the same real player: a write that names its owner (this shell, alive for the whole
# file) leaves a copy, and --undo puts the queue back byte for byte. Every "came back" below is
# the WHOLE order read off --queue-show, for the reason the move checks above give: one index
# can look right against a verb that did nothing.
uorder=$(shell/ting-play --queue-show -j | jq -c '[.items[].url]')
uenq=$(jq -nc --arg a "$U1" --arg b "$U2" '[{engine:"yt",url:$a},{engine:"yt",url:$b}]' |
    shell/ting-play --enqueue - --owner $$ -j 2>/dev/null)
report "--enqueue --owner carries undo.deadline" 0 \
    "$(printf '%s' "$uenq" | jq -e '.status=="ok" and .added==2 and (.undo.deadline|type)=="number"' >/dev/null 2>&1; echo $?)"
report "--undo takes the enqueue back" 0 \
    "$(shell/ting-play --undo --owner $$ -j 2>/dev/null | jq -e '.status=="ok" and .undone=="enqueue" and (.queue|has("pos"))' >/dev/null 2>&1; echo $?)"
report "…and the order came home" "$uorder" \
    "$(shell/ting-play --queue-show -j | jq -c '[.items[].url]')"
# A tail to edit, written WITHOUT an owner — and so with the envelope it always had.
report "--enqueue without --owner: no undo field" 0 \
    "$(jq -nc --arg a "$U1" --arg b "$U2" '[{engine:"yt",url:$a},{engine:"yt",url:$b}]' |
       shell/ting-play --enqueue - -j 2>/dev/null | jq -e 'has("undo")|not' >/dev/null 2>&1; echo $?)"
uorder=$(shell/ting-play --queue-show -j | jq -c '[.items[].url]')
ulast=$(shell/ting-play --queue-show -j | jq -r '.len - 1')
uurl=$(shell/ting-play --queue-show -j | jq -r '.items[-1].url')
shell/ting-play --queue-rm "$ulast" --expect-url "$uurl" --owner $$ -j >/dev/null 2>&1
report "--queue-rm, then --undo: the track is back in its slot" "$uorder" \
    "$(shell/ting-play --undo --owner $$ -j >/dev/null 2>&1; shell/ting-play --queue-show -j | jq -c '[.items[].url]')"
shell/ting-play --queue-clear --owner $$ -j >/dev/null 2>&1
report "--queue-clear, then --undo: the whole tail is back" "$uorder" \
    "$(shell/ting-play --undo --owner $$ -j >/dev/null 2>&1; shell/ting-play --queue-show -j | jq -c '[.items[].url]')"

# --queue-clear drops the tail and nothing else. The track being heard surviving IS the claim:
# clearing a queue is not stopping a player, and --stop is the verb for that.
eplaying=$(shell/ting-play --status -j | jq -r '.players[0].url // empty')
report "--queue-clear is ok" 0 \
    "$(shell/ting-play --queue-clear -j >/dev/null 2>&1; echo $?)"
report "…len is pos + 1" 0 \
    "$(shell/ting-play --queue-show -j | jq -e '.len == (.pos + 1)' >/dev/null 2>&1; echo $?)"
report "…and the playing track is untouched" "$eplaying" \
    "$(shell/ting-play --status -j | jq -r '.players[0].url // empty')"
# Idempotent, and honest about it: nothing left to clear is a successful call that cleared
# nothing, never a failure.
report "…clearing again clears 0" "0" \
    "$(shell/ting-play --queue-clear -j 2>/dev/null | jq -r '.cleared')"
# A TRACK BOUNDARY BETWEEN THE WRITE AND THE UNDO. The player rewrites pos when it moves on,
# so the queue is no longer what the write left and the undo must refuse — putting back the
# bytes from before would rewind pos and replay a track. --next is the boundary, driven.
jq -nc --arg a "$U1" --arg b "$U2" '[{engine:"yt",url:$a},{engine:"yt",url:$b}]' |
    shell/ting-play --enqueue - -j >/dev/null 2>&1
ulast=$(shell/ting-play --queue-show -j | jq -r '.len - 1')
uurl=$(shell/ting-play --queue-show -j | jq -r '.items[-1].url')
shell/ting-play --queue-rm "$ulast" --expect-url "$uurl" --owner $$ -j >/dev/null 2>&1
shell/ting-play --next -j >/dev/null 2>&1
uafter=$(shell/ting-play --queue-show -j | jq -c '{pos, urls: [.items[].url]}')
report "an undo across a track boundary is undo_stale" "undo_stale" \
    "$(shell/ting-play --undo --owner $$ -j 2>/dev/null | jq -r '.reason // empty')"
report "…and the queue is left as the player has it" "$uafter" \
    "$(shell/ting-play --queue-show -j | jq -c '{pos, urls: [.items[].url]}')"
# The copy dies with its player. undo_none and not undo_stale is the discriminating answer: a
# copy that outlived the queue would still be found, and refused for the missing file.
jq -nc --arg a "$U1" '[{engine:"yt",url:$a}]' | shell/ting-play --enqueue - -j >/dev/null 2>&1
ulast=$(shell/ting-play --queue-show -j | jq -r '.len - 1')
uurl=$(shell/ting-play --queue-show -j | jq -r '.items[-1].url')
shell/ting-play --queue-rm "$ulast" --expect-url "$uurl" --owner $$ -j >/dev/null 2>&1
shell/ting-play --stop --all -j >/dev/null 2>&1
report "--stop takes the player's undo copy with it" "undo_none" \
    "$(shell/ting-play --undo --owner $$ -j 2>/dev/null | jq -r '.reason // empty')"
no_orphans "no orphan mpv after the queue edits"
# The failure tombstone check lives at the end of this file, driven by a real failing player.

echo "── the listening log, written by a player that really played ─────"
# The WIRING — that a track
# ending makes a row exist — can only be proved where a real track really ends, so it is
# proved on a track chosen to end: 19 seconds, permanent and public, the same handle
# contract.sh resolves. A seek to the end of one of the long tracks above would be cheaper
# and it is what the queue section does, but it cannot carry this claim: `duration` is null
# on a live stream, and this file must not have a check that goes green or red depending on
# whether the track was streaming that afternoon.
SHORT=${YT_TEST_SHORT:-https://www.youtube.com/watch?v=jNQXAC9IVRw}
# How many rows this one track has, out of an envelope already in hand. Every claim below is
# keyed by its url rather than by a total: this file leaves players stopping in the background
# and a row landing from one of them mid-window would move a total for a reason that has
# nothing to do with what is being asserted.
h_url() { printf '%s' "$1" | jq -r --arg u "$SHORT" '[.items[]|select(.url==$u)]|length'; }

shell/ting-play -d -j --volume 0 -- "$SHORT" >/dev/null 2>&1
# Poll the LOG, not the clock: the row appears when the track ends, and how long the track
# takes to start is network-bound (the rule wait_for_sock follows, applied to the artefact).
# Poll for THIS track's row: the queue section also ends a track on its own, so a poll for
# "any row that ended by itself" returns immediately and waits for nothing.
i=0
while [ $i -lt 240 ]; do
    [ "$(h_url "$(shell/ting-history --ls -n 50 -j 2>/dev/null)")" != "0" ] && break
    sleep 0.25; i=$((i + 1))
done
HIST=$(shell/ting-history --ls -n 50 -j 2>/dev/null)
# THE RECORD POINT, and the one claim that separates a history from a death record: a track
# that ended ON ITS OWN is in the log, carrying no reason at all. If the row were written
# only when a player dies, this is the check that would be empty.
report "a track that ended is logged" 0 \
    "$(printf '%s' "$HIST" | jq -e --arg u "$SHORT" 'any(.items[]; .url==$u and .reason==null)' >/dev/null 2>&1; echo $?)"
# And it is that TRACK's row, not a placeholder: the title the engine returned, and a played
# length in the neighbourhood of the 19 seconds the thing actually is.
report "…with its own title and length" 0 \
    "$(printf '%s' "$HIST" | jq -e --arg u "$SHORT" 'any(.items[]; .url==$u and (.title|type)=="string" and .seconds >= 15 and .seconds <= 40)' >/dev/null 2>&1; echo $?)"
# The other half, off the players every section above stopped: an interrupted track is
# recorded too, which is what keeps the log from being a record of what went uninterrupted.
report "an interrupted track says so" 0 \
    "$(printf '%s' "$HIST" | jq -e '[.items[]|select(.reason=="stopped_by_user")]|length >= 1' >/dev/null 2>&1; echo $?)"
# A row is a CALL: `engine` plus `url` is `ting-play --engine E -- <handle>`, which is why the
# log stores the pair and not a bare handle. Both are asserted as the GRAMMAR each side of
# that argv has — an engine name the player will paste into `<engine>-resolve`, a handle with
# no whitespace in it — and not as "yt" or as "starts with http": this run drives three engines,
# one of them on a bare BV id and one on a numeric song id, and a fourth must pass this check
# unedited.
report "…and every row is a call"    0 \
    "$(printf '%s' "$HIST" | jq -e 'all(.items[]; (.engine|test("^[a-z0-9][a-z0-9_-]*$")) and (.url|test("^[^[:space:]]+$")))' >/dev/null 2>&1; echo $?)"
# One shape for every source: the three engines the sections above drove are all in the log,
# with no per-site branch anywhere between them and the row. An ne-search that returned no
# playable row leaves this red as well, which is correct — the ne row was never written.
report "…from all three engines, one shape" 0 \
    "$(printf '%s' "$HIST" | jq -e '[.items[].engine]|unique|length >= 3' >/dev/null 2>&1; echo $?)"

# The off switch is the whole switch: not a shorter row, no row. One more real player,
# because a knob only read on a path nothing drives is a knob nobody has tested.
#
# The short track is replayed here because exactly one row for it exists by now, and only
# this player could write a second.
h_before=$(h_url "$HIST")
o7=$(TING_HISTORY=0 shell/ting-play -d -j --volume 0 -- "$SHORT" 2>/dev/null)
id7=$(printf '%s' "$o7" | jq -r '.id // empty')
sock7=$(printf '%s' "$o7" | jq -r '.sock // empty')
# mpv has to really PLAY, or "the switch wrote nothing" is true of a track that never started
# and the check is vacuous — so the stop waits for the playhead to leave zero, the same live
# reading every other player here is proved by.
if wait_for_sock "$sock7" && wait_live "$id7" position >/dev/null; then
    shell/ting-play --stop --all -j >/dev/null 2>&1
    # An ABSENCE cannot be polled for — you can only wait long enough — so it is read once the
    # PRECONDITION holds: the row is written by the player's own exit path, and --stop returns
    # only after that whole process group is gone (do_stop, stop_group). No process that could
    # write a row is left, so the answer below is final.
    report "TING_HISTORY=0 writes nothing" "$h_before" \
        "$(h_url "$(shell/ting-history --ls -n 50 -j 2>/dev/null)")"
else
    bad "the TING_HISTORY=0 player never started playing — the off switch is untested"
fi

echo "── --watch: one player as a stream, across its queue, to its end ──"
# The stream a TUI draws its clock from and an agent follows a queue with. Every claim is a
# line the verb really wrote about a player that was really doing it, polled for — never a
# count after a sleep, because the cadence is the playhead's and the start is the network's.
# The queue is SHORT twice, so crossing a track (--next) and a track playing out on its own
# are both reached inside one player.
wq=$(printf '[{"engine":"yt","url":"%s"},{"engine":"yt","url":"%s"}]' "$SHORT" "$SHORT" |
    shell/ting-play -d -j --volume 0 --queue - 2>/dev/null)
wid=$(printf '%s' "$wq" | jq -r '.id // empty')
WOUT="$TING_TEST_TMP/watch.out"
shell/ting-play --watch --id "$wid" -j >"$WOUT" 2>/dev/null &
wpid=$!
# wait_watch <jq over the slurped lines>. A line still being written makes the slurp fail,
# which is one more turn of the poll, not a red.
wait_watch() {
    local i
    for i in $(seq 1 240); do
        jq -e -s "$1" "$WOUT" >/dev/null 2>&1 && return 0
        sleep 0.25
    done
    return 1
}
w_ok() { if wait_watch "$2"; then ok "$1"; else bad "$1 — never seen"; fi; }
w_ok "first line is a snapshot of THIS player" \
    '.[0].status=="watch" and .[0].event=="snapshot" and .[0].id=="'"$wid"'"'
# `ready` is a FIELD on every line, and an event only when it turns: a watch that connects
# after the sound started carries ready:true on its snapshot and never sends the event.
w_ok "…and says when the sound started" 'any(.[]; .ready==true)'
shell/ting-play --set-volume 20 --id "$wid" -j >/dev/null 2>&1
w_ok "another caller's --set-volume arrives as an event" \
    'any(.[]; .event=="volume" and .volume==20)'
shell/ting-play --pause --id "$wid" -j >/dev/null 2>&1
w_ok "--pause arrives, carrying paused:true" 'any(.[]; .event=="pause" and .paused==true)'

# A reader that leaves is noticed on the next WRITE, and a paused player writes nothing — so
# the one moment this can leak is exactly now. The reader is a FIFO read for one line and
# closed, which makes "the reader is gone" a fact rather than a guess about when `head` exits.
# The next event (a volume change) must then take the verb AND its nc down.
RFIFO="$TING_TEST_TMP/watch-reader"
mkfifo "$RFIFO"
shell/ting-play --watch --id "$wid" -j >"$RFIFO" 2>/dev/null &
rpid=$!
IFS= read -r _first <"$RFIFO"
# ITS nc, by parent, taken while it is still attached: the first watcher holds an nc on the
# same socket, so counting every nc in the state dir would count that one too.
rnc=$(pgrep -P "$rpid" -f "nc(at)? -U" 2>/dev/null | head -1)
report "a second watcher gets its own snapshot" snapshot \
    "$(printf '%s' "$_first" | jq -r '.event' 2>/dev/null)"
shell/ting-play --set-volume 10 --id "$wid" -j >/dev/null 2>&1
for i in $(seq 1 40); do kill -0 "$rpid" 2>/dev/null || break; sleep 0.25; done
report "a watcher whose reader left exits on the next event" 1 \
    "$(kill -0 "$rpid" 2>/dev/null && echo 0 || echo 1)"
report "…and takes its nc with it" 1 \
    "$([ -n "$rnc" ] && ! kill -0 "$rnc" 2>/dev/null && echo 1 || echo 0)"

shell/ting-play --resume --id "$wid" -j >/dev/null 2>&1
w_ok "--resume arrives, carrying paused:false" 'any(.[]; .event=="resume" and .paused==false)'
# The cadence claim: a heartbeat per whole second of playhead, so positions never repeat —
# mpv pushes ~19 time-pos changes a second, and forwarding them would repeat each one ~19 times.
w_ok "heartbeats come once per whole second, not per frame" \
    '[.[]|select(.event=="heartbeat")|.position] as $h | ($h|length) >= 3 and ($h|length) == ($h|unique|length)'
shell/ting-play --next --id "$wid" -j >/dev/null 2>&1
# The track that ENDED is what `transitioning` describes (the queue file has not moved yet
# when mpv says end-file); the next one is announced by the snapshot of the next connection.
w_ok "--next: transitioning names the track that ended, as interrupted" \
    'any(.[]; .event=="transitioning" and .ended=="interrupted" and .queue.pos==0)'
w_ok "…and the stream carries on into the next track's snapshot" \
    'any(.[]; .event=="snapshot" and .queue.pos==1)'
# Track two plays out on its own; the verb's exit IS the signal the player is gone.
for i in $(seq 1 240); do kill -0 "$wpid" 2>/dev/null || break; sleep 0.25; done
wait "$wpid"
report "--watch exits 0 when the player ends" 0 $?
report "a track that played out ends as finished" 0 \
    "$(jq -e -s 'any(.[]; .event=="transitioning" and .ended=="finished" and .queue.pos==1)' "$WOUT" >/dev/null 2>&1; echo $?)"
report "the last line is end, with no failure to report" 0 \
    "$(jq -e -s '.[-1] | .event=="end" and .exit_code==null and .reason==null' "$WOUT" >/dev/null 2>&1; echo $?)"
# Whole state on every line: a reader that joins late, or drops a line, is wrong about nothing.
report "every line is the whole record" 0 \
    "$(jq -e -s 'all(.[]; has("title") and has("queue") and has("media") and has("volume") and has("position") and has("cpu"))' "$WOUT" >/dev/null 2>&1; echo $?)"
report "no nc outlives the stream" 0 "$(pgrep -f "nc(at)? -U $STATE_DIR" 2>/dev/null | wc -l | tr -d ' ')"

# An mpv that DIES says nothing: kill -9 sends no end-file, so the only thing that can end the
# connection is the guard in watch_connect. Under BSD/OpenBSD nc the client leaves on the EOF
# by itself; under ncat it does not (ARCH-player.md「状态流」), and without the guard this
# verb sat on the dead connection forever. The bound is the guard's one-second tick plus the
# reap, with room to spare; "never" is what it separates from.
kq=$(shell/ting-play -d -j --volume 0 --engine yt -- "$SHORT" 2>/dev/null)
kid=$(printf '%s' "$kq" | jq -r '.id // empty')
kpgid=$(printf '%s' "$kq" | jq -r '.pid // empty')
WOUT="$TING_TEST_TMP/watch-kill.out"
shell/ting-play --watch --id "$kid" -j >"$WOUT" 2>/dev/null &
kwpid=$!
if wait_watch 'any(.[]; .ready==true)'; then
    kmpv=$(pgrep -g "$kpgid" -x mpv 2>/dev/null | head -1)
    [ -n "$kmpv" ] && kill -9 "$kmpv" 2>/dev/null
    for i in $(seq 1 40); do kill -0 "$kwpid" 2>/dev/null || break; sleep 0.25; done
    report "--watch ends by itself when its mpv is killed -9" 1 \
        "$(kill -0 "$kwpid" 2>/dev/null && echo 0 || echo 1)"
    report "…and its last line is end" 0 \
        "$(jq -e -s '.[-1].event=="end"' "$WOUT" >/dev/null 2>&1; echo $?)"
else
    bad "the kill -9 player never got ready — the guard is untested"
fi
kill "$kwpid" 2>/dev/null
wait "$kwpid" 2>/dev/null
shell/ting-play --stop --id "$kid" -j >/dev/null 2>&1
report "no nc outlives the killed player's stream" 0 "$(pgrep -f "nc(at)? -U $STATE_DIR" 2>/dev/null | wc -l | tr -d ' ')"

echo "── the death record: real player failure and reaping ───────────────"
# An unresolvable handle fails in the ENGINE, before any mpv exists: the detached child's
# resolve returns the engine's non-zero code (play_url_directly), detached_epitaph records the
# exit event, and ting-play --status reaps it into .failed[]. No synthetic json, no fake log
# stubs: a real engine, a real failure, the real reaper.
f_out=$(shell/ting-play -d -j --engine yt -- "https://www.youtube.com/watch?v=00000000000" 2>/dev/null)
f_id=$(printf '%s' "$f_out" | jq -r '.id // empty')
report "failing player launched" 0 "$([ -n "$f_id" ] && echo 0 || echo 1)"
# Watched from launch: the engine's refusal takes seconds of network, so the watch is attached
# long before the player dies — and it dies before mpv ever answers, so this is also the path
# where `end` has no live state to fall back on. The tombstone is what it reports.
shell/ting-play --watch --id "$f_id" -j >"$TING_TEST_TMP/fwatch.out" 2>/dev/null &
fwpid=$!
for i in $(seq 1 240); do kill -0 "$fwpid" 2>/dev/null || break; sleep 0.25; done
wait "$fwpid"
report "--watch on a failing player exits 0" 0 $?
report "…its end line carries the tombstone" 0 \
    "$(jq -e -s '.[-1] | .event=="end" and .exit_code > 0 and (.reason|type)=="string"' "$TING_TEST_TMP/fwatch.out" >/dev/null 2>&1; echo $?)"
wait_failed() {
    local id=$1 i
    for i in $(seq 1 40); do
        [ "$(shell/ting-play --status -j 2>/dev/null | jq -r --arg i "$id" '[.failed[]|select(.id==$i)]|length')" = "1" ] && return 0
        sleep 0.25
    done
    return 1
}
report "reaper puts dead player in failed[]" 0 "$(wait_failed "$f_id" && echo 0 || echo 1)"
report "death records non-zero exit code" 0 \
    "$(shell/ting-play --status -j | jq -e --arg i "$f_id" '.failed[]|select(.id==$i)|.exit_code > 0' >/dev/null 2>&1; echo $?)"
report "death record identifies engine" "yt" \
    "$(shell/ting-play --status -j | jq -r --arg i "$f_id" '.failed[]|select(.id==$i)|.engine')"

echo
printf '%s: %d ok, %d failed\n' "$(basename "$0")" "$pass" "$fail"
if [ "$fail" -ne 0 ]; then printf 'failures:\n%s' "$FAILED"; exit 1; fi
exit 0
