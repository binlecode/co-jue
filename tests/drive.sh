#!/usr/bin/env bash
# Drive the TUI in a real terminal, then ALWAYS clean up after it.
#
# This is a DRIVER, not a test: it asserts nothing about layout and reports no checks. It
# exists because four mechanical facts have to be got right every single time, and prose
# telling a human or an agent to get them right is a runner nobody executes reliably:
#
#   1. `ting` refuses a non-TTY (exit 1), so it cannot be run from a pipe or a Bash tool
#      call. tmux is the terminal.
#   2. The pane size must be set AT SESSION CREATION (-x/-y). LINES/COLUMNS do nothing — the
#      TUI reads the real ioctl via `stty size </dev/tty` (term_size in shell/ting), so a harness
#      that skips TIOCSWINSZ gets a 0x0 terminal and a one-row list whose frames still look
#      plausible enough to trust.
#   3. `Enter` starts mpv DETACHED, in its own process group. Killing the tmux session does
#      not stop it; the audio keeps playing after everything else is gone. This script's
#      EXIT trap is the whole reason it exists.
#   4. That trap's reach is the state dir, so the state dir has to be this run's own — see
#      the isolation block below. Reaping unconditionally is only safe once nothing else
#      lives there.
#
# The keymap it can send lives in README.md (Keys) and the YT_* knobs in
# ARCH-cli-contract.md「配置面」 — this file restates neither.
#
# Portability: bash 3.2 (macOS system bash). Needs tmux; jq only for the cleanup report.
#
# Usage:
#   tests/drive.sh                                  100x30, default query, dump the frame
#   tests/drive.sh -x 62 -y 20                      a narrower geometry
#   tests/drive.sh -q '周杰伦'                       another query (exercises CJK widths)
#   tests/drive.sh -k 'i'                           send keys after the list is ready
#   tests/drive.sh -k 'Enter' -w Playing            send Enter, wait for the banner (en chrome)
#   tests/drive.sh -k 'Enter' -w 播放中              the same under a zh chrome
#   TING_LANG=en tests/drive.sh -k Enter -w Playing  pin the chrome for one run
#   tests/drive.sh -i                               leave it up and ATTACH (interactive)
#   TING_ASCII=1 tests/drive.sh                     any YT_* var is passed through
#
# -w greps the pane for a string, and the pane speaks whichever chrome language the COPIED
# config (below) or the locale picks — so a banner marker is per language. The language is not
# pinned here, and deliberately: this driver exists to photograph what a human sees, and a
# pinned `en` would make every zh frame it dumps a frame nobody actually gets. An exported
# TING_LANG is forwarded and beats the config file, so pinning is one word at the call site.
# Exit: 0 = reached a ready frame and cleaned up; 1 = never got a frame, or orphans remained.
set -uo pipefail
cd "$(cd -P "$(dirname "$0")/.." && pwd -P)" || exit 1

COLS=100; ROWS=30; QUERY="lofi hip hop"; KEYS=""; WAIT_FOR=""; ATTACH=0
while [ $# -gt 0 ]; do
    case "$1" in
    -x) COLS=$2; shift 2 ;;
    -y) ROWS=$2; shift 2 ;;
    -q) QUERY=$2; shift 2 ;;
    -k) KEYS=$2; shift 2 ;;
    -w) WAIT_FOR=$2; shift 2 ;;
    -i) ATTACH=1; shift ;;
    -h | --help) sed -n '/^# Usage:/,/^# Exit:/p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "drive.sh: unknown argument '$1' (try --help)" >&2; exit 1 ;;
    esac
done

command -v tmux >/dev/null 2>&1 || { echo "drive.sh: tmux is required (ting needs a real tty)" >&2; exit 1; }

# ---- a state dir of this run's own --------------------------------------------------
# Why, once, for all three files under tests/: contract.sh's header. What is specific
# to a DRIVER: the pane holds a real `ting` driving a real `t-play` and a real mpv, so only
# whose state it lands on changes — and their playlists and history still render, because
# TING_STATE_DIR is deliberately NOT redirected (a frame captured here should show the store a
# human sees). What is suppressed is the HISTORY half of that store's write side, via
# TING_HISTORY=0 in the pane: a track this script starts and reaps a second later is not a
# listening, and unlike a player, a log is not something --stop takes back.
#
# THE PLAYLISTS ARE NOT PROTECTED, and nothing here makes them so. The same real store that
# renders in the frame is the one ting's playlist keys write to, so `-k` with a/d/D/R — or a
# human pressing them under `-i` — adds to, deletes from, deletes or renames the user's REAL
# playlists, exactly as it would outside tmux, and there is no undo for it. That is the price
# of frames that show the real store; drive a playlist-editing key only against a store you
# can lose, by exporting TING_STATE_DIR to a scratch dir for that run (it is forwarded).
TEST_TMP=$(mktemp -d "${TMPDIR:-/tmp}/ting-drive.XXXXXX") || exit 1
export TMPDIR="$TEST_TMP"
STATE_DIR="$TMPDIR/ting-$(id -u)"

# The config the pane reads is a COPY of the one a human reads, in this run's own temp dir —
# the same trade as TING_HISTORY=0 above, one level further in. ting WRITES eleven preference keys
# back to the user's config file now (a cycle key that has to be re-pressed every session is
# not a preference), and `-k t`/`-k l` are exactly the keys that would rewrite the developer's
# file as a side effect of driving a frame. Copied rather than left empty because the read
# side is the whole point of not redirecting TING_STATE_DIR either: a frame captured here should
# show the theme and the chrome language a human actually has.
DRIVE_CFG="$TEST_TMP/config"
cp "${TING_CONFIG:-${XDG_CONFIG_HOME:-$HOME/.config}/ting/config}" "$DRIVE_CFG" 2>/dev/null ||
    : >"$DRIVE_CFG"

S="drive-$$"

# The point of this script. A leaked player outlives the shell, the session, and the terminal,
# so the trap runs on EVERY exit path — including the ready-frame timeout below.
cleanup() {
    local rc=0
    tmux kill-session -t "$S" 2>/dev/null
    # Unconditional. It used to run only when the keys contained Enter, because --stop --all
    # reached the user's players and reaping without cause was the worse bug — which meant `-i`,
    # the one mode where a HUMAN presses Enter, never reaped at all. The state dir above is what
    # retires that trade: there is nothing here but this run's own players.
    shell/t-play --stop --all -j >/dev/null 2>&1
    # Report rather than assume: --stop is idempotent, but an mpv that escaped its record would
    # not be reaped by it, and that is exactly the failure worth seeing. Scoped to this run's
    # socket dir, as playback.sh scopes its own: a bare `mpv .*--input-ipc-server` counts the
    # user's players too, so on any machine where ting is actually used it was a coin toss.
    if pgrep -f "mpv .*--input-ipc-server=$STATE_DIR" >/dev/null 2>&1; then
        echo "drive.sh: ORPHAN mpv still running after --stop --all:" >&2
        pgrep -fl "mpv .*--input-ipc-server=$STATE_DIR" >&2
        rc=1
    else
        echo "drive.sh: this run's players stopped, no orphan mpv"
    fi
    rm -rf "$TEST_TMP"
    return $rc
}
trap 'cleanup || exit 1' EXIT
trap 'exit 130' INT TERM

# The suite's own knobs are forwarded EXPLICITLY, not inherited. A new tmux session does not
# get this shell's environment: it gets the tmux SERVER's, and the server is whichever one is
# already running — often started hours ago from another window. `TING_ASCII=1 tests/drive.sh`
# looked like it worked only when no server was up yet; with one up it silently drove the
# default. Verified by driving TING_STATE_DIR: the pane reported an empty store while the same
# variable listed four playlists outside tmux.
#
# TING_SYNC=0 (tmux and DCS frame sync do not mix), TMPDIR and TING_CONFIG are placed AFTER
# the forwarded block so the driver's own choice wins over an inherited one — TMPDIR because
# the isolation above is not negotiable, and it is not a TING_* name so it is never forwarded
# anyway; TING_CONFIG because a forwarded one would put the pane's writes back on the real file,
# which is the one thing the copy above exists to prevent. An exported TING_CONFIG is still
# honoured where it can do no harm: it picks WHICH file gets copied.
# TING_HISTORY=0 goes BEFORE it: suppressing the log write is a default, not a rule, so
# `TING_HISTORY=1 tests/drive.sh -k Enter` still drives the writing path.
env_prefix=""
while IFS= read -r line; do
    case "$line" in
    TING_*=*)
        _n=${line%%=*}
        _v=${line#*=}
        _v=$(printf '%s' "$_v" | sed "s/'/'\\\\''/g")
        env_prefix="$env_prefix $_n='$_v'"
        ;;
    esac
done < <(env)

tmux new-session -d -s "$S" -x "$COLS" -y "$ROWS" \
    "cd '$PWD' && TING_HISTORY=0$env_prefix TMPDIR='$TMPDIR' TING_CONFIG='$DRIVE_CFG' TING_SYNC=0 shell/ting '$QUERY'"

# Wait on the ready MARKER, never on a sleep: a captured spinner frame is a picture of the
# loading state, not of the layout. A cold yt-dlp search takes ~10s.
#
# The marker is the TITLE line's source field, not the status line. It was `results=`, and
# the status line no longer spells anything that way — it is segments now, and `40 results`
# is a string a fetch notice could also produce. `query='` is on the one line that only the
# list frame draws, it names the row source rather than a count, and it survives a chrome
# that is still being rearranged. A marker keyed to chrome wording is a coupling either way;
# this one is at least keyed to the part of the chrome that says what the screen IS.
i=0
while [ $i -lt 100 ]; do
    tmux capture-pane -t "$S" -p 2>/dev/null | grep -q "query='" && break
    sleep 0.3; i=$((i + 1))
done
if [ $i -ge 100 ]; then
    echo "drive.sh: never reached a ready frame in 30s — still fetching, or it died:" >&2
    tmux capture-pane -t "$S" -p 2>/dev/null >&2
    exit 1
fi

if [ -n "$KEYS" ]; then
    # -l (literal) is deliberately NOT used: these are key names (Tab, Enter, Down), which is
    # what a driver sends. Literal filter text is an interactive job — use -i for that.
    # shellcheck disable=SC2086
    tmux send-keys -t "$S" $KEYS
    if [ -n "$WAIT_FOR" ]; then
        j=0
        while [ $j -lt 100 ]; do
            tmux capture-pane -t "$S" -p 2>/dev/null | grep -q "$WAIT_FOR" && break
            sleep 0.3; j=$((j + 1))
        done
        [ $j -ge 100 ] && echo "drive.sh: '$WAIT_FOR' never appeared" >&2
    fi
fi

if ((ATTACH)); then
    echo "drive.sh: attaching — press q inside the TUI to quit; cleanup runs on detach."
    trap - INT TERM                     # let tmux own the keyboard while attached
    tmux attach -t "$S"
    exit 0
fi

# SETTLE BEFORE PHOTOGRAPHING — wait until the frame STOPS CHANGING, on every path that
# reaches the capture. This used to be a fixed 0.6s on the one path that waits for nothing,
# which is the path least likely to need it.
#
# Waiting on a marker does not remove the need; it CONCENTRATES it. The capture that matches
# a marker is, by definition, the first one to see the new frame's TOP — so it is biased to
# land inside that frame's paint rather than after it, and polling faster or slower does not
# move the bias. A frame torn that way does not look torn, which is what makes it expensive:
# the top is the view you asked for and the bottom is the view you left, so it reads as a
# renderer that forgot to erase. It is not. `ting` repaints in place — every line erases its
# own tail and the render ends on \033[J (display_list_menu in shell/ting).
#
# THE NUMBER THIS IS SIZED AGAINST, measured 2026-08-30 at 100x30 by sampling the pane every
# 0.1s from the keypress: `i` on a row with 28 chapters spends 2.7s on the fetch (the spinner,
# changing every ~0.1s), writes the frame's head at 2.76s, then goes QUIET before writing the
# rest. That quiet stretch is the trap: it is not a slow trickle with small pauses, it is one
# stall mid-frame — the width layer measuring what the frame is about to print
# (ARCH-tui.md) — so a settle shorter than it photographs the tear no matter how it is
# spelled. A first attempt at 0.6s did, and so did a quiescence test with a 0.25s window. The
# stall itself was 0.88s when this was written and is 0.20s since disp_fits bounded the
# measurement; the window is not re-tuned down, because what it is sized against is the SLOW
# machine, not this one. For scale, the list frame whose 15-24ms repaint display_list_menu
# in shell/ting records is two orders of magnitude away from this one.
#
# So: unchanged across a 1.5s window, not a 1.5s sleep. On this machine the two would behave
# the same; they part on a slower one, where the stall grows and a fixed sleep goes back to
# photographing tears silently while a stability window simply waits longer. Same rule as the
# ready marker above — watch the thing, do not time it.
#
# The bound is a fuse, not the mechanism, and it is sized as one: settling costs at most the
# stall plus the window (0.88 + 1.5 = ~2.4s) on the worst frame measured, so 4s clears it with
# margin. It exists for the frame that CANNOT go still — a running player's clock moves once a
# second, so `-k Enter -w Playing` always spends the fuse and says so. That path is the reason
# the fuse is 4s and not the 10s a first cut used: doubling the cost of the most common
# playback drive to protect a frame that was never going to settle is the wrong trade, and
# photographing after 4s of a ticking clock is no more torn than photographing after 10s.
STILL=0; k=0
prev=$(tmux capture-pane -t "$S" -p 2>/dev/null)
while [ $k -lt 16 ]; do
    sleep 0.25
    cur=$(tmux capture-pane -t "$S" -p 2>/dev/null)
    if [ "$cur" = "$prev" ]; then
        STILL=$((STILL + 1))
        [ $STILL -ge 6 ] && break          # 6 x 0.25s = 1.5s with nothing moving
    else
        STILL=0
    fi
    prev=$cur; k=$((k + 1))
done
[ $k -ge 16 ] && echo "drive.sh: frame still moving after 4s (a running player's clock does that) — photographing it anyway" >&2
echo "── ${COLS}x${ROWS}  query=$QUERY${KEYS:+  keys=$KEYS} ──"
tmux capture-pane -t "$S" -p
# Let ting quit on its own — it reaps its own player on the way out — before the trap kills
# the session. Polled on the pane's command having exited, which is what ting returning is:
# the session closes then, or — under a tmux server with remain-on-exit on — stays up with a
# dead pane, so both end the wait. Bounded, because a ting that ignores q must not hang the
# driver; the trap reaps whatever is left either way.
tmux send-keys -t "$S" 'q' 2>/dev/null
k=0
while [ $k -lt 20 ] && tmux has-session -t "$S" 2>/dev/null &&
    [ "$(tmux display-message -p -t "$S" '#{pane_dead}' 2>/dev/null)" != "1" ]; do
    sleep 0.1; k=$((k + 1))
done
exit 0
