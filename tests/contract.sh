#!/usr/bin/env bash
# The CLI contract, asserted by RUNNING it. No rig: a command-line tool is tested by
# invoking it and reading its exit code and its stdout, which is all this file does.
#
# What it covers: the search envelope's shape, every documented rejection (a flag on the
# wrong verb, a bare query where a URL belongs, two actions at once, a selector with no
# action), the read-only --transcript verb both ways, the idle lifecycle, the shape of the
# tombstone record (its behavior is playback.sh's), --version, the non-TTY refusal, the failure
# taxonomy — 1 is usage, 2 is a tool that failed, and the two engines' envelopes agreeing
# key for key — and the documented PIPELINES between commands, run rather than printed
# (ARCH-cli-contract.md「调用面」; the one that launches a player is playback.sh's).
#
# This replaced a skill that carried the same commands as prose for an agent to copy out by
# hand. That version rotted silently: it listed a resident socket server as a check (it hangs
# and asserts nothing), asserted the same exit code in two phases, described the network path
# in a sentence with no command behind it, referenced capture files that were never made, and
# had no coverage at all for --transcript. A test suite that cannot be executed reports green
# by default, which is worse than having none.
#
# Portability: bash 3.2 (macOS system bash). No bash-4 idioms; see docs/ARCHITECTURE.md「可移植性契约」.
#
# Cost, measured 2026-09-23 on the author's machine: ~195s in full (665 checks), of which
# `--offline` is the first ~48s with no packet sent. The live half is dominated by real
# engine round trips (2-8s each) and the tmux TUI panes, which each wait for a real search and
# a real first frame. The total count moves by a few between runs, and that is not sloppiness:
# a handful of checks report only when today's results give them something to report (a
# chapterless row, a view that opens, a parts view to read a total off), and the alternative
# to skipping them is a check that cannot fail on the days the site is generous. A skip line
# names each one, and 0 failed is the number that means passing. The biggest single item in
# the offline half is one deliberate 5.5s lock spin — a FRESH held lock has to be waited out,
# that being what the spin is for; the stale-lock steal beside it costs 0.1s because
# staleness is tested before the spin, not after (shell/t-playlist:lock_playlist).
#
# Per-SECTION figures are deliberately absent: this file's output is block-buffered the moment
# it is piped or redirected, so timestamping its section headers dates the flush, not the work.
# The two totals above are wall-clock around the whole command, which is the only shape of
# this measurement that survives being taken.
#
# Numbers in this paragraph have been wrong before, three at once — ~80s for the full run,
# "one 5s lock spin" where three were paid, "~25s of tmux" for 4s of it — which is its own
# lesson: a cost comment is a claim, and this file's rule is that a claim gets executed, not
# read.
# It starts no process it did not have to and talks to no peer — every live claim is
# tests/playback.sh's.
# The TUI section is the near-exception and is held to the same line: the process there is a
# real `ting` on a real tty, it is CHECKED to leave no player behind, and the EXIT trap reaps
# one if it ever does.
#
# Usage:  tests/contract.sh            all checks
#         tests/contract.sh --offline  the hermetic half only — every gate, both stores, the
#                                      idle lifecycle, and no packet sent
#         tests/contract.sh --only undo  the TUI undo pane alone (`z`, the held-back stop, the
#                                      queue keys, the zh chrome): one real search, a real
#                                      player, ~50s. The same checks the full run makes
# Exit:   0 = every check held, 1 = at least one regression

set -uo pipefail
cd "$(cd -P "$(dirname "$0")/.." && pwd -P)" || exit 1

# --offline exists because CLAUDE.md asks for this file on ANY change at all, including a
# comment fix, and a gate that cannot run without YouTube is a gate people learn to skip. It
# is a PREFIX of the same run, never a different one: the hermetic checks are the same
# checks, in the same order, and the flag only stops before the first live call. What it
# gives up is stated where it stops, so nobody mistakes a green --offline for a green suite.
OFFLINE=0
# --only runs ONE section and nothing else, for the loop of editing that section. It is not a
# second suite: it calls the same function the full run calls, so the two cannot disagree
# about what the checks are. One name today; a name it does not know is refused, not ignored.
ONLY=""
while [ $# -gt 0 ]; do
    case "$1" in
    --offline) OFFLINE=1; shift ;;
    --only)
        [ $# -ge 2 ] || { echo "contract.sh: --only needs a section name (undo)" >&2; exit 1; }
        case "$2" in
        undo) ONLY=$2 ;;
        *) echo "contract.sh: --only knows one section: undo (got '$2')" >&2; exit 1 ;;
        esac
        shift 2 ;;
    -h | --help) sed -n '/^# Usage:/,/^# Exit:/p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "contract.sh: unknown argument '$1' (try --help)" >&2; exit 1 ;;
    esac
done
if [ "$OFFLINE" = 1 ] && [ -n "$ONLY" ]; then
    echo "contract.sh: --offline and --only cannot be combined (the undo pane needs a real search)" >&2
    exit 1
fi

# ---- the player's state dir, pointed somewhere disposable ---------------------------
# THE ARGUMENT FOR THIS LIVES HERE, and the other two files under tests/ point at it rather
# than restating it. ARCHITECTURE.md「风险登记」 carries the one-line risk row; this is its why.
#
# `t-play` derives its state dir from TMPDIR ("${TMPDIR:-/tmp}/ting-$(id -u)", shell/t-play)
# and takes no override of its own. Left at the user's real TMPDIR, this file --stop --all's a
# player they are listening to, touches players in their real players/, and
# rm -rf's their real failure record — three side effects on live user state, in a suite whose
# instruction is "run it before every commit".
#
# Redirecting TMPDIR is what makes that instruction safe. It changes nothing about WHAT is
# invoked or asserted: the player is the real one and its state is really written, just not on
# top of the user's. The playlist store already had this in UT_STATE_DIR; the half that kills
# processes is the half that needed it more.
UT_TEST_TMP=$(mktemp -d "${TMPDIR:-/tmp}/ting-contract.XXXXXX") || exit 1
export TMPDIR="$UT_TEST_TMP"
STATE_DIR="$TMPDIR/ting-$(id -u)"

# ---- the config file, pointed somewhere disposable ----------------------------------
# The same argument as TMPDIR above, one layer out. Every command in the suite now reads
# ${XDG_CONFIG_HOME:-~/.config}/ting/config, so without this line a developer whose real
# config sets UT_MAX_SEARCH_RESULTS or UT_SORT_FIELD would see this file go red on their
# machine and green on everyone else's — the worst failure a suite can have, because the
# red is not in the subject. It points at a real, EMPTY file rather than a missing path so
# the loader's read path is the one exercised for the rest of the run; the checks that
# prove the loader actually loads something write their own file and set UT_CONFIG
# themselves.
# TING_* is DROPPED rather than mirrored, and that is the whole isolation story in one line:
# every export below is a UT_ name, and TING_ now outranks UT_ in the loader — so a developer
# with TING_CONFIG or TING_STATE_DIR exported would have this file's own redirection silently
# overruled and would run against their real files. The checks that prove the TING_ names work
# set them themselves, one command at a time.
unset TING_CONFIG TING_STATE_DIR
export UT_CONFIG="$UT_TEST_TMP/config"
: > "$UT_CONFIG"

# ---- …and the real one, WATCHED --------------------------------------------------------
# The export above redirects every command this shell runs. It does not reach a tmux pane —
# a new session inherits the tmux SERVER's environment, not this shell's, which is why the
# TUI section passes its own knobs explicitly — and it does not reach whatever a future check
# forks in a way nobody predicted here. That gap used to be harmless because nothing in the
# suite WROTE a config; ting now writes eleven preference keys back to the user's file, so an
# unisolated caller does not merely read a developer's config, it edits it, and the value it
# leaves is one they never chose.
#
# Two fingerprints around the whole run catch ANY such caller, including one added long after
# this line was written — which is exactly what three call sites each remembering to export
# cannot do. cksum rather than a timestamp: it is POSIX (macOS `stat` and GNU `stat` do not
# share a format string), and the claim is that the file's CONTENT is the one the user left.
#
# BOTH spellings are watched, not just the one this checkout would create. The rename gave
# the loader a two-name chain (ting first, the pre-rename uting second), so which file a
# leaked write lands in depends on the developer's machine, not on this suite — and a guard
# that watches only one of them is green on exactly the machine it was meant to protect.
REAL_CFG="${XDG_CONFIG_HOME:-$HOME/.config}/ting/config"
REAL_CFG_LEGACY="${XDG_CONFIG_HOME:-$HOME/.config}/uting/config"
_cfg_sum() { [ -r "$1" ] && cksum < "$1" || echo absent; }
cfg_fingerprint() { printf '%s|%s' "$(_cfg_sum "$REAL_CFG")" "$(_cfg_sum "$REAL_CFG_LEGACY")"; }
REAL_CFG_SUM=$(cfg_fingerprint)
# Called before each summary, so both exits make the claim. Absent on both sides is a skip and
# not a green: a machine with no config to damage proves nothing about one that has it. Absent
# then PRESENT is a fail, which is the shape a leaked write takes on that same machine.
report_real_config() {
    if [ "$REAL_CFG_SUM" = "absent|absent" ] && [ "$(cfg_fingerprint)" = "absent|absent" ]; then
        echo "  skip  (no config at $REAL_CFG or $REAL_CFG_LEGACY to watch)"
        return 0
    fi
    report "your own config is untouched" "$REAL_CFG_SUM" "$(cfg_fingerprint)"
}

# ---- …and the real STATE DIR, watched the same way ------------------------------------
# The config file got this guard when ting learned to write one. The playlist store and the
# listening log have been writable by every check in this file since long before that, and
# they had no guard at all — the discipline was three sections each remembering to point
# UT_STATE_DIR somewhere disposable, which is exactly the kind of discipline that holds until
# it doesn't. It didn't: a section added after the one that ends with `unset UT_STATE_DIR`
# assigned the variable without exporting it, and every t-playlist call in it went to the
# user's real store and left a playlist there.
#
# So the same two fingerprints the config gets, around the same run, over the whole state
# tree. `ls -R` piped through cksum rather than the files' contents: what must not change is
# WHICH lists and logs exist, and a real listening session running in another window will
# legitimately grow today's .jsonl while this file runs. A name appearing or vanishing is the
# shape a leak takes, and it is the shape this catches.
# Both spellings again, and for the same reason the config guard above takes both.
REAL_STATE="${XDG_STATE_HOME:-$HOME/.local/state}/ting"
REAL_STATE_LEGACY="${XDG_STATE_HOME:-$HOME/.local/state}/uting"
state_fingerprint() { ls -R "$REAL_STATE" "$REAL_STATE_LEGACY" 2>/dev/null | cksum; }
REAL_STATE_SUM=$(state_fingerprint)
report_real_state() {
    if [ ! -d "$REAL_STATE" ] && [ ! -d "$REAL_STATE_LEGACY" ]; then
        echo "  skip  (no state dir at $REAL_STATE or $REAL_STATE_LEGACY to watch)"
        return 0
    fi
    report "your own store is untouched" "$REAL_STATE_SUM" "$(state_fingerprint)"
}

# The reap comes FIRST and the directory second — the order playback.sh's cleanup already
# uses, and for a reason this file learned the hard way. Nothing here presses Enter, but the
# TUI section runs a real `ting`, and on 2026-08-25 a run whose `q` check came back red left
# an `t-play --engine yt -f audio` child and its mpv behind. With `rm -rf` as the whole of
# the cleanup, the player's RECORD went with the directory: the process was orphaned to PID 1
# and `--stop --all` could no longer reach it — a suite that "does not touch your state" had
# left audio running that nothing but `kill` could stop.
#
# The orphan report is scoped to this run's own socket dir, for the reason playback.sh scopes
# its own: a bare `mpv .*--input-ipc-server` counts the user's players too. It is a report and
# not a check because the CHECK for it is in the TUI section, where it can name the cause.
cleanup() {
    shell/t-play --stop --all -j >/dev/null 2>&1
    if pgrep -f "mpv .*--input-ipc-server=$STATE_DIR" >/dev/null 2>&1; then
        echo "contract.sh: ORPHAN mpv still running after --stop --all:" >&2
        pgrep -fl "mpv .*--input-ipc-server=$STATE_DIR" >&2
    fi
    rm -rf "$UT_TEST_TMP"
    return 0
}
# INT/TERM exit rather than run the cleanup and carry on: the reap must not happen with the
# rest of the file still to run. Same two lines as drive.sh, and the EXIT trap does the work.
trap cleanup EXIT
trap 'exit 130' INT TERM

pass=0; fail=0
FAILED=""

# report <name> <want> <got>   — the only bookkeeping in this file.
report() {
    if [ "$2" = "$3" ]; then
        pass=$((pass + 1))
        printf '  ok    %-34s %s\n' "$1" "$3"
    else
        fail=$((fail + 1))
        FAILED="${FAILED}    ${1}: want ${2}, got ${3}"$'\n'
        printf '  FAIL  %-34s want %s, got %s\n' "$1" "$2" "$3"
    fi
}

# Four one-liners, and deliberately nothing else. Each runs a real entry point the way a
# caller does and hands back one value for report() to compare.
#
# There is no watchdog: a wedged network call hangs this file until you interrupt it. That is
# the accepted cost of having no orchestration to get wrong — the hand-rolled timeout that
# used to live here inferred "did it fire?" from a background process's liveness and was
# wrong in both directions (a command that succeeded at the boundary aborted the whole run;
# a real timeout was reported as a content mismatch).

# summary                        — the tally, and the exit status the whole file is. A
# function because --offline leaves early and the two exits must be the same words and the
# same status; a second copy of five lines is how the two of them would drift apart.
summary() {
    echo
    printf '%s: %d ok, %d failed\n' "$(basename "$0")" "$pass" "$fail"
    if [ "$fail" -ne 0 ]; then
        printf 'regressions:\n%s' "$FAILED"
        exit 1
    fi
    exit 0
}

# rc <command...>                 — its exit code, output discarded.
rc() { "$@" >/dev/null 2>&1; echo $?; }

# rc_in <payload> <command...>    — the same, for a verb that READS STDIN. `rc` cannot serve
# those: the payload has to arrive on the command's own stdin, and for these verbs an empty
# stdin is a DIFFERENT error with the SAME exit code — a check that would pass for the wrong
# reason. Measured, not reasoned: "idle --enqueue is 4" once came back 1 this way.
rc_in() { local payload=$1; shift; printf '%s' "$payload" | "$@" >/dev/null 2>&1; echo $?; }

# jqv <jq-filter> <json>          — does an envelope already in hand satisfy the filter?
jqv() { printf '%s' "$2" | jq -e "$1" >/dev/null 2>&1; echo $?; }

# jq_ok <jq-filter> <command...>  — run it, then filter what it printed. The output is
# captured FIRST and never piped straight from the command: `set -o pipefail` makes a
# pipeline carry the LEFT side's status, so `yt-resolve … | jq -e …` reports the command's
# own exit as jq's verdict. Both error-path checks below went red against correct behaviour
# that way.
jq_ok() { local f=$1; shift; local o; o=$("$@" 2>/dev/null); jqv "$f" "$o"; }

# jq_in <jq-filter> <payload> <command...>  — jq_ok for a verb that reads STDIN. Same
# capture-first discipline and the same reason, so the reason is stated once, above.
jq_in() { local f=$1 p=$2; shift 2; jqv "$f" "$(printf '%s' "$p" | "$@" 2>/dev/null)"; }

# lines <json>                    — 1 for a single-line envelope.
lines() { printf '%s\n' "$1" | wc -l | tr -d ' '; }

# err_has <pattern> <command...>  — does what it printed on STDERR match? 0 yes, 1 no.
# Captured FIRST, never piped straight from the command, and for a sharper version of
# jq_ok's reason: every command asked this question is a REFUSAL, so it exits non-zero BY
# CONSTRUCTION. Under `set -o pipefail` the pipeline then carries the refusal's status and
# the grep verdict is thrown away — which is not a hypothetical, it is how the two
# capability checks below first read GREEN against an engine that had neither verb.
err_has() {
    local pat=$1
    shift
    local e
    e=$("$@" 2>&1 >/dev/null)
    printf '%s' "$e" | grep -qi -- "$pat" && echo 0 || echo 1
}

# tmux is the tty for every pane below; a machine without it skips them, it does not fail.
tmux_ok() {
    command -v tmux >/dev/null 2>&1 || return 1
    local _probe="ctest-probe-$$"
    if tmux new-session -d -s "$_probe" "exit 0" 2>/dev/null; then
        tmux kill-session -t "$_probe" 2>/dev/null || true
        return 0
    fi
    return 1
}

# ONE POLLER FOR EVERY PANE IN THIS FILE. There used to be twenty-five copies of the same five-line
# loop, each with its own counter, its own `sleep 0.25` and its own spelling of "did it
# happen yet" — and a quarter of a second is a terrible granularity to watch a 15-25ms redraw
# with: every one of those polls missed its first capture and then slept 250ms, so the
# section spent seconds of wall clock waiting for something that had already happened. The
# BUDGET is unchanged (the numbers below are seconds, and they are the same seconds the
# counters spelled as `-lt 40` × 0.25); only the granularity moved.
#
# `poll_until <secs> <predicate…>` echoes 1 the moment the predicate holds and 0 when the
# budget is gone, which is exactly the shape `report` wants — so a check is one line and
# cannot drift from the poll that fed it.
poll_until() {
    local secs=$1 n i=0; shift
    n=$((secs * 20))
    while [ $i -lt $n ]; do
        "$@" >/dev/null 2>&1 && { echo 1; return 0; }
        sleep 0.05; i=$((i + 1))
    done
    echo 0
}
# The predicates. `-J` everywhere (join wrapped lines) so a pattern cannot miss because the
# terminal folded the line it was on.
pane_has()   { tmux capture-pane -t "$TS" -p -J 2>/dev/null | grep -qE "$1"; }
pane_lacks() { ! pane_has "$1"; }
# Left a room and came back: the pane must no longer show the room's marker AND must show the
# one it returned to. Both halves, because a view that never changed still shows the second.
pane_back()  { pane_lacks "$1" && pane_has "$2"; }
cfg_has()    { grep -qE "$1" "$TUI_CFG"; }

# The undo pane, as a function so `--only undo` can run it without the rest of the file
# (see the dispatch just below). It is CALLED where it always ran, inside the live TUI block.
undo_pane() {
    # ── Undo replaces the confirmation: `z` ─────────────────────────────────────────────
    # d, D and X used to stop the frame for a y/N; now they act on one key and the frame
    # offers `z` for three seconds (ARCH-tui.md「可撤销取代预先确认」). What is claimed here is
    # the TUI half — the offer is on the first frame and gone when `z` stops working, `z`
    # puts back what the store says it restored, and the one side effect a copy cannot restore
    # (a stopped player) is held back until the offer closes. The store's own half is proved
    # command by command in the offline section and in playback.sh.
    #
    # A pane of its own, because the held-back stop needs a REAL player and the pane above is
    # the one whose row and banner assertions must not be moved by one. Every list is made by
    # real commands: a real search envelope into the store, and the one-track list by `a`, the
    # key itself. Every "is the list as it was" is read off the store, never the frame.
    UNDO_STATE=$(mktemp -d "${TMPDIR:-/tmp}/ting-undostore.XXXXXX")
    UNDO_CFG="$UT_TEST_TMP/undo-config"
    : >"$UNDO_CFG"
    printf '%s' "$YT_S" | UT_STATE_DIR="$UNDO_STATE" shell/t-playlist --add undo-list -j >/dev/null 2>&1
    [ "$(UT_STATE_DIR="$UNDO_STATE" shell/t-playlist --show undo-list -j 2>/dev/null | jq -r '.count // 0')" -ge 3 ] ||
        { echo "contract.sh: the undo list did not seed — suite error, not a failure" >&2; exit 1; }
    ul_show() { UT_STATE_DIR="$UNDO_STATE" shell/t-playlist --show "$1" -j 2>/dev/null; }
    ul_count() { ul_show "$1" | jq -r '.count // "none"'; }
    TS="ctest-undo-$$"
    tmux kill-session -t "$TS" 2>/dev/null
    tmux new-session -d -s "$TS" -x 100 -y 30 \
        "cd '$PWD' && env YT_SYNC=0 UT_HISTORY=0 TMPDIR='$TMPDIR' UT_STATE_DIR='$UNDO_STATE' TING_STATE_DIR='$UNDO_STATE' UT_CONFIG='$UNDO_CFG' TING_CONFIG='$UNDO_CFG' YT_LANG=en shell/ting --volume 0 'lofi hip hop'; printf 'RC=%s\n' \$?; sleep 20"
    undo_up=$(poll_until 40 pane_has "query='")
    report "the undo pane paints a list" 1 "$undo_up"
    if [ "$undo_up" = 1 ]; then
        # Row numbers on, so "the cursor is on row 2" is a thing the frame can say.
        tmux send-keys -t "$TS" '#'
        tmux send-keys -t "$TS" b
        poll_until 10 pane_has '1\. undo-list' >/dev/null
        tmux send-keys -t "$TS" 1 Enter
        report "undo: the list opens" 1 "$(poll_until 10 pane_has "playlist='undo-list'")"
        U_BEFORE=$(ul_show undo-list)
        tmux send-keys -t "$TS" Down
        poll_until 5 pane_has '^[>▶▎] +2\.' >/dev/null
        # d on row 2: one key, no question, and the offer on the very next frame.
        tmux send-keys -t "$TS" d
        report "d offers z on the first frame" 1 "$(poll_until 10 pane_has 'Removed from .* z to undo')"
        report "…and asks nothing" 1 "$(pane_lacks 'y/N' && echo 1 || echo 0)"
        report "…and the store lost the row" "$(($(printf '%s' "$U_BEFORE" | jq -r '.count') - 1))" "$(ul_count undo-list)"
        tmux send-keys -t "$TS" z
        report "z says it undid it" 1 "$(poll_until 10 pane_has 'Undo: Undone')"
        report "…the list is byte-for-byte back" 0 "$([ "$(ul_show undo-list)" = "$U_BEFORE" ]; echo $?)"
        report "…and the cursor is on the row it put back" 1 "$(poll_until 5 pane_has '^[>▶▎] +2\.')"
        # The offer is visible exactly while it can act: once the hint is gone, z says so and
        # changes nothing. The wait is on the hint leaving the frame, not on a clock.
        tmux send-keys -t "$TS" d
        poll_until 10 pane_has 'z to undo' >/dev/null
        report "the offer leaves the frame by itself" 1 "$(poll_until 8 pane_lacks 'z to undo')"
        U_AFTER=$(ul_show undo-list)
        tmux send-keys -t "$TS" z
        report "…after which z has nothing to undo" 1 "$(poll_until 10 pane_has 'nothing to undo')"
        report "…and the list keeps the removal" 0 "$([ "$(ul_show undo-list)" = "$U_AFTER" ]; echo $?)"
        # A run of d leaves only the LAST one undoable: one slot, never a stack. Each d is
        # waited out on the store's own count, because the offer text looks the same twice.
        ul_count_is() { [ "$(ul_count undo-list)" = "$1" ]; }
        U_N=$(ul_count undo-list)
        tmux send-keys -t "$TS" d
        poll_until 10 ul_count_is $((U_N - 1)) >/dev/null
        U_MID=$(ul_show undo-list)
        tmux send-keys -t "$TS" d
        poll_until 10 ul_count_is $((U_N - 2)) >/dev/null
        tmux send-keys -t "$TS" z
        poll_until 10 pane_has 'Undo: Undone' >/dev/null
        report "two d then z: only the second comes back" 0 "$([ "$(ul_show undo-list)" = "$U_MID" ]; echo $?)"
        # The store moves under the offer — a real write from this shell, the way an agent or a
        # second ting would make it — and z must refuse, say why, and leave that write alone.
        tmux send-keys -t "$TS" d
        poll_until 10 pane_has 'z to undo' >/dev/null
        ul_show undo-list | jq -c '{items: .items[0:1]}' |
            UT_STATE_DIR="$UNDO_STATE" shell/t-playlist --add undo-list -j >/dev/null 2>&1
        U_MOVED=$(ul_show undo-list)
        tmux send-keys -t "$TS" z
        report "z on a list changed elsewhere says so" 1 "$(poll_until 10 pane_has 'changed elsewhere, not undone')"
        report "…and keeps the other writer's change" 0 "$([ "$(ul_show undo-list)" = "$U_MOVED" ]; echo $?)"
        U_AFTER=$U_MOVED
        # R, then z: the name on screen goes back with the file.
        tmux send-keys -t "$TS" R
        poll_until 10 pane_has 'New name for playlist' >/dev/null
        tmux send-keys -t "$TS" undo-renamed Enter
        poll_until 10 pane_has "playlist='undo-renamed'" >/dev/null
        tmux send-keys -t "$TS" z
        report "R then z: the title is the old name" 1 "$(poll_until 10 pane_has "playlist='undo-list'")"
        report "…and so is the store" '["undo-list"]' \
            "$(UT_STATE_DIR="$UNDO_STATE" shell/t-playlist --ls -j 2>/dev/null | jq -c '[.playlists[].name]')"
        # D, then z from the search it left us on: the list is reopened.
        tmux send-keys -t "$TS" D
        report "D lands on the search with the offer" 1 "$(poll_until 10 pane_has 'Deleted playlist .* z to undo')"
        tmux send-keys -t "$TS" z
        report "…and z reopens the list" 1 "$(poll_until 10 pane_has "playlist='undo-list'")"
        report "…which the store has again" 0 "$([ "$(ul_show undo-list)" = "$U_AFTER" ]; echo $?)"
        # `a` makes the one-track list, and its own undo is proved on the way: z on a list the
        # add CREATED takes the list away again.
        tmux send-keys -t "$TS" b
        poll_until 10 pane_back "playlist='" "query='" >/dev/null
        tmux send-keys -t "$TS" a
        poll_until 10 pane_has 'Add to which' >/dev/null
        tmux send-keys -t "$TS" undo-solo Enter
        report "a offers z" 1 "$(poll_until 10 pane_has 'Added to .* z to undo')"
        tmux send-keys -t "$TS" z
        poll_until 10 pane_has 'Undo: Undone' >/dev/null
        report "…and z unmakes the list it made" none "$(ul_count undo-solo)"
        tmux send-keys -t "$TS" a
        poll_until 10 pane_has 'Add to which' >/dev/null
        tmux send-keys -t "$TS" undo-solo Enter
        poll_until 10 pane_has 'Added to' >/dev/null
        report "a made the one-track list" 1 "$(ul_count undo-solo)"
        # Removing the last track: back to the search, no question, the empty-list sentence
        # AND the offer on one line — and z brings the track back into the list it reopens.
        tmux send-keys -t "$TS" b
        poll_until 10 pane_has 'undo-solo' >/dev/null
        tmux send-keys -t "$TS" undo-solo Enter
        poll_until 10 pane_has "playlist='undo-solo'" >/dev/null
        U_SOLO=$(ul_show undo-solo)
        tmux send-keys -t "$TS" d
        solo_emptied() { pane_has "query='" && pane_has 'that playlist is empty .* z to undo'; }
        report "the last track out: the search, empty and undoable" 1 "$(poll_until 10 solo_emptied)"
        report "…with no question about deleting it" 1 "$(pane_lacks 'y/N' && echo 1 || echo 0)"
        tmux send-keys -t "$TS" z
        report "…and z reopens it with the track" 1 "$(poll_until 10 pane_has "playlist='undo-solo'")"
        report "…as the store has it" 0 "$([ "$(ul_show undo-solo)" = "$U_SOLO" ]; echo $?)"
        # Let the empty list stand: it is still a list, and the picker still offers it.
        tmux send-keys -t "$TS" d
        poll_until 10 pane_has 'z to undo' >/dev/null
        poll_until 8 pane_lacks 'z to undo' >/dev/null
        tmux send-keys -t "$TS" b
        report "an emptied list stays in the picker" 1 "$(poll_until 10 pane_has 'undo-solo +0 items')"
        tmux send-keys -t "$TS" Escape
        # Waited out before the next key: an Esc with a letter right behind it is Alt+letter to
        # the reader, and the `b` below would be eaten as the tail of one.
        poll_until 10 pane_lacks 'Open which' >/dev/null
        # THE HELD-BACK STOP. Round one takes no undo: the player must survive the offer and
        # stop when it closes. Round two takes it: the player must survive the offer's END,
        # which is proved by outliving a second offer (the next d's) that closes after it.
        ul_players() { shell/t-play --status -j 2>/dev/null | jq '.players | length'; }
        tmux send-keys -t "$TS" b
        poll_until 10 pane_has '[0-9]\. undo-list' >/dev/null
        tmux send-keys -t "$TS" undo-list Enter
        poll_until 10 pane_has "playlist='undo-list'" >/dev/null
        tmux send-keys -t "$TS" Enter
        undo_playing=$(poll_until 40 pane_has 'Playing: ')
        report "the undo pane's player starts" 1 "$undo_playing"
        if [ "$undo_playing" != 1 ]; then
            echo "  ---- undo pane when its player did not start ----" >&2
            tmux capture-pane -t "$TS" -p -J >&2 2>/dev/null
            echo "  ---- end of pane ----" >&2
        fi
        if [ "$undo_playing" = 1 ]; then
            tmux send-keys -t "$TS" d
            poll_until 10 pane_has 'z to undo' >/dev/null
            report "d on the playing row: it plays on while z is offered" 1 \
                "$( [ "$(ul_players)" = 1 ] && pane_has 'Playing: ' && echo 1 || echo 0)"
            poll_until 8 pane_lacks 'z to undo' >/dev/null
            report "…and stops when the offer closes" 1 "$(poll_until 10 pane_lacks 'Playing: ')"
            report "…the player is gone, not just the banner" 0 "$(ul_players)"
            tmux send-keys -t "$TS" Enter
            poll_until 40 pane_has 'Playing: ' >/dev/null
            tmux send-keys -t "$TS" d
            poll_until 10 pane_has 'z to undo' >/dev/null
            tmux send-keys -t "$TS" z
            poll_until 10 pane_has 'Undo: Undone' >/dev/null
            tmux send-keys -t "$TS" Down
            tmux send-keys -t "$TS" d
            poll_until 10 pane_has 'z to undo' >/dev/null
            poll_until 8 pane_lacks 'z to undo' >/dev/null
            report "d then z on the playing row: it never stops" 1 \
                "$( [ "$(ul_players)" = 1 ] && pane_has 'Playing: ' && echo 1 || echo 0)"
            # `+` is the queue's write with an undo, and it is on the same player.
            tmux send-keys -t "$TS" +
            poll_until 10 pane_has 'Queued .* z to undo' >/dev/null
            tmux send-keys -t "$TS" z
            poll_until 10 pane_has 'Undo: Undone' >/dev/null
            report "+ then z: the queue is one track again" 1 \
                "$(shell/t-play --status -j 2>/dev/null | jq -r '.players[0].queue.len // empty')"
            # ONE SLOT ACROSS BOTH STORES: a queue write after a playlist write drops the
            # playlist's copy, so z takes back the + and the d before it stays done.
            q_len_is() { [ "$(shell/t-play --status -j 2>/dev/null | jq -r '.players[0].queue.len // empty')" = "$1" ]; }
            tmux send-keys -t "$TS" d
            poll_until 10 pane_has 'Removed from .* z to undo' >/dev/null
            U_N=$(ul_count undo-list)
            tmux send-keys -t "$TS" +
            poll_until 10 q_len_is 2 >/dev/null
            tmux send-keys -t "$TS" z
            report "d, +, z: z takes back the +" 1 "$(poll_until 10 q_len_is 1)"
            report "…and not the d before it" "$U_N" "$(ul_count undo-list)"
            # The queue view's own keys. A tail of two to edit, then the queue from the search
            # (u opens from search rows only). Every "came home" is the whole url order.
            q_urls() { shell/t-play --queue-show -j 2>/dev/null | jq -c '[.items[].url]'; }
            q_state() { shell/t-play --queue-show -j 2>/dev/null | jq -c '{pos, urls: [.items[].url]}'; }
            tmux send-keys -t "$TS" +
            poll_until 10 q_len_is 2 >/dev/null
            tmux send-keys -t "$TS" Down
            tmux send-keys -t "$TS" +
            poll_until 10 q_len_is 3 >/dev/null
            tmux send-keys -t "$TS" b
            poll_until 10 pane_back "playlist='" "query='" >/dev/null
            tmux send-keys -t "$TS" u
            report "u opens the queue for its keys" 1 "$(poll_until 15 pane_has "queue='")"
            Q0=$(q_urls)
            tmux send-keys -t "$TS" Down
            poll_until 5 pane_has '^[>▶▎] +2\.' >/dev/null
            tmux send-keys -t "$TS" x
            report "x offers z" 1 "$(poll_until 10 pane_has 'Removed from the queue .* z to undo')"
            tmux send-keys -t "$TS" z
            poll_until 10 pane_has 'Undo: Undone' >/dev/null
            report "…and z puts the track back in its slot" "$Q0" "$(q_urls)"
            tmux send-keys -t "$TS" X
            report "X asks nothing and offers z" 1 "$(poll_until 10 pane_has 'Cleared the queue .* z to undo')"
            tmux send-keys -t "$TS" z
            poll_until 10 pane_has 'Undo: Undone' >/dev/null
            report "…and z brings the whole tail back" "$Q0" "$(q_urls)"
            # A TRACK BOUNDARY between x and z: the player has moved pos, so putting the old
            # bytes back would replay a track. The TUI must say so and change nothing.
            tmux send-keys -t "$TS" Down
            poll_until 5 pane_has '^[>▶▎] +2\.' >/dev/null
            tmux send-keys -t "$TS" x
            poll_until 10 pane_has 'Removed from the queue .* z to undo' >/dev/null
            tmux send-keys -t "$TS" '>'
            q_pos_is() { [ "$(shell/t-play --queue-show -j 2>/dev/null | jq -r '.pos')" = "$1" ]; }
            poll_until 10 q_pos_is 1 >/dev/null
            Q1=$(q_state)
            tmux send-keys -t "$TS" z
            report "x, a skip, then z: refused and said" 1 "$(poll_until 10 pane_has 'changed elsewhere, not undone')"
            report "…and the queue is left as the player has it" "$Q1" "$(q_state)"
            # Back to the playlist, where the exit check below leaves its open copy.
            tmux send-keys -t "$TS" u
            poll_until 10 pane_back "queue='" "query='" >/dev/null
            tmux send-keys -t "$TS" b
            poll_until 10 pane_has '[0-9]\. undo-list' >/dev/null
            tmux send-keys -t "$TS" undo-list Enter
            poll_until 10 pane_has "playlist='undo-list'" >/dev/null
        fi
        # The copies go with the process that could have used them — so one is left OPEN when
        # q lands, or this would pass on a copy some earlier z had already spent.
        tmux send-keys -t "$TS" d
        poll_until 10 pane_has 'z to undo' >/dev/null
        tmux send-keys -t "$TS" q
        report "the undo pane quits with 0" 1 "$(poll_until 10 pane_has 'RC=0')"
        report "…and leaves no undo copy behind" 0 \
            "$(ls -d "$UNDO_STATE"/undo/playlist-* "$TMPDIR/ting-$(id -u)"/undo-queue-* 2>/dev/null | wc -l | tr -d ' ')"
        # The zh chrome, on a pane of its own: every string above was the en one, and the
        # offer is a sentence the user reads, so its other spelling is proved too — first
        # frame and after z, the two places it appears.
        tmux kill-session -t "$TS" 2>/dev/null
        tmux new-session -d -s "$TS" -x 100 -y 30 \
            "cd '$PWD' && env YT_SYNC=0 UT_HISTORY=0 TMPDIR='$TMPDIR' UT_STATE_DIR='$UNDO_STATE' TING_STATE_DIR='$UNDO_STATE' UT_CONFIG='$UNDO_CFG' TING_CONFIG='$UNDO_CFG' YT_LANG=zh shell/ting 'lofi hip hop'; printf 'RC=%s\n' \$?; sleep 20"
        if [ "$(poll_until 40 pane_has "query='")" = 1 ]; then
            tmux send-keys -t "$TS" b
            poll_until 10 pane_has '[0-9]\. undo-list' >/dev/null
            tmux send-keys -t "$TS" undo-list Enter
            poll_until 10 pane_has "playlist='undo-list'" >/dev/null
            U_ZH=$(ul_show undo-list)
            tmux send-keys -t "$TS" d
            report "zh: the offer reads z 撤销" 1 "$(poll_until 10 pane_has '已移出 .* z 撤销')"
            tmux send-keys -t "$TS" z
            report "zh: …and z answers 已撤销" 1 "$(poll_until 10 pane_has '撤销: 已撤销')"
            report "zh: …with the list back" 0 "$([ "$(ul_show undo-list)" = "$U_ZH" ]; echo $?)"
            tmux send-keys -t "$TS" q
            poll_until 10 pane_has 'RC=0' >/dev/null
        else
            report "zh: the undo pane paints a list" 1 0
        fi
    fi
    shell/t-play --stop --all -j >/dev/null 2>&1
    tmux kill-session -t "$TS" 2>/dev/null
    rm -rf "$UNDO_STATE"
}

# --only undo: the pane above and nothing else. It needs the one input the full run hands it
# — a real search envelope, made the way the live preamble makes it (search-yt) — and the
# tally. Placed after every helper and before the first check, so it skips everything and
# borrows nothing it would not get in a full run.
if [ "$ONLY" = undo ]; then
    echo "── only: the undo pane ────────────────────────────────────────────"
    if ! tmux_ok; then
        echo "  skip  (needs tmux for a real tty)"
        summary
    fi
    YT_S=$(shell/yt-search -j -n 10 -- lofi 2>/dev/null)
    [ "$(printf '%s' "$YT_S" | jq -r '.count // 0' 2>/dev/null)" -ge 3 ] ||
        { echo "contract.sh: the search for the undo pane returned too little — suite error, not a failure" >&2; exit 1; }
    undo_pane
    summary
fi

# ---- the offline half, FIRST -------------------------------------------------------
# Everything below this line to the live preamble runs without a network: flag gates,
# the idle lifecycle, the two halves of the user-level store, --version, and the host allowlist.
# It used to sit AFTER ~15 live engine round trips, so the most common regression of all —
# a gate or an envelope broken by the edit you are about to commit — cost 80 seconds to see.
# The gates are still red within the first seconds; the half as a whole is the figure in the
# header, with no network call made.
#
# It is also a HALF you can run on its own, which is the point of --offline: the boundary was
# already load-bearing, and a boundary nobody can stop at is a boundary only the author uses.
#
# THE ORDER IS PART OF THE FILE, not an accident of how it grew:
#   · offline first, so a broken gate is red before anything is fetched;
#   · the TUI section LAST, driven in a real tmux session.
# Moving a section is therefore a deliberate act.
echo "── rejections (1 = usage error) ───────────────────────────────────"
report "core no args"             1 "$(rc /bin/bash shell/t-play)"
report "yt-search no args"        1 "$(rc /bin/bash shell/yt-search)"
report "yt-search --detach"       1 "$(rc shell/yt-search --detach -- x)"
report "yt-search -f audio"       1 "$(rc shell/yt-search -f audio -- x)"
report "t-play bare query"       1 "$(rc shell/t-play "a query")"
report "t-play -n"               1 "$(rc shell/t-play -n 5 -- URL)"
report "t-play two actions"      1 "$(rc shell/t-play --status --stop)"
report "t-play selector alone"   1 "$(rc shell/t-play --status --id X)"
report "t-play -d + action"      1 "$(rc shell/t-play -d --stop)"
report "t-play -- <query>"       1 "$(rc shell/t-play -- "a query")"
report "t-play --status rejects a handle" 1 "$(rc shell/t-play --status -- URL)"
report "t-play --stop rejects a handle"   1 "$(rc shell/t-play --stop -- URL)"
# The gating wrapper is gone, so these three are the checks that it took its gate with it
# rather than dropping it: an unknown long flag must not reach getopts as a bare `-`, and
# the two verbs that moved to the engine must name the engine instead of half-working.
report "t-play unknown long flag" 1 "$(rc shell/t-play --json-full -- URL)"
report "--get-url is retired"     1 "$(rc shell/t-play --get-url -- URL)"
report "--info is the engine's"   1 "$(rc shell/t-play --info -- URL)"

echo "── idle lifecycle: exit 0, ONE compact line, idempotent ───────────"
report "--status exit"      0 "$(rc shell/t-play --status -j)"
report "--status one line"  1 "$(shell/t-play --status -j | wc -l | tr -d ' ')"
report "--status is empty"  0 "$(jq_ok '.players==[]' shell/t-play --status -j)"
report "--stop --all exit"  0 "$(rc shell/t-play --stop --all -j)"
report "--stop --all line"  1 "$(shell/t-play --stop --all -j | wc -l | tr -d ' ')"
# --stop treats an empty set as idempotent success; --set-volume must NOT — there is no
# volume it could have set, so this is the did-not-take-effect class (4), and the envelope
# names the why so a caller can tell it from ambiguity (ARCH-cli-contract.md「数据契约」与「退出码」).
report "idle --set-volume is 4"   4 "$(rc shell/t-play --set-volume 50 -j)"
report "idle --set-volume says why" 0 "$(jq_ok '.status=="not_playing"' shell/t-play --set-volume 50 -j)"
# Every socket verb answers the empty set the way --set-volume does — ONE taxonomy, not one
# per verb. Stated as a loop over the verbs so a sixth one is covered the day it lands
# instead of needing its own copied pair of lines.
for v in --pause --resume "--seek +30" "--seek-to 0"; do
    # shellcheck disable=SC2086  # $v carries a flag AND its value on purpose
    report "idle $v is 4"        4 "$(rc shell/t-play $v -j)"
done
# The envelope text comes from ONE helper (require_live_target), so asserting it once per
# verb is raising a count, not covering a case — the exit codes above are what catch a verb
# wired to the wrong helper.
report "idle --pause says why"   0 "$(jq_ok '.status=="not_playing"' shell/t-play --pause -j)"
# THE STOCK CLIENT IS ENOUGH. Every socket verb needs a unix-socket netcat, and macOS ships
# one — but the probe used to be `nc -h | grep -q` under pipefail, which carries nc's own
# exit, and BSD `nc -h` exits 1. So the stock client was refused everywhere and the suite
# passed only because this machine also had nmap's ncat. Here PATH holds jq and the system
# dirs and nothing else: 4 (no player) is the client accepted, 2 is the client refused.
# Skipped, with a line, where the premise is not there to test: no BSD nc with -U in /usr/bin,
# or an ncat in the system dirs that would make the check pass without the nc branch.
NC_ONLY=$(mktemp -d "${TMPDIR:-/tmp}/ting-nconly.XXXXXX")
ln -s "$(command -v jq)" "$NC_ONLY/jq"
# The premise is read the way the fix reads it — captured, then matched: this file runs under
# pipefail too, and `nc -h | grep -q` here would skip on every machine for the same reason.
_nch=""
[ -x /usr/bin/nc ] && _nch=$(/usr/bin/nc -h 2>&1 || true)
if [[ "$_nch" =~ [[:space:]]-U[[:space:]] ]] &&
    ! env "PATH=/usr/bin:/bin" command -v ncat >/dev/null 2>&1; then
    report "the stock nc drives the socket verbs" 4 "$(env "PATH=$NC_ONLY:/usr/bin:/bin" shell/t-play --pause -j >/dev/null 2>&1; echo $?)"
else
    echo "  skip  (no BSD nc with -U in /usr/bin, or an ncat beside it — the stock-client claim cannot be tested here)"
fi
rm -rf "$NC_ONLY"
# The 1-vs-4 split on the one verb that can fail both ways. A malformed value never reaches a
# player, so it is usage (1); a well-formed call with no player to receive it is 4. Getting
# these the same way round is what makes an agent retry a call it should have fixed instead.
report "--seek unsigned is 1"     1 "$(rc shell/t-play --seek 30 -j)"
report "--seek non-numeric is 1"  1 "$(rc shell/t-play --seek abc -j)"
report "--seek-to negative is 1"  1 "$(rc shell/t-play --seek-to -5 -j)"
# --seek -15 is a VALUE, not an unknown flag: the parser must take $2 verbatim.
report "--seek accepts -15"       4 "$(rc shell/t-play --seek -15 -j)"
# --start is the LAUNCH-time offset, and its whole gate is the value one. Whole seconds and
# nothing else: mpv's own --start grammar (-60 counts from the end, 50% is a fraction) is
# deliberately not published on this surface, so every spelling of it that a caller might
# reach for has to come back 1 rather than start somewhere surprising. --start -60 is also
# the mirror of the --seek case above — there a leading dash is a legal VALUE, here it is a
# legal value that this flag refuses, and both go through $2 verbatim.
report "--start negative is 1"    1 "$(rc shell/t-play --start -60 -- URL)"
report "--start hh:mm:ss is 1"    1 "$(rc shell/t-play --start 10:00 -- URL)"
report "--start non-numeric is 1" 1 "$(rc shell/t-play --start abc -- URL)"
report "--start fractional is 1"  1 "$(rc shell/t-play --start 1.5 -- URL)"
report "--start needs a value"    1 "$(rc shell/t-play --start)"
# …and it is refused BESIDE a lifecycle verb rather than silently ignored. Both verbs below
# answer 4 when idle and this call has no player either, so a 1 can only have come from the
# combination gate — the check cannot pass by accident on the idle path.
report "--start with --status is 1" 1 "$(rc shell/t-play --start 60 --status -j)"
report "--start with --seek is 1"   1 "$(rc shell/t-play --start 60 --seek +5 -j)"
# --id now names the playback verbs too, so it has to be ACCEPTED by one of them; the arms
# that reject it elsewhere are already covered by "t-play selector alone" and
# "t-play -d + action" above, which exercise the same case statement.
report "--id on --pause parses"   4 "$(rc shell/t-play --pause --id nope -j)"

# ── the queue verbs, idle. They address a player exactly as the socket verbs do (same
# require_live_target, same 4), but they reach its queue FILE rather than mpv — so they are
# checked here rather than folded into the loop above, and they must answer without nc.
Q1='[{"engine":"yt","url":"https://www.youtube.com/watch?v=jNQXAC9IVRw"}]'
report "idle --next is 4"        4 "$(rc shell/t-play --next -j)"
report "idle --next says why"    0 "$(jq_ok '.status=="not_playing"' shell/t-play --next -j)"
report "idle --enqueue is 4"     4 "$(rc_in "$Q1" shell/t-play --enqueue - -j)"
# The one place a repeated envelope assertion is NOT raising a count: --enqueue can exit 4
# for two different reasons (no such player, or a queue it could not write), and only the
# envelope says which. Proved by making it skip require_live_target — the exit code stayed 4
# and this line is what went red.
report "idle --enqueue says why" 0 "$(jq_in '.status=="not_playing"' "$Q1" shell/t-play --enqueue - -j)"
report "--id on --next parses"   4 "$(rc shell/t-play --next --id nope -j)"

# The 1-vs-4 split again, on the verbs that take a PAYLOAD: a queue this process could not
# parse never reaches a player, so it is usage (1) — and it is refused in the PARENT, which
# is the whole reason stdin is read here and not in the detached child, where a die would
# only reach a log. Driven through --enqueue rather than --queue on purpose: a --queue that
# got past its gate would LAUNCH A PLAYER, and this file starts none. The pairing with
# "idle --enqueue is 4" above is what gives each of these teeth — 1 where the payload is
# wrong, 4 where only the player is missing.
report "bad JSON is 1"           1 "$(rc_in 'not json' shell/t-play --enqueue -)"
report "an empty queue is 1"     1 "$(rc_in '[]' shell/t-play --enqueue -)"
report "a url with a space is 1" 1 "$(rc_in '[{"engine":"yt","url":"a b"}]' shell/t-play --enqueue -)"
report "an empty url is 1"       1 "$(rc_in '[{"engine":"yt","url":""}]' shell/t-play --enqueue -)"
report "a record with no url is 1" 1 "$(rc_in '[{"engine":"bili"}]' shell/t-play --enqueue - -j)"
report "a bad engine name is 1"  1 "$(rc_in '[{"engine":"../evil","url":"x"}]' shell/t-play --enqueue -)"
# The three shapes the verb takes, each proved by the SAME rejection: a payload that parses
# reaches the player check (4), one that does not is usage (1). A search envelope is accepted
# because a search result does not carry `engine` — that field is on the envelope, so only
# taking the whole thing can label an item with its source (ARCH-cli-contract.md「数据契约」).
report "a --show envelope parses" 4 "$(rc_in '{"status":"playlist","items":[{"engine":"yt","url":"x"}]}' shell/t-play --enqueue - -j)"
report "a search envelope parses" 4 "$(rc_in '{"status":"ok","engine":"yt","results":[{"url":"x"}]}' shell/t-play --enqueue - -j)"
report "a shapeless object is 1"  1 "$(rc_in '{"status":"ok"}' shell/t-play --enqueue -)"
# --queue is a LAUNCH modifier: it needs -d, and it takes its handles from stdin ONLY. Each
# arm names what to do instead rather than saying "invalid combination".
report "--queue needs -d"        1 "$(rc_in "$Q1" shell/t-play --queue -)"
report "--queue rejects a handle" 1 "$(rc_in "$Q1" shell/t-play -d --queue - -- URL)"
report "--enqueue rejects a handle" 1 "$(rc_in "$Q1" shell/t-play --enqueue - -- URL)"
report "--queue rejects an action" 1 "$(rc_in "$Q1" shell/t-play -d --queue - --status)"

# ── the five queue-EDIT verbs, idle. The whole point of this block is the 1-vs-4 line: an
# argument that is not a queue position at all is the argv being wrong, and no player could
# make it right, so it is refused BEFORE a player is addressed (1). An argument that is a
# position but not a waiting one depends on where the player got to, so it is decided under
# the lock against the pos of the moment (4). Every check below is on the 1 side; the 4 side
# needs a real queue and lives in playback.sh.
#
# "idle --queue-show is 4" is what gives the 1s their teeth: without it a gate that quietly
# accepted a bad index would still exit non-zero here and look green.
report "idle --queue-show is 4"   4 "$(rc shell/t-play --queue-show -j)"
report "idle --queue-show says why" 0 "$(jq_ok '.status=="not_playing"' shell/t-play --queue-show -j)"
report "idle --queue-clear is 4"  4 "$(rc shell/t-play --queue-clear -j)"
report "--id on --queue-show parses" 4 "$(rc shell/t-play --queue-show --id nope -j)"
# No index at all: the flag needs a value, and the message says which flag wanted one.
report "--queue-rm needs an index" 1 "$(rc shell/t-play --queue-rm -j)"
report "--queue-mv needs an index" 1 "$(rc shell/t-play --queue-mv -j)"
report "--queue-jump needs an index" 1 "$(rc shell/t-play --queue-jump -j)"
# An index that is not a non-negative integer is argv, not state. Kept separate from the
# range check (4, in playback.sh) on purpose: -1 can never name a track, while 9 might.
report "a negative index is 1"     1 "$(rc shell/t-play --queue-rm -1 --expect-url x -j)"
report "a non-numeric index is 1"  1 "$(rc shell/t-play --queue-rm two --expect-url x -j)"
report "a negative --to is 1"      1 "$(rc shell/t-play --queue-mv 2 --to -1 --expect-url x -j)"
# THE PIN IS MANDATORY, and this is the line that says so. It is the only guard standing
# between a stale index and a track removed from a queue that dies with its player — an
# optional one would be missing exactly when it was needed.
report "--queue-rm demands the pin" 1 "$(rc shell/t-play --queue-rm 2 -j)"
report "--queue-mv demands the pin" 1 "$(rc shell/t-play --queue-mv 2 --to 1 -j)"
report "--queue-jump demands the pin" 1 "$(rc shell/t-play --queue-jump 2 -j)"
report "…and the message names the flag" 0 "$(err_has 'expect-url' shell/t-play --queue-rm 2)"
# --to belongs to exactly one verb, and --queue-mv cannot do without it: a move with no
# destination is not a move, and guessing one is how a track lands somewhere nobody asked.
report "--queue-mv demands --to"   1 "$(rc shell/t-play --queue-mv 2 --expect-url x -j)"
report "--to is only --queue-mv's" 1 "$(rc shell/t-play --queue-rm 2 --to 1 --expect-url x -j)"
report "--expect-url is not --queue-show's" 1 "$(rc shell/t-play --queue-show --expect-url x -j)"
report "--expect-url is not --queue-clear's" 1 "$(rc shell/t-play --queue-clear --expect-url x -j)"
# The verbs are mutually exclusive actions like every other one, named in the error.
report "two queue verbs conflict"  1 "$(rc shell/t-play --queue-show --queue-clear -j)"
# The undo pair. Idle, --undo has no copy to find (4, undo_none), and that 4 is what gives
# the 1s beside it their teeth; --discard with nothing to drop is 0. What a copy restores
# needs a real queue and is playback.sh's.
report "idle --undo is 4, undo_none" 0 "$(jq_ok '.reason=="undo_none"' shell/t-play --undo --owner $$ -j)"
report "idle --undo --discard is 0"  0 "$(jq_ok '.status=="ok" and .discarded==false' shell/t-play --undo --discard --owner $$ -j)"
report "--undo needs --owner"        1 "$(rc shell/t-play --undo -j)"
report "--owner not a number is 1"   1 "$(rc shell/t-play --queue-clear --owner x -j)"
report "--owner is not --queue-mv's" 1 "$(rc shell/t-play --queue-mv 2 --to 1 --expect-url x --owner $$ -j)"
report "--discard needs --undo"      1 "$(rc shell/t-play --queue-clear --discard -j)"
report "--undo takes no --id"        1 "$(rc shell/t-play --undo --owner $$ --id nope -j)"

# ── the loop mode, idle. REPEAT is what the player has; playing ON to the next track is a
# queue, and the two are told apart at the door. --loop and --set-loop are one enum with two
# spellings, so both are driven — a value that is not off|one never reaches a player (1),
# and a well-formed one with no player to receive it is the did-not-take-effect class (4).
report "--loop needs a value"        1 "$(rc shell/t-play --loop)"
report "--loop bogus is 1"           1 "$(rc shell/t-play --loop bogus -- URL)"
report "--set-loop bogus is 1"       1 "$(rc shell/t-play --set-loop bogus -j)"
# The value a caller is most likely to reach for, and the one arm whose TEXT is asserted:
# playing on to the next track is a real feature under a different flag, so the message has
# to route them to it. A plain "must be off or one" passes the exit code above and fails
# here, which is what makes the pair worth two lines instead of one.
report "--loop sequential is 1"      1 "$(rc shell/t-play --loop sequential -- URL)"
report "…and it names the queue"     0 "$(err_has 'queue' shell/t-play --loop sequential -- URL)"
# The same 1-vs-4 split the socket verbs carry, on the verb that reaches a player's RECORD
# rather than its socket — so, like --enqueue and --next, it must answer without nc.
report "idle --set-loop is 4"        4 "$(rc shell/t-play --set-loop one -j)"
report "idle --set-loop says why"    0 "$(jq_ok '.status=="not_playing"' shell/t-play --set-loop one -j)"
report "--id on --set-loop parses"   4 "$(rc shell/t-play --set-loop one --id nope -j)"
report "--set-loop rejects a handle" 1 "$(rc shell/t-play --set-loop one -- URL)"
# --loop is a LAUNCH modifier: beside a verb that addresses a running player it is a caller
# who means --set-loop. Both verbs below answer 4 when idle, so a 1 can only have come from
# the combination gate — the check cannot pass on the idle path by accident. (--start's own
# pair of lines above has the same shape and is there for the same reason.)
report "--loop with --pause is 1"    1 "$(rc shell/t-play --loop one --pause -j)"
# --watch is a socket verb and takes their taxonomy whole: nothing to follow is 4 with the
# same not_playing shape, a stray handle is argv (1). What it streams needs a real player
# and is playback.sh's.
report "idle --watch is 4"           4 "$(rc shell/t-play --watch -j)"
report "idle --watch says why"       0 "$(jq_ok '.status=="not_playing"' shell/t-play --watch -j)"
report "--id on --watch parses"      4 "$(rc shell/t-play --watch --id nope -j)"
report "--watch rejects a handle"    1 "$(rc shell/t-play --watch -- URL)"
report "--loop with --status is 1"   1 "$(rc shell/t-play --loop one --status -j)"

# The tombstone list itself — a player that died unasked, a normal finish leaving nothing,
# the cap — is proved where a real player really dies: playback.sh. What an idle machine can
# say about it is the SHAPE, which every --status caller reads: the key is always there.
echo "── the death record: contract fields present ───────────────────────"
report "failed[] always present"   0 "$(jq_ok '.failed|type=="array"' shell/t-play --status -j)"

echo "── the playlist store: durable state, one file, one lock ──────────"
# UT_STATE_DIR is exported, and that is the whole reason the knob exists: without it every
# check below would write into the user's real playlists. It points somewhere disposable
# for the rest of this file.
export UT_STATE_DIR
UT_STATE_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ting-plstore.XXXXXX")
PL=shell/t-playlist
ENV_JSON='{"status":"ok","engine":"yt","query":"q","count":2,"results":[{"id":"a1","title":"One","url":"https://www.youtube.com/watch?v=a1","channel":"c","duration":213,"duration_fmt":"00h:03m:33s","view_count":5,"live_status":"not_live"},{"id":"a2","title":"Two","url":"https://www.youtube.com/watch?v=a2","channel":"c","duration":null,"duration_fmt":null,"view_count":null,"live_status":"is_live"}]}'

report "empty store: ok, exit 0"      0 "$(jq_ok '.status=="ok" and .count==0 and .playlists==[]' $PL --ls -j)"
# ── ARCH-cli-contract.md「调用面」's first pipeline, RUN rather than printed:
#     yt-search -j -n 20 -- "lofi hip hop" | t-playlist --add chill
# That block is the one place the suite documents commands COMPOSING, and until now nothing
# executed a line of it: the storage side had checks, the pipeline did not, so a flag
# misspelled there, an argument reordered, or a combination that stopped being legal would sit
# in the doc being wrong. The direction is fixed — the CHECK is the authority and the doc is
# its reader's view; when the two disagree, the doc moves.
#
# The left half is a real search, which this hermetic half may not make, so it is a FIXTURE:
# a search envelope is DATA the real t-playlist really reads, not something that RUNS in
# place of yt-search (CLAUDE.md's testing rules). The right half is the doc's argv verbatim,
# `-j` and all — prose mode, because that is what the documented line says, and the prose
# writer is a different exit path from the -j one.
printf '%s' "$ENV_JSON" | $PL --add chill >/dev/null 2>&1
report "search envelope | --add: 0"    0 "$?"
report "a search envelope tags engine" 0 "$(jq_ok '.count==2 and ([.items[].engine]|unique==["yt"])' $PL --show chill -j)"
# An ITEM carries no engine — the envelope does. An engine tag that survived the store is
# the only thing that makes a stored record a callable `t-play --engine E -- URL`.
echo '[{"engine":"bili","id":"BV1","url":"https://www.bilibili.com/video/BV1","title":"三","duration":90}]' | $PL --add chill -j >/dev/null 2>&1
report "an array keeps its own engine"  0 "$(jq_ok '[.items[].engine]|unique==["bili","yt"]' $PL --show chill -j)"
report "--show is ONE line"             1 "$($PL --show chill -j | wc -l | tr -d ' ')"
report "duration_fmt derived on read"   0 "$(jq_ok '.items[0].duration_fmt=="00h:03m:33s" and (.items[1].duration_fmt==null)' $PL --show chill -j)"
# 4, not 1: the argv was well formed and the store had nothing to answer with — the same
# split t-play makes when --set-volume finds no player. 1 stays for a malformed call.
report "--show missing: 4, not_found"   4 "$(rc $PL --show nope)"
report "…and says so in the envelope"   0 "$(jq_ok '.status=="error" and .reason=="not_found"' $PL --show nope -j)"
report "--rm out of range: 1"           1 "$(rc $PL --rm chill --index 9)"
report "--rm removes exactly one"       0 "$(jq_ok '.count==2' $PL --rm chill --index 1 -j)"
# Idempotent, like --stop on a player that already exited: the caller asked for an end state
# and the end state holds. `deleted` is the field that says which of the two happened.
report "--del missing: 0, deleted=false" 0 "$(jq_ok '.status=="ok" and .deleted==false' $PL --del ghost -j)"
$PL --rename chill mellow -j >/dev/null 2>&1
report "--rename moves the file"        0 "$(jq_ok '.playlists[0].name=="mellow" and .count==1' $PL --ls -j)"
printf '%s' "$ENV_JSON" | $PL --add other -j >/dev/null 2>&1
report "--rename onto a name: 4"        4 "$(rc $PL --rename other mellow)"
report "…with reason exists"            0 "$(jq_ok '.reason=="exists"' $PL --rename other mellow -j)"
# The store round trip: its own --show output is accepted by --add, which is what copying
# one list into another is.
$PL --show mellow -j | $PL --add copy -j >/dev/null 2>&1
report "a playlist envelope re-adds"    0 "$(jq_ok '.count==2' $PL --show copy -j)"
# ── ARCH-cli-contract.md「调用面」's last pipeline, minus the player it needs:
#     t-playlist --show chill -j | t-play --enqueue -
# "a --show envelope parses" further up proves t-play accepts the SHAPE, but it is a
# hand-written object and so cannot notice --show drifting away from it. This one can: a real
# --show on the left, the real player's gate on the right. 4 is the whole claim — the payload
# got past the parser and only a player to receive it was missing. The 1s beside it (bad JSON,
# empty queue, a shapeless object) are what make a 4 here mean "shape accepted"; a --show that
# stopped emitting `items` would come back 1.
#
# --enqueue rather than the doc's `-d --queue -` on purpose: --queue would LAUNCH a player and
# this file starts none. The launch off a real --show envelope is proved in playback.sh.
report "a real --show reaches the gate" 4 "$($PL --show mellow -j | shell/t-play --enqueue - -j >/dev/null 2>&1; echo $?)"
# An unreadable file on disk. Before this, jq's parse error escaped as exit 5 with no
# envelope at all under -j — the failure yt-search was fixed for, reintroduced in a second
# command. --show fails (the question was about that list); --ls still answers (the question
# was about the store, and one bad file must not hide the rest).
printf '%s' '{ not json' > "$UT_STATE_DIR/playlists/wrecked.json"
report "--show on a corrupt file: 4"    4 "$(rc $PL --show wrecked)"
report "…with reason corrupt"           0 "$(jq_ok '.status=="error" and .reason=="corrupt"' $PL --show wrecked -j)"
report "--ls survives a corrupt file"   0 "$(jq_ok '.status=="ok" and (.playlists|length)>0' $PL --ls -j)"
# `schema` is WRITTEN by every add; this is the check that makes writing it worth anything.
printf '%s' '{"schema":99,"name":"future","created_at":"x","updated_at":"x","count":0,"items":[]}' \
    > "$UT_STATE_DIR/playlists/future.json"
report "a newer schema is refused: 4"   4 "$(rc $PL --show future)"
rm -f "$UT_STATE_DIR/playlists/wrecked.json" "$UT_STATE_DIR/playlists/future.json"
report "a name with / is refused"       1 "$(rc $PL --del "a/b")"
report "…with reason invalid_name"      0 "$(jq_ok '.reason=="invalid_name"' $PL --del "a/b" -j)"
report "a selector with no verb: 1"     1 "$(rc $PL --show mellow --index 2)"
# …including on the one verb that takes no name: the check used to live inside the branch
# that does, so `--ls --index 3` exited 0 having silently ignored it.
report "…--ls too, not just the named" 1 "$(rc $PL --ls --index 3)"
report "two actions at once: 1"         1 "$(rc $PL --ls --show mellow)"
report "a playback flag: 1"             1 "$(rc $PL --status)"
report "a handle after --: 1"           1 "$(rc $PL -- "https://youtu.be/x")"
report "bad stdin: 1"                   1 "$(echo not-json | $PL --add mellow >/dev/null 2>&1; echo $?)"
report "…and an error envelope under -j" 0 "$(jq_in '.status=="error" and .reason=="invalid_input"' not-json $PL --add mellow -j)"

# THE LOCK, driven rather than asserted from prose. Without it these eight writers are eight
# read-modify-write races on one file and the list ends up with ONE item — measured, by
# stubbing lock_playlist out and re-running this exact loop.
i=0
while [ "$i" -lt 8 ]; do
    printf '[{"engine":"yt","url":"https://x/%s"}]' "$i" | $PL --add race -j >/dev/null 2>&1 &
    i=$((i + 1))
done
wait
report "8 concurrent adds keep all 8"   0 "$(jq_ok '.count==8' $PL --show race -j)"
# A held lock is did-not-take-effect (4), never a usage error and never a silent unlocked
# write: this store is durable, so proceeding without the lock could drop what the user just
# added.
#
# ONE run, both claims — the exit code and the reason come off the same invocation, the way
# the dead-id pair further down already does it. It used to be two, which meant sitting
# through the 5s spin TWICE to learn two facts about one failure; what the second run added
# was that the code is 4 in prose mode as well as under -j, and the taxonomy section asserts
# that mode-parity on a failure of its own for 40ms.
mkdir -p "$UT_STATE_DIR/playlists/.lock-race"
LOCKED=$(printf '[{"engine":"yt","url":"https://x/z"}]' | $PL --add race -j 2>/dev/null); LOCKED_ST=$?
report "a held lock: 4, not 1"          4 "$LOCKED_ST"
report "…with reason locked"            0 "$(jqv '.reason=="locked"' "$LOCKED")"
# A lock left by a SIGKILLed writer must not wedge a playlist forever — and must not make the
# next caller WAIT for it either: staleness is tested on the first failed mkdir, so this is
# the fast path, not a second 5s spin (shell/t-playlist:lock_playlist). Measured before the
# reorder: 5.46s. After: 0.10s.
touch -t 202001010000 "$UT_STATE_DIR/playlists/.lock-race"
report "a stale lock is stolen"         0 "$(printf '[{"engine":"yt","url":"https://x/z"}]' | $PL --add race -j >/dev/null 2>&1; echo $?)"

# THE UNDO COPY. The owner is this shell: it is alive for the whole run, which is all an
# owner has to be. Every "same as before" below compares the store's own --show output
# before the write and after the undo, so what is proved is what a reader of the store sees.
#
# The window closes on a clock, so it is proved off the critical path: a background case,
# owned by a `sleep` it outlives by nothing (a subshell cannot name its own pid on bash 3.2).
# It polls --undo against a copy it made stale ON PURPOSE — a successful undo would consume
# the copy it is waiting on — so the answer goes undo_stale while the window is open and
# undo_expired once it has closed, and the poll ends on that answer, not on a sleep.
# Its own store, too: every call reaps dead owners' copies, and a poller sharing this one
# would reap the dead-owner copy further down before that check could see it written.
sleep 30 & UNDO_HOLD=$!
UNDO_EXPIRY_OUT="$UT_STATE_DIR.expiry"
UNDO_EXPIRY_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ting-plundo.XXXXXX")
(
    UT_STATE_DIR=$UNDO_EXPIRY_DIR
    printf '%s' "$ENV_JSON" | $PL --add expiry >/dev/null 2>&1
    $PL --rm expiry --index 0 --owner "$UNDO_HOLD" >/dev/null 2>&1
    printf '%s' "$ENV_JSON" | $PL --add expiry >/dev/null 2>&1
    s=$SECONDS; last=""; r=""
    while [ $((SECONDS - s)) -le 5 ]; do
        r=$($PL --undo --owner "$UNDO_HOLD" -j 2>/dev/null | jq -r '.reason // "ok"')
        [ "$r" = undo_expired ] && break
        last=$r
    done
    echo "$last $r" >"$UNDO_EXPIRY_OUT"
) &
UNDO_EXPIRY_PID=$!

printf '%s' "$ENV_JSON" | $PL --add u >/dev/null 2>&1
echo '[{"engine":"bili","url":"https://www.bilibili.com/video/BV1","title":"三"}]' | $PL --add u >/dev/null 2>&1
U0=$($PL --show u -j)
report "--rm --owner: undo.deadline"   0 "$(jq_ok '.status=="ok" and .removed==1 and (.undo.deadline|type)=="number"' $PL --rm u --index 1 --owner $$ -j)"
report "--undo reports what it undid"  0 "$(jq_ok '.status=="ok" and .undone=="rm" and .name=="u" and .index==1' $PL --undo --owner $$ -j)"
report "…and the list is as it was"    0 "$([ "$($PL --show u -j)" = "$U0" ]; echo $?)"
report "a copy is used once"           4 "$(rc $PL --undo --owner $$ -j)"
report "…reason undo_none"             0 "$(jq_ok '.reason=="undo_none"' $PL --undo --owner $$ -j)"
$PL --del u --owner $$ -j >/dev/null 2>&1
report "--del --owner then --undo: 0"  0 "$(rc $PL --undo --owner $$ -j)"
report "…the list is back"             0 "$([ "$($PL --show u -j)" = "$U0" ]; echo $?)"
$PL --rename u v --owner $$ -j >/dev/null 2>&1
report "--rename undo names both"      0 "$(jq_ok '.undone=="rename" and .name=="u" and .from=="v"' $PL --undo --owner $$ -j)"
report "…the old name is back"         0 "$([ "$($PL --show u -j)" = "$U0" ]; echo $?)"
report "…the new name is gone"         4 "$(rc $PL --show v)"
printf '%s' "$ENV_JSON" | $PL --add u --owner $$ -j >/dev/null 2>&1
report "--add onto a list, undone"     0 "$($PL --undo --owner $$ >/dev/null 2>&1; [ "$($PL --show u -j)" = "$U0" ]; echo $?)"
printf '%s' "$ENV_JSON" | $PL --add fresh --owner $$ -j >/dev/null 2>&1
report "--add that made a list, undone" 4 "$($PL --undo --owner $$ >/dev/null 2>&1; rc $PL --show fresh)"
# Without --owner nothing is kept, and the envelope is the one it always was.
report "no --owner: no undo field"     0 "$(jq_ok 'has("undo")|not' $PL --rm u --index 0 -j)"
report "…on --add either"              0 "$(jq_in 'has("undo")|not' "$ENV_JSON" $PL --add u -j)"
report "…and nothing to undo"          0 "$(jq_ok '.reason=="undo_none"' $PL --undo --owner $$ -j)"
# The store moved between the write and the undo: the undo refuses and changes nothing.
$PL --rm u --index 0 --owner $$ -j >/dev/null 2>&1
printf '%s' "$ENV_JSON" | $PL --add u -j >/dev/null 2>&1
U1=$($PL --show u -j)
report "changed since: 4"              4 "$(rc $PL --undo --owner $$ -j)"
report "…reason undo_stale"            0 "$(jq_ok '.reason=="undo_stale"' $PL --undo --owner $$ -j)"
report "…and the change is kept"       0 "$([ "$($PL --show u -j)" = "$U1" ]; echo $?)"
$PL --del u --owner $$ -j >/dev/null 2>&1
printf '%s' "$ENV_JSON" | $PL --add u -j >/dev/null 2>&1
report "deleted then re-made: stale"   0 "$(jq_ok '.reason=="undo_stale"' $PL --undo --owner $$ -j)"
# Removing the last item keeps an empty list, and the undo brings the item back into it.
echo '[{"engine":"yt","url":"https://x/solo"}]' | $PL --add solo >/dev/null 2>&1
S0=$($PL --show solo -j)
$PL --rm solo --index 0 --owner $$ -j >/dev/null 2>&1
report "the last item out: count 0"    0 "$(jq_ok '.status=="ok" and .count==0' $PL --show solo -j)"
report "…and back in"                  0 "$($PL --undo --owner $$ >/dev/null 2>&1; [ "$($PL --show solo -j)" = "$S0" ]; echo $?)"
# --discard drops the copy without restoring, and is 0 whether there was one or not.
$PL --rm solo --index 0 --owner $$ -j >/dev/null 2>&1
report "--discard: 0"                  0 "$(jq_ok '.status=="ok" and .discarded==true' $PL --undo --discard --owner $$ -j)"
report "…again, with none: 0"          0 "$(jq_ok '.status=="ok" and .discarded==false' $PL --undo --discard --owner $$ -j)"
report "…and nothing is left to undo"  0 "$(jq_ok '.reason=="undo_none"' $PL --undo --owner $$ -j)"
# An owner that died without discarding (kill -9): ANY later call clears its copy.
UNDO_DEAD=$(sh -c 'echo $$')
$PL --rm u --index 0 --owner "$UNDO_DEAD" -j >/dev/null 2>&1
report "a dead owner's copy is written" 0 "$([ -d "$UT_STATE_DIR/undo/playlist-$UNDO_DEAD" ]; echo $?)"
report "…and the next --ls clears it"  1 "$($PL --ls >/dev/null 2>&1; [ -d "$UT_STATE_DIR/undo/playlist-$UNDO_DEAD" ]; echo $?)"
# The gates: each is a malformed call, so each is 1 whatever the store holds.
report "--owner not a number: 1"       1 "$(rc $PL --rm u --index 0 --owner x)"
report "--owner 0: 1"                  1 "$(rc $PL --del u --owner 0)"
report "--owner on --show: 1"          1 "$(rc $PL --show u --owner $$)"
report "…and it says where it belongs" 0 "$(err_has 'belongs to' $PL --show u --owner $$)"
report "--undo without --owner: 1"     1 "$(rc $PL --undo)"
report "--discard without --undo: 1"   1 "$(rc $PL --ls --discard)"
report "--undo beside a verb: 1"       1 "$(rc $PL --undo --ls --owner $$)"

wait "$UNDO_EXPIRY_PID"
kill "$UNDO_HOLD" 2>/dev/null; wait "$UNDO_HOLD" 2>/dev/null
report "the window closes by itself"   "undo_stale undo_expired" "$(cat "$UNDO_EXPIRY_OUT" 2>/dev/null)"
rm -rf "$UNDO_EXPIRY_OUT" "$UNDO_EXPIRY_DIR"
rm -rf "$UT_STATE_DIR"

echo "── the listening log: append-only, one line, bounded ──────────────"
# The eighth entry point, and the second half of the user-level store. Same disposable
# UT_STATE_DIR discipline as the playlist section above, and for a sharper reason: without it
# these checks append to the log of what the user actually listened to, and --clear deletes
# from it.
UT_STATE_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ting-histore.XXXXXX")
HL=shell/t-history
# A listening is the ITEM record plus the four fields a listening has and a list entry does
# not. `channel` is in here on purpose: it is the field a caller would carry in by accident,
# and the row on disk must not have it.
H_ROW='{"engine":"yt","id":"a1","url":"https://www.youtube.com/watch?v=a1","title":"One","duration":213,"channel":"c","played_at":"2026-06-02T10:00:00Z","ended_at":"2026-06-02T10:01:37Z","seconds":97,"reason":null}'

report "empty log: ok, count 0"        0 "$(jq_ok '.status=="ok" and .count==0 and .items==[]' $HL --ls -j)"
printf '%s' "$H_ROW" | $HL --record - -j >/dev/null 2>&1
report "--record then --ls reads it"   0 "$(jq_ok '.count==1 and .items[0].url=="https://www.youtube.com/watch?v=a1"' $HL --ls -j)"
report "--ls is ONE line"              1 "$($HL --ls -j | wc -l | tr -d ' ')"
# DERIVED on read, never stored — the same rule --show follows for duration_fmt. A stored
# copy would be a second truth about the same number.
report "both _fmt derived on read"     0 "$(jq_ok '.items[0].duration_fmt=="00h:03m:33s" and .items[0].seconds_fmt=="00h:01m:37s"' $HL --ls -j)"
# The row is CONSTRUCTED field by field, so a key the caller happened to carry cannot land on
# disk: `channel` expires into a lie, and this is the check that keeps it out.
report "an unknown key never lands"    0 "$(jq_ok '(.items[0]|has("channel"))==false' $HL --ls -j)"
# Newest first, sorted by played_at rather than trusted in file order — a record can arrive
# back-dated (a track that started last month and ended this one).
printf '%s' "$H_ROW" | jq -c '.played_at="2026-08-02T10:00:00Z" | .id="a2" | .url="https://www.youtube.com/watch?v=a2"' | $HL --record - -j >/dev/null 2>&1
report "--ls is newest first"          0 "$(jq_ok '.items[0].id=="a2" and .items[1].id=="a1"' $HL --ls -j)"
report "-n bounds what is printed"     0 "$(jq_ok '.count==1 and .items[0].id=="a2"' $HL --ls -n 1 -j)"
# THE CLAIM THE ROW SHAPE EXISTS FOR: a listening is a CALL, so --ls drops into --add with no
# field mapping in between. If the two envelopes ever drift, this is what says so.
#
# The argv is ARCH-cli-contract.md「调用面」's third pipeline verbatim — `-n 20` on the left, no
# `-j` on the right — for the reason the playlist section states at its own first pipeline:
# that block documents commands COMPOSING, and a documented composition nothing runs is a
# claim that reports green by default. Both halves here are the real commands; nothing offline
# about this one is a substitute.
$HL --ls -n 20 -j | shell/t-playlist --add rediscover >/dev/null 2>&1
report "--ls feeds t-playlist --add"  0 "$(jq_ok '.count==2 and ([.items[].engine]|unique==["yt"])' shell/t-playlist --show rediscover -j)"
# …and the fourth pipeline, `t-history --ls -n 20 -j | t-play -d --queue -`, at the SHAPE
# level only — --queue launches, and this file starts nothing. A distinct producer from the
# --show envelope the playlist section pipes in: both land on read_queue_items' `.items` arm,
# but this one is emitted by a different command, so a --ls that renamed its array or dropped
# `url` off a row would come back 1 here and nowhere else. What the 4 does NOT say is that the
# per-item engine tag survived: read_queue_items falls back to t-play's default engine for an
# untagged item, so both spellings pass this gate. That claim is the store's own
# ("an unknown key never lands" above reads the row; "--ls feeds t-playlist --add" reads the
# engine), and it is not restated here.
report "--ls reaches the queue gate"   4 "$($HL --ls -n 20 -j | shell/t-play --enqueue - -j >/dev/null 2>&1; echo $?)"

# THE 4096-BYTE PREMISE. The lock-free append is only atomic while one line fits under
# PIPE_BUF, so the title is truncated to 200 bytes and the whole row is measured after. A
# check that only ever sees ordinary input is not a check of a premise.
H_BIG=$(printf '%s' "$H_ROW" | jq -c --arg t "$(printf 'x%.0s' $(seq 1 8000))" '.title=$t | .id="big" | .url="https://www.youtube.com/watch?v=big" | .played_at="2026-08-03T10:00:00Z"')
H_OUT=$(printf '%s' "$H_BIG" | $HL --record - -j 2>/dev/null)
report "an 8KB title is recorded"      0 "$(jqv '.status=="ok" and .recorded==1' "$H_OUT")"
report "…and reports truncated"        0 "$(jqv '.truncated==true' "$H_OUT")"
report "…and --ls still parses it"     0 "$(jq_ok '.count==3 and ([.items[]|select(.id=="big")]|length)==1' $HL --ls -j)"
# Bytes, not characters: the budget is PIPE_BUF and awk counts what the kernel writes.
report "…and no line reaches 4096B"    0 "$(LC_ALL=C awk 'length($0) >= 4096 { bad = 1 } END { print bad + 0 }' "$UT_STATE_DIR"/history/*.jsonl)"

# One unreadable line must not hide the rest — the rule --ls already applies to a corrupt
# playlist file, on the format where a hand edit is likeliest.
printf '%s\n' '{ not json' >> "$UT_STATE_DIR/history/2026-08.jsonl"
report "a broken line hides nothing"   0 "$(jq_ok '.status=="ok" and .count==3' $HL --ls -j)"
# `schema` is written by every record; this is what makes writing it worth anything.
printf '%s\n' '{"schema":99,"engine":"yt","url":"https://x/9","played_at":"2026-08-09T10:00:00Z"}' >> "$UT_STATE_DIR/history/2026-08.jsonl"
report "a newer schema is skipped"     0 "$(jq_ok '.count==3' $HL --ls -j)"

# --clear is mostly an `rm`: shards older than the boundary go whole. Everything above lands
# in the 2026-08 shard except the very first record, so a cut at that month must take exactly
# one row and leave the August shard — junk lines and all — untouched.
report "--clear --before cuts by month" 0 "$(jq_ok '.status=="ok" and .removed==1' $HL --clear --before 2026-08 -j)"
report "…and left the rest alone"      0 "$(jq_ok '.count==2 and ([.items[].id]|sort==["a2","big"])' $HL --ls -j)"
report "--clear empties the log"       0 "$(jq_ok '.status=="ok"' $HL --clear -j)"
# Idempotent, like --stop on a player that already exited: the caller asked for an end state.
report "--clear on an empty log: 0"    0 "$(jq_ok '.status=="ok" and .removed==0' $HL --clear -j)"

# The gate. Same shape as t-playlist's, and every arm names the command that owns the flag
# rather than answering "unknown flag" to a caller who reached for a sibling.
report "two actions at once: 1"        1 "$(rc $HL --ls --clear)"
report "no action at all: 1"           1 "$(rc $HL -n 5)"
report "-n on --clear: 1"              1 "$(rc $HL --clear -n 5)"
report "--before on --ls: 1"           1 "$(rc $HL --ls --before 2026-01)"
report "a playback flag: 1"            1 "$(rc $HL --status)"
report "a playlist verb: 1"            1 "$(rc $HL --add jazz)"
report "a positional argument: 1"      1 "$(rc $HL --ls -- extra)"
report "--record without '-': 1"       1 "$(rc $HL --record /tmp/x)"
report "bad stdin: 1"                  1 "$(rc_in 'not-json' $HL --record -)"
H_OUT=$(printf 'not-json' | $HL --record - -j 2>/dev/null)
report "…and an error envelope under -j" 0 "$(jqv '.status=="error" and .reason=="invalid_input"' "$H_OUT")"
# Every field the row is validated on, one check each: the engine name is a command prefix,
# the url is a handle, played_at names the shard file, and the reason is the PLAYBACK enum.
report "a bad engine name: 1"          1 "$(rc_in "$(printf '%s' "$H_ROW" | jq -c '.engine="yt; rm -rf /"')" $HL --record -)"
report "a url with whitespace: 1"      1 "$(rc_in "$(printf '%s' "$H_ROW" | jq -c '.url="ht tp://x"')" $HL --record -)"
report "a malformed played_at: 1"      1 "$(rc_in "$(printf '%s' "$H_ROW" | jq -c '.played_at="last tuesday"')" $HL --record -)"
report "a reason off the enum: 1"      1 "$(rc_in "$(printf '%s' "$H_ROW" | jq -c '.reason="bored"')" $HL --record -)"

rm -rf "$UT_STATE_DIR"
unset UT_STATE_DIR

echo "── version and the non-TTY refusal ────────────────────────────────"
# Stated over every entry point the checkout HAS, not over a list of six names: a hardcoded
# list is a check that silently stops covering the thing it was written for the moment a
# seventh command lands. Anything in shell/ with a shebang is an entry point.
ENTRY_POINTS=""
for f in shell/*; do
    [ -f "$f" ] || continue
    head -n 1 "$f" | grep -q '^#!' || continue      # VERSION is data, not a command
    ENTRY_POINTS="$ENTRY_POINTS $f"
done
# No separate count check: an empty ENTRY_POINTS makes the `sort -u | wc -l` below 0, not 1,
# so the vacuous case is already caught by the check that does the work.
report "one version, every entry point" 1 \
    "$(for c in $ENTRY_POINTS; do "$c" --version | awk '{print $NF}'; done | sort -u | wc -l | tr -d ' ')"
# …and that the one version is the FILE's, asserted through a SYMLINK — the documented
# install (ROADMAP 的打包 NO: users symlink these onto their own PATH) and the configuration this
# breaks in. A script that does not resolve its own symlink chain looks for VERSION next to
# the LINK, finds none, and prints "unknown". Seven entry points all printing "unknown" agree
# with each other perfectly, so the check above stays green while every one of them is wrong;
# pinning the value to the file is what gives it teeth. Real symlinks to real scripts, read by
# the real command — real setup, not a stand-in.
UT_VER=$(cat VERSION)
LINKDIR="$UT_TEST_TMP/bin"
mkdir -p "$LINKDIR"
for c in $ENTRY_POINTS; do ln -sf "$PWD/$c" "$LINKDIR/$(basename "$c")"; done
report "…and it is VERSION, via a symlink" "$UT_VER" \
    "$(for c in "$LINKDIR"/*; do "$c" --version | awk '{print $NF}'; done | sort -u | tr -d '\n')"

echo "── gates: verbs, engine names and the host allowlist (no network) ─"
# The last of the hermetic checks, and the ones most likely to be broken by the edit you are
# about to commit: every one of these is a REFUSAL, decided from argv alone, before a
# dependency gate or a transport exists. They used to be scattered through the live half —
# green in 50 seconds, behind fifteen engine round trips they do not need — which is how the
# file's own "offline first" contract had drifted. Nothing about them changed but their
# position, and the position is the point.

# The two handles the whole file resolves, declared here because the gates name them too. A
# short, permanent, caption-bearing public video, and a permanent single-part one on the
# second site. 19 seconds long: nothing here plays it, but a resolve that accidentally starts
# a download costs a second rather than a minute. Single-part matters on the second site — a
# handle that is a 50-track collection resolves to part one, which is correct but makes a
# title assertion depend on which part that is.
MEDIA_ID="jNQXAC9IVRw"
BILI_ID="BV1mL411E7Fb"
# A third handle, and it earns its own line because BILI_ID above is deliberately SINGLE-part
# and --parts has nothing to say about a list of one. A long-lived public 100-part course; the
# checks on it assert `>= 2` and never the count, because the site's own numbers change and a
# regression on 100 would be a regression in Bilibili's catalogue, not in this engine.
BILI_PARTS_ID="BV1vKEn6eE6Q"
# An unreachable proxy is the cheapest deliberate network failure, and it works offline too.
# Declared here because the host-allowlist checks below borrow it; the failure-taxonomy
# section in the live half is where it is asserted ON.
NOPROXY="http://127.0.0.1:1"
# The transcript handles, beside the other handles because the live half fetches them in one
# batch: the ok-path one must HAVE captions and the error-path one must not — pointing the
# ok path at a long music stream is how that check first went red against working code.
CAPTIONED="https://www.youtube.com/watch?v=8S0FDjFBj8o"
BARE="https://www.youtube.com/watch?v=n61ULEU7CO0"
# The same pair for the third engine, whose caption track is a LYRIC — and the second of them
# is the one that carries the finding. An instrumental here does NOT set the site's documented
# `nolyric` flag (measured 2026-09-02: not one of eight did); it returns a real lyric holding
# the composer credits and then the site's own sentinel line, so an engine reading only the
# flag answers status:"ok" with three lines of credits where the words should be. NE_SILENT is
# that shape, and it is the input that separates the two implementations.
NE_LYRIC="1824020871"
NE_SILENT="478507889"
# The container handles, one per site, each chosen for what it can prove:
#   YT_LIST    a long-lived public playlist, well under the 500 ceiling, so count==total.
#   BILI_MENU  the audio menu yt-dlp's own extractor is tested against (16 tracks).
#   NE_LIST    an official chart, 99 tracks — the number matters: this site returns ALL of a
#              playlist's ids and only a handful of full records, so a `count` that reaches
#              `total` here is the batch path having completed the truncated first response.
#              It needs NE_INCLUDE_VIP=1, because a chart is mostly VIP-only and the default
#              filter is doing its job when it drops those.
# No count is asserted as a literal: these are living catalogues, and a red that is someone
# adding a track is a red nobody can act on.
YT_LIST="PLLdzS5ShOfOw"
BILI_MENU="am10624"
NE_LIST="https://music.163.com/playlist?id=19723756"
# The video-side containers, added with the cursor. Each is here for a property the audio ones
# cannot state:
#   BILI_FAV     a public favourites list holding BOTH of the rows this site keeps in one and a
#                player cannot open — a dead upload (attr bit 0, and a REAL duration, so the
#                playable judgement alone would let it through) and an OGV episode (type 24,
#                whose bvid does not name a /video/ page). count below total is those being
#                dropped, which is the only place that claim can be made against real data.
#   BILI_SEASON  a creator's collection: a second endpoint, a second page size, and the only
#                one of the three whose "does not exist" is answered in the body code.
#   YT_CHANNEL   a channel small enough to finish in one batch — the case that proves total is
#                filled in when the walk reaches the end (it is null while a batch is full).
#   YT_BIG       a channel far past the ceiling: the only handle here that can exercise a
#                cursor at all, since a container under 500 never mints one.
# No count is asserted as a literal here either, for the same reason as above.
BILI_FAV="ml148005847"
BILI_SEASON="https://space.bilibili.com/946974/lists/3097767?type=season"
YT_CHANNEL="https://www.youtube.com/@RickAstleyYT/videos"
YT_BIG="https://www.youtube.com/@TED/videos"

# Shape validation lives in the ENGINE now — the player cannot tell a good id from a bad one.
report "resolve rejects a non-id" 1 "$(rc shell/yt-resolve -j -- "not an id")"
report "resolve rejects -d"       1 "$(rc shell/yt-resolve -d -- "$MEDIA_ID")"
report "resolve rejects -n"       1 "$(rc shell/yt-resolve -n 5 -- "$MEDIA_ID")"
# The read-only verb refuses the two flags that would make it write or play. Asserted on the
# plain handle, not on the captioned handle the envelope checks use: the gate is decided
# before the handle is looked at, and that handle's reason to exist (it must HAVE captions)
# belongs to the live check that needs it.
report "transcript rejects -f"    1 "$(rc shell/yt-resolve --transcript -f audio -- "$MEDIA_ID")"
report "transcript rejects -d"    1 "$(rc shell/yt-resolve --transcript -d -- "$MEDIA_ID")"
report "bili-resolve rejects a non-id" 1 "$(rc shell/bili-resolve -j -- "not an id")"
report "bili-resolve rejects audio menu URL" 1 \
    "$(rc shell/bili-resolve -j -- "https://www.bilibili.com/audio/am10624")"
report "bili-resolve rejects bare am id" 1 \
    "$(rc shell/bili-resolve -j -- am10624)"
# Capability differs per engine and is stated, not faked: this site's videos carry no
# caption track, so the verb is absent rather than always answering "none".
report "bili-resolve has no --transcript" 1 "$(rc shell/bili-resolve --transcript -- "$BILI_ID")"
# The third engine states its own two absences the same way, and they are absences of
# DIFFERENT kinds — which is the point of asserting both. `--parts` is a verb this site has no
# shape for (one song id is one file), so the engine has no such flag at all. `--sub-lang` is
# the opposite —
# the verb it belongs to IS here, but the CAPABILITY behind it is not: one lyric per song, tagged
# with no language, so there is nothing to choose between and the flag is refused rather than
# accepted and ignored (ARCH-engine.md「字幕」).
report "ne-resolve has no --parts"  1 "$(rc shell/ne-resolve --parts -- "$NE_LYRIC")"
report "ne-resolve has no --sub-lang" 1 \
    "$(rc shell/ne-resolve --transcript --sub-lang zh-Hans -- "$NE_LYRIC")"

# --parts is the other half of that same statement-by-capability rule, read from the other
# direction: this site HAS multi-part videos and the sibling site does not, so the verb
# exists on one engine and must never appear on the other.
#
# It is asked with NO handle, so no request is spent: the engine that has the verb answers with
# a usage error about the missing handle, and the engine that does not falls into the
# unknown-flag arm every gate in this suite shares (ARCH-cli-contract.md「门模型」). BOTH exit 1,
# which is why what these pin is the stderr WORDING, not the exit code.
report "bili-resolve has --parts"  1 "$(err_has 'unknown flag' shell/bili-resolve --parts)"
report "bili --parts needs a handle" 1 "$(rc shell/bili-resolve --parts)"
report "yt-resolve has no --parts"  0 "$(err_has 'unknown flag' shell/yt-resolve --parts)"
report "yt --parts is usage"        1 "$(rc shell/yt-resolve --parts)"
# A flag that cannot act is REJECTED, not ignored: -f and -S select a stream format, and
# enumerating parts resolves no stream. Same rule --info is already held to above.
report "bili --parts takes ONE handle" 1 \
    "$(rc shell/bili-resolve --parts -- "$BILI_ID" "$BILI_ID")"

# --items is the one read-only verb EVERY engine has, so presence is not the discriminator —
# the per-engine GRAMMAR is, and each of these three refusals is a different site's reason.
# All of it is offline: a handle is judged before a request is spent, which is itself the
# claim (a container verb that had to ask the site whether a handle was a container would
# cost a request per typo).
report "--items refuses a video id"     0 \
    "$(err_has 'not a container' shell/yt-resolve --items -- "$MEDIA_ID")"
# A MIX IS REFUSED BY NAME, and it is the one shape that stayed refused after channels were let
# in: a mix is regenerated on every request, so two calls are not two pages of one list and a
# cursor over it could promise nothing. A channel's uploads measured identical across segmented
# reads, which is exactly the property that made them admissible.
report "--items refuses an endless list" 0 \
    "$(err_has 'no last item' shell/yt-resolve --items -- RDdQw4w9WgXcQ)"
report "--items refuses a BV id"        0 \
    "$(err_has 'not a Bilibili container' shell/bili-resolve --items -- "$BILI_ID")"
# A SERIES IS NOT A COLLECTION on this site — different endpoint, same-looking URL — so it is
# refused by name rather than read with the wrong one and answered with someone else's videos.
report "--items refuses a bili series"  0 \
    "$(err_has 'is a series' shell/bili-resolve --items -- 'https://space.bilibili.com/946974/lists/12345?type=series')"
# The third site's own reason, and it is not fussiness: `song`, `album` and `playlist` ids
# share no namespace here, so a bare number cannot say what it identifies. The song verb
# accepts one only because it has already decided what it means.
report "--items refuses a bare number"  0 \
    "$(err_has 'does not say what it identifies' shell/ne-resolve --items -- "$NE_LYRIC")"
report "--items refuses a song URL"     0 \
    "$(err_has 'not an album or playlist' shell/ne-resolve --items -- "https://music.163.com/song?id=$NE_LYRIC")"
# The cross-engine half of this — one verb per invocation, a handle required, exactly one —
# is stated over every DISCOVERED engine, and it lives in the discovery section below where
# $ENGINES exists.
report "bili-search rejects -d" 1 "$(rc shell/bili-search -d -- 音乐)"
# A mistyped engine must be a USAGE error. If it fell into 2+ an agent would read it as
# "the tool failed, retry later" and retry a name that will never exist.
report "unknown engine is usage"  1 "$(rc shell/t-play --engine nope -- "$MEDIA_ID")"
report "engine name is validated" 1 "$(rc shell/t-play --engine ../evil -- "$MEDIA_ID")"
# The quality tier is validated at the door, before any dependency gate: a mistyped tier
# is a usage error, and a legal one still falls into the gates the handle and the engine
# own — the tier must not change what a wrong verb is worth (ARCH-cli-contract.md「命令规格」).
report "t-play rejects a bogus tier"     1 "$(rc shell/t-play --quality ultra -- "$MEDIA_ID")"
report "t-play --quality needs a handle" 1 "$(rc shell/t-play --quality low)"
report "t-play --quality keeps the engine gate" 1 \
    "$(rc shell/t-play --quality low --engine nope -- "$MEDIA_ID")"
# A bogus SCALAR knob in the user's config dies in ting the same way, naming the key the
# user actually wrote. Stated over every scalar door rather
# than the tier that
# happened to be written first: each one is its own `case`, not one loop through one
# validator the way the four *_CYCLE keys are, so a check driving only the quality tier is
# green on a door that was never closed — which is the shape UT_KEYS arrived in.
# And the claim is the MESSAGE, not the exit code: every one of these exits 1 and so does the
# TTY gate a few lines further down the same file, so an exit code alone cannot separate
# "refused the value" from "refused the pipe" and the check could not fail.
for spec in UT_PLAY_QUALITY=bogus UT_KEYS=bogus YT_BG=sideways UT_RESOURCE=maybe UT_RESOURCE_TICKS=fast UT_IMAGE=bogus; do
    KNOB_OUT=$(env "$spec" shell/ting </dev/null 2>&1 || true)
    case "$KNOB_OUT" in
    *"${spec%%=*}"*) KNOB_HIT=yes ;;
    *) KNOB_HIT=no ;;
    esac
    report "${spec%%=*}: a bogus value dies naming the key" "yes" "$KNOB_HIT"
done

# UT_VIZ_STYLE is the player's own scalar door and lives behind a MODE, so the loop above —
# which drives ting — cannot reach it. Three claims, and the discriminator is the MESSAGE
# for the same reason it is up there: all three exit 1. A handle on a host no engine claims
# keeps every one of them offline, because the host gate answers before yt-dlp is reached.
VIZ_URL="https://example.com/x"
viz_says_key() {
    case "$(env "$1" shell/t-play -f "$2" -- "$VIZ_URL" 2>&1 || true)" in
    *UT_VIZ_STYLE*) echo yes ;;
    *) echo no ;;
    esac
}
report "UT_VIZ_STYLE: a bogus value dies naming the key" "yes" "$(viz_says_key UT_VIZ_STYLE=bogus viz)"
# …and the gate is at the DOOR, before the handle's own: a legal style has to fall THROUGH
# to the resolve failure rather than be answered here.
report "UT_VIZ_STYLE: a legal value reaches the handle gate" "no" "$(viz_says_key UT_VIZ_STYLE=wave viz)"
# …and it is scoped to the mode that draws. A door that fires for -f audio would reject a
# config the audio path never reads — which is the shape a mode-blind `case` arrives in.
report "UT_VIZ_STYLE: silent outside -f viz" "no" "$(viz_says_key UT_VIZ_STYLE=bogus audio)"

viz_says_color_key() {
    case "$(env "$1" shell/t-play -f "$2" -- "$VIZ_URL" 2>&1 || true)" in
    *UT_VIZ_COLOR*) echo yes ;;
    *) echo no ;;
    esac
}
report "UT_VIZ_COLOR: a bogus value dies naming the key" "yes" "$(viz_says_color_key 'UT_VIZ_COLOR=bad color!' viz)"
report "UT_VIZ_COLOR: a legal value reaches the handle gate" "no" "$(viz_says_color_key 'UT_VIZ_COLOR=magenta' viz)"
report "UT_VIZ_COLOR: silent outside -f viz" "no" "$(viz_says_color_key 'UT_VIZ_COLOR=bad color!' audio)"

# THE SPELLING, and it is a CONFIG-FILE fact that no environment check can reach: the three
# above hand the key over env, where `#` is just a character. In the FILE the loader cuts the
# line at the first `#`, so a hex written that way does not arrive wrong — it does not arrive
# at all, and the user silently gets cyan while his config plainly says red. That is the trap
# ARCH-tui.md「值的拼法是 `0xRRGGBB` 而不是 `#RRGGBB`」named this key for, and the shipped
# config's comment is the only thing standing between a user and it.
#
# The discriminating input is a value ILLEGAL under either spelling: written with 0x it
# reaches the gate and the gate quotes it back, written with # there is nothing left to
# reject and the run falls through to the handle gate. Both stay offline for the same reason
# the checks above do — no engine claims this host.
VIZCFG="$UT_TEST_TMP/vizcolor.config"
viz_cfg_says() {
    printf '%s\n' "$1" > "$VIZCFG"
    case "$(UT_CONFIG="$VIZCFG" shell/t-play -f viz -- "$VIZ_URL" 2>&1 || true)" in
    *"0xff0000zz"*) echo quoted ;;
    *UT_VIZ_COLOR*) echo other ;;
    *) echo gone ;;
    esac
}
report "UT_VIZ_COLOR: 0xRRGGBB survives the config file" "quoted" \
    "$(viz_cfg_says 'UT_VIZ_COLOR=0xff0000zz!')"
report "…and a #-spelled value never arrives"           "gone" \
    "$(viz_cfg_says 'UT_VIZ_COLOR=#0xff0000zz!')"

# UT_DEAD_KEEP is the player's history-pruning count; must fail fast on non-numeric or negative.
dk_out=$(env UT_DEAD_KEEP=bogus shell/t-play --status 2>&1 || true)
case "$dk_out" in
*UT_DEAD_KEEP*) dk_hit=yes ;;
*) dk_hit=no ;;
esac
report "UT_DEAD_KEEP: a bogus value dies naming the key" "yes" "$dk_hit"
report "UT_DEAD_KEEP: a negative value exits 1" "1" \
    "$(rc env UT_DEAD_KEEP=-1 shell/t-play --status)"

# ── ARCH-player.md「终端可视化」's five worked calls, each run once. The PICTURE those
# lines are about needs a real resolve and a real tty, so it stays 实测 in that doc — a
# foreground blocking play with no --length is not time this suite spends, and bounding it
# would take a stand-in it does not keep. What CAN be held here is the half that rots
# silently: that each of those argv lines is still ACCEPTED — every flag parsed, every
# combination legal, the call travelling all the way to the engine.
#
# The claim is the MESSAGE, for the reason the scalar-knob loop above states: a rejected flag
# and a refused host both exit 1, so an exit code cannot separate "this combination is legal"
# and "one of these flags is not", and a check that cannot separate them cannot fail. The
# handle is the check's own — a host no engine claims, which keeps every one of these offline
# (the engine's host gate answers before yt-dlp is reached; measured at 0.05s) while still
# proving the call got past t-play entirely. Reaching the ENGINE is the pass, and the engine
# NAME in the message is what makes the --engine line more than a repeat of the first.
#
# LC_ALL is PINNED, and that is not decoration: -f viz refuses a non-UTF-8 locale (tct draws
# in half blocks), so on a machine running under LC_ALL=C every line below would come back red
# for a reason that is the environment's and not the subject's — the worst failure a suite can
# have. Pinning also costs nothing to make honest: the gate reads the variable, it does not
# require the locale to be installed, so this works on a host that has no en_US at all. The
# locale gate gets its own check further down, where refusing IS the claim.
# The dead proxy rides along for the auto-route checks: their handles are real hosts of a real
# shape (BV…, song?id=…, watch?v=…), so the claiming engine would otherwise go on to a live
# extraction — three network calls in --offline, reading the developer's own browser cookies,
# for a message ("could not resolve") that any resolve failure prints the same.
viz_reaches_engine() { # <engine> <env assignments and argv…> — yes if it got as far as <engine>
    local want=$1
    shift
    case "$(env LC_ALL=en_US.UTF-8 http_proxy=$NOPROXY https_proxy=$NOPROXY "$@" 2>&1 </dev/null || true)" in
    *"$want-resolve could not resolve"*) echo yes ;;
    *) echo no ;;
    esac
}
report "-f viz: the minimal call"     yes "$(viz_reaches_engine yt shell/t-play -f viz -- "$VIZ_URL")"
# `bars` beside `wave`: the check above proves a legal style is not answered at the door, but
# it drives one member of a two-member enum, and the default is the OTHER one — so a door that
# only ever admitted its own default would be green up there and red here.
report "…UT_VIZ_STYLE=bars, the default" yes "$(viz_reaches_engine yt UT_VIZ_STYLE=bars shell/t-play -f viz -- "$VIZ_URL")"
report "…with --volume 0"             yes "$(viz_reaches_engine yt shell/t-play -f viz --volume 0 -- "$VIZ_URL")"
# Three flags at once, which is the line most likely to rot: --start and --quality each have a
# value gate of their own and each is checked alone above, but nothing had ever given both to
# a MODE whose own gate refuses -d and --queue. A combination gate that grew one arm too wide
# is exactly what this catches, and it is invisible to any single-flag check.
report "…with --start 90 --quality low" yes "$(viz_reaches_engine yt shell/t-play -f viz --start 90 --quality low -- "$VIZ_URL")"
# The mode is engine-agnostic — it is the player's, not a site's — so the same -f viz has to
# survive being pointed at the other engine. The name in the message is the assertion: a
# --engine that was parsed and then dropped would come back naming `yt`.
report "…and --engine bili keeps it"  yes "$(viz_reaches_engine bili shell/t-play --engine bili -f viz -- "$VIZ_URL")"
report "…with --viz-color magenta"     yes "$(viz_reaches_engine yt shell/t-play --viz-color magenta -f viz -- "$VIZ_URL")"
report "…with bogus --viz-color is 1"  1 "$(rc shell/t-play --viz-color 'bad color!' -f viz -- "$VIZ_URL")"
# URL auto-sniffing when --engine is omitted:
report "…auto-routes bili URL to bili"   yes "$(viz_reaches_engine bili shell/t-play -f viz -- "https://www.bilibili.com/video/BV0000000000")"
report "…auto-routes netease URL to ne"  yes "$(viz_reaches_engine ne shell/t-play -f viz -- "https://music.163.com/song?id=000000")"
report "…auto-routes youtube URL to yt"  yes "$(viz_reaches_engine yt shell/t-play -f viz -- "https://www.youtube.com/watch?v=00000000000")"
report "…explicit --engine overrides"    yes "$(viz_reaches_engine yt shell/t-play --engine yt -f viz -- "https://www.bilibili.com/video/BV0000000000")"

# THE TERMINAL-RENDERING MODES CANNOT DETACH, and the refusal is a usage error, not a
# tool failure — an agent reading 2+ would retry a combination that can never work. Stated
# over BOTH such modes rather than the one that happened to be written first: they share a
# single gate, so a check driving only `viz` would be green if the gate ever narrowed to it.
# The queue is the same claim from the other side: it STARTS a detached player, so it
# inherits the same impossibility without naming a mode at all.
for _m in ascii viz; do
    report "-d refuses -f $_m" 1 "$(rc shell/t-play -d -f "$_m" -- "$VIZ_URL")"
    report "--queue refuses -f $_m" 1 "$(rc_in '[]' shell/t-play -f "$_m" --queue - )"
    # The third arm, and it was missing until 2026-09-01: -j captures the player's whole
    # stdout to emit one envelope, and stdout is where tct draws — so `-f viz -j` used to be
    # ACCEPTED, run the track to its end, and answer with a success-shaped envelope having
    # drawn nothing. The suite's only silent trap, and silent is why it had no check: an
    # unresolvable handle under -j also exits 1, so the exit code cannot separate "refused the
    # combination" from "could not resolve". The claim is the MESSAGE, like both siblings.
    case "$(shell/t-play -j -f "$_m" -- "$VIZ_URL" </dev/null 2>&1 || true)" in
    *"-j cannot use -f $_m"*) _jhit=yes ;;
    *) _jhit=no ;;
    esac
    report "-j refuses -f $_m" yes "$_jhit"
    # The fourth arm, and the only one that comes from the ENVIRONMENT rather than argv: tct
    # draws in half blocks (U+2584), so under a C locale the pane used to fill with mojibake
    # or stay empty with nothing said. Refusing was chosen over degrading to an ASCII canvas
    # (ARCH-player.md「终端可视化」), which makes it checkable at all — the degraded picture
    # would have been another 「实测」 row. Message again, not exit code: every gate here is 1.
    #
    # LC_ALL=C rather than an unset environment: `env -u` is not portable to the 3.2 floor's
    # macOS env, and C is the locale the real reports came from (cron, launchd, a bare CI
    # shell). Its partner is viz_reaches_engine above, which pins a UTF-8 locale and asserts
    # the call goes THROUGH — a gate that fired unconditionally would be green here and red
    # there, so neither check alone can pass by accident.
    case "$(env LC_ALL=C shell/t-play -f "$_m" -- "$VIZ_URL" </dev/null 2>&1 || true)" in
    *"needs a UTF-8 locale"*) _lhit=yes ;;
    *) _lhit=no ;;
    esac
    report "a C locale refuses -f $_m" yes "$_lhit"
    # The TUI states the same impossibility from its own side — its playback IS detached, so
    # the mode could never reach a terminal — and there the claim has to be the MESSAGE: the
    # TTY gate a few lines further into `ting` also exits 1, so an exit code cannot separate
    # "refused the mode" from "refused the pipe". Captured then matched, per this file's rule.
    case "$(shell/ting -f "$_m" q </dev/null 2>&1 || true)" in
    *"must be one of"*) _mhit=yes ;;
    *) _mhit=no ;;
    esac
    report "ting refuses -f $_m, naming the modes" yes "$_mhit"
done

# ── THE ORDER OF `ting`'s TWO GATES, and ARCH-tui.md「调用面」's worked calls, which are
# the same check from two sides. That doc states the order as a fact — the flag gate answers
# first, the TTY gate second — and both gates exit 1, so the order can only be pinned by
# feeding the SAME stdin twice and reading two different messages. The `-f viz` arm of the
# loop directly above is one half: a pipe is present, and what comes back is the MODE gate.
# Below is the other: the same pipe, a legal -f, and what comes back is the TTY gate. Either
# check alone is consistent with a single gate; together they are not.
#
# The same loop is also the doc's example block executed. Every line there ends at the TTY
# gate when it is piped, so one assertion covers both claims — and it caught the block's fifth
# line being wrong: it read `--theme nord --lang zh`, and `--lang` is not a ting flag at all
# (the chrome language is YT_LANG, cycled live by the `l` key). Nothing had ever run it. The
# argv below is the corrected line, and the doc now matches it — the CHECK is the authority.
ting_gate() { # <env assignments and argv…> — which gate answered
    case "$(env "$@" </dev/null 2>&1 || true)" in
    *"requires a terminal"*) echo tty ;;
    *"must be one of"*) echo mode ;;
    *"unknown value"* | *"must name at least one value"*) echo cycle ;;
    *"UT_ACCENT"*) echo accent ;;
    *"unknown flag"*) echo unknown-flag ;;
    *) echo other ;;
    esac
}
report "ting: no query reaches the TTY gate" tty "$(ting_gate shell/ting)"
report "…a bare query too"          tty "$(ting_gate shell/ting "lofi hip hop")"
report "…search args forwarded"     tty "$(ting_gate shell/ting --engine bili -n 40 "周杰伦")"
# The legal -f, and the half that pins the order: identical stdin to the `-f viz` check above,
# a different gate in the answer. A ting that checked the tty first would answer `tty` up
# there too and this pair would say nothing.
report "…menu args, and -f is legal" tty "$(ting_gate shell/ting -f video --volume 60 "lofi")"
report "…chrome args"               tty "$(ting_gate YT_LANG=zh shell/ting --theme nord "lofi")"

# ── WHERE A THIRD-PARTY ENGINE MAY LIVE: three places, one order, two files that have to
# agree about them. `ting` scanned PATH only when the sibling glob came up empty, which made
# the one situation an installed third-party engine can actually be in — a checkout carrying
# yt/bili/ne, the new pair somewhere else — unreachable: the TUI offered three sources while
# `t-play --engine` happily played a fourth. Two faces, one word "engine", different answers.
# ARCH-cli-contract.md「加一个引擎 —— 清单」's last item states the claim and nothing had run it.
#
# What is put in each place is the REAL yt engine reached under a second name: the variables
# under test are its LOCATION and the name it answers to, and nothing runs in PLACE of an
# engine (CLAUDE.md's testing rules). A fourth name is what the condition needs — a symlink
# called `yt-*` would be deduplicated against the sibling copy and prove nothing.
#
# The TUI's registry is READ OUT of the --engine gate: a name it does not have comes back as
# "must be one of: <the registry, in discovery order>". That message is the only place the
# list is observable, and the ORDER in it is what pins precedence.
engine_list() { # <env assignments and argv…> — the registry, in discovery order
    env "$@" shell/ting --engine zzz-none q </dev/null 2>&1 | sed -n 's/.*must be one of: //p'
}
PLUG=$UT_TEST_TMP/plugin-engines
PATH_ENG=$UT_TEST_TMP/path-engines
XDG_HOME=$UT_TEST_TMP/xdg-data
mkdir -p "$PLUG" "$PATH_ENG" "$XDG_HOME/ting/engines"
for _d in "$PLUG" "$PATH_ENG" "$XDG_HOME/ting/engines"; do
    ln -sf "$PWD/shell/yt-search" "$_d/zz-search"
    ln -sf "$PWD/shell/yt-resolve" "$_d/zz-resolve"
done
SIBLINGS=$(engine_list shell/ting)
report "the checkout's own pairs are the registry" "bili ne yt" "$SIBLINGS"
report "…a pair on PATH joins it"        "$SIBLINGS zz" "$(engine_list PATH="$PATH_ENG:$PATH" shell/ting)"
report "…a pair in UT_ENGINE_DIR too"    "$SIBLINGS zz" "$(engine_list UT_ENGINE_DIR="$PLUG" shell/ting)"
# The DEFAULT of that knob, driven rather than read: nothing sets UT_ENGINE_DIR here, so the
# pair is only found if the inline default really chains through XDG_DATA_HOME. `ting` and
# `t-play` each declare that default in their own file (ten peers, no shared library), and
# this pair of checks is what stops the two copies drifting apart.
report "…and its default chains through XDG_DATA_HOME" "$SIBLINGS zz" \
    "$(engine_list XDG_DATA_HOME="$XDG_HOME" shell/ting)"
# PRECEDENCE, which only the ORDER can state: the same name in the plugin dir does not appear
# twice and does not move to the front, so the built-in is what runs. A plugin directory is
# reachable by anything that can write one directory; letting it replace `yt-resolve` would
# make "which yt am I running" unanswerable.
ln -sf "$PWD/shell/yt-search" "$PLUG/yt-search"
ln -sf "$PWD/shell/yt-resolve" "$PLUG/yt-resolve"
report "a plugin cannot shadow a built-in" "$SIBLINGS zz" \
    "$(engine_list UT_ENGINE_DIR="$PLUG" shell/ting)"
# A pair is a PAIR, in the plugin dir as everywhere else: one half is a source that would list
# results nothing can resolve, so the name never enters the registry.
ln -sf "$PWD/shell/yt-search" "$PLUG/lone-search"
report "…and a lone search half is not one" "$SIBLINGS zz" \
    "$(engine_list UT_ENGINE_DIR="$PLUG" shell/ting)"
# UT_ENGINE_DIR IS REFUSED FROM A CONFIG FILE, and this is the check that says why the name
# is on that list at all: it points at a directory of EXECUTABLES the suite runs, so a file
# that could set it would be PATH under another spelling — exactly what「配置面」's prefix rule
# buys, and what the YT_IPC_SOCK refusal further down protects from the other direction.
ENGCFG=$UT_TEST_TMP/engine-dir.config
printf 'UT_ENGINE_DIR=%s\n' "$PLUG" > "$ENGCFG"
report "a config file cannot point at engines" "$SIBLINGS" \
    "$(engine_list UT_CONFIG="$ENGCFG" shell/ting)"
# THE PLAYER'S HALF of the same three places. It has no registry to print, so the claim is the
# message: a resolver that was FOUND gets as far as the host gate, one that was not names the
# three places it looked. Both exit 1.
report "the player finds a plugin engine"  yes \
    "$(viz_reaches_engine zz UT_ENGINE_DIR="$PLUG" shell/t-play --engine zz -- "$VIZ_URL")"
report "…by the same XDG default"          yes \
    "$(viz_reaches_engine zz XDG_DATA_HOME="$XDG_HOME" shell/t-play --engine zz -- "$VIZ_URL")"
report "…and a config file cannot aim it"  no \
    "$(viz_reaches_engine zz UT_CONFIG="$ENGCFG" shell/t-play --engine zz -- "$VIZ_URL")"

# One engine, one site. `yt-resolve` used to accept ANY http(s) URL and hand it to yt-dlp,
# which supports 1700+ sites — so a Bilibili URL resolved fine and came back labelled
# `engine:"yt"`. It WORKED, which is why it went unnoticed, and it made the one field whose
# job is routing a result back to its resolver into a field that lies.
#
# Engine-DISCOVERED, not hardcoded: the pair convention (`<name>-search` + `<name>-resolve`)
# is the one `ting` already builds its registry from, so a third engine is covered the day
# its pair lands rather than when someone remembers to add it here. And the claim is stated
# as an invariant over ALL engines, needing no table of who owns what — which is why it
# cannot drift from the engines themselves. Every host-gate function is duplicated per
# engine (`url_host` is byte-identical across the pair today), so a check that drove only
# one engine would be green while the other copy, and engine #3, said nothing.
ENGINES=""
for f in shell/*-resolve; do
    n=$(basename "$f"); n=${n%-resolve}
    [ -x "shell/$n-search" ] && ENGINES="$ENGINES $n"
done
NENG=$(echo "$ENGINES" | wc -w | tr -d ' ')
# >= 2, not == 2: this section's whole premise is that engine #3 is covered the day its pair
# lands, and a hardcoded count is the one line that would go red on exactly that day. What it
# has to rule out is NENG=0, which would make every `refusals` check below pass vacuously.
[ "$NENG" -ge 2 ] ||
    { echo "contract.sh: fewer than two engine pairs discovered — the invariants below cannot fail" >&2; exit 1; }

# A search half resolves no format, so -S (a stream-format sort) is a value it cannot act
# on. Stated over EVERY discovered engine, not just the one that got it right: yt-search
# took the flag and forwarded it into a --flat-playlist dump where it changed nothing, so
# the two halves disagreed about what a search IS and the add-an-engine checklist copied
# the wrong one. Engine #3 is covered the day it lands.
_sdash=0
for n in $ENGINES; do
    [ "$(rc "shell/$n-search" -S abr -- q)" = 1 ] && _sdash=$((_sdash + 1))
done
report "every search half refuses -S" "$NENG" "$_sdash"
# The tier abstraction is held to the same two shapes as -S: a flag that cannot act is
# rejected (--parts resolves no stream), and a search half resolves no format at all.
_qdash=0
for n in $ENGINES; do
    [ "$(rc "shell/$n-search" --quality high -- q)" = 1 ] && _qdash=$((_qdash + 1))
done
report "every search half refuses --quality" "$NENG" "$_qdash"
# --items' cross-engine obligations, stated over every discovered engine because the verb is
# on all of them (unlike --parts and --transcript, which are capabilities of one site each):
# a handle is required, exactly one is taken, and two verbs in one invocation is a caller who
# has not said what it wants — refused rather than resolved by picking the last one
# (ARCH-cli-contract.md「门模型」). Engine #4 is covered the day it lands.
#
# The two-verb claim is the MESSAGE, not the code: an absent verb, a missing handle and this
# all exit 1, so a count of exit codes could not tell them apart.
_items_gate=0
for n in $ENGINES; do
    [ "$(rc "shell/$n-resolve" --items)" = 1 ] &&
        [ "$(rc "shell/$n-resolve" --items -- x y)" = 1 ] &&
        [ "$(err_has 'two verbs' "shell/$n-resolve" --items --info -- x)" = 0 ] &&
        _items_gate=$((_items_gate + 1))
done
report "every --items refuses no handle, two handles, two verbs" "$NENG" "$_items_gate"

# THE CURSOR IS ARGV, so both ways of getting it wrong cost no request — and this is stated over
# every discovered engine because the token's whole point is being ONE shape across them: an
# engine that minted its own spelling would take a sibling's cursor to the network and fail
# there instead of here. The handle is deliberately junk: a cursor decided before the handle is
# a cursor decided before anything is spent.
_cursor_gate=0
for n in $ENGINES; do
    [ "$(rc "shell/$n-resolve" --cursor o:10 -- x)" = 1 ] &&
        [ "$(rc "shell/$n-resolve" --items --cursor nope -- x)" = 1 ] &&
        [ "$(err_has 'is not a cursor' "shell/$n-resolve" --items --cursor p:2 -- x)" = 0 ] &&
        _cursor_gate=$((_cursor_gate + 1))
done
report "every --cursor needs --items and one token shape" "$NENG" "$_cursor_gate"

# --capabilities: what each half of each engine accepts, as one envelope. It exists so a caller
# can learn what to ask without sniffing stderr, which is what `ting` did before and what the
# discovery below did until this verb landed (ARCH-cli-contract.md「命令规格」). Stated over
# every discovered engine, both halves, so engine #4 is covered the day it lands.
_caps_env=0
for n in $ENGINES; do
    for _h in search resolve; do
        _o=$("shell/$n-$_h" --capabilities -j 2>/dev/null) || _o=""
        [ "$(lines "$_o")" = 1 ] &&
            [ "$(jqv '.status=="ok" and .engine=="'"$n"'"
                      and (.flags|type)=="array" and (.flags|all(type=="string"))
                      and (.flags|index("--capabilities"))!=null' "$_o")" = 0 ] &&
            _caps_env=$((_caps_env + 1))
    done
done
report "every engine half answers --capabilities -j" "$((NENG * 2))" "$_caps_env"

# THE ANSWER MUST BE TRUE, in both directions, and the truth is read off the parser's BEHAVIOUR
# rather than off the list the refusal prints — both now come from the same array, so comparing
# them would only prove the array equals itself.
#
# Listed: handed alone, the flag must not fall into the unknown-flag arm. It may still be
# refused (a verb needs a handle, a value flag needs its value); what it may not be is unknown.
#
# Not listed: the vocabulary is every flag ANY engine's same half lists, so no table names who
# owns what. A flag this engine leaves out must be refused before the host gate. The handle is
# one no engine claims, so a flag that was accepted after all is caught when the call gets
# through to the host gate's own sentence. Checking for refusal rather than for the unknown-flag
# wording keeps a friendlier named refusal legal (ne-resolve --sub-lang explains that the site
# tags no lyric with a language).
_caps_true=0
_caps_true_n=0
for _h in search resolve; do
    _vocab=$(for n in $ENGINES; do
        "shell/$n-$_h" --capabilities -j 2>/dev/null | jq -r '.flags[]'
    done | sort -u)
    for n in $ENGINES; do
        _mine=" $("shell/$n-$_h" --capabilities -j 2>/dev/null | jq -r '.flags | join(" ")') "
        for _f in $_vocab; do
            _caps_true_n=$((_caps_true_n + 1))
            case "$_mine" in
            *" $_f "*)
                [ "$(err_has 'unknown flag' "shell/$n-$_h" "$_f")" = 1 ] &&
                    _caps_true=$((_caps_true + 1))
                ;;
            *)
                [ "$(rc "shell/$n-$_h" "$_f" -- "$VIZ_URL")" = 1 ] &&
                    [ "$(err_has 'needs its own engine' "shell/$n-$_h" "$_f" -- "$VIZ_URL")" = 1 ] &&
                    _caps_true=$((_caps_true + 1))
                ;;
            esac
        done
    done
done
# The floor: every engine's two halves list at least what the checklist requires of every
# engine, so fewer cases than that means discovery collapsed, not that engines are small.
[ "$_caps_true_n" -ge "$((NENG * 10))" ] ||
    { echo "contract.sh: --capabilities discovered fewer than $((NENG * 10)) flag cases" >&2; exit 1; }
report "every --capabilities answer matches what the parser accepts" "$_caps_true_n" "$_caps_true"

# A flag that cannot act is REJECTED, not ignored (ARCH-cli-contract.md「门模型」): the verb asks
# about the engine, so a handle or a query is a usage error; nothing is resolved or searched,
# so -f, -n and -J have nothing to act on; and it is one verb, so a second is refused rather
# than one of them silently winning. The message is the claim for the two-verb case, because
# every refusal here exits 1.
_caps_gate=0
for n in $ENGINES; do
    [ "$(rc "shell/$n-resolve" --capabilities -- HANDLE)" = 1 ] &&
        [ "$(rc "shell/$n-resolve" --capabilities -f audio)" = 1 ] &&
        [ "$(rc "shell/$n-resolve" --capabilities -J)" = 1 ] &&
        [ "$(err_has 'two verbs' "shell/$n-resolve" --capabilities --info)" = 0 ] &&
        [ "$(rc "shell/$n-search" --capabilities -- query)" = 1 ] &&
        [ "$(rc "shell/$n-search" --capabilities -n 5)" = 1 ] &&
        [ "$(rc "shell/$n-search" --capabilities -J)" = 1 ] &&
        _caps_gate=$((_caps_gate + 1))
done
report "every --capabilities refuses a handle, a query and flags it cannot act on" "$NENG" "$_caps_gate"

# It answers ahead of every dependency gate, jq included: the envelope is how a caller learns
# what to ask, so it must come back on a machine that has none of the tools yet. /usr/bin has a
# jq on current macOS, so the bare PATH that proves --auth cannot prove this one: the path here
# is every command in /usr/bin and /bin EXCEPT jq, as links in a directory of its own. It
# isolates, it seeds nothing.
CAPS_BIN="$UT_TEST_TMP/caps-nojq-bin"
mkdir -p "$CAPS_BIN"
for _d in /usr/bin /bin; do
    for _c in "$_d"/*; do
        _b=${_c##*/}
        [ -e "$CAPS_BIN/$_b" ] || ln -s "$_c" "$CAPS_BIN/$_b"
    done
done
rm -f "$CAPS_BIN/jq" "$CAPS_BIN/yt-dlp" "$CAPS_BIN/curl"
env "PATH=$CAPS_BIN" command -v jq >/dev/null 2>&1 &&
    { echo "contract.sh: jq is still on the no-jq PATH — the claim below cannot fail here" >&2; exit 1; }
_caps_nod=0
for n in $ENGINES; do
    for _h in search resolve; do
        _o=$(env "PATH=$CAPS_BIN" "shell/$n-$_h" --capabilities -j 2>/dev/null) &&
            [ "$(jqv '.status=="ok"' "$_o")" = 0 ] && _caps_nod=$((_caps_nod + 1))
    done
done
report "every --capabilities -j needs no jq, curl or yt-dlp" "$((NENG * 2))" "$_caps_nod"

# THE READ-ONLY RESOLVE VERBS ARE HELD TO THE SAME RULE, and this replaces three lines that
# named ONE engine's ONE verb: `--info` — the verb EVERY engine has — had no coverage at all.
# `--info`, `--transcript` and `--parts` resolve no stream, so all three stream-format flags
# are values they cannot act on.
#
# Which verbs an engine HAS is read from its own `--capabilities -j`, held true above. Two
# nearer sources were tried before that verb existed and both lie. An error string: a missing
# verb is reported two different ways (`yt-resolve --parts` says "unknown flag",
# `bili-resolve --transcript` says the site carries no captions), so a probe keyed on either
# message concludes the wrong thing about the other engine. And `-h`: `bili-resolve -h`
# explains "There is no --transcript" — capability by absence, stated in the help — so a grep
# for the verb MATCHES on the engine that does not have it. Both mistakes end the same way:
# counting a refusal that happened because the VERB is absent as proof the FLAG was rejected.
# Green for the wrong reason is what this discovery exists to avoid, and it is the reason the
# count below is 12 and not 15.
#
# The claim is the MESSAGE, for the same reason: an absent verb and a refused flag both exit
# 1. And the handle is one no engine claims, which keeps every case offline AND pins the gate
# ORDER — a flag error must not need a good handle to be reported.
_ro_verb_has() {
    local _caps
    _caps=" $("shell/$1-resolve" --capabilities -j 2>/dev/null | jq -r '.flags | join(" ")' 2>/dev/null) " || true
    case "$_caps" in *" $2 "*) return 0 ;; *) return 1 ;; esac
}
_ro=0
_ro_n=0
for n in $ENGINES; do
    for _v in --info --transcript --parts --items; do
        _ro_verb_has "$n" "$_v" || continue
        for _bad in "-f audio" "-S abr" "--quality low"; do
            _ro_n=$((_ro_n + 1))
            [ "$(err_has "does not apply to $_v" "shell/$n-resolve" $_v $_bad -- "$VIZ_URL")" = 0 ] &&
                _ro=$((_ro + 1))
        done
    done
done
# >= 6, not a literal: two engines x one shared verb x three flags is the floor, and engine
# #3 or a fourth read-only verb must RAISE this, never break the line.
[ "$_ro_n" -ge 6 ] ||
    { echo "contract.sh: fewer than six read-only verb x format-flag cases discovered" >&2; exit 1; }
report "every read-only resolve verb refuses a format flag" "$_ro_n" "$_ro"

# THE OTHER HALF OF THAT PRODUCT: with no bad flag beside it, each read-only verb must be
# ACCEPTED. What comes back is still a refusal — the handle belongs to no engine — but it has
# to be the HOST gate's refusal, and that is the claim: the verb parsed, the argv cleared the
# flag gate, and only the site was wrong.
#
# It exists because ARCH-engine.md「调用面」 prints these exact argv as the way to CALL an
# engine, and nothing held them. `-j` ahead of the verb, `--` before the handle, a companion
# flag in its documented place — reorder any of it and the example goes silently wrong, because
# an absent verb and a refused host both exit 1 and a caller reading the number cannot tell
# which happened. The check above cannot cover this: it hands every verb a BAD flag, so it
# proves the flag gate fires, never that the clean line the doc prints gets through it.
#
# The message is the discriminator, and it is the host gate's own sentence — engine-agnostic on
# purpose, so engine #3's copy matches it the day the pair lands. An engine that does not accept
# the verb answers `unknown flag '<verb>' (resolve flags: …)` and goes red here while its exit
# code stays exactly 1.
#
# What neither check catches, stated so nobody reads more into the count: a verb DELETED from
# one engine. Discovery adapts — the case simply stops being generated — and pinning it would
# need the per-engine table of who owns what that this section exists to do without. The floor
# below catches the collapse, not the retreat.
_ro_host=0
_ro_host_n=0
for n in $ENGINES; do
    for _v in --info --transcript --parts --items; do
        _ro_verb_has "$n" "$_v" || continue
        # The companion flag rides along where the documented line has one — and whether
        # THIS engine has it is discovered, never tabled. --sub-lang is --transcript's and
        # nothing else's, and a check that drops it is not running the example; but a caption
        # track and a LANGUAGE CHOICE are two capabilities, not one, and the second engine to
        # grow --transcript had only the first (NetEase serves one lyric per song and tags it
        # with no language, so it refuses --sub-lang rather than accepting a flag it cannot
        # act on). Hardcoding the pair sent that engine a flag it does not have and read the
        # resulting flag refusal as a failure to reach the host gate. Same probe as the verb
        # discovery above, pointed at the companion.
        _with=""
        case "$_v" in
        --transcript) _ro_verb_has "$n" "--sub-lang" && _with="--sub-lang zh-Hans" ;;
        esac
        _ro_host_n=$((_ro_host_n + 1))
        [ "$(err_has "needs its own engine" "shell/$n-resolve" -j $_v $_with -- "$VIZ_URL")" = 0 ] &&
            _ro_host=$((_ro_host + 1))
    done
done
# >= NENG, not a literal: --info is the verb EVERY engine has, so one case per discovered
# engine is the floor and engine #3 raises it.
[ "$_ro_host_n" -ge "$NENG" ] ||
    { echo "contract.sh: read-only verbs discovered for fewer than $NENG engines" >&2; exit 1; }
report "every read-only verb reaches the host gate" "$_ro_host_n" "$_ro_host"

# --auth: the cookie DECISION, stated over every discovered engine. It is the one resolve
# verb that takes no handle, makes no request and runs no yt-dlp, so all four of those are
# what these checks pin. All of it is hermetic, which is why it sits above the --offline cut.
#
# The envelope's own rule is pinned too — auth=="cookie" IFF cookie_browser is not "none"
# AND profile_found — because it is the line a third engine is likeliest to get subtly
# wrong: reporting "cookie" from the env var alone, without checking the profile is really
# there, which is exactly the case that silently degrades to anonymous at play time.
#
# What no check here claims, and none can: that the login behind those cookies is valid, or
# that a valid one is worth anything. Measured 2026-08-26 — 3159 cookies extracted from
# chrome, the profile browser-confirmed logged in, and this site still served exactly the
# anonymous audio ladder because the account is not a premium member. The verb reports what
# is SENT; what is ACCEPTED needs an authenticated round trip this suite does not make, and
# what that buys is an account-tier question no envelope here answers.
_auth=0
for n in $ENGINES; do
    [ "$(jq_ok '.status=="ok" and .engine=="'"$n"'"
                and (.auth=="cookie" or .auth=="anonymous")
                and (.cookie_browser|type)=="string"
                and (.profile_found|type)=="boolean"
                and ((.auth=="cookie") == (.cookie_browser!="none" and .profile_found))' \
            "shell/$n-resolve" --auth -j)" = 0 ] && _auth=$((_auth + 1))
done
report "every engine answers --auth -j" "$NENG" "$_auth"

# The set-once knob is <ENGINE>_COOKIE_BROWSER, upper-cased from the engine name — the same
# concatenation-not-a-registry convention the command names follow. Asserting it over every
# discovered engine is what MAKES it a convention rather than two coincidences, and it is on
# the add-an-engine checklist for that reason. Upper-cased with `tr`, never with the
# bash-4 case-conversion expansion: the floor here is 3.2.
_anon=0
for n in $ENGINES; do
    _v="$(echo "$n" | tr '[:lower:]' '[:upper:]')_COOKIE_BROWSER"
    [ "$(jq_ok '.auth=="anonymous" and .cookie_browser=="none" and .profile_found==false' \
            env "$_v=none" "shell/$n-resolve" --auth -j)" = 0 ] && _anon=$((_anon + 1))
done
report "every engine honours _BROWSER=none" "$NENG" "$_anon"

# The DISCRIMINATING input, and the reason this section needs no broken build to trust it: a
# browser name that is not in the case arm at all. cookie_browser is then NOT "none", yet the
# profile cannot exist, so the only correct answer is "anonymous". An engine that derives auth
# from the env var alone — the plausible shortcut, and the one that silently degrades to
# anonymous at play time while reporting "cookie" — answers "cookie" here and goes red. No
# other check in this file separates those two implementations.
_bogus=0
for n in $ENGINES; do
    _v="$(echo "$n" | tr '[:lower:]' '[:upper:]')_COOKIE_BROWSER"
    [ "$(jq_ok '.auth=="anonymous" and .cookie_browser=="definitely-not-a-browser"
                and .profile_found==false' \
            env "$_v=definitely-not-a-browser" "shell/$n-resolve" --auth -j)" = 0 ] &&
        _bogus=$((_bogus + 1))
done
report "an unknown browser is anonymous" "$NENG" "$_bogus"

# A flag that cannot act is REJECTED, not ignored (ARCH-cli-contract.md「门模型」). --auth asks
# about the engine, so a handle is a usage error; -f selects a stream format and --auth
# resolves no stream; -J returns the raw yt-dlp record and --auth runs no yt-dlp.
for _bad in "--auth -- HANDLE" "--auth -f video" "--auth -J"; do
    _r=0
    for n in $ENGINES; do
        # shellcheck disable=SC2086
        [ "$(rc "shell/$n-resolve" $_bad)" = 1 ] && _r=$((_r + 1))
    done
    report "every engine refuses ${_bad}" "$NENG" "$_r"
done

# --auth answers ahead of the dependency gate, the way -V does: it reports how the engine is
# configured, so needing the tool it describes would be backwards. Prose mode is the form
# that proves it — it needs no jq either, so nothing but the script itself is on the path.
# The guard above the loop is what stops the claim passing vacuously on a machine where
# yt-dlp happens to live in /usr/bin.
NODEP_PATH="/usr/bin:/bin"
# The premise of the loop below, as an ABORT: on a machine where yt-dlp lives in /usr/bin the
# claim passes vacuously, and that is this file's problem to notice, not a check to count.
env "PATH=$NODEP_PATH" command -v yt-dlp >/dev/null 2>&1 &&
    { echo "contract.sh: yt-dlp is on the bare PATH — the no-dependency claim below cannot fail here" >&2; exit 1; }
_nod=0
for n in $ENGINES; do
    # `env`, not a `VAR=x rc …` prefix: an assignment in front of a FUNCTION call persists
    # in bash after the call returns, and a leaked PATH would silently reshape every check
    # below this line.
    [ "$(env "PATH=$NODEP_PATH" "shell/$n-resolve" --auth >/dev/null 2>&1; echo $?)" = 0 ] &&
        _nod=$((_nod + 1))
done
report "every engine --auth needs no yt-dlp" "$NENG" "$_nod"

# refusals <url> — how many engines reject it as a USAGE error (1)? A rejected host dies
# before the dependency gate, so a refusal costs ~20ms and no network.
#
# THE PROXY IS WHAT PUTS THIS SECTION OFFLINE. For a real URL one engine does NOT refuse, and
# that engine used to go on and extract it — 2.5s of live yt-dlp per call, for an answer this
# function throws away: it counts REFUSALS. Pointed at an unreachable proxy the claimer fails
# with 2 (network) in ~0.8s instead of succeeding with 0, the refusers still exit 1 before any
# transport exists, and the count — the only thing asserted — is identical. Same dead proxy
# the failure-taxonomy section uses, and it works with the cable out.
refusals() {
    local u=$1 n r=0
    for n in $ENGINES; do
        [ "$(http_proxy=$NOPROXY https_proxy=$NOPROXY rc "shell/$n-resolve" -j -- "$u")" = 1 ] && r=$((r + 1))
    done
    echo $r
}

# A real URL is claimed by EXACTLY ONE engine: the other N-1 refuse it with 1 — usage, not
# extraction failure, because nothing was attempted and nothing is retryable.
report "only 1 engine claims a yt URL"   $((NENG - 1)) "$(refusals "https://www.youtube.com/watch?v=$MEDIA_ID")"
report "only 1 engine claims a bili URL" $((NENG - 1)) "$(refusals 'https://www.bilibili.com/video/BV1mL411E7Fb')"
# The ordering probe: `url_host` strips userinfo BEFORE the port, so `user:pass@host`
# resolves to the host. Swap those two expansions — a plausible tidy-up — and this resolves
# to host `user`, is refused by every engine, and nothing else in this file notices.
report "userinfo stripped before port"   $((NENG - 1)) "$(refusals "https://user:pass@www.youtube.com/watch?v=$MEDIA_ID")"

# A confusable is refused by EVERY engine. These are the shapes `url_host`'s expansion ORDER
# decides, and the two plain URLs above exercise three of its eight lines:
#   evil<host>.com    an explicit host list, never a substring test
#   <host>@evil.com   the LAST `@` is the separator, the way browsers read it
#   https:///         an empty host must match nothing
#   <host>.           trailing dot refused — the safe direction, pinned so a change is deliberate
for u in 'https://evilyoutube.com/watch?v=x' 'https://evilbilibili.com/x' \
         'https://evilmusic.163.com/song?id=1' \
         'https://youtube.com@evil.com/' 'https://bilibili.com@evil.com/' \
         'https://music.163.com@evil.com/song?id=1' \
         'https:///watch?v=x' 'https://youtube.com./watch?v=x'; do
    report "all refuse ${u#https://}" "$NENG" "$(refusals "$u")"
done
# The opposite failure is just as real: a host list tightened too far silently drops a
# spelling users actually type. youtu.be is the one every share button produces.
#
# The claim is the GATE, so the assertion is "not refused" rather than "resolved": under the
# same dead proxy a gate that ACCEPTS this host reaches the transport and fails 2, and a gate
# that dropped it dies at 1 without one. Extraction itself is proved on the canonical URL
# form by the resolve envelope, live, in the half below.
report "yt-resolve still takes youtu.be" 1 \
    "$([ "$(http_proxy=$NOPROXY https_proxy=$NOPROXY rc shell/yt-resolve -j -- https://youtu.be/$MEDIA_ID)" != 1 ] && echo 1 || echo 0)"

# ── ONE SONG, FOUR SPELLINGS, ONE CANONICAL URL ────────────────────────────────────────────
# Every handle grammar in this suite is per-engine and duplicated (each `normalize_target` is
# its own), so this is the shape of check that has to be written once per engine. It is here
# rather than in the live half because none of it needs the site: the ERROR ENVELOPE reports
# the canonical url, so the dead proxy turns a resolve into a ~0.8s read of exactly the field
# under test.
#
# THE CLAIM: NetEase publishes one song under four spellings a person really pastes — the
# plain URL, the desktop app's single-page route (the id lives in a FRAGMENT there, invisible
# to a query parser), the mobile share host, and the bare number — and all four must
# canonicalise to the one string `ne-search` puts in results[].url. They must, because
# `t-playlist --add` stores that string: two spellings of one track that do not collapse are
# two rows in a playlist and two rows in the listening log.
#
# It cannot pass vacuously — an engine that passed the typed handle through would answer four
# different urls, and one that dropped the fragment route would answer 1 instead of 2.
NE_CANON="https://music.163.com/song?id=1824020871"
_nec=0
for h in "$NE_CANON" \
         'https://music.163.com/#/song?id=1824020871' \
         'https://y.music.163.com/m/song?id=1824020871' \
         '1824020871'; do
    [ "$(http_proxy=$NOPROXY https_proxy=$NOPROXY shell/ne-resolve -j -- "$h" 2>/dev/null |
         jq -r '.url' 2>/dev/null)" = "$NE_CANON" ] && _nec=$((_nec + 1))
done
report "ne: four spellings, one canonical url" 4 "$_nec"

# THE OTHER SIDE OF THAT GATE, and the reason it is not just "does it have an id". EVERY
# resource on this site is `?id=N` — an artist, an album, a playlist, a user — so an engine
# reading the query and ignoring the path takes `/artist?id=6452` and resolves SONG 6452:
# a different track, played silently, with nothing in the envelope to say so. That is the
# worst failure a resolver has, and `/artist?id=` below is the input that separates the two
# implementations. The other three are the ordinary refusals: right host and no id at all, a
# handle that is neither URL nor number, and a bare `-` that must not be read as a flag.
_ner=0
for h in 'https://music.163.com/artist?id=6452' \
         'https://music.163.com/song' \
         'notanid' \
         '-'; do
    [ "$(http_proxy=$NOPROXY https_proxy=$NOPROXY rc shell/ne-resolve -j -- "$h")" = 1 ] &&
        _ner=$((_ner + 1))
done
report "ne refuses a non-song handle" 4 "$_ner"

echo "── --parts: the offline gate ──────────────────────────────────────"
# The part-list pipeline itself (a part list feeding the store and the queue with no field
# renamed) runs live, on a real part list, in the half below.
# --parts runs ONE HTTP request and no yt-dlp — the same backwards gate --auth refuses, one
# verb over. Under the dead proxy this verb reaches its transport and fails with 2; a version
# that had grown a yt-dlp call on this path (to fetch the title, say) would die at the
# dependency gate on the bare PATH before any transport existed. The PATH guard further up is
# what stops it passing vacuously on a machine with yt-dlp in /usr/bin.
#
# THE EXIT CODE ALONE NO LONGER SEPARATES THOSE TWO. A missing dependency exits 2 as well
# (ARCH-cli-contract.md「退出码、TTY、依赖」), so `2` is the answer either way and an exit-code
# check would pass for precisely the regression it exists to catch. What still separates them
# is WHO SPOKE: the gate names the tool it wanted, a dead transport never does. So the value
# compared is the code AND the shape of the message — one run, both facts.
_parts_err=$(env "PATH=$NODEP_PATH" "http_proxy=$NOPROXY" "https_proxy=$NOPROXY" \
    shell/bili-resolve --parts -j -- "$BILI_ID" 2>&1 >/dev/null)
_parts_rc=$?
case "$_parts_err" in
*'required command not found'*) _parts_who=gate ;;
*) _parts_who=transport ;;
esac
report "--parts needs no yt-dlp"          "2 transport" "$_parts_rc $_parts_who"

echo "── the config file: precedence, and what it refuses ───────────────"
# WHY THESE CHECKS EXIST AT ALL. The config file is the one input in the suite that a user
# hand-writes and no command validates on their behalf, so its failure modes are not the
# usual ones: a knob that silently does not apply, a precedence order that quietly inverts,
# and — the one that matters — a file that reaches past the suite into the environment. Each
# check below feeds the discriminating input rather than the happy one: the happy path is
# already covered by every other check in this file, all of which now read a config.
CFGD=$(mktemp -d "${TMPDIR:-/tmp}/ting-cfg.XXXXXX")
CFG="$CFGD/config"

# Precedence, proved on ONE observable in three runs. `--engine` names a missing engine, so
# the error text says which value won — a real gate on a real entry point, no parsing of an
# internal. A naive loader that exported over the environment would answer "file" to the
# second, and one that ran before argv parsing would answer "env" to the third.
printf 'UT_DEFAULT_ENGINE=cfgwins\n' > "$CFG"
eng() { UT_CONFIG="$CFG" "$@" 2>&1 | sed -n "s/.*unknown engine '\([^']*\)'.*/\1/p"; }
report "config file sets the default engine" "cfgwins" \
    "$(eng shell/t-play -- https://x/y)"
report "environment beats the config file" "envwins" \
    "$(UT_DEFAULT_ENGINE=envwins eng shell/t-play -- https://x/y)"
report "TING_DEFAULT_ENGINE environment works" "envwins" \
    "$(TING_DEFAULT_ENGINE=envwins eng shell/t-play -- https://x/y)"
report "the flag beats both" "flagwins" \
    "$(UT_DEFAULT_ENGINE=envwins eng shell/t-play --engine flagwins -- https://x/y)"

# ── THE TWO SPELLINGS. Every knob answers to a TING_ name and to the pre-rename UT_ one, and
# "both work" is the easy half — the half that a mirroring loop gets wrong without anything
# else noticing is the case where BOTH are set at once. There is only one right answer to
# that (the new name is the one the suite documents, so it is the one that wins), and until
# this check existed the loop shipped with the opposite one: it filled in whichever side was
# missing and left UT_ standing when neither was.
report "TING_ beats UT_ when both are set" "tingwins" \
    "$(TING_DEFAULT_ENGINE=tingwins UT_DEFAULT_ENGINE=utwins eng shell/t-play -- https://x/y)"
printf 'TING_DEFAULT_ENGINE=tingcfg\n' > "$CFG"
report "a TING_ key in the config file is read" "tingcfg" \
    "$(eng shell/t-play -- https://x/y)"
report "…and the environment still beats it" "envwins" \
    "$(UT_DEFAULT_ENGINE=envwins eng shell/t-play -- https://x/y)"

# TING_CONFIG is not in that loop — it is the name that says WHICH FILE the loop then reads,
# so it is resolved before it, by hand, in all ten entry points. Same rule, proved separately.
CFG_TING="$CFGD/relocated"
printf 'UT_DEFAULT_ENGINE=relocated\n' > "$CFG_TING"
engv() { "$@" 2>&1 | sed -n "s/.*unknown engine '\([^']*\)'.*/\1/p"; }
report "TING_CONFIG relocates the file" "relocated" \
    "$(TING_CONFIG="$CFG_TING" engv shell/t-play -- https://x/y)"
report "TING_CONFIG beats UT_CONFIG" "relocated" \
    "$(TING_CONFIG="$CFG_TING" UT_CONFIG="$CFG" engv shell/t-play -- https://x/y)"
printf 'UT_DEFAULT_ENGINE=cfgwins\n' > "$CFG"

# ── THE PRE-RENAME PATHS, which are the actual promise. A user who ran the suite under its
# old name has a config at .../uting/config and a store at .../uting, and neither moved when
# the commands did. Nothing in the suite would go red if that chain were dropped — every
# other check in this file points the knobs somewhere disposable — which is exactly why the
# discriminating input has to be built here: an XDG root that holds ONLY the old spelling.
XDGD=$(mktemp -d "${TMPDIR:-/tmp}/ting-xdg.XXXXXX")
mkdir -p "$XDGD/uting"
printf 'UT_DEFAULT_ENGINE=legacycfg\n' > "$XDGD/uting/config"
report "a pre-rename config is still read" "legacycfg" \
    "$(env -u UT_CONFIG -u TING_CONFIG "XDG_CONFIG_HOME=$XDGD" \
        shell/t-play -- https://x/y 2>&1 | sed -n "s/.*unknown engine '\([^']*\)'.*/\1/p")"
mkdir -p "$XDGD/ting"
printf 'UT_DEFAULT_ENGINE=newcfg\n' > "$XDGD/ting/config"
report "…and the new path wins when both exist" "newcfg" \
    "$(env -u UT_CONFIG -u TING_CONFIG "XDG_CONFIG_HOME=$XDGD" \
        shell/t-play -- https://x/y 2>&1 | sed -n "s/.*unknown engine '\([^']*\)'.*/\1/p")"

# The store side of the same promise, and the same shape of discriminator: a state root that
# holds only the old spelling, with a playlist really written into it by the real command.
XSTATE=$(mktemp -d "${TMPDIR:-/tmp}/ting-xstate.XXXXXX")
mkdir -p "$XSTATE/uting"
printf '%s' "$ENV_JSON" |
    env -u UT_STATE_DIR -u TING_STATE_DIR "XDG_STATE_HOME=$XSTATE" \
        shell/t-playlist --add legacystore >/dev/null 2>&1
report "a pre-rename store is still written" 0 \
    "$([ -f "$XSTATE/uting/playlists/legacystore.json" ] && echo 0 || echo 1)"
report "…and read back from there" 1 \
    "$(env -u UT_STATE_DIR -u TING_STATE_DIR "XDG_STATE_HOME=$XSTATE" \
        shell/t-playlist --ls -j | jq '.playlists|length')"

# TING_STATE_DIR, with the UT_ name pointed at a DIFFERENT directory in the same command —
# so the check cannot pass by both names happening to mean the same place.
TSD=$(mktemp -d "${TMPDIR:-/tmp}/ting-tsd.XXXXXX")
TSD_UT=$(mktemp -d "${TMPDIR:-/tmp}/ting-utsd.XXXXXX")
printf '%s' "$ENV_JSON" |
    TING_STATE_DIR="$TSD" UT_STATE_DIR="$TSD_UT" shell/t-playlist --add tingstate >/dev/null 2>&1
report "TING_STATE_DIR is where the store writes" 0 \
    "$([ -f "$TSD/playlists/tingstate.json" ] && echo 0 || echo 1)"
report "…and the UT_ name set beside it did not get it" 1 \
    "$([ -f "$TSD_UT/playlists/tingstate.json" ] && echo 0 || echo 1)"

# THE SECURITY BOUNDARY, and the reason the file is read as data instead of sourced. A config
# that could be sourced would run the command substitution below and set PATH from a file the
# suite never audited; the check is that nine characters arrive as nine characters and that
# the file cannot name anything outside the suite's own namespaces.
printf 'PATH=/nonexistent\nLD_PRELOAD=/evil.so\nlowercase_key=x\nUT_INJECT=$(touch %s/PWNED)\n' \
    "$CFGD" > "$CFG"
# Asserted through a command that NEEDS its PATH after the file is read: `t-play --status -j`
# runs jq, so a PATH=/nonexistent that got through would fail it. `ting --version` could not
# — it answers from a builtin printf, and was green whether the key was inert or not.
report "a config key outside TING_/UT_/YT_/BILI_/NE_ is inert" "0" \
    "$(UT_CONFIG="$CFG" rc shell/t-play --status -j)"
report "command substitution is never executed" "absent" \
    "$([ -e "$CFGD/PWNED" ] && echo present || echo absent)"

# The player's own four. A file-level YT_IPC_SOCK would aim every player at one socket, so it
# is refused INSIDE an allowed namespace — which is the case a prefix allowlist alone misses.
# Only survival is asserted here: the socket is chosen on an mpv launch, and nothing in the
# offline half launches one, so an "the hijack socket was not created" check could not fail.
printf 'YT_IPC_SOCK=%s/hijack.sock\n' "$CFGD" > "$CFG"
report "the player still answers with YT_IPC_SOCK set" "0" \
    "$(UT_CONFIG="$CFG" rc shell/t-play --stop --all -j)"

# UT_VERSION is the constant from VERSION; a config file cannot overwrite it.
printf 'UT_VERSION=fake\n' > "$CFG"
report "UT_VERSION in config is refused" "$UT_VER" \
    "$(UT_CONFIG="$CFG" shell/ting --version | awk '{print $NF}')"

# A TYPO MUST BE LOUD. An emptied cycle would otherwise abort on the first keypress (an empty
# array expansion under set -u aborts on bash 3.2) and an unknown member would put a mode the
# engines reject under the v key — both a long way from the line the user actually wrote.
# Stated over EVERY cycle key rather than the one that happened to be written first: the four
# are built by one loop of the same three lines and validated by one function, so a check
# driving only the theme cycle is green on a fourth cycle that forgot to call it — which is
# exactly the shape UT_QUALITY_CYCLE arrived in.
# The first member of each is VALID and only the second is bogus: an emptied cycle dies at
# the line above this one, so a pair like `UT_MODE_CYCLE=bogus` would go green whether the
# member check ran or not. (Written as a literal list rather than a case inside $( ): on
# bash 3.2 a case pattern's `)` closes the command substitution.)
for spec in UT_MODE_CYCLE=audio,bogus UT_SORT_CYCLE=relevance,bogus \
    UT_THEME_CYCLE=nord,bogus UT_THEME_CYCLE=custom,bogus UT_QUALITY_CYCLE=auto,bogus \
    UT_LOOP_CYCLE=off,bogus; do
    printf '%s\n' "$spec" > "$CFG"
    # WHICH gate, not exit 1: the TTY refusal right after this one exits 1 too, so a bare exit
    # code was green with the member check deleted.
    report "${spec%%=*}: an unknown member is refused" cycle "$(ting_gate UT_CONFIG="$CFG" shell/ting q)"
done
printf 'UT_MAX_SEARCH_RESULTS=-5\n' > "$CFG"
# Over EVERY discovered engine, not just bili: the ceiling is cross-engine, so a check
# driving one of them would be green while the other spent an unbounded fetch.
for n in $ENGINES; do
    report "$n-search rejects a negative ceiling" "1" \
        "$(UT_CONFIG="$CFG" rc "shell/$n-search" -j -- q)"
done

# A restricted cycle must still START. The -f and -s defaults are validated against their
# cycles, so a literal "audio" default would make `UT_MODE_CYCLE=video` a config that cannot
# run — the user narrows the cycle and gets told their flag is wrong. Reaching the TTY refusal
# is the pass: it is the gate immediately after the one under test.
printf 'UT_MODE_CYCLE=video\nUT_SORT_CYCLE=duration\n' > "$CFG"
report "a narrowed cycle reaches the TTY gate, not a flag error" tty "$(ting_gate UT_CONFIG="$CFG" shell/ting q)"

# ── THE CUSTOM PALETTE'S ACCENT (UT_ACCENT / UT_ACCENT_LIGHT) ───────────────────────────
# Asserted on WHICH GATE ANSWERED, never on a bare exit 1: `custom` is a legal theme name, so
# a build with no accent gate at all reaches the TTY refusal and exits 1 too. A check reading
# only the code could not fail. ting_gate's `accent` arm is what separates the two.
#
# Every one of these runs offline — the flag/config gates all answer before the TTY refusal,
# which is the order the pair of checks above this one pins.
for _spec in zzz 40 99 0xd65d0 0xd65d0e/40 0xD65D0E/9; do
    printf 'YT_THEME=custom\nUT_ACCENT=%s\n' "$_spec" > "$CFG"
    report "UT_ACCENT=$_spec dies at the accent gate" accent \
        "$(ting_gate UT_CONFIG="$CFG" shell/ting q)"
done
# The three legal spellings pass the door. 0x, not #RRGGBB: a # cannot survive this config
# format's comment strip at all (ARCH-tui.md「为什么是 0x 而不是 #RRGGBB」), so the syntax
# a user would reach for first is the one that must not silently read back as empty.
for _spec in 0xd65d0e 33 97 0xd65d0e/33 0xD65D0E/97; do
    printf 'YT_THEME=custom\nUT_ACCENT=%s\n' "$_spec" > "$CFG"
    report "UT_ACCENT=$_spec is accepted" tty "$(ting_gate UT_CONFIG="$CFG" shell/ting q)"
done
# THE WRITE-BACK TRAP. The t key writes YT_THEME=custom into the user's own config, so a
# config can name custom long after the UT_ACCENT that justified it was cleared. Refusing to
# start there would lock the user out over a key they never typed — it must degrade, silently,
# to minimal. The pair matters: a build that dies on an unset accent still passes the row
# above it, because that row always sets one.
printf 'YT_THEME=custom\n' > "$CFG"
report "custom with no accent still starts" tty "$(ting_gate UT_CONFIG="$CFG" shell/ting q)"
printf 'YT_THEME=custom\nUT_ACCENT=\n' > "$CFG"
report "…and an explicitly empty one too" tty "$(ting_gate UT_CONFIG="$CFG" shell/ting q)"
# The light rung carries the same ruler and names ITSELF in the message — a shared validator
# that reported the wrong key would send the user editing the wrong line.
printf 'UT_ACCENT_LIGHT=nope\n' > "$CFG"
CFG_OUT=$(UT_CONFIG="$CFG" shell/ting q </dev/null 2>&1 || true)
case "$CFG_OUT" in
*"UT_ACCENT_LIGHT must be"*) CFG_HIT=yes ;;
*) CFG_HIT=no ;;
esac
report "UT_ACCENT_LIGHT is refused under its own name" "yes" "$CFG_HIT"
# Validated whether or not custom is the CURRENT theme: the t key can arrive at custom
# mid-session, and a gate that only fired on the startup theme would let a malformed spec
# through to the printf that builds an SGR — half an escape sequence, in the user's terminal.
printf 'YT_THEME=minimal\nUT_ACCENT=zzz\n' > "$CFG"
report "a bad accent is caught under a non-custom theme" accent \
    "$(ting_gate UT_CONFIG="$CFG" shell/ting q)"
# A cycle narrowed to custom alone must still start, like every other narrowed cycle above.
printf 'UT_THEME_CYCLE=custom\nUT_ACCENT=0xd65d0e/33\n' > "$CFG"
report "a cycle of just custom reaches the TTY gate" tty "$(ting_gate UT_CONFIG="$CFG" shell/ting q)"
report "--theme custom is accepted" tty "$(ting_gate shell/ting --theme custom q)"
# ARCH-tui.md「调用面」's custom line, run verbatim rather than printed.
report "…with an accent on it, as the doc prints it" tty \
    "$(ting_gate UT_ACCENT=0xd65d0e/33 shell/ting --theme custom "lofi hip hop")"
# --theme takes ONE name. The membership test is an exact compare over the name list, not a
# substring of it: "gruvbox onedark" IS a substring of that list and a substring gate would
# pass it, then fall off the end of set_theme's case with no accent set at all.
report "--theme rejects two names at once" mode "$(ting_gate shell/ting --theme "gruvbox onedark" q)"

# PROSE AND THE DOOR MAY NOT DIVERGE. The theme names used to be spelled four times; they are
# one constant now, but usage() stays literal English and can still drift from it. Both sides
# here are things the COMMAND said — the gate's own refusal message and its own --help — so
# this compares two live surfaces rather than grepping the source for the constant.
THEME_GATE_SET=$(shell/ting --theme __not_a_theme__ </dev/null 2>&1 |
    sed -n 's/.*must be one of: //p' | tr -d ' ' | tr ',' '\n' | sort | tr '\n' ' ')
THEME_HELP=$(shell/ting -h 2>&1 || true)
THEME_USAGE_FLAG=$(printf '%s\n' "$THEME_HELP" | tr '\n' ' ' |
    sed -e 's/.*Palette: //' -e 's/\. Every theme.*//' -e 's/(default)//' |
    tr '|' '\n' | tr -d ' ' | grep -v '^$' | sort | tr '\n' ' ')
# Joined into one line BEFORE the cut, the way the flag's list above already is: fourteen
# names is 128 columns on one help line, so the env list wraps like the flag's does, and a
# per-line regex would have read only the first half and called the other half a drift.
THEME_USAGE_ENV=$(printf '%s\n' "$THEME_HELP" | tr '\n' ' ' |
    sed -e 's/.*YT_THEME=//' -e 's/ *Palette family.*//' |
    tr '|' '\n' | tr -d ' ' | grep -v '^$' | sort | tr '\n' ' ')
report "usage()'s --theme list == the gate's" "$THEME_GATE_SET" "$THEME_USAGE_FLAG"
report "usage()'s YT_THEME list == the gate's" "$THEME_GATE_SET" "$THEME_USAGE_ENV"
# Not vacuous: the gate set must really hold names, or all three could agree on nothing.
# Written OUTSIDE the command substitution — on bash 3.2 a case pattern's `)` closes the
# `$( )`, the same trap the cycle loop above already carries a note about.
THEME_SET_OK=no
if [[ "$THEME_GATE_SET" == *minimal* && "$THEME_GATE_SET" == *custom* ]]; then THEME_SET_OK=yes; fi
report "…and that set really holds names" "yes" "$THEME_SET_OK"

# THE BROKEN CHECKOUT. Defaults now live in <checkout>/config and nowhere else, so a copy of
# a script without that file has no values at all. The failure must be this one line and exit
# 2 — not `set -u` reporting an unbound variable from 100 lines further down, which is what
# the first attempt produced when --version and --help were let through. Driven by really
# copying an entry point somewhere that has a VERSION and no config, which is exactly the
# shape of a half-installed checkout.
CFG_BROKE="$CFGD/broke"
mkdir -p "$CFG_BROKE/shell"
cp shell/t-play "$CFG_BROKE/shell/" && echo 0.0.0 > "$CFG_BROKE/VERSION"
report "no shipped defaults exits 2" "2" "$(rc "$CFG_BROKE/shell/t-play" --version)"
CFG_OUT=$("$CFG_BROKE/shell/t-play" --version 2>&1 || true)
case "$CFG_OUT" in
*"cannot read the shipped defaults"*) CFG_HIT=yes ;;
*) CFG_HIT=no ;;
esac
report "…naming the file, not an unbound variable" "yes" "$CFG_HIT"
rm -rf "$CFGD"

# ── A PASTE IS TEXT, NOT KEYS ───────────────────────────────────────────────────────────
# Hermetic because the STARTUP prompt is up before any search: ting asks for a query with
# nothing fetched, so the whole claim is provable with no packet sent.
#
# The payload carries a NEWLINE, and that is what makes the check discriminating rather than
# decorative. A build with bracketed paste off is not a build that mangles the text — tmux
# would simply deliver the bytes raw and the prompt would show `jazz` — so a single-line
# payload passes either way and proves nothing. With a line break in it the two builds part
# company: the fixed one joins the lines with a space and stays at the prompt, the broken one
# submits `jazz` on the newline and leaves the prompt for a search. Asserting on ONE LINE
# reading `jazz #ch` therefore fails for both halves of the bug this pins — the terminal mode
# never being asked for, and the prompt reading the paste's \e[200~ opener as its cancel key.
#
# `#` in the payload is not incidental: it is a bound key (row numbers) in the list, which is
# where a pasted query used to be run as commands, and keeping it here documents the class.
#
# The SAME pane then proves the other half of the Esc contract, and it is a clock claim: how
# long the prompt takes to close. `read -t` on the bash 3.2 floor takes whole seconds, so the
# reader that waits to see whether a byte follows an Esc held the prompt open for a full one
# before cancelling — and at the startup prompt nothing ever follows an Esc, because the app
# is leaving, so it was paid every time (measured 1062 ms from the keypress to exit; a second
# of dead terminal before the shell prompt returns reads as a hang). 600 ms is the
# discriminating bound: unreachable for a one-second wait, and four times the ~130 ms the
# tty-timed reader takes. It is spent as ONE sleep and ONE capture rather than a poll loop —
# a loop's own forks would inflate the very window being measured.

echo
echo "── a paste is text, not keys ──────────────────────────────────────"
if ! tmux_ok; then
    echo "  skip  (needs tmux for a real tty)"
else
    PS_TS="ctest-paste-$$"
    # A state dir of this check's own, for the reason every other section has one: the pane
    # inherits the tmux SERVER's environment, not this shell's, so the value is passed into
    # the command line rather than exported — the same reason the TUI section spells it out.
    PS_STATE=$(mktemp -d "${TMPDIR:-/tmp}/ting-paste.XXXXXX")
    # UT_CONFIG rides along for the same reason and one more: the three greps below name an
    # ENGLISH chrome string, and this pane's language comes from whichever config it reads.
    # The export at the top of this file reaches a pane only when THIS run happens to start
    # the tmux server; a developer who already had tmux open gets a server without it, the
    # pane reads their own config, and a `YT_LANG=zh` in it draws 搜索 where the grep wants
    # Search — three checks red on their machine and green here (reproduced 2026-09-03 with
    # `env -u UT_CONFIG tmux -L … new-session`). YT_LANG=en is pinned beside it because the
    # config is only one of the two ways the language is decided: blank means auto-detect,
    # so a zh* locale would draw the same 搜索 through an empty file. Both spelled out, the
    # way the TUI section spells its knobs out, so the pane's language is an input.
    tmux kill-session -t "$PS_TS" 2>/dev/null
    tmux new-session -d -s "$PS_TS" -x 80 -y 20 \
        "UT_STATE_DIR='$PS_STATE' TING_STATE_DIR='$PS_STATE' TMPDIR='$TMPDIR' UT_CONFIG='$UT_CONFIG' TING_CONFIG='$UT_CONFIG' YT_LANG=en UT_HISTORY=0 '$PWD/shell/ting'; echo __GONE__; sleep 5" 2>/dev/null
    pasted=0
    i=0
    while [ $i -lt 100 ]; do
        tmux capture-pane -t "$PS_TS" -p -J 2>/dev/null | grep -q 'Search' && { pasted=1; break; }
        sleep 0.05; i=$((i + 1))
    done
    report "the startup prompt is up with nothing fetched" 1 "$pasted"
    tmux set-buffer -b ctpaste 'jazz
#ch' 2>/dev/null
    tmux paste-buffer -p -b ctpaste -t "$PS_TS" 2>/dev/null
    kept=0
    i=0
    while [ $i -lt 60 ]; do
        tmux capture-pane -t "$PS_TS" -p -J 2>/dev/null | grep -q 'Search.*jazz #ch' && { kept=1; break; }
        sleep 0.05; i=$((i + 1))
    done
    # One match proves both halves: `Search.*jazz #ch` is the prompt's own line still holding
    # the text, which a submitted query would have replaced with a list.
    report "a pasted newline joins the query instead of submitting it" 1 "$kept"
    # Esc with the pasted text still on the line — the same cancel a user makes, and a harder
    # input than an empty prompt.
    tmux send-keys -t "$PS_TS" Escape 2>/dev/null
    sleep 0.6
    report "Esc closes the prompt inside 600ms, not a whole second" 1 \
        "$(tmux capture-pane -t "$PS_TS" -p -J 2>/dev/null | grep -c '__GONE__' | awk '{print ($1 > 0) ? 1 : 0}')"
    tmux kill-session -t "$PS_TS" 2>/dev/null
    rm -rf "$PS_STATE"
fi

echo
echo "── a cookie store that cannot be read ─────────────────────────────"
# The failure a user hit on 2026-09-22: their terminal app was not allowed to read Chrome's
# data folder, yt-dlp's directory walk reported "could not find chrome cookies database",
# every search failed as `unknown`, and the TUI said only "search failed". Reproduced with
# nothing seeded that a command reads as data: a HOME holding an EMPTY Chrome folder is a
# profile that exists and a store that does not — what the blocked read looks like to both
# the engines' existence check and yt-dlp's walk — and a real cookie file with its mode
# taken away is the Unix-permission twin. macOS privacy protection itself cannot be switched
# on for a scratch folder, so the `blocked` branch is the one state no check here reaches.
#
# The dead proxy keeps it offline AND is what makes the fallback checks discriminating:
# without the anonymous retry a verb stops at the cookie error (`cookies`); WITH it the
# retry reaches the transport and fails `network`.
CK_BASE="$UT_TEST_TMP/cookie-homes"
CK_CHROME="Library/Application Support/Google/Chrome"
[ "$(uname -s)" = Darwin ] || CK_CHROME=".config/google-chrome"
mkdir -p "$CK_BASE/missing/$CK_CHROME" "$CK_BASE/denied/$CK_CHROME/Default" "$CK_BASE/readable/$CK_CHROME/Default"
printf x >"$CK_BASE/denied/$CK_CHROME/Default/Cookies"; chmod 000 "$CK_BASE/denied/$CK_CHROME/Default/Cookies"
printf x >"$CK_BASE/readable/$CK_CHROME/Default/Cookies"
YT_WATCH="https://www.youtube.com/watch?v=$MEDIA_ID"
# `ck <home> <cmd…>`: the envelope's reason on stdout, the engine's stderr into CK_ERR. All
# three cookie knobs are set, so each engine reads chrome from the scratch HOME whichever it is.
CK_ERR="$UT_TEST_TMP/cookie.err"
ck() { local h=$1; shift
       HOME="$CK_BASE/$h" YT_COOKIE_BROWSER=chrome BILI_COOKIE_BROWSER=chrome NE_COOKIE_BROWSER=chrome \
           http_proxy=$NOPROXY https_proxy=$NOPROXY "$@" 2>"$CK_ERR" | jq -r '.reason // "none"' 2>/dev/null; }
ck_said() { grep -c "$1" "$CK_ERR" 2>/dev/null | awk '{print ($1 > 0) ? 1 : 0}'; }
report "search retries without an unreadable store" network "$(ck missing shell/yt-search -j -n 3 -- lofi)"
report "…and says the store is missing, and the fix" 1 "$(ck_said 'no chrome cookies: chrome has no cookie database - sign in')"
report "--info retries without it too" network "$(ck missing shell/yt-resolve --info -j -- "$YT_WATCH")"
# The permission twin reaches the classifier as a raw OS error on the cookie file, not as
# "could not find" — so it is driven through the OTHER wrapped call site, and the pair covers
# both call sites and both wordings with one launch each.
report "--transcript retries on a cookie file it may not open" network \
    "$(ck denied shell/yt-resolve --transcript -j -- "$YT_WATCH")"
report "…and names the error and the file" 1 "$(ck_said 'no chrome cookies: Permission denied reading ')"
# The probe, the diagnosis and the wrapper are COPIED into every engine that reads cookies
# (site knowledge stays per engine), so each copy is driven, not only yt's: its own --info,
# its own knob, its own prefix on the sentence.
for n in bili ne; do
    case $n in
    bili) CK_URL="https://www.bilibili.com/video/BV1mL411E7Fb" ;;
    ne) CK_URL="https://music.163.com/song?id=1824020871" ;;
    esac
    report "$n-resolve --info retries without the store" network "$(ck missing "shell/$n-resolve" --info -j -- "$CK_URL")"
    report "…and $n-resolve says why" 1 "$(ck_said "^$n-resolve: no chrome cookies: ")"
done
# --auth: the decision was always `cookie` whenever the folder existed; the new field is
# whether the store can be read, in both directions so an always-false field fails too —
# over every engine that has --auth.
ck_auth() { HOME="$CK_BASE/$1" YT_COOKIE_BROWSER=chrome BILI_COOKIE_BROWSER=chrome NE_COOKIE_BROWSER=chrome \
                "shell/$2-resolve" --auth -j 2>/dev/null | jq -r '.cookie_readable'; }
for n in $ENGINES; do
    report "$n --auth: a missing store is not readable" false "$(ck_auth missing "$n")"
    report "$n --auth: a file it may not open is not readable" false "$(ck_auth denied "$n")"
    report "$n --auth: a readable store is" true "$(ck_auth readable "$n")"
done
# The TUI's half: under -j the reason is on stdout and the advice on stderr, and a failed
# first search printed neither. It must lead with the reason and carry the advice.
if tmux_ok; then
    CK_TS="ctest-cookie-$$"
    CK_STATE=$(mktemp -d "${TMPDIR:-/tmp}/ting-cookie.XXXXXX")
    tmux kill-session -t "$CK_TS" 2>/dev/null
    tmux new-session -d -s "$CK_TS" -x 200 -y 20 \
        "HOME='$CK_BASE/missing' YT_COOKIE_BROWSER=chrome http_proxy='$NOPROXY' https_proxy='$NOPROXY' UT_STATE_DIR='$CK_STATE' TING_STATE_DIR='$CK_STATE' TMPDIR='$TMPDIR' UT_CONFIG='$UT_CONFIG' TING_CONFIG='$UT_CONFIG' YT_LANG=en UT_HISTORY=0 '$PWD/shell/ting' lofi; echo __GONE__; sleep 5" 2>/dev/null
    said=0
    i=0
    while [ $i -lt 200 ]; do
        tmux capture-pane -t "$CK_TS" -p -J 2>/dev/null | grep -qE 'search failed \(network\): no chrome cookies: ' && { said=1; break; }
        sleep 0.05; i=$((i + 1))
    done
    report "a failed first search gives the reason, then the fix" 1 "$said"
    tmux kill-session -t "$CK_TS" 2>/dev/null
    rm -rf "$CK_STATE"
else
    echo "  skip  (needs tmux for a real tty)"
fi
chmod 600 "$CK_BASE/denied/$CK_CHROME/Default/Cookies" 2>/dev/null

if [ "$OFFLINE" = 1 ]; then
    echo
    echo "── the live half: SKIPPED (--offline) ─────────────────────────────"
    # Named, not counted: what is missing is the only thing that makes a green here smaller
    # than a green run, and a reader who cannot see the list will assume it is nothing.
    echo "  not run: both engines' live envelopes and their parity, --transcript, the dead"
    echo "  id, the network taxonomy, and the TUI under tmux. Run without --offline to push."
    report_real_config
    report_real_state
    summary
fi

export UT_STATE_DIR
UT_STATE_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ting-live-store.XXXXXX")

# ---- the quiet half of the cookie fallback, which needs the site ------------------------
# The offline block proves a blocked store fails loudly. The case the user actually sat in
# is the other one: the anonymous retry SUCCEEDS, results arrive, and nothing says they came
# without the login — while the status line said "signed in". Both halves are read off one
# live frame: the status segment and the notice under the title. The pane is STARTED here and
# READ after the fetch batch below, so its live search runs alongside the batch instead of
# ahead of it.
CQ_TS="ctest-cookieq-$$"
CQ_UP=0
if tmux_ok; then
    CQ_STATE=$(mktemp -d "${TMPDIR:-/tmp}/ting-cookieq.XXXXXX")
    tmux kill-session -t "$CQ_TS" 2>/dev/null
    tmux new-session -d -s "$CQ_TS" -x 200 -y 24 \
        "HOME='$CK_BASE/missing' YT_COOKIE_BROWSER=chrome UT_STATE_DIR='$CQ_STATE' TING_STATE_DIR='$CQ_STATE' TMPDIR='$TMPDIR' UT_CONFIG='$UT_CONFIG' TING_CONFIG='$UT_CONFIG' YT_LANG=en UT_HISTORY=0 '$PWD/shell/ting' lofi; echo __GONE__; sleep 30" 2>/dev/null && CQ_UP=1
fi

# ---- fetch once, assert many, and fetch them ALL AT ONCE ------------------------------
# A live engine call costs a yt-dlp start (~2s) whether one question is asked of its answer
# or four, and nearly every assertion below is about an envelope's SHAPE. Two identical
# queries cannot answer a shape question differently, so each query is fetched once and
# interrogated as many times as it has claims. The command is still the real entry point and
# the answer is still its real stdout.
#
# A call keeps its own invocation when its ARGV differs (-j and -J are two envelopes, not two
# questions about one), when its ENVIRONMENT differs (the proxy checks), or when its INPUT
# differs (a second query, chosen for content the first one does not have).
#
# AND THEY ALL GO OUT TOGETHER. Twenty-odd of these calls have no dependency on each other —
# they ask different engines different questions — and run one after another they were most
# of this file's wall clock: ~70s of a ~130s run, spent waiting on a network that was idle
# between calls. They are fired as background jobs and collected once; the assertions below
# read the answers off disk, in the order they always read them, and are not otherwise
# touched. What is NOT here is anything whose input is another fetch's output (the offset
# block picks its handle out of a search) — that goes in the second wave, after this one.
#
# The suite's own runtime is not a claim: nothing below asserts on how long a fetch took
# (see `bili --parts` for why that check went), so overlapping them cannot make anything pass
# that would otherwise fail.
LIVE="$UT_TEST_TMP/live"; mkdir -p "$LIVE"
# `spawn_once <slot> <cmd…>` — the command's stdout, stderr and exit code, kept by name.
spawn_once() {
    local slot=$1; shift
    { "$@" >"$LIVE/$slot.out" 2>"$LIVE/$slot.err"; echo $? >"$LIVE/$slot.rc"; } &
}
# `spawn <slot> <cmd…>` — the same, but it does not report the NETWORK as a finding. An
# envelope whose reason is `network` is the one answer this suite already knows means "ask
# again": Bilibili's view endpoint throttles a repeated caller (measured: one refusal in
# three back-to-back calls, and the same handle answers ok on the next), and a red that is
# the site's rate limiter still costs someone a look. Two extra tries, only ever paid on a
# fetch that already failed; an engine that is really broken answers `network` three times
# and is reported. The checks whose SUBJECT is a network failure use spawn_once — there the
# reason is the finding.
spawn() {
    local slot=$1; shift
    {
        local try=0
        while :; do
            "$@" >"$LIVE/$slot.out" 2>"$LIVE/$slot.err"; echo $? >"$LIVE/$slot.rc"
            try=$((try + 1))
            [ $try -ge 3 ] && break
            grep -q '"reason":"network"' "$LIVE/$slot.out" 2>/dev/null || {
                if [ "$slot" = "bili-zh" ] && [ "$(jq -r '.count // 0' "$LIVE/$slot.out" 2>/dev/null)" -lt 15 ]; then
                    sleep 1; continue
                fi
                break
            }
            sleep 2
        done
    } &
}
out() { cat "$LIVE/$1.out" 2>/dev/null; }
src() { cat "$LIVE/$1.rc" 2>/dev/null; }

printf 'UT_MAX_SEARCH_RESULTS=3\n' > "$UT_TEST_TMP/cfg-cap"
printf 'UT_SEARCH_RESULTS=4\n'     > "$UT_TEST_TMP/cfg-dflt"
# The searches go first and are waited on BY PID, because one thing downstream needs an
# answer out of them (the offset block's handle) and everything else does not. Waiting on the
# whole batch to start that one would serialise the two slowest calls in the file behind each
# other for no reason.
SEARCH_PIDS=""
for n in $ENGINES; do
    spawn "search-$n" shell/"$n"-search -j -n 10 -- lofi
    SEARCH_PIDS="$SEARCH_PIDS $!"
done
for n in $ENGINES; do
    spawn "searchJ-$n"   shell/"$n"-search  -J -n 5  -- lofi
    spawn "cap-$n"       env UT_CONFIG="$UT_TEST_TMP/cfg-cap"  shell/"$n"-search -j -n 20 -- lofi
    spawn "dflt-$n"      env UT_CONFIG="$UT_TEST_TMP/cfg-dflt" shell/"$n"-search -j -- lofi
done
spawn yt-resolve   shell/yt-resolve   -j -- "$MEDIA_ID"
spawn yt-info      shell/yt-resolve   --info -j -- "$MEDIA_ID"
spawn yt-trans     shell/yt-resolve   --transcript -j -- "$CAPTIONED"
spawn yt-transJ    shell/yt-resolve   --transcript -J -- "$CAPTIONED"
spawn yt-nocap     shell/yt-resolve   --transcript -j -- "$BARE"
spawn yt-argv      shell/yt-search    -j -n 1 -- --status
spawn yt-dead      shell/t-play      -j -- AAAAAAAAAAA
spawn bili-resolve shell/bili-resolve -j -- "$BILI_ID"
spawn bili-info    shell/bili-resolve --info -j -- "$BILI_ID"
spawn bili-zh      shell/bili-search  -j -n 20 -M 600 -- 周杰伦
spawn yt-zh        shell/yt-search    -J -n 15 -- 周杰伦
spawn bili-offset  shell/bili-resolve -j -- "https://www.bilibili.com/video/$BILI_PARTS_ID?p=2&t=601"
spawn bili-parts   shell/bili-resolve --parts -j -- "$BILI_PARTS_ID"
spawn bili-part1   shell/bili-resolve --parts -j -- "$BILI_ID"
spawn bili-nopart  shell/bili-resolve --parts -j -- av999999999999
spawn bili-route   shell/t-play      --engine bili -j -- BV1111111111
spawn ne-vip       env NE_INCLUDE_VIP=1 shell/ne-search -j -n 20 -- 周杰伦
spawn ne-trans     shell/ne-resolve --transcript -j -- "$NE_LYRIC"
spawn ne-notrans   shell/ne-resolve --transcript -j -- "$NE_SILENT"
spawn ne-novip     shell/ne-search  -j -n 20 -- 周杰伦
spawn yt-items     shell/yt-resolve   --items -j -- "$YT_LIST"
spawn bili-items   shell/bili-resolve --items -j -- "$BILI_MENU"
spawn ne-items     env NE_INCLUDE_VIP=1 shell/ne-resolve --items -j -- "$NE_LIST"
spawn ne-items-def shell/ne-resolve   --items -j -- "$NE_LIST"
spawn bili-fav     shell/bili-resolve --items -j -- "$BILI_FAV"
spawn bili-season  shell/bili-resolve --items -j -- "$BILI_SEASON"
spawn yt-channel   shell/yt-resolve   --items -j -- "$YT_CHANNEL"
# The two halves of one cursor round trip, fired TOGETHER: the second is not waiting on the
# first's token, it asserts that the token the first hands out is the offset the second reads.
spawn yt-big1      shell/yt-resolve   --items -j -- "$YT_BIG"
spawn yt-big2      shell/yt-resolve   --items -j --cursor o:500 -- "$YT_BIG"
spawn yt-nolist    shell/yt-resolve   --items -j -- PLzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz
spawn bili-nofav   shell/bili-resolve --items -j -- ml999999999999
spawn bili-nomenu  shell/bili-resolve --items -j -- am999999999
spawn ne-nolist    shell/ne-resolve   --items -j -- "https://music.163.com/album?id=999999999"
for n in $ENGINES; do
    spawn_once "net-j-$n" env http_proxy="$NOPROXY" https_proxy="$NOPROXY" shell/"$n"-search -j -n 2 -- lofi
    spawn_once "net-t-$n" env http_proxy="$NOPROXY" https_proxy="$NOPROXY" shell/"$n"-search    -n 2 -- lofi
done

# The dependent four, fired as soon as their handle exists rather than after the whole batch.
# shellcheck disable=SC2086
wait $SEARCH_PIDS
for n in $ENGINES; do
    SU=$(jq -r '[.results[] | select(.live_status == null and (.duration|type) == "number")]
                | sort_by(.duration)[0].url // empty' "$LIVE/search-$n.out")
    # No row to resolve is this file's problem, not the engine's — an abort, not a check.
    [ -n "$SU" ] ||
        { echo "contract.sh: $n-search returned no non-live row — no handle to test the offset on" >&2; exit 1; }
    case "$SU" in *\?*) SEP='&' ;; *) SEP='?' ;; esac
    spawn "off601-$n" shell/"$n"-resolve -j -- "${SU}${SEP}t=601"
    spawn "off0-$n"   shell/"$n"-resolve -j -- "${SU}${SEP}t=0"
done
wait   # …and now everything, both waves

echo
echo "── an unreadable cookie store, when the search still works ────────"
if ((CQ_UP)); then
    cq_frame=""
    i=0
    while [ $i -lt 400 ]; do
        cq_frame=$(tmux capture-pane -t "$CQ_TS" -p -J 2>/dev/null)
        printf '%s\n' "$cq_frame" | grep -qE '[0-9]+ results|__GONE__' && break
        sleep 0.05; i=$((i + 1))
    done
    report "results still arrive without the store" 1 \
        "$(printf '%s\n' "$cq_frame" | grep -cE '[0-9]+ results' | awk '{print ($1 > 0) ? 1 : 0}')"
    report "…the status line does not say signed in" 1 \
        "$(printf '%s\n' "$cq_frame" | grep -c 'anonymous (cookies unreadable)' | awk '{print ($1 > 0) ? 1 : 0}')"
    report "…and a notice says why" 1 \
        "$(printf '%s\n' "$cq_frame" | grep -c 'Search: no chrome cookies: ' | awk '{print ($1 > 0) ? 1 : 0}')"
    tmux kill-session -t "$CQ_TS" 2>/dev/null
    rm -rf "$CQ_STATE"
else
    echo "  skip  (needs tmux for a real tty)"
fi

echo "── the config file, on a real fetch ───────────────────────────────"
# THE TWO CLAIMS THAT ONLY A REAL FETCH CAN SETTLE. Everything about the config file in the
# offline half is about parsing and refusal; these two are about the values actually reaching
# the code that spends requests, and the observable is the row count in a real envelope.
#
# Both are stated over EVERY discovered engine, because the whole point of these two keys is
# that they are cross-engine: a check driving one of them would be green while the other
# ignored the ceiling entirely.
for n in $ENGINES; do
    # The ceiling. -n asks for 20 and the file caps at 3, so an engine that honours it
    # returns at most 3 — and one that does not returns up to 20. That gap IS the check:
    # before this key, bili-search capped at ten pages and yt-search was bounded only by what
    # the site stopped sending, so "unclamped" is a real implementation, not a strawman.
    report "$n-search honours the row ceiling" "true" \
        "$(out "cap-$n" | jq -r '(.results | length) <= 3' 2>/dev/null)"
    # The shared default really is shared. No -n at all, so the count comes from
    # UT_SEARCH_RESULTS — the check that would have caught the drift the centralisation was
    # for: an engine still carrying its own inlined 25 answers with more than 4 here.
    report "$n-search takes -n from the config" "true" \
        "$(out "dflt-$n" | jq -r '(.results | length) <= 4' 2>/dev/null)"
done

echo "── search envelope ────────────────────────────────────────────────"
# ONE LIVE SEARCH PER ENGINE, for the whole live half: the envelope checks here, the
# cross-engine parity check further down, the row-is-a-call invariant, and the offset block's
# choice of handle all read the same two answers. They used to make their own calls — six
# yt-search round trips a run, all of them the same query.
YT_S=$(out search-yt)
YT_SJ=$(out searchJ-yt)
# `<=10`, not `==10`: `-n` says how many to FETCH and never how many come back — -m/-M have
# always shortened it, and since the container gate the row count can drop by one on a query
# whose page holds a channel (ARCH-engine.md「kind 与 access」). The ceiling-is-honoured
# claim is the cap-/dflt- pair above, which is where it belongs; asserting an exact count here
# only ever held because `lofi`'s top ten happen to be all videos, and would have gone red on
# a YouTube ranking change rather than on a bug of ours.
report "search -j envelope" 0 \
    "$(jqv '.query and .count and (.results|length)>0 and (.results|length<=10)' "$YT_S")"
# The engine names itself in its own envelope. This is what lets a caller route a chosen
# result back to the matching <engine>-resolve without pattern-matching its URL, so a new
# engine that forgets the field breaks routing rather than merely looking different.
report "search -j names its engine" 0 \
    "$(jqv '.status=="ok" and .engine=="yt"' "$YT_S")"
report "search -J has raw id" 0 \
    "$(jqv '.results[0]|has("id")' "$YT_SJ")"
# Was an open R8 drift (26 lines for -n 3); fixed, so it is a hard check now — a "known"
# label on a passing behaviour is how a real regression gets waved through later.
report "search -j is one line" 1 "$(lines "$YT_S")"

echo "── resolve envelope: the half that turns a handle into bytes ──────"
YT_R=$(out yt-resolve)
# Every key the PLAYER reads. A new engine that renames one, or omits http_headers, breaks
# playback in a way no other check here would notice: the search half would still look fine.
# http_headers is asserted PRESENT rather than non-empty — {} is a legal answer, absent is not.
report "resolve -j envelope" 0 \
    "$(jqv '.status=="ok" and .engine=="yt" and (.stream_urls|length)>0
              and has("http_headers") and (.http_headers|type)=="object"
              and has("title") and has("format") and has("retried")' "$YT_R")"
report "resolve -j is one line" 1 "$(lines "$YT_R")"
echo "── the player's engine seam ───────────────────────────────────────"
# A well-formed id that resolves to nothing is a PROPAGATED tool failure (2+), not usage,
# and it must still say why — the semantics the shape check sitting in the engine buys.
# One resolve attempt answers both: the exit code and the envelope come off the same run,
# which is also the only way they are guaranteed to be describing the same failure.
DEAD=$(out yt-dead); DEAD_ST=$(src yt-dead)
report "dead id is 2+, not 1"     2 "$DEAD_ST"
report "dead id keeps its reason" 0 \
    "$(jqv '.status=="error" and .exit_code>=2 and (.reason|type)=="string"' "$DEAD")"

echo "── argv order: a flag-shaped query after -- is SEARCHED ───────────"
# Not a player list: --status after -- is eight characters of query text. The check lives on
# yt-search because that is where searching lives now; the player has no search branch left
# to confuse a flag-shaped token with (ARCH-cli-contract.md「门模型」).
# Asserted POSITIVELY, on the query the engine echoes back. The old form folded stderr into
# the pipe and asked only "is line one not JSON?", so `Error: search failed (network)` — a
# yt-search that did not run at all — satisfied it. It was also the one live call in this file
report "yt-search -- --status searches" 0 \
    "$(jqv '.status=="ok" and .query=="--status"' "$(out yt-argv)")"

echo "── --transcript: the read-only verb, both envelopes (gate above) ──"
report "transcript envelope"      0 \
    "$(jqv '.status=="ok" and .id and .lang and .chars>0 and (.is_auto|type=="boolean") and (.text|length>0)' \
        "$(out yt-trans)")"
report "transcript -J has segments" 0 \
    "$(jqv '.segments[0]|has("start") and has("text")' "$(out yt-transJ)")"
NOCAP=$(out yt-nocap); NOCAP_ST=$(src yt-nocap)
report "no captions -> error"     0 \
    "$(jqv '.status=="error" and .reason=="no_subtitles_available"' "$NOCAP")"
report "no captions exit"         1 "$NOCAP_ST"

# THE SAME VERB ON A SITE WHOSE CAPTION TRACK IS A LYRIC. Asserted as the same envelope,
# because that is the claim `--transcript` makes across engines — but with `lang` explicitly
# NULL rather than a string, which is the one field these two engines honestly differ on:
# YouTube tags its caption tracks with a language, this site tags nothing and its catalogue is
# not one language, so an engine that filled the key would be guessing. Both spellings are
# legal, and pinning the null side is what stops a later "tidy-up" writing a constant into it.
report "ne transcript envelope"   0 \
    "$(jqv '.status=="ok" and .id and .lang==null and .is_auto==false and .chars>0
            and .segment_count>0 and (.text|length>0)' "$(out ne-trans)")"
# THE INSTRUMENTAL, and the reason this line is worth its round trip: the site does NOT set
# its own `nolyric` flag on these (measured — not one of eight did). It sends the composer
# credits and then a sentinel line, so an engine trusting the flag answers status:"ok" here
# with three lines of credits in `text`, passes every other check in this file, and hands an
# agent someone's name to summarise as a song. There is no other input that separates those
# two implementations.
NE_NOLYR=$(out ne-notrans); NE_NOLYR_ST=$(src ne-notrans)
report "an instrumental is a miss, not empty words" 0 \
    "$(jqv '.status=="error" and .reason=="no_subtitles_available"' "$NE_NOLYR")"
report "…and it exits 1, like the other engine's" 1 "$NE_NOLYR_ST"

echo "── --items: a container is not a row ─────────────────────────────"
# ONE ENVELOPE OVER THREE SITES, and the shape is the claim: whatever a container is called
# there, what comes back is `{id, url, title, count, total, items[]}` and every element is an
# item record — an engine to route to, a playable url of its own, a title and a duration. The
# offline half already proved such a list feeds t-playlist and t-play with no field renamed;
# this half is what proves the engines still EMIT it.
#
# `count == total` is asserted where the container fits under the ceiling, because that is the
# only place it CAN be: it is the statement that nothing was silently dropped. The counts
# themselves are never literals — see the handles.
_items_ok=0
for _slot in yt-items bili-items ne-items; do
    [ "$(jqv '.status=="ok" and (.id|type)=="string" and (.url|type)=="string"
              and (.title|type)=="string"
              and (.count|type)=="number" and (.total|type)=="number"
              and .count==.total and .count>10
              and (.items|length)==.count
              and ([.items[].n]==[range(1; (.count+1))])
              and all(.items[];
                      (.engine|type)=="string" and (.engine|length)>0
                      and (.url|type)=="string" and (.url|startswith("http"))
                      and (.id|type)=="string" and (.id|length)>0
                      and (.duration|type)=="number" and .duration>0
                      and (.duration_fmt|test("h:[0-9][0-9]m:[0-9][0-9]s$")))' "$(out $_slot)")" = 0 ] &&
        _items_ok=$((_items_ok + 1))
done
report "every --items envelope is a list of calls" 3 "$_items_ok"
# Each site's own id spelling, checked once: an item's url has to be the one that RESOLVES,
# and the three engines build it three different ways. A record that named the container, or
# that carried the site's collection query along, would pass every check above.
report "yt items are watch urls, no list="   0 \
    "$(jqv 'all(.items[]; (.url|test("^https://www\\.youtube\\.com/watch\\?v=[A-Za-z0-9_-]{11}$")))' "$(out yt-items)")"
report "bili items are au urls"              0 \
    "$(jqv 'all(.items[]; (.url|test("^https://www\\.bilibili\\.com/audio/au[0-9]+$")))' "$(out bili-items)")"
report "ne items are song urls"              0 \
    "$(jqv 'all(.items[]; (.url|test("^https://music\\.163\\.com/song\\?id=[0-9]+$")))' "$(out ne-items)")"
# THE FILTER, on the site that has one. Default drops every row a caller would not actually be
# served, and `total` does NOT move when it does — that is the whole reason the envelope
# carries both numbers. A chart is the input that separates the two: it is mostly VIP-only, so
# an implementation that forgot the filter comes back with the same count as the VIP run.
report "ne --items filters by access, and total stands" 0 \
    "$(jqv '.status=="ok" and .count<.total and .total>10' "$(out ne-items-def)")"
# The two runs COMPARED, which is the half a single envelope cannot state: the filter is what
# separates them, so more rows with the same `total` is the filter having been the only
# difference. Read as numbers rather than through jq, because the two envelopes are two
# documents and jqv takes one.
_ne_all=$(printf '%s' "$(out ne-items)" | jq -r '.count // 0' 2>/dev/null) || _ne_all=0
_ne_def=$(printf '%s' "$(out ne-items-def)" | jq -r '.count // 0' 2>/dev/null) || _ne_def=0
_ne_tot_all=$(printf '%s' "$(out ne-items)" | jq -r '.total // 0' 2>/dev/null) || _ne_tot_all=0
_ne_tot_def=$(printf '%s' "$(out ne-items-def)" | jq -r '.total // 0' 2>/dev/null) || _ne_tot_def=0
_ne_more=no
[ "${_ne_all:-0}" -gt "${_ne_def:-0}" ] && [ "${_ne_tot_all:-0}" = "${_ne_tot_def:-0}" ] && _ne_more=yes
report "…and asking for VIP rows returns more, same total" yes "$_ne_more"
# A CONTAINER THAT IS NOT THERE IS A STATEMENT ABOUT THE HANDLE, over all three sites — and it
# is exit 2 with a reason, never 1: by the time a request has been spent the caller's argv was
# fine. It is also the one thing each site says most differently (measured 2026-09-10: yt-dlp
# exits 1 saying "does not exist", Bilibili answers HTTP 200 / code 0 / data null, NetEase a
# body code of 404), so an engine reading only the exit code or only the HTTP status gets it
# wrong on two of the three.
_items_gone=0
for _slot in yt-nolist bili-nomenu ne-nolist; do
    [ "$(src $_slot)" = 2 ] &&
        [ "$(jqv '.status=="error" and .reason=="unavailable"' "$(out $_slot)")" = 0 ] &&
        _items_gone=$((_items_gone + 1))
done
report "a missing container is 2 + unavailable, everywhere" 3 "$_items_gone"
# The fourth shape of "not there", and it is its own check because its evidence is the thinnest
# of the four: a favourites list nobody created answers 200 / code 0 / message "OK" and says so
# only by leaving `data.title` null (measured 2026-09-12). An engine reading the code or the
# status is green everywhere else and wrong here.
report "a favourites list that is not there is 2 too" 2 "$(src bili-nofav)"
report "…and says unavailable" 0 \
    "$(jqv '.status=="error" and .reason=="unavailable"' "$(out bili-nofav)")"

echo "── --items: the video side, and the cursor ────────────────────────"
# THE SAME ENVELOPE OVER THE SITES' OTHER LISTS. The keys are the audio containers' keys plus
# the two the cursor added, and the item records are the same records — which is the claim:
# a favourites list and a collection reach t-playlist through the same pipe an audio menu does,
# with the site's own video ids in them.
_vid_ok=0
for _slot in bili-fav bili-season yt-channel; do
    [ "$(jqv '.status=="ok" and (.title|type)=="string"
              and (.count|type)=="number" and (.count|tostring|test("^[0-9]+$"))
              and (.items|length)==.count
              and ([.items[].n]==[range(1; (.count+1))])
              and all(.items[];
                      (.engine|type)=="string" and (.url|startswith("http"))
                      and (.id|type)=="string" and (.id|length)>0
                      and (.duration|type)=="number" and .duration>0)' "$(out $_slot)")" = 0 ] &&
        _vid_ok=$((_vid_ok + 1))
done
report "every video container is a list of calls too" 3 "$_vid_ok"
# Both Bilibili video containers name their rows the way RESOLVING one of them does — the BV
# spelling, not the favourites list's own numeric id and not a bangumi link.
report "bili video items are /video/BV urls" 0 \
    "$(jqv 'all(.items[]; (.url|test("^https://www\\.bilibili\\.com/video/BV[A-Za-z0-9]+$")))' "$(out bili-fav)")"
report "…the collection's too" 0 \
    "$(jqv 'all(.items[]; (.url|test("^https://www\\.bilibili\\.com/video/BV[A-Za-z0-9]+$")))' "$(out bili-season)")"
# THE TWO ROWS A FAVOURITES LIST HOLDS THAT NOTHING CAN PLAY, and the input is what proves the
# judgement: this list really contains dead uploads and OGV episodes (measured), so `count` has
# to come back under `total` and neither kind may appear among the items. A build that filtered
# on duration alone passes nothing here — the dead rows carry real durations.
report "a favourites list drops what cannot be played" 0 \
    "$(jqv '.count < .total and .total > 10
            and ([.items[] | select(.title == "已失效视频")] | length) == 0
            and ([.items[] | select(.url | contains("bangumi"))] | length) == 0' "$(out bili-fav)")"
# A collection is the control for that claim: same envelope, same row shape, nothing to drop.
report "a collection lists all of itself" 0 \
    "$(jqv '.count == .total and .total > 1' "$(out bili-season)")"
# A CHANNEL IS ADMISSIBLE, which is this change's whole point — it used to exit 1 by name. The
# small one finishes inside one batch, and that is where `total` stops being null: yt-dlp reports
# playlist_count when the walk reaches the end and not before (measured 2026-09-12).
report "a channel's uploads import, tail and all" 0 \
    "$(jqv '.status=="ok" and (.total|type)=="number" and .count<=.total
            and .has_more==false and .next_cursor==null' "$(out yt-channel)")"
# THE CURSOR, on the only kind of container that can mint one: a full batch says there is more
# and hands back the offset to ask with.
report "a full batch hands back a cursor" 0 \
    "$(jqv '.count==500 and .has_more==true and .next_cursor=="o:500" and .total==null' "$(out yt-big1)")"
report "…and the batch it names starts at 501" 0 \
    "$(jqv '.status=="ok" and .count>0 and .items[0].n==1' "$(out yt-big2)")"
# THE PROPERTY THAT MAKES PAGING WORTH ANYTHING, and it cannot be read off one envelope: the two
# batches must not overlap. Read as two documents, compared by hand, like the VIP pair above.
_b1=$(printf '%s' "$(out yt-big1)" | jq -r '[.items[].id] | join(" ")' 2>/dev/null) || _b1=""
_b2=$(printf '%s' "$(out yt-big2)" | jq -r '[.items[].id] | join(" ")' 2>/dev/null) || _b2=""
_overlap=$(printf '%s\n%s\n' "$_b1" "$_b2" | jq -R -s '
    (split("\n") | map(select(length > 0) | split(" "))) as $p
    | if ($p | length) == 2 then (($p[0]) - (($p[0]) - ($p[1])) | length) else -1 end' 2>/dev/null) || _overlap=-1
report "two batches share no row" 0 "$_overlap"
# THE ENVELOPE INVARIANT, over every container this suite fetched: has_more is a boolean, and
# next_cursor is null exactly when it is false. Stated across all six because a key that is
# right on the container that needed it and absent on the others is the shape of drift that
# makes a caller test for the key instead of reading it.
_cursor_shape=0
for _slot in yt-items bili-items ne-items bili-fav bili-season yt-channel yt-big1; do
    [ "$(jqv '(.has_more|type)=="boolean"
              and (if .has_more then (.next_cursor|test("^o:[0-9]+$")) else .next_cursor==null end)' "$(out $_slot)")" = 0 ] &&
        _cursor_shape=$((_cursor_shape + 1))
done
report "has_more and next_cursor answer together" 7 "$_cursor_shape"

BILI_ITEMS_OUT=$(out bili-items)
report "an item list adds to a playlist, unmapped" 0 \
    "$(jq_in '.status=="ok" and .added>=1 and .count>=1' "$BILI_ITEMS_OUT" shell/t-playlist --add items -j)"
report "…and every stored row is a call"  0 \
    "$(jq_ok '(.items|length)>=1 and all(.items[];
                 .engine=="bili"
                 and (.url|startswith("https://www.bilibili.com/audio/au"))
                 and (.id|type)=="string"
                 and (.title|type)=="string" and (.title|length)>0
                 and (.duration|type)=="number")' shell/t-playlist --show items -j)"
report "an item list enqueues"            4 "$(rc_in "$BILI_ITEMS_OUT" shell/t-play --enqueue - -j)"
report "…parsed, not refused"             0 \
    "$(jq_in '.status=="not_playing"' "$BILI_ITEMS_OUT" shell/t-play --enqueue - -j)"

echo "── the second engine: the same envelope, or the split is a fiction ─"
# The second engine's envelopes. The SEARCH is the one the live half already made — a key
# set does not care what was searched for, and this used to be a fourth round trip asking
# the same engine the same kind of question.
BILI_S=$(out search-bili)
BILI_R=$(out bili-resolve)
YT_I=$(out yt-info)
BILI_I=$(out bili-info)

# THE check the engine split exists for. Two engines are only interchangeable if a caller
# cannot tell which one answered, so the assertion is on the KEY SETS THEMSELVES rather
# than on a list of names written out twice: a field renamed, added or dropped in EITHER
# engine fails here, including one added to yt-search years from now and forgotten on the
# other side. Nothing else in this file would notice — each engine's own checks would still
# pass, and playback would break only for the engine nobody happened to run.
report "search envelopes agree" \
    "$(printf '%s' "$YT_S" | jq -Sc 'keys' 2>/dev/null)" \
    "$(printf '%s' "$BILI_S" | jq -Sc 'keys' 2>/dev/null)"
report "search result keys agree" \
    "$(printf '%s' "$YT_S" | jq -Sc '.results[0]|keys' 2>/dev/null)" \
    "$(printf '%s' "$BILI_S" | jq -Sc '.results[0]|keys' 2>/dev/null)"

# The row's own premise, over every DISCOVERED engine and BOTH envelope shapes — two places
# a real implementation has already been wrong, and neither is caught by the parity check
# above (two engines agree on a key set they are both missing, and it only ever reads -j).
#
#   · `kind`/`access` are the ENGINE'S JUDGEMENT about a row (ARCH-cli-contract.md「数据契约」), which
#     is why they are injected before the lean projection rather than inside it: an engine
#     that adds them to the projection alone hands the caller who asked for MORE data (-J) an
#     envelope missing two required fields, and every -j check in this file stays green.
#   · A row whose `url` is null is not a row: `t-play` has nothing to call. bili-search
#     shipped exactly that — search_type=video mixes in `ketang` (paid-course) records that
#     carry no `bvid`, 3 of 20 on "钢琴", and an EMPTY bvid is TRUTHY in jq, so the `.id !=
#     null` gate passed them through with `id: ""` and `url: null`.
#   · A row with NO LENGTH AND NO REASON is not a row either, and this is the clause that
#     catches a CONTAINER — the shape a buildable url gets past the check above. A playable
#     call either has a duration or says why it has none: a live stream carries
#     live_status "is_live" with duration null, an archived one carries "was_live" with a
#     number. A channel row carries NEITHER (measured 2026-09-03: row 15 of 周杰伦 on
#     yt-search is the artist's channel — duration null, and yt-dlp does not even put the
#     live_status key on that entry, while every video entry beside it has it). That is the
#     engine-independent way to say "a row is a playable call" without any site knowledge
#     leaking in here: this file must not know what a /channel/ url looks like.
#
# Asserted against the CLOSED ENUM, never against `has("kind")`: an engine writing
# kind:"video" — the site's own word, the likeliest wrong answer — satisfies presence and
# fails here. The enum is TWO values: a container has no value here because it is not a
# row at all (the clause above is what drops it), so `kind:"collection"` fails too. The non-empty result requirement is what stops the whole thing passing
# vacuously on an engine that returned nothing.
ROW_IS_A_CALL='(.results|length)>0 and all(.results[];
      (.url|type=="string") and (.url|length)>0
      and (.id|type=="string") and (.id|length)>0
      and (.kind|IN("track","multipart"))
      and (.access|IN("full","preview","paywalled"))
      and ((.duration|type)=="number" or (.live_status|type)=="string"))'
for n in $ENGINES; do
    report "$n-search -j rows are calls" 0 \
        "$(jqv "$ROW_IS_A_CALL" "$(out "search-$n")")"
    report "$n-search -J rows are calls" 0 \
        "$(jqv "$ROW_IS_A_CALL" "$(out "searchJ-$n")")"
done
# The same predicate over EVERY OTHER live search this file already paid for — the ceiling
# and default envelopes, and the three 周杰伦 ones. Not one extra request, and it is what
# stops the invariant going vacuous: a container row appears in a MINORITY of queries, so
# asserting it on one query per engine is asserting it on a sample that usually has nothing
# to catch. `lofi` at -n 10 has never carried one; 周杰伦 does today.
for e in yt-zh bili-zh ne-vip ne-novip $(for n in $ENGINES; do echo "cap-$n dflt-$n"; done); do
    [ -s "$LIVE/$e.out" ] || continue
    report "$e rows are calls" 0 "$(jqv "$ROW_IS_A_CALL" "$(out "$e")")"
done
# THE COVER, over every discovered engine and BOTH envelope shapes — no extra request, the
# same envelopes the clause above already read.
#
# Two claims, and the second is the one with teeth. Presence is the cheap half: the key must
# be there on every row of both shapes, string or null, which is the suite's standing rule
# for a field an engine may not know (ARCH-cli-contract.md「数据契约」). BOTH shapes,
# because `kind`/`access` were wrong in exactly this way once — projected into -j alone, so
# the caller who asked for MORE data got LESS, and every -j check stayed green.
#
# The scheme is the discriminating half, and the input for it exists today on two of the
# three engines (measured 2026-09-03): Bilibili serves `pic` PROTOCOL-RELATIVE
# (`//i0.hdslb.com/…`) and NetEase serves `.al.picUrl` over PLAIN http — so an engine that
# passes the site's string through untouched hands a TUI a URL curl cannot fetch, or one it
# fetches over a downgraded connection. Neither failure is visible in the envelope's shape;
# both are visible here. yt needs no rewrite and is held to the same line, which is what
# makes this an invariant rather than two site-specific patches.
THUMB_IS_FETCHABLE='(.results|length)>0 and all(.results[];
      has("thumbnail")
      and ((.thumbnail|type)=="null" or (.thumbnail|type)=="string")
      and ((.thumbnail|type)=="null" or (.thumbnail|startswith("https://"))))'
for n in $ENGINES; do
    report "$n-search -j covers are fetchable" 0 \
        "$(jqv "$THUMB_IS_FETCHABLE" "$(out "search-$n")")"
    report "$n-search -J covers are fetchable" 0 \
        "$(jqv "$THUMB_IS_FETCHABLE" "$(out "searchJ-$n")")"
done

# WHICH cover, and this one can only be asked of the engine whose site offers a CHOICE.
# YouTube publishes a `thumbnails[]` array per flat entry — measured 2026-09-03: 360x202 and
# 720x404 on `lofi hip hop`, every row — and the display box is ~192px across, so the answer
# is the smallest at or above 200px. `.[0]`, `.[-1]` and "the biggest" are the three
# plausible wrong implementations and all three are separated by this input: the first two
# only by accident of order, the third by 3x the escape payload for a worse downscale.
#
# Not stated over $ENGINES: `pick_thumb` is NOT one duplicated function here. Each site
# hands its engine a different shape — an array, a protocol-relative string, an http string
# — so there is no cross-engine invariant to state, only the one above (what a cover must BE)
# which every engine already answers. The vacuity guard is `($c|length) > 0`: if this query
# ever stops carrying a row that offers two sizes, the check goes red instead of passing on
# an empty set.
if [ -n "$YT_SJ" ]; then
    report "yt picks the smallest cover at or above 200px" "true" \
        "$( printf '%s' "$YT_SJ" | jq -r '
            [ .results[]
              | select((.thumbnails|type)=="array")
              | select([.thumbnails[]|select((.width//0)>=200)]|length > 0)
              | {got: .thumbnail,
                 want: ([.thumbnails[]|select((.width//0)>=200)]|sort_by(.width)|first|.url)} ] as $c
            | ($c|length) > 0 and all($c[]; .got == .want)' 2>/dev/null)"
fi

# `access` IS A COMPUTED FIELD ON ONE ENGINE, and only a live search can show it. The
# invariant above holds every engine to the closed enum, which a row hardcoding "full"
# satisfies — and both shipping engines DO hardcode it, honestly, because their search
# responses carry no such signal. NetEase's does (`fee`, in the page already fetched), so this
# is the check that separates an engine which reads it from one which copied the constant.
#
# 周杰伦 is the input rather than a song id: pinning a track's fee would nail someone else's
# commercial decision into this suite, while "an artist's page is mostly VIP here" is the
# measured shape of the catalogue (2026-09-02: 70% of that query's rows were not "full", and
# the four artist queries measured ran 64-94%). An engine printing the constant answers false
# on the first line below and goes red.
#
# The second line is the DEFAULT FILTER, and it is the same envelope pair read from the other
# side: with NE_INCLUDE_VIP unset, every row that survives must be playable. That is what
# makes a stored row a CALL rather than a reference — and the two lines cannot both pass
# unless the filter and the mapping are really wired to each other, since they read the SAME
# query through the one knob.
report "ne computes access from the site's own fee" "true" \
    "$(out ne-vip | jq -r '(.results|length) > 0 and any(.results[]; .access != "full")' 2>/dev/null)"
report "…and NE_INCLUDE_VIP=0 keeps only playable rows" "true" \
    "$(out ne-novip | jq -r '(.results|length) > 0 and all(.results[]; .access == "full")' 2>/dev/null)"

report "resolve envelopes agree" \
    "$(printf '%s' "$YT_R" | jq -Sc 'keys' 2>/dev/null)" \
    "$(printf '%s' "$BILI_R" | jq -Sc 'keys' 2>/dev/null)"

# THE START OFFSET, over every discovered engine. Only a real resolve can observe it: an
# engine's reading of a timestamp has no dry-run face. Stated as an invariant rather than
# against yt because the two engines fill this key from OPPOSITE SIDES — yt-dlp publishes
# .start_time for YouTube and nothing at all for Bilibili, so yt-resolve normalises what it
# is handed and bili-resolve parses the query itself. A check driving one of them proves
# nothing about the other, and engine #3 is covered the day it lands.
#
# The handle comes from the engine's OWN search rather than a table of ids, so the only
# site-specific thing left is the separator — and even that is DERIVED, not tabled: a url
# already carrying a query takes &, one that does not takes ?.
#
# The row is FILTERED, and the filter is the whole reason this block is not flaky. "lofi"
# returns broadcasts: a 24/7 radio stream (live_status "is_live", no duration) and twelve-hour
# recordings of one ("was_live", a fragmented manifest). Resolving either for a fixed format
# runs for MINUTES — this file went from under two minutes to over nine the first time the
# top row happened to be one, and the first cut of this block excluded only `is_live` and so
# still picked a `was_live` twelve-hour row. So the filter demands live_status null — never
# broadcast, in either tense — and then takes the SHORTEST such row, which needs no duration
# threshold to argue about and is by construction the cheapest handle in the page to resolve
# (measured 4s for yt, 3s for bili). The live output itself is asserted, so a query that stops
# returning one is a red with a name rather than four mysteries under it.
for n in $ENGINES; do
    SR=$(out "off601-$n")
    report "$n-resolve reads a t= offset" 0 "$(jqv '.start_seconds == 601' "$SR")"
    # The url answers WHICH MEDIA, never where to start — t-playlist --add stores exactly
    # this string, so an offset riding along in it would make a saved track replay from
    # 10:01 for ever. Not a property inherited from the extractor: bili's webpage_url keeps
    # the whole query, because ?p=N lives in it, so for that engine this is a real strip.
    report "$n-resolve keeps the offset out of url" "false" \
        "$(printf '%s' "$SR" | jq -r '.url | test("[?&]t=")' 2>/dev/null)"
    # …and it strips ONLY the offset. Both engines carry their id in the url — in the query
    # for yt (v=…), in the path for bili — so an implementation that answers the check above
    # by throwing the query away, or the whole url, fails here.
    report "$n-resolve strips only the offset" "true" \
        "$(printf '%s' "$SR" | jq -r '.id as $i | .url | contains($i)' 2>/dev/null)"
    # THE DISCRIMINATING INPUT. ?t=0 says "start at the top", which is a different answer
    # from "this handle carried no offset" — and every shortcut that folds the two together
    # (jq's `// null` over a falsy 0, a bash `[[ -n ]]` over an empty string) prints null
    # here. Both must print 0, and the null half is asserted on the no-offset envelopes the
    # two engines already fetched, so this costs one resolve rather than two.
    report "$n-resolve tells t=0 from no t" "0" \
        "$(out "off0-$n" | jq -r '.start_seconds')"
done
report "yt-resolve has no offset to report"   "null" "$(printf '%s' "$YT_R"   | jq -r '.start_seconds')"
report "bili-resolve has no offset to report" "null" "$(printf '%s' "$BILI_R" | jq -r '.start_seconds')"

# Bilibili's own fact, so it lives beside the engine that has it rather than inside the loop
# above: one video number is many playable files here, and ?p=N is the only thing that says
# which. Stripping the offset must not take the part with it — that would silently repoint a
# stored record at part one.
BILI_P=$(out bili-offset)
report "bili keeps ?p= while dropping t=" "true" \
    "$(printf '%s' "$BILI_P" | jq -r '.start_seconds == 601
        and (.url | test("[?&]t=") | not) and (.url | test("[?&]p=2"))' 2>/dev/null)"

# `selected` is the ANSWER to the request `format` states, and the whole reason both keys
# exist is that they differ: `format` is the selection string this engine SENT ("ba/b"),
# `selected` is what yt-dlp came back having picked ("251 - audio only (medium)"). So the
# inequality is the check — an engine that echoes the request into `selected`, which is the
# cheapest wrong implementation and the one a reader of the field names would write first,
# satisfies every other assertion here and fails only this one. Presence alone would pass it.
#
# Read off the two envelopes already in hand: this claim costs no round trip, and a second
# resolve could not answer a shape question differently anyway (see the note above the
# config-on-a-real-fetch block). What generalises it to engine #3 is the parity check
# immediately above — a third engine that omits either key fails there against both.
SELECTED_IS_AN_ANSWER='(.selected|type)=="string" and (.selected|length)>0
      and .selected != .format
      and (.selected_resolution|type)=="string" and (.selected_resolution|length)>0'
report "yt resolve selected is an answer"   0 "$(jqv "$SELECTED_IS_AN_ANSWER" "$YT_R")"
report "bili resolve selected is an answer" 0 "$(jqv "$SELECTED_IS_AN_ANSWER" "$BILI_R")"
# --info gets the same parity treatment: it is the third envelope both engines publish
# (ARCH-cli-contract.md「数据契约」), and nothing else here would notice a field renamed on one
# side. The ok/engine assertion is what keeps the key comparison from passing vacuously —
# two ERROR envelopes agree on their keys too.
report "info -j is ok and named" 0 \
    "$(jqv '.status=="ok" and .engine=="yt"' "$YT_I")"
report "info envelopes agree" \
    "$(printf '%s' "$YT_I" | jq -Sc 'keys' 2>/dev/null)" \
    "$(printf '%s' "$BILI_I" | jq -Sc 'keys' 2>/dev/null)"
report "--info -j is one line" 1 "$(lines "$YT_I")"

report "bili-search names its engine" 0 \
    "$(jqv '.status=="ok" and .engine=="bili"' "$BILI_S")"
report "bili-search -j is one line" 1 "$(lines "$BILI_S")"
# The site sends duration as "MM:SS" with unbounded minutes ("222:28"), which every surface
# above would silently mis-sort and mis-render as a string. It is parsed in the engine, so
# the assertion is that what leaves the engine is a NUMBER — never the raw string.
#
# `null` is ALLOWED and is not a miss: ARCH-engine.md「搜索子系统」and ARCH-cli-contract.md「数据契约」make duration/duration_fmt null together when the
# row has no duration, and this endpoint does return such rows intermittently (observed: one
# null among five, on a result set the site swapped in between two identical requests). An
# earlier `all(type=="number")` here failed on exactly those runs and read as flaky — it was
# asserting against the contract rather than for it. What must hold: nothing is a string, and
# the page is not ALL nulls (which would mean the parser stopped working).
report "bili duration is seconds" 0 \
    "$(jqv '[.results[].duration]
               | length>0
               and all(type=="number" or type=="null")
               and any(type=="number")' "$BILI_S")"
# The site filters duration itself, in four coarse buckets, and a -m/-M window that fits
# inside ONE of them is pushed down — the only lever there is on a 20-per-page endpoint with
# no page-size knob. `-M 600` fits bucket 1 exactly and `-M 601` fits nothing, which makes
# this the discriminating input rather than a restatement of the bounds: measured 2026-08-26
# on one request each, the pushed-down form returned 20 usable rows and the local-only form
# returned 1. An engine that stops sending `duration` — a renamed parameter, a bucket
# mis-mapped, the plan computed after the first page — passes every other check in this file
# and quietly hands back a handful of rows where -n asked for twenty. The bound itself is
# asserted with it, because the buckets are COARSE and pushing one down must never widen the
# answer: 600 is the ceiling the caller named, not the ten minutes the site understood.
BILI_ZH=$(out bili-zh)
report "bili pushes -M to the site" 0 \
    "$(jqv '.count >= 15 and ([.results[].duration] | all(. == null or . < 600))' "$BILI_ZH")"
# Titles arrive as search-result HTML (<em class="keyword">) and entity-escaped. Markup that
# survives into a title is counted by the width layer, which reflows every row wrongly. Same
# envelope as the bound above: the de-markup is per row and does not care what -M asked for,
# so a second identical query for it was a round trip spent on nothing.
report "bili titles carry no markup" 0 \
    "$(jqv '[.results[].title]|all((test("<") or test("&[a-z#]+;"))|not)' "$BILI_ZH")"

# This site's CDN checks Referer: the bare stream URL answers 403 and the same URL with
# these headers answers 206 (measured). An empty http_headers here is a silently unplayable
# engine, which is exactly the contract hole the key was added to close.
report "bili resolve sends a Referer" 0 \
    "$(jqv '.http_headers|has("Referer")' "$BILI_R")"
# --parts, live: the claim the hermetic half above structurally cannot make — the engine
# still emits this shape against the real site.
#
# What used to be here as well: a stopwatch asserting the verb is still ONE request (< 5s
# against a measured 0.5s). It went, and the reason it went is the rule: a check earns its
# place by separating a correct implementation from a wrong one, and that one separated a
# correct implementation from a slow afternoon — every red it ever produced would have been
# the network's. A second round trip is a code review's job, not a stopwatch's.
BILI_P=$(out bili-parts)
report "bili --parts is one line"    1 "$(lines "$BILI_P")"
# Every part is asserted, not just the first: `?p=N` is built per element, and an off-by-one
# or a base URL that kept the caller's own query string shows up on element two onwards. The
# base is taken from the envelope's OWN top-level url, so the claim is internal consistency
# — the thing a caller relies on when it pipes .parts straight into the player.
#
# THE TITLE IS ASSERTED AS string-or-null, and that is not a weakening for its own sake. The
# verb has two endpoints since 2026-09-01 — `view` preferred, `player/pagelist` as fallback
# once `view` began answering 412 to every request — and only `view` carries the collection
# title, so the field's honest domain is now both. Both spellings are still pinned: a string
# must be non-empty, and null is the only other member. What this check does NOT do is pick
# which endpoint answered, because the caller cannot either — the envelope is the contract,
# not the route to it. Everything the verb exists FOR is asserted below at full strength on
# either path: per-part titles are non-empty strings whichever endpoint filled them.
report "bili --parts envelope"       0 \
    "$(jqv '.status=="ok" and .engine=="bili" and (.id|startswith("BV"))
              and ((.title|type)=="null"
                   or ((.title|type)=="string" and (.title|length)>0))
              and (.count|type)=="number" and .count>=2 and .count==(.parts|length)
              and (.total_duration|type)=="number"
              and (.total_duration_fmt|type)=="string"
              and (.url as $b | all(.parts[];
                    (.n|type)=="number" and .engine=="bili"
                    and (.title|type)=="string" and (.title|length)>0
                    and (.duration|type)=="number"
                    and (.duration_fmt|type)=="string"
                    and .url == ($b + "?p=" + (.n|tostring))))' "$BILI_P")"
BILI_PARTS_ITEMS=$(printf '%s' "$BILI_P" | jq -c '{items: .parts}')
report "a part list adds to a playlist" 0 \
    "$(jq_in '.status=="ok" and .added>=2 and .count>=2' "$BILI_PARTS_ITEMS" shell/t-playlist --add parts -j)"
report "…and every stored row is a call"  0 \
    "$(jq_ok '(.items|length)>=2 and all(.items[];
                 .engine=="bili"
                 and (.url|contains("?p="))
                 and (.title|type)=="string" and (.title|length)>0
                 and (.duration|type)=="number")' shell/t-playlist --show parts -j)"
report "a part list enqueues"             4 "$(rc_in "$BILI_PARTS_ITEMS" shell/t-play --enqueue - -j)"
report "…parsed, not refused"             0 \
    "$(jq_in '.status=="not_playing"' "$BILI_PARTS_ITEMS" shell/t-play --enqueue - -j)"
# A single-part video is a list of ONE and is NOT an error — the contract says so, and the
# plausible wrong implementation (treat "no parts to choose between" as a failure) would pass
# every other --parts check in this file. BILI_ID is that handle, which is why it is separate
# from BILI_PARTS_ID above.
report "one part is still a list"    0 \
    "$(jqv '.status=="ok" and .count==1 and (.parts|length)==1
                and .parts[0].url==(.url + "?p=1")' "$(out bili-part1)")"
# A HANDLE THAT WILL NEVER RESOLVE MUST NOT BE REPORTED AS RETRYABLE, and since 2026-09-01
# that is a claim about the verb's TWO endpoints rather than one. `view` answers 412 to
# everything now, and 412 is `network` — so a fallback that simply reported the preferred
# endpoint's verdict would tell an agent to keep asking about a video that does not exist.
# The engine spends the second request, reads `pagelist`'s 200/-404, and lets that verdict
# win precisely because `unavailable` is a statement about the HANDLE. This is the check
# that separates the two: the wrong implementation answers `network` and stays exit 2, so
# the exit code alone cannot see it — the reason is the whole discriminator.
report "a nonexistent id is not retryable" 0 \
    "$(jqv '.status=="error" and .reason=="unavailable"' "$(out bili-nopart)")"
report "…and it is still a tool failure" 2 "$(src bili-nopart)"

# The player routes by NAME, and the name is the command prefix — the whole reason the
# lookup is a string concatenation instead of a registry.
report "t-play routes to the bili engine" 0 \
    "$(jqv '.status=="error" and .exit_code>=2 and (.reason|type)=="string"' "$(out bili-route)")"

echo "── failure taxonomy: 2 is a tool failure, never 1 ─────────────────"
# A SEARCH THAT COULD NOT REACH ITS SITE STILL ANSWERS IN JSON. The exit code is the easy
# half and was never the whole promise: `-j` says a caller gets {status:"error", …, reason}
# and branches on the reason, so an empty stdout beside a bare 2 is a broken contract wearing
# a correct number (ARCH-cli-contract.md「数据契约」).
#
# STATED OVER EVERY DISCOVERED ENGINE, and that is not tidiness — it is the whole finding.
# This check drove yt-search alone, where the failure path prints from the top level and is
# fine. `bili-search` reached its site through a page loop, called search_fail from INSIDE the
# command substitution that collected the page, and so printed its error envelope into a shell
# variable: the caller got nothing on stdout and 2. Every exit-code check in this file stayed
# green for as long as that lasted, because the exit code really was 2. The third engine pages
# the same way and would have shipped the same defect; asserting the ENVELOPE, per engine, is
# what separates a failure path that answers from one that only exits.
report "an unreachable network is an ERROR ENVELOPE, over every engine" "$NENG" \
    "$(_ne=0; for n in $ENGINES; do
           [ "$(jqv '.status=="error" and .reason=="network"' "$(out "net-j-$n")")" = 0 ] &&
               _ne=$((_ne + 1))
       done; echo $_ne)"
report "…and it exits 2, in both output modes" $((NENG * 2)) \
    "$(_nx=0; for n in $ENGINES; do
           [ "$(src "net-j-$n")" = 2 ] && _nx=$((_nx + 1))
           [ "$(src "net-t-$n")" = 2 ] && _nx=$((_nx + 1))
       done; echo $_nx)"

echo "── the TUI boots, paints, survives a resize, and leaves on q ──────"
# NOT a renderer assertion: no cell arithmetic, no width table, no captured frame compared
# against an expected picture. The claim is only that the interactive surface starts on a
# real tty, paints a list, stays up across two resizes, and exits 0 on `q`.
#
# It earns its place because every other check in this file is BLIND to the TUI: they all
# reach it through a non-tty, where it correctly refuses to run. A `ting` that aborts on
# boot or wedges on exit would leave this whole suite green.
#
# tmux is the tty. Wait on the ready marker, never on a sleep — a captured spinner frame is
# a picture of the loading state, and a blind sleep here has produced a wrong result before.
#
# The poller and the pane predicates are defined up with the other helpers (search for
# poll_until): the undo pane, which `--only undo` runs on its own, needs them before this line.
if ! tmux_ok; then
    echo "  skip  (needs tmux for a real tty)"
else
    TS="ctest-tui-$$"
    tmux kill-session -t "$TS" 2>/dev/null
    # The session outlives the TUI on purpose: what the tty looks like AFTER `q` is a claim
    # of its own, and the pane is the only place to read it from once ting has gone.
    # TMPDIR is passed explicitly: a tmux SERVER that was already running carries the
    # environment of whoever started it, so the export at the top of this file does not reach
    # the pane, and ting's --status polls would create a players/ dir in the user's real
    # state dir. Nothing destructive happens there — every --stop and every check below runs
    # in this shell, where TMPDIR is redirected — but "this file does not touch your state"
    # should be true without a footnote.
    # A state dir of the pane's own.
    TUI_STATE=$(mktemp -d "${TMPDIR:-/tmp}/ting-tuistore.XXXXXX")
    # Seed the stores from real command envelopes ($YT_R and $YT_S) — real command output,
    # never synthetic JSON.
    printf '%s' "$YT_R" | UT_STATE_DIR="$TUI_STATE" shell/t-history --record - -j >/dev/null 2>&1
    [ "$(UT_STATE_DIR="$TUI_STATE" shell/t-history --ls -j 2>/dev/null | jq -r '.count // 0')" = 1 ] ||
        { echo "contract.sh: the log did not seed — suite error, not a failure" >&2; exit 1; }
    printf '%s' "$YT_S" | UT_STATE_DIR="$TUI_STATE" shell/t-playlist --add seeded-list -j >/dev/null 2>&1
    [ "$(UT_STATE_DIR="$TUI_STATE" shell/t-playlist --ls -j 2>/dev/null | jq -r '.count // 0')" -ge 1 ] ||
        { echo "contract.sh: the playlist did not seed — suite error, not a failure" >&2; exit 1; }
    # A config file of the pane's own. No staged behavior keys: UT_ROW_INDEX and UT_LIST_MODE
    # start unset and are driven by real keystrokes.
    # It is a SYMLINK to the real file to verify the preference write-back preserves symlinks.
    TUI_CFG="$UT_TEST_TMP/tui-config"
    TUI_CFG_REAL="$UT_TEST_TMP/tui-config.real"
    printf '%s\n' '# a config a human wrote' 'UT_PLAY_MODE=audio    # keep me' >"$TUI_CFG_REAL"
    ln -s "$TUI_CFG_REAL" "$TUI_CFG"
    # UT_SORT_FIELD in the pane's ENVIRONMENT is the discriminating input for the refusal:
    # the environment beats the file at every startup, so a ting that wrote this key would
    # record view_count and then discard it on the next run. The value it would write
    # (view_count) differs from the pinned one (relevance), so the check cannot pass by
    # accident — which is exactly what a file that agreed with the environment would do.
    # YT_LANG=en pins the pane's CHROME LANGUAGE. Every assertion in this section used to be
    # language-neutral by necessity — the default is "zh under a zh* locale, English
    # otherwise", so the pane spoke whichever language the machine did, and a check that named
    # a chrome string would have been green on one host and red on the next. The `i` checks
    # below need to name one (the `i` view's own label, and a field the list cannot hold), so
    # the language becomes an input rather than an accident. Nothing else in the
    # section reads a chrome string, so nothing else changes.
    # UT_IMAGE=on, and it is the discriminating value rather than the realistic one: `auto`
    # under tmux returns before sending a byte, so it would prove nothing, while `on` runs
    # the whole cover path — the cell-size query, the fetch, the transcode, the emit — on a
    # pane this block already drives through a resize, a filter and nine preference keys.
    #
    # WHAT IT CANNOT SEE IS WHAT THE PICTURE LOOKS LIKE — not whether one was sent. The
    # distinction is `capture-pane` versus `pipe-pane`, and this comment used to get it
    # wrong: capture-pane renders the GRID, and a placement is not in the grid, so a check
    # written against it could never fail; pipe-pane copies the bytes the program WROTE
    # before tmux decides what to do with them, so the escapes are readable even though tmux
    # forwards none of them. Two checks further down use exactly that. What stays out of
    # reach is the rendering — position, scale, whether it is the right image — which is a
    # picture and not a byte, and no suite here proves it.
    #
    # What forcing `on` catches is the cover path BREAKING THE TUI, and on the day the draw
    # path landed it caught three separate ways, none of them predicted:
    #   · `ls … | head -1` on a cold cache — an unmatched pipeline under pipefail carries
    #     ITS status, `set -e` takes the process, and the frame dies mid-detail-block;
    #   · the transcode's mpv sharing this tty and EATING KEYS, so # and v stopped toggling
    #     (--really-quiet does not cover it: that is output, this is input — --no-terminal);
    #   · a 39KB base64 write interrupted by SIGCHLD/SIGWINCH — `printf: write error:
    #     Interrupted system call` — again fatal under set -e.
    # Every one of them shows up here as this block's own boot / key / quit assertions going
    # red, which is why the value of forcing `on` is not that it draws but that it runs.
    # BOTH names of every redirection, in every pane of this file. The loader reads TING_ before
    # UT_, and a pane gets the tmux SERVER's environment, not this shell's — so the `unset` at
    # the top cannot reach it, and a server started from a shell that exported TING_CONFIG or
    # TING_STATE_DIR would have walked this pane's R and D y onto the user's own playlists.
    TUI_CMD="cd '$PWD' && env -u NO_COLOR YT_SYNC=0 UT_IMAGE=on TMPDIR='$TMPDIR' UT_STATE_DIR='$TUI_STATE' TING_STATE_DIR='$TUI_STATE' UT_CONFIG='$TUI_CFG' TING_CONFIG='$TUI_CFG' UT_SORT_FIELD=relevance YT_LANG=en shell/ting 'lofi hip hop'"
    TUI_CMD="$TUI_CMD"'; printf "RC=%s\n" $?'
    TUI_CMD="$TUI_CMD"'; stty -a </dev/tty | tr " " "\n" | grep -E "^-?(echo|icanon)$" | tr "\n" " " | sed "s/^/FLAGS= /"; echo; sleep 20'
    tmux new-session -d -s "$TS" -x 100 -y 30 "$TUI_CMD"
    TUI_TTY=$(tmux display-message -p -t "$TS" '#{pane_tty}' 2>/dev/null)
    # `-echo` with ICANON still SET is the termios signature of getpass(), and terminals poll
    # the pty for exactly that pair: Ghostty flips macOS Secure Input on it, iTerm2 draws a
    # padlock at the cursor — which the fetch spinner parks on its own glyph. Two greps, not a
    # case glob: `-echo` is a prefix of `-echoe`/`-echok`.
    #
    # This is a SAMPLE, and the name says so. The state it looks for is transient, so the
    # sampling rate is what the check is worth: at the capture-pane cadence (0.3s) it could
    # miss a flip that lasted a frame and report a pass it had not earned. termios is read
    # every 0.05s and the pane only every sixth pass — same wall clock, 6x the chance of
    # catching it. Waiting for the first frame is the right window: it is the one stretch of
    # the session where no `read` is running and the tty carries whatever the app left on it.
    booted=0; i=0; getpass=0
    while [ $i -lt 480 ]; do
        if [ -n "$TUI_TTY" ]; then
            flags=$(stty -f "$TUI_TTY" -a 2>/dev/null | tr ' ' '\n')
            [ "$(printf '%s\n' "$flags" | grep -c '^-echo$')" = 1 ] &&
                [ "$(printf '%s\n' "$flags" | grep -c '^icanon$')" = 1 ] && getpass=1
        fi
        if [ $((i % 6)) = 5 ]; then
            # `query='` and not `results=`: the status line is segments now and spells no
            # key=value at all. The title line's source field is what identifies a painted
            # list, at every width this block resizes to.
            tmux capture-pane -t "$TS" -p 2>/dev/null | grep -q "query='" && { booted=1; break; }
        fi
        sleep 0.05; i=$((i + 1))
    done
    report "TUI boots and paints a list" 1 "$booted"
    report "no password prompt sampled" 0 "$getpass"

    # Reflow is width-conditional, so the two geometries that change layout are the ones
    # worth walking. The assertion is survival, not shape: still up, still showing a list.
    alive=1
    for geom in "62x20" "26x24"; do
        gw=${geom%x*}; gh=${geom#*x}
        tmux resize-window -t "$TS" -x "$gw" -y "$gh" 2>/dev/null
        [ "$(poll_until 5 pane_has "query='")" = 1 ] || alive=0
    done
    report "survives 62x20 and 26x24" 1 "$alive"

    tmux resize-window -t "$TS" -x 100 -y 30 2>/dev/null

    # `pane_results` behind the same poller: the row count is a NUMBER on the header line, so
    # the predicate is a comparison rather than a grep, and an empty read (mid-repaint) must
    # not be mistaken for zero.
    results_gt() { local n; n=$(pane_results); [ -n "$n" ] && [ "$n" -gt "$1" ]; }
    results_is() { [ "$(pane_results)" = "$1" ]; }
    results_nonzero() { local n; n=$(pane_results); [ -n "$n" ] && [ "$n" != 0 ]; }

    # ---- the key-hint tier (?) and the j/k pair ----------------------------------------
    # Both ride the pane that is already up, and both read the ONE hint block —
    # the only place in the app where a key is written down, so a tier that did not filter
    # and a tier that filtered everything are both visible from here.
    #
    # `-/= volume` is the marker because the full tier is the only thing that prints it, and
    # it is written `[-]/=` so the pattern does not start with a dash: pane_has passes it
    # straight to grep, which would read a leading `-` as an option and not a pattern.
    # One grep says which tier is up.
    report "the core block leaves the playback keys out" 0 \
        "$(tmux capture-pane -t "$TS" -p -J 2>/dev/null | grep -c '[-]/= volume')"
    # No `p` in front of this any more. It used to be sent first to prove the retired view
    # toggle was "nothing at all", but since the clipboard paste landed `p` IS something: it
    # reads the machine's clipboard, so with text on it the key opened the new-search prompt,
    # the `?` below was typed into that prompt, and every check after it went red — on
    # whichever machine had copied something last (measured 2026-09-23: 24 FAILs from one
    # copied phrase, on this commit and on 0.12.2 alike). Driving `p` for real would mean
    # writing the user's clipboard, which a suite that leaves your state alone may not do.
    tmux send-keys -t "$TS" '?'
    opened=$(poll_until 10 pane_has '[-]/= volume')
    report "? opens the full tier" 1 "$opened"
    # EVERY key that can act is printed by the full tier, and `t` is the one that was not:
    # it sat in usage() and the README while appearing in neither tier, so the block — the
    # one place a user reads what a key is for — was the only surface that did not know it
    # existed. The pane is a tty with colors on, which is exactly `t`'s own gate, so a
    # correct block has to print it here. `i` rides along: the block, the header field
    # and the row source itself all say `chapters` now — the one word the key is about. The
    # source was called `versions` until 2026-08-31, which put `versions='<title>'` on the
    # one line that says what you are looking at.
    report "the full tier prints the theme key" 1 \
        "$(tmux capture-pane -t "$TS" -p -J 2>/dev/null | grep -c 't theme')"
    report "…and names i by what it opens" 1 \
        "$(tmux capture-pane -t "$TS" -p -J 2>/dev/null | grep -c 'i chapters')"
    # The EIGHTH preference key, on the same deferred write as the seven below. The config
    # carries no UT_KEYS line, so this can only APPEND — and the value is asserted in BOTH
    # directions, because a tier that wrote itself once and then stopped would leave the file
    # saying `full` on a screen that had gone back to core.
    wrote=$(poll_until 10 cfg_has '^UT_KEYS=full$')
    report "? writes the tier to your config" 1 "$wrote"
    tmux send-keys -t "$TS" '?'
    hidden=$(poll_until 10 pane_lacks '[-]/= volume')
    report "? switches to hidden tier" 1 "$hidden"
    report "…and drops the key block entirely" 0 \
        "$(tmux capture-pane -t "$TS" -p -J 2>/dev/null | grep -cE '\? keys')"
    wrote=$(poll_until 10 cfg_has '^UT_KEYS=hidden$')
    report "…and writes hidden to your config" 1 "$wrote"
    tmux send-keys -t "$TS" '?'
    restored=$(poll_until 10 pane_has '\? keys')
    report "? cycles back to core tier" 1 "$restored"
    wrote=$(poll_until 10 cfg_has '^UT_KEYS=core$')
    report "…and the file follows it back" 1 "$wrote"
    # Row numbers start off by default. Press # to turn them on via real keypress.
    tmux send-keys -t "$TS" '#'
    shown=$(poll_until 10 pane_has '^[[:space:]>▶▎]*1\. ')
    report "# puts the row numbers on" 1 "$shown"
    wrote=$(poll_until 10 cfg_has '^UT_ROW_INDEX=on$')
    report "…and writes that to your config" 1 "$wrote"

    # Switch to page mode with Tab via a real keystroke — no pre-staged config keys.
    tmux send-keys -t "$TS" Tab
    shown=$(poll_until 10 pane_has 'page [0-9]+/[0-9]+')
    report "Tab enters page mode" 1 "$shown"
    shown_arr=$(poll_until 10 pane_has '(←→|←/→) page')
    report "…and the arrows spend a cell" 1 "$shown_arr"
    wrote=$(poll_until 10 cfg_has '^UT_LIST_MODE=page$')
    report "…and writes the mode to your config" 1 "$wrote"

    # j/k are ↓/↑ in the list view and nowhere else. Ten presses is the page (10 rows on this
    # geometry, 20 in hand), so the marker is the page CROSSING — row 11 appearing — which no
    # amount of j that failed to move the cursor can produce, and which also proves the keys
    # reached move_selection's real arms rather than an arm of their own that forgot the
    # paging arithmetic. The walk back is asserted too: a k bound to the wrong direction
    # would leave the pane on page 2 and the first check would still be green.
    # THE COVER IS READ OFF THE WIRE HERE, and it retires this section's own stated blind
    # spot. `capture-pane` renders the GRID, and a kitty placement is not in the grid — which
    # is why the note above says the picture is the one thing this block cannot see, and why
    # forcing UT_IMAGE=on was worth doing only for what it RUNS. `pipe-pane` is the other
    # end: it copies the bytes the program WROTE, before tmux decides what to do with them,
    # so the graphics escapes are readable even though tmux never passes them on. Nothing is
    # simulated and no key is added — the two windows below wrap keystrokes this block was
    # already sending, and the whole cost is four tmux calls and two greps.
    #
    # WHY A WINDOW AROUND THE WALK: it is the liveness half. `#` below asserts that nothing
    # was sent, and a pane that never drew a cover at all — no network for the thumbnail, a
    # gate that closed — would pass that by having nothing to re-send. So the walk is asked
    # for the opposite: twenty keypresses across ten rows and back must put a cover on the
    # wire at least once, and the zero underneath it only means something after this does.
    IMG_WALK="$UT_TEST_TMP/tui-cover-walk.raw"
    : >"$IMG_WALK"
    tmux pipe-pane -o -t "$TS" "cat >> '$IMG_WALK'"
    tmux send-keys -t "$TS" j j j j j j j j j j
    turned=$(poll_until 10 pane_has '^[[:space:]>▶▎]*11\. ')
    report "j walks the selection onto the next page" 1 "$turned"
    tmux send-keys -t "$TS" k k k k k k k k k k
    back=$(poll_until 10 pane_lacks '^[[:space:]>▶▎]*11\. ')
    report "and k walks it back" 1 "$back"
    # POLLED, NOT SLEPT THROUGH, and the two conditions are why the walk is where this
    # window lives. A cover is fetched on the one-second clock and transcoded by an mpv, so
    # the last `k` returns long before the picture it asks for exists — a window that closed
    # on the pane's own settle read zero every time and blamed the emitter for the clock
    # (measured 2026-09-03: this check went red on its first live run for exactly that).
    # And the stream has to be QUIET as well as non-empty, because the next window asserts a
    # zero: a cover still in flight when the walk's window closes lands inside the toggle's
    # and reads as a re-send. `a=T` is the transmit-and-place escape, and image_emit is the
    # only thing in the suite that writes one.
    img_sent()  { LC_ALL=C /usr/bin/grep -aq '_Ga=T' "$IMG_WALK" 2>/dev/null; }
    img_still() { local a b; a=$(wc -c <"$IMG_WALK" 2>/dev/null); sleep 0.4
                  b=$(wc -c <"$IMG_WALK" 2>/dev/null); [ "$a" = "$b" ]; }
    img_drew=$(poll_until 12 img_sent)
    poll_until 5 img_still >/dev/null
    tmux pipe-pane -t "$TS"

    # The display toggle # tested in both directions.
    IMG_TOG="$UT_TEST_TMP/tui-cover-toggle.raw"
    : >"$IMG_TOG"
    tmux pipe-pane -o -t "$TS" "cat >> '$IMG_TOG'"
    tmux send-keys -t "$TS" '#'
    gone=$(poll_until 10 pane_lacks '^[[:space:]>▶▎]*1\. ')
    report "# takes the row numbers off" 1 "$gone"
    wrote=$(poll_until 10 cfg_has '^UT_ROW_INDEX=off$')
    report "…and writes that to your config" 1 "$wrote"
    tmux send-keys -t "$TS" '#'
    shown=$(poll_until 10 pane_has '^[[:space:]>▶▎]*1\. ')
    report "# puts them back" 1 "$shown"
    wrote=$(poll_until 10 cfg_has '^UT_ROW_INDEX=on$')
    report "…and the file follows it back" 1 "$wrote"
    tmux pipe-pane -t "$TS"
    if [ "$img_drew" = 1 ]; then
        report "a redraw that cannot move the cover does not re-send it" 0 \
            "$(LC_ALL=C /usr/bin/grep -ao '_Ga=T' "$IMG_TOG" 2>/dev/null | wc -l | tr -d ' ')"
    else
        echo "  skip  (no cover reached the pane — nothing to prove about redrawing one)"
    fi

    # → past the last page fetches one more batch (in page mode).
    pane_results() {
        tmux capture-pane -t "$TS" -p -J 2>/dev/null |
            grep -oE '[0-9]+ results' | head -1 | cut -d' ' -f1
    }
    grew=0; i=0
    while [ $i -lt 3 ]; do
        tmux send-keys -t "$TS" Right Right
        [ "$(poll_until 12 results_gt 20)" = 1 ] && grew=1
        [ "$grew" = 1 ] && break
        i=$((i + 1))
    done
    report "the right edge grows the count" 1 "$grew"

    # ← on page 1 is the mirror, truncating back to the floor.
    tmux send-keys -t "$TS" Left Left Left Left Left Left Left Left Left Left Left Left
    shrank=$(poll_until 10 results_is 20)
    report "the left edge drops it again" 1 "$shrank"
    tmux send-keys -t "$TS" Left Left Left Left Left Left
    results_not20() { local n; n=$(pane_results); [ -n "$n" ] && [ "$n" != 20 ]; }
    report "and stops at a screenful" 0 "$(poll_until 1 results_not20)"
    appended=$(poll_until 6 cfg_has '^UT_START_RESULTS=20$')
    report "the count lands in its own key" 1 "$appended"
    report "and not in the step key" 0 "$(grep -c '^UT_FETCH_BATCH' "$TUI_CFG")"

    # ---- the page counter across a tier change, and keys that arrive as one burst --------
    # 22 rows is short enough that the full block takes rows from the page and hidden gives
    # them back, so the page size moves under the counter. The counter is printed before the
    # reflow decides that size, and it used to divide by the previous frame's — `page 1/3`
    # over a list of ten. It is read off the FIRST frame without the block, not polled: with
    # a preference write pending, the one-second tick redraws within a second and a poll
    # would have caught the next, corrected frame and passed the bug.
    pane_rows()  { printf '%s\n' "$1" | grep -cE '^[[:space:]>▶▎]*[0-9]+\. '; }
    pane_pages() { printf '%s\n' "$1" | grep -oE 'page [0-9]+/[0-9]+' | head -1 | cut -d/ -f2; }
    frame_without_block() {
        FRAME=$(tmux capture-pane -t "$TS" -p -J 2>/dev/null)
        ! printf '%s\n' "$FRAME" | grep -qE '\? keys'
    }
    tmux resize-window -t "$TS" -x 100 -y 22 2>/dev/null
    tmux send-keys -t "$TS" '?'
    poll_until 10 pane_has '[-]/= volume' >/dev/null
    full_rows=$(pane_rows "$(tmux capture-pane -t "$TS" -p -J 2>/dev/null)")
    tmux send-keys -t "$TS" '?'
    FRAME=""
    poll_until 10 frame_without_block >/dev/null
    hid_rows=$(pane_rows "$FRAME"); hid_pages=$(pane_pages "$FRAME")
    report "hidden gives page mode back the rows full took" 1 \
        "$([ "$hid_rows" -gt "$full_rows" ] && echo 1 || echo 0)"
    report "…and its first frame counts pages by the rows it shows" 1 \
        "$([ "$hid_rows" -gt 0 ] && [ "$hid_pages" = $(( (20 + hid_rows - 1) / hid_rows )) ] && echo 1 || echo 0)"

    # Two presses in one tmux write land in the tty together, inside the unbracketed-paste
    # probe's window — which is also what a held key or a quick double-tap looks like. One
    # repeated character is keystrokes, not text: `? ?` from hidden walks core and on to full,
    # and a paste would have opened the new-search prompt with `??` in it instead.
    tmux send-keys -t "$TS" '?' '?'
    burst=$(poll_until 10 pane_has '[-]/= volume')
    report "a double-tapped ? is two presses, not a paste" 1 "$burst"
    report "…and opens no search prompt" 0 \
        "$(tmux capture-pane -t "$TS" -p -J 2>/dev/null | grep -c 'New search')"
    # The same for a row jump typed at speed: `12j` in one write goes to row 12 (page 2 of
    # this 20-row list), where a paste would search for "12j".
    tmux send-keys -t "$TS" 1 2 j
    jumped=$(poll_until 10 pane_has '▎[[:space:]]*12\. ')
    report "a fast 12j jumps to row 12, not a search" 1 "$jumped"
    # Back to core at the geometry the rest of the section expects: full -> hidden -> core.
    tmux send-keys -t "$TS" '?' '?'
    poll_until 10 pane_back '[-]/= volume' '\? keys' >/dev/null
    tmux send-keys -t "$TS" 1 j
    tmux resize-window -t "$TS" -x 100 -y 30 2>/dev/null
    poll_until 5 pane_has '▎[[:space:]]*1\. ' >/dev/null

    # Leave page mode back to scroll mode via Tab
    tmux send-keys -t "$TS" Tab
    gone=$(poll_until 10 pane_lacks 'page [0-9]+/[0-9]+')
    report "Tab leaves page mode" 1 "$gone"
    gone_arr=$(poll_until 10 pane_lacks '(←→|←/→) page')
    report "…and the arrows stop spending a cell" 1 "$gone_arr"
    wrote=$(poll_until 10 cfg_has '^UT_LIST_MODE=scroll$')
    report "…and the file follows it back" 1 "$wrote"

    # THE ELEVENTH preference key, and the one that changes what the next Enter LAUNCHES.
    # Asserted on the STATUS SEGMENT rather than on the key's hint cell: that cell is printed
    # from a literal and stays green under a build that bound r to nothing, while the segment
    # is rendered from LOOP_MODE itself. Three presses rather than one, because the discriminator
    # is the ROTATION — a key wired to set one value passes the first check and fails the
    # second — and because the third has to bring the default back, which spends no width at
    # all (the rule quality= and min=/max= already follow). This pane's chrome is pinned to
    # English by YT_LANG, so naming the segment is safe here.
    tmux send-keys -t "$TS" 'r'
    shown=$(poll_until 10 pane_has 'loop seq')
    report "r puts the loop mode on the status line" 1 "$shown"
    wrote=$(poll_until 10 cfg_has '^UT_LOOP_MODE=seq$')
    report "…and writes it to your config" 1 "$wrote"
    tmux send-keys -t "$TS" 'r'
    shown=$(poll_until 10 pane_has 'loop one')
    report "r rotates on rather than toggling" 1 "$shown"
    tmux send-keys -t "$TS" 'r'
    gone=$(poll_until 10 pane_lacks 'loop (seq|one)')
    report "…and the default state spends no width" 1 "$gone"
    wrote=$(poll_until 10 cfg_has '^UT_LOOP_MODE=off$')
    report "…and the file follows it back" 1 "$wrote"

    # THE SCROLLBAR, in both modes and on every visible row: the gutter is what replaced the
    # ●○○○ row, and it is the only thing on screen that says where the window sits when there
    # are no pages. Asserted as a COUNT of rows carrying a gutter cell rather than as a
    # picture: which rows hold the thumb is layout, and layout is capture-pane's (assert_pane
    # measures the column). A list longer than one screen must show BOTH glyphs — all thumb
    # or all track would mean the geometry collapsed — which no implementation that forgot to
    # size the thumb can produce.
    report "every row carries a scrollbar cell" 1 \
        "$(tmux capture-pane -t "$TS" -p -J 2>/dev/null |
            grep -cE '[0-9]:[0-9][0-9] [█│]$' | awk '{print ($1 > 0) ? 1 : 0}')"
    report "…and the thumb is shorter than the track" 1 \
        "$(tmux capture-pane -t "$TS" -p -J 2>/dev/null |
            awk '/[0-9]:[0-9][0-9] █$/ {t++} /[0-9]:[0-9][0-9] │$/ {k++}
                 END {print (t > 0 && k > 0) ? 1 : 0}')"
    # WHAT THIS SECTION DELIBERATELY DOES NOT PROVE: scroll mode's own two ends. In that mode
    # the row count is grown and shrunk by ↓ on the last row and ↑ on the first, and reaching
    # them from here is a twenty-key walk plus a real fetch. The functions behind them —
    # more_results / fewer_results — are the SAME ones page mode's ←/→ drive a few checks up,
    # including the floor; what scroll mode adds is only which key reaches them. That is
    # coverage this file does not have, and saying so beats a check that walks the list and
    # then asserts something the arrows already pinned.

    # ---- the preference write-back and the two count edges ----------------------------
    # All of it on the pane that is ALREADY up: no second cold start, no second cold search.
    # The rows on screen are the state these keys need, and the keys are the only way to
    # reach the write path — there is no verb for it, deliberately (the agent surface for a
    # preference IS the config file, ARCHITECTURE.md「两个根数据文件」).
    #
    # The write is DEFERRED — a cycle sets a dirty bit and the flush happens on the reader's
    # idle tick — so every assertion below POLLS the file instead of reading it once. That is
    # not a workaround for a race; it is the claim: a preference must reach the disk without
    # anyone quitting the app.
    # The count is read by pane_results (defined above) as a SEGMENT — `40 results`, not
    # `results=40`. The unit word is what keeps it from matching any other number on the line,
    # and YT_LANG=en (pinned in TUI_CMD) is what makes naming that word safe here.
    tmux send-keys -t "$TS" v
    wrote=$(poll_until 10 cfg_has '^UT_PLAY_MODE=video')
    report "v writes the mode to your config" 1 "$wrote"
    report "the comment on that line survived" 1 "$(grep -c '# keep me' "$TUI_CFG")"
    report "your config is still the symlink" 1 "$(test -L "$TUI_CFG" && echo 1 || echo 0)"
    report "and the real file behind it moved" 1 \
        "$(grep -c '^UT_PLAY_MODE=video' "$TUI_CFG_REAL")"

    # The SEVENTH preference key, on the same pane and the same deferred write. Two
    # discriminators, neither of which a naive implementation gets for free:
    #   * the config has no UT_PLAY_QUALITY line, so this key can only APPEND — the mode
    #     check above only proves the in-place edit;
    #   * `auto` is deliberately NOT printed on the status line (a field sitting at its
    #     default is pure width — the rule min=/max= already follow), so the line is grepped
    #     BEFORE the press too. An implementation that printed every tier passes the after
    #     check and fails the before one.
    # medium, not high: the shipped UT_QUALITY_CYCLE is `auto medium high`, so one press from
    # the default lands on the second member — an off-by-one that started the rotation at the
    # head would write auto and go red here.
    # The pattern is `quality <tier>`, not bare `quality`: the hint block prints `f quality`
    # on every frame, so the bare word is always on screen and would make this check
    # unfailable. The tier alternation is what separates the segment from the key hint.
    report "the quality tier is absent at auto" 0 \
        "$(tmux capture-pane -t "$TS" -p -J 2>/dev/null | grep -cE 'quality (auto|medium|high)')"
    tmux send-keys -t "$TS" f
    wrote=$(poll_until 10 cfg_has '^UT_PLAY_QUALITY=medium$')
    report "f writes the quality tier to your config" 1 "$wrote"
    shown=$(poll_until 10 pane_has 'quality medium')
    report "…and the status line says so" 1 "$shown"

    # A filter is a page of MATCHES, so running off its end is not a request for more rows.
    # This went red before the guard landed (measured 2026-08-29): `/` then `zzz` then `→`
    # against 20 rows fetched 20 more and dropped the filter — filter_live drives the same
    # move_selection, so more_results' "no filter can be open here" was an assertion, not a
    # fact. `zzz` matches nothing, which is what makes the check discriminating: the filtered
    # count is 0, and an unguarded edge replaces it with a whole re-fetched row set.
    # `/` and the text go in two writes, with the filter's own instruction line as the marker
    # between them: in one write the list reader sees `/zzz` arrive as a single burst, which
    # is the shape of a paste, and a paste in the list is a search — no person types a key
    # and three more inside one tty write, and the suite should not either.
    tmux send-keys -t "$TS" /
    poll_until 10 pane_has 'type to narrow' >/dev/null
    tmux send-keys -t "$TS" z z z
    narrowed=$(poll_until 10 results_is 0)
    report "a filter narrows to nothing" 1 "$narrowed"
    # Esc right behind the arrow, so the wait has a MARKER instead of a guessed duration:
    # leaving the filter restores the rows, and the count that comes back is the answer —
    # 20 if the arrow did nothing, 40 if it re-fetched (Esc is read after the blocking fetch
    # returns, so the number is settled by the time it is non-zero again).
    tmux send-keys -t "$TS" Right
    tmux send-keys -t "$TS" Escape
    poll_until 15 results_nonzero >/dev/null
    n=$(pane_results)
    report "the edge does not fire under it" 20 "$n"

    # The refusal. `o` re-fetches and rotates the sort on screen either way — what must not
    # happen is the WRITE, because the environment pins this key and the next startup would
    # read the file's value and throw it away. The notice names the key, which is what makes
    # this greppable in either chrome language.
    tmux send-keys -t "$TS" o
    said=$(poll_until 15 pane_has 'UT_SORT_FIELD')
    report "a pinned key is refused out loud" 1 "$said"
    report "and never reaches the file" 0 "$(grep -c '^UT_SORT_FIELD' "$TUI_CFG")"
    # A notice is a frame line and ANY key clears it; Space is inert here (there is no player
    # to pause), so this is a plain "get the unadorned frame back" step and asserts nothing.
    tmux send-keys -t "$TS" Space
    poll_until 5 pane_lacks 'UT_SORT_FIELD' >/dev/null

    # `c` must do NOTHING here: it is the third key of that same row-source family, but it is
    # gated on the engine having --parts, and yt does not (one id there is one file). An
    # UNGATED c would call `yt-resolve --parts`, collect the unknown-flag refusal the offline
    # half pins, and put it on the frame under the Parts label — so that label IS the witness,
    # read directly.
    #
    # It used to be read through the NEXT key: a notice owned a blocking read, so an ungated c
    # parked the pane and `h` never opened the log, which made one measurement serve two
    # claims. A notice is a frame line now and eats no keystroke — that being the point of the
    # change — so this check had to stop borrowing its witness or become a check that cannot
    # fail.
    #
    # A poll for an ABSENCE passes on its first look, so this one spends its whole budget:
    # one second of that label never arriving (poll_until counts whole seconds). What it
    # watches for is a FLAG refusal, not a fetch — when it lands, it lands in milliseconds.
    tmux send-keys -t "$TS" c
    said=$(poll_until 1 pane_has 'Parts:')
    report "c is inert on an engine with no --parts" 0 "$said"
    # A store is a room with a door, not a one-way trip — and the door is the key that opened
    # it (ARCH-tui.md). `h` REPLACES the rows with the log (`history='` on the title
    # line, where a search says `query='`) and `h` again puts the search back; until it did, the
    # only exits from that room were retyping a query and quitting. Both halves are asserted:
    # an `h` that quietly did nothing would leave the search on screen and make the return
    # leg pass for free.
    tmux send-keys -t "$TS" h
    opened=$(poll_until 10 pane_has "history='")
    report "h opens the log as the row source" 1 "$opened"
    # Same witness the `q` check keeps, and for the same reason: the pane is the only place a
    # key that went somewhere else is legible. A reader that is not the menu loop (the `n`
    # prompt, the `a`/`b` picker) shows up here and nowhere else.
    if [ "$opened" != 1 ]; then
        echo "  ---- pane at the moment h did not open the log ----" >&2
        tmux capture-pane -t "$TS" -p -J >&2 2>/dev/null
        echo "  ---- end of pane ----" >&2
    fi
    # The toggle. A plain byte, so unlike the Esc this shipped as it needs no disambiguation
    # window — but the poll stays: a redraw is not instant either.
    tmux send-keys -t "$TS" h
    backed=$(poll_until 10 pane_back "history='" "query='")
    report "h again leaves it for search" 1 "$backed"

    # A NOTICE IS NOT AN EXIT. Every row source answers "did not open" with a notice — a frame
    # line the next key clears — for an empty log, a one-part video, a video with no chapters, and each of
    # them hands a 1 back to the menu loop's case arm. An arm without the `|| true` guard
    # turns that 1 into set -e, so the TUI dies ON THE KEY that was supposed to dismiss the
    # notice. It shipped that way for `h`, `c` and `i`.
    #
    # The log is cleared HERE rather than seeded empty, by the same real command that seeded
    # it, because the check above needs rows and this one needs none: the two claims disagree
    # about the store's state, not about the pane. Deterministic either way — no query decides
    # whether this door is closed, which is what the `i` walk below cannot say for itself.
    UT_STATE_DIR="$TUI_STATE" shell/t-history --clear -j >/dev/null 2>&1
    [ "$(UT_STATE_DIR="$TUI_STATE" shell/t-history --ls -j 2>/dev/null | jq -r '.count // 0')" = 0 ] ||
        { echo "contract.sh: the log did not clear — suite error, not a failure" >&2; exit 1; }
    tmux send-keys -t "$TS" h
    said=$(poll_until 10 pane_has 'nothing listened to yet')
    report "an empty log answers with a notice" 1 "$said"
    # THE DISCRIMINATING INPUT for "a notice is content, not a modal" lives HERE rather than
    # in the `i` walk below, because this notice is DETERMINISTIC: the log was cleared by a
    # real command, so it is empty on every run, while whether any of today's rows carries
    # chapters is YouTube's business and the walk can break before it ever meets a notice.
    #
    # The next key is a plain arrow, sent with nothing in between and nothing fed to any
    # reader, and THREE things have to be true of it — each its own report, because they fail
    # for different reasons. The notice goes. The TUI is alive (a dead one leaves the notice
    # on screen and prints RC= under it). And the key DID ITS OWN JOB: the cursor moved one
    # row. That last one is what a modal notice cannot do — it would have spent this
    # keystroke on its own dismissal and left the cursor where it was.
    #
    # The row is read off the pane rather than assumed (everything above this line has been
    # moving the cursor), and the direction is chosen from it so the key always has somewhere
    # to go: a cursor already on the last row would not move DOWN, and that is not a finding.
    cur_row=$(tmux capture-pane -t "$TS" -p -J 2>/dev/null | sed -n 's/^[>▶▎] *\([0-9][0-9]*\)\..*/\1/p' | head -1)
    case "$cur_row" in
    '' | *[!0-9]*)
        echo "contract.sh: no row cursor on the pane before the notice check — suite error, not a failure" >&2
        exit 1 ;;
    esac
    if [ "$cur_row" -gt 1 ]; then nav_key=Up; want=$((cur_row - 1)); else nav_key=Down; want=$((cur_row + 1)); fi
    tmux send-keys -t "$TS" "$nav_key"
    notice_gone() { pane_lacks 'nothing listened to yet' && pane_lacks 'RC='; }
    alive=$(poll_until 10 notice_gone)
    report "…and the next key clears it without exiting" 1 "$alive"
    moved=$(poll_until 5 pane_has "^[>▶▎] +$want\.")
    report "…and that key did its own job, not the notice's" 1 "$moved"
    if [ "$alive" != 1 ]; then
        echo "  ---- pane after the notice was dismissed ----" >&2
        tmux capture-pane -t "$TS" -p -J >&2 2>/dev/null
        echo "  ---- end of pane ----" >&2
    fi

    # `b` is the same door as `h`, but it has to ASK which room — and asking used to mean one
    # line of the store's own prose above a caret identical to the search prompt, with the
    # name typed from memory. It now prints the store NUMBERED, and the number is resolved in
    # the TUI so the store still only ever hears a name. Three claims, one sequence: the
    # picker lists what is stored, a digit opens THAT list (the header names it, so an
    # off-by-one is legible), and `b` again is still the way out.
    tmux send-keys -t "$TS" b
    picked=$(poll_until 10 pane_has '1\. seeded-list')
    report "b lists the stored playlists" 1 "$picked"
    if [ "$picked" != 1 ]; then
        echo "  ---- pane at the moment b did not list the store ----" >&2
        tmux capture-pane -t "$TS" -p -J >&2 2>/dev/null
        echo "  ---- end of pane ----" >&2
    fi
    # The digit, then Enter: prompt_name's reader ends on Enter like every other prompt here.
    tmux send-keys -t "$TS" 1
    tmux send-keys -t "$TS" Enter
    byname=$(poll_until 10 pane_has "playlist='seeded-list'")
    report "1 opens that playlist by number" 1 "$byname"

    # R renames the playlist currently on screen
    tmux send-keys -t "$TS" R
    poll_until 10 pane_has "New name for playlist" >/dev/null
    tmux send-keys -t "$TS" "renamed-list" Enter
    renamed=$(poll_until 10 pane_has "playlist='renamed-list'")
    report "R renames the playlist on screen" 1 "$renamed"

    tmux send-keys -t "$TS" b
    backed=$(poll_until 10 pane_back "playlist='" "query='")
    report "b again leaves it for search" 1 "$backed"

    # reopen the renamed playlist and test D (delete playlist)
    tmux send-keys -t "$TS" b
    poll_until 10 pane_has '1\. renamed-list' >/dev/null
    tmux send-keys -t "$TS" 1 Enter
    opened=$(poll_until 10 pane_has "playlist='renamed-list'")
    report "1 reopens the renamed playlist by number" 1 "$opened"

    # No y/N any more (the undo section below says why): D acts on the one key, and the frame
    # it lands on is the search with the undo offer on it.
    tmux send-keys -t "$TS" D
    del_backed=$(poll_until 10 pane_back "playlist='" "query='")
    report "D deletes the playlist and returns to search" 1 "$del_backed"
    report "…without asking first" 1 "$(pane_lacks 'y/N' && echo 1 || echo 0)"
    del_stored=$(UT_STATE_DIR="$TUI_STATE" shell/t-playlist --ls -j 2>/dev/null | jq -r '.count // 0')
    report "…and the playlist file is deleted from store" 0 "$del_stored"

    # `i` — the fifth row source, and its whole round trip. Three claims in one sequence, and
    # the middle one is the point: a view that opened carrying only what the LIST already
    # shows (title, duration, id) would be the degenerate frame the view exists to beat, and it would sail
    # through an "it opened" check. So the witness is a field the list CANNOT hold — the
    # upload date the fetch went and got, on the status line.
    #
    # It WALKS the rows rather than naming one, because the door is conditional on live data:
    # `i` opens only where the item has chapters, and which of today's results does is not
    # something this file gets to decide. A row without them answers with the notice instead,
    # which is dismissed and the walk continues (how far it walks: below, beside chap_settled).
    #
    # EVERY KEY WAITS FOR THE THING THAT MAKES IT MEAN SOMETHING. The walk used to send
    # `Space Down i` as one burst; the keys were read in order (that was never the bug) but
    # the settle condition was `no chapters` being on screen, and that text stays up until
    # the next redraw — so a lap's first capture could match the PREVIOUS lap's notice, call
    # itself settled in milliseconds, and fire the next lap on top of an in-flight fetch
    # (measured both ways 2026-09-03: honest ~3.1s laps in one run, an instant advance in
    # another, a capture being 30-60ms). It was then fixed by waiting for the notice to be
    # gone — which worked, and which was this file paying interest on a design debt: the
    # notice owned a blocking read, so the walk had to feed it a Space before the TUI would
    # accept another key. The notice is now a line in the frame that the next key clears
    # (ROADMAP.md「横切规范」只为一个答案停下来), so both the Space and the wait are gone.
    #
    # WHICH MAKES THE `Down` BELOW THE DISCRIMINATING INPUT for that design, not a step in
    # the walk: it is sent straight after the notice with nothing in between, so a build
    # whose notice still ate a keypress would spend this key dismissing it, the cursor would
    # not move, and `walked` goes 0 — red, and named as the walk rather than as the door.
    #
    # EIGHT rows is the bet and the walk stops at the first hit: measured 2026-09-03,
    # chapters sat on rows 3, 7 and 8 of the first eight, which is what made FOUR rows a coin
    # flip on the day's ordering rather than a bet on the door. A lap is a real `--info`
    # round trip (~3.1s), so the bound is ~25s and a typical run pays ~10. The pane is dumped
    # if none of the eight opened, so a failure still says whether the door is broken or the
    # query simply went chapterless.
    chap_settled() { pane_has 'chapters=' || pane_has 'no chapters'; }
    iso_on_pane() { pane_has '(19|20)[0-9][0-9]-[0-9][0-9]-[0-9][0-9]'; }
    shown=0; paid=0; row=1; walked=1; nochap=0; kept=0
    while :; do
        tmux send-keys -t "$TS" i
        poll_until 12 chap_settled >/dev/null
        pane_has 'chapters=' && { shown=1; break; }
        nochap=1
        # The round trip is not thrown away when the view does not open: `i` on a chapterless
        # row draws the date it just fetched on the status line anyway. Read HERE, on the
        # first such row, because this is the only moment it can be told apart from the
        # chapter view's own copy of the same segment — after the break below, a date on the
        # pane proves nothing about this claim.
        [ $kept = 0 ] && iso_on_pane && kept=1
        [ $row -ge 8 ] && break
        tmux send-keys -t "$TS" Down
        row=$((row + 1))
        walked=$(poll_until 5 pane_has "^[>▶▎] +$row\.")
        [ "$walked" = 1 ] || break
    done
    report "i opens the chapter rows" 1 "$shown"
    # Named separately, because a walk that stopped early and a walk that found nothing are
    # different failures and the first one is about this file rather than about the TUI.
    report "…and the walk got where it was going" 1 "$walked"
    # Reported only when the walk actually met a chapterless row. Today's ordering decides
    # that, and a claim nothing exercised is stated as unproved rather than counted green —
    # the alternative is a check that cannot fail on the day chapters sit on row 1.
    if [ $nochap = 1 ]; then
        report "…and a chapterless row keeps what it fetched" 1 "$kept"
    else
        echo "  skip  (no chapterless row in today's first eight — nothing to keep)"
    fi
    # The date lost its `uploaded=` label with the rest of the key=value status line: a
    # date is the one value on that line that cannot be mistaken for anything else. So the
    # pattern is the SHAPE of an ISO date, which no other segment on this line can produce
    # (the counts are bare integers and the like count has a word in front of it).
    tmux capture-pane -t "$TS" -p -J 2>/dev/null |
        grep -qE '(19|20)[0-9][0-9]-[0-9][0-9]-[0-9][0-9]' && paid=1
    report "…and it carries what the fetch got" 1 "$paid"
    # The rail is a SPAN here, and that is a claim about MEANING rather than about layout: a
    # chapter is `0:00 → 2:30`, not a length, and the column it sits in is the one a search
    # row uses to say how LONG it is. Printing the start alone — which is what shipped —
    # passes every other check on this view while telling the reader a number whose meaning
    # silently changed between two lists that are otherwise identical. The pattern is a
    # SHAPE, never a value: which chapters today's item has is the site's business. The gap
    # after the arrow is ` +` and not ` ` for the same reason — both times are right-aligned
    # in a field sized over the whole table, so a page whose ends are all shorter than the
    # widest one is padded, and pinning one space would make this check depend on which page
    # the item's hour mark falls on. Alignment itself is not asserted here: layout belongs to
    # capture-pane and drive.sh.
    report "…and a chapter row reads as a span" 1 \
        "$(tmux capture-pane -t "$TS" -p -J 2>/dev/null |
            grep -cE '[0-9]:[0-9][0-9] (→|->) +[0-9]+:[0-9][0-9]' | awk '{print ($1 > 0) ? 1 : 0}')"
    if [ "$shown" != 1 ] || [ "$paid" != 1 ]; then
        echo "  ---- pane at the moment i did not open the chapter rows ----" >&2
        tmux capture-pane -t "$TS" -p -J >&2 2>/dev/null
        echo "  ---- end of pane ----" >&2
    fi
    # The way back, which is the same key — the rule b, h and c already follow, and the line
    # that says so rather than the commit message. It cannot pass by accident: `chapters='`
    # and `query='` are different field NAMES on the same title line, so a view that never
    # changed would still be showing the first one.
    # GATED ON `shown`, because the assertion is "no chapters=' and a query='" and a screen
    # that never entered the chapter view satisfies both by standing still. Un-gated, this
    # was green on exactly the runs where the walk had already gone red — a check whose
    # comment claimed it could not pass by accident, passing by accident.
    if [ $shown = 1 ]; then
        tmux send-keys -t "$TS" i
        backed=$(poll_until 10 pane_back "chapters='" "query='")
        report "i again leaves it for search" 1 "$backed"
    else
        echo "  skip  (the walk never opened a chapter view to leave)"
    fi

    # A PASTE INTO THE LIST IS A QUERY, not a run of keybindings. This is the half the
    # offline check above cannot reach — it needs a list, and a list needs a fetch. The
    # payload's first two characters are the discriminating input: `j` moves the cursor and
    # `a` opens the playlist-name prompt, so a build that still dispatches a paste as keys
    # lands on a store prompt reading "Name the new playlist", never on the search prompt.
    tmux set-buffer -b ctpaste2 'jazz #hop' 2>/dev/null
    tmux paste-buffer -p -b ctpaste2 -t "$TS" 2>/dev/null
    prefilled=$(poll_until 10 pane_has 'New search.*jazz #hop')
    report "a paste opens the search prompt with the text in it" 1 "$prefilled"
    report "…and not the playlist prompt j+a used to open" 0 \
        "$(tmux capture-pane -t "$TS" -p -J 2>/dev/null | grep -c 'Name the new playlist')"
    tmux send-keys -t "$TS" Escape
    backed=$(poll_until 10 pane_lacks 'New search')
    report "Esc leaves the prompt the paste opened" 1 "$backed"

    # ── A row is reached by TYPING its number: <digits>j ────────────────────────────────
    # `08` is the discriminating input, and it discriminates because a row number is a run of
    # digit LITERALS with `0` a legal leading one (it stopped meaning "the tenth" when the
    # jump went absolute). Without an explicit base, $((08)) on the 3.2 floor is not a wrong
    # answer but
    #     bash: ((: 08: value too great for base (error token is "08")
    # and MEASURED against a deliberately broken copy, 2026-09-02: the TUI does not survive
    # it. The whole pane goes, which is why the witness is the `n` prompt rather than the
    # error text — a dead pane captures empty and greps clean, so a check looking for that
    # message would go green on the very build it exists to catch.
    #
    # Where the cursor LANDED is deliberately not asserted: that is a claim about a picture,
    # and tests/drive.sh drives it while capture-pane proves it. There is likewise no check
    # here for an out-of-range number — a build with no bound test lands `selected` past the
    # end, the renderer clamps it, and the TUI lives, so nothing this file may assert on can
    # tell the two apart. ARCH-tui.md says so where a reader will look for it.
    tmux send-keys -t "$TS" 0
    tmux send-keys -t "$TS" 8
    tmux send-keys -t "$TS" j
    tmux send-keys -t "$TS" n
    jumped=$(poll_until 10 pane_has 'New search')
    report "a leading zero in a row number is read base-10" 1 "$jumped"
    if [ "$jumped" != 1 ]; then
        echo "  ---- pane after 08j (empty means the TUI took the octal down) ----" >&2
        tmux capture-pane -t "$TS" -p -J >&2 2>/dev/null
        echo "  ---- end of pane ----" >&2
    fi
    tmux send-keys -t "$TS" Escape
    backed=$(poll_until 10 pane_lacks 'New search')
    report "…and the prompt it opened closes again" 1 "$backed"

    # `q` used to be asserted by waiting for tmux to tear the session down, which proves the
    # pty is not wedged but says nothing about the status or about what was handed back. The
    # pane now outlives the TUI, so both come out of the same exit.
    tmux send-keys -t "$TS" q
    left=$(poll_until 10 pane_has 'RC=0')
    report "quits on q with 0" 1 "$left"
    # A red here is TWO reds: the FLAGS line the next check reads is printed by the same
    # command line, after ting returns, so a TUI that did not leave takes the tty check down
    # with it. And the pane is the only witness there will ever be. `q` cannot be SLOW — the
    # dispatch arm prints and exits, and with no player the nav read blocks with no timeout —
    # so the byte was eaten by a reader that is not the menu loop (the `n` prompt or the
    # `a`/`b` picker — the fetch spinner is a background loop that reads nothing), and which one it was is
    # legible in the frame and nowhere else. Measured once, 2026-08-25, and unreproducible
    # since. The list is one reader shorter than it was: a notice no longer owns one.
    if [ "$left" != 1 ]; then
        echo "  ---- pane at the moment q was not honoured ----" >&2
        # `>&2` BEFORE `2>/dev/null`: the other order points stdout at stderr's CURRENT
        # target, which by then is /dev/null, and the dump silently prints nothing.
        tmux capture-pane -t "$TS" -p -J >&2 2>/dev/null
        echo "  ---- end of pane ----" >&2
    fi
    restored=0
    tui_flags=" $(tmux capture-pane -t "$TS" -p -J 2>/dev/null | grep -o 'FLAGS=.*' | head -1) "
    case "$tui_flags" in *" echo "*) case "$tui_flags" in *" icanon "*) restored=1 ;; esac ;; esac
    report "hands the tty back on exit" 1 "$restored"
    # The one place in this file where a PROCESS can outlive the run. Nothing above presses
    # Enter, so zero is the honest expectation — and on 2026-08-25 it was not what a machine
    # running this file got: a real player was up behind a red `q`, started from a URL off the
    # result list. The trap reaps it now whatever happens, which is why this is a check rather
    # than a silent stop: the reap makes the leak harmless, and only this line makes it VISIBLE.
    report "the TUI left no player behind" 0 \
        "$(shell/t-play --status -j 2>/dev/null | jq '.players | length')"
    tmux kill-session -t "$TS" 2>/dev/null
    rm -rf "$TUI_STATE"

    undo_pane

    # ── The install goes away under a running session: `q` must still stop the player ─────
    # Measured at the 0.14.0 release: `brew upgrade` deleted the old cellar a second after a
    # session opened from it had started a track, and `q` then ran a deleted `t-play --stop`
    # and left the mpv playing with nothing attached. The upgrade is reproduced for real: a
    # copy of the suite is the install, the pane runs `ting` from it, and the copy is removed
    # while the track plays. Two answers, told apart by what PATH still holds — the stable
    # name a package manager keeps for the current version (this checkout's shell/ stands in
    # for it), or nothing at all, where the stop cannot happen and the exit must say so.
    #
    # PATH is narrowed to a directory of links to the external tools, which isolates rather
    # than stages: nothing in it behaves differently from the real tool it points at.
    RL_BASE=$(mktemp -d "${TMPDIR:-/tmp}/ting-relocate.XXXXXX")
    mkdir -p "$RL_BASE/tools" "$RL_BASE/state"
    : >"$RL_BASE/cfg"
    for _t in jq mpv yt-dlp curl nc ncat; do
        command -v "$_t" >/dev/null 2>&1 && ln -s "$(command -v "$_t")" "$RL_BASE/tools/$_t"
    done
    rl_players() { shell/t-play --status -j 2>/dev/null | jq '.players | length'; }
    # rl_run <PATH for the pane> — boots from a fresh copy, plays, deletes the copy, presses q.
    rl_run() {
        rm -rf "$RL_BASE/inst"
        mkdir -p "$RL_BASE/inst"
        cp -R shell config VERSION "$RL_BASE/inst/"
        tmux kill-session -t "$TS" 2>/dev/null
        tmux new-session -d -s "$TS" -x 100 -y 30 \
            "env PATH='$1' YT_SYNC=0 UT_HISTORY=0 TMPDIR='$TMPDIR' UT_STATE_DIR='$RL_BASE/state' TING_STATE_DIR='$RL_BASE/state' UT_CONFIG='$RL_BASE/cfg' TING_CONFIG='$RL_BASE/cfg' YT_LANG=en '$RL_BASE/inst/shell/ting' --volume 0 'lofi hip hop'; printf 'RC=%s\n' \$?; sleep 20"
        poll_until 40 pane_has "query='" >/dev/null
        tmux send-keys -t "$TS" Enter
        [ "$(poll_until 40 pane_has 'Playing: ')" = 1 ] || return 1
        rm -rf "$RL_BASE/inst"
        tmux send-keys -t "$TS" q
        poll_until 15 pane_has 'RC=' >/dev/null
        return 0
    }
    TS="ctest-relocate-$$"
    shell/t-play --stop --all -j >/dev/null 2>&1
    if rl_run "$PWD/shell:$RL_BASE/tools:/usr/bin:/bin"; then
        report "install gone, t-play on PATH: q stops the player" 0 "$(rl_players)"
    else
        report "the relocate pane's player starts" 1 0
    fi
    shell/t-play --stop --all -j >/dev/null 2>&1
    if rl_run "$RL_BASE/tools:/usr/bin:/bin"; then
        report "install gone, no t-play anywhere: q says how to stop it" 1 \
            "$(pane_has 'still playing .* t-play --stop --id [A-Za-z0-9]+' && echo 1 || echo 0)"
        report "…and the player it names is really still up" 1 "$(rl_players)"
    else
        report "the relocate pane's player starts (no t-play)" 1 0
    fi
    shell/t-play --stop --all -j >/dev/null 2>&1
    tmux kill-session -t "$TS" 2>/dev/null
    rm -rf "$RL_BASE"

    # ── Startup adoption: the player this screen did NOT launch ─────────────────────────
    # The bug this section pins was audible. With `t-play -d` already playing, ting started
    # with an empty banner (the state block initialises to "nothing is attached", and nothing
    # ever asked otherwise), every key that needs a target fell through its own
    # `[[ -n "$CURRENT_PLAY_ID" ]]` guard as a silent no-op, and Enter launched a SECOND mpv
    # over the first — two tracks in the speakers at once, and the older player unreachable
    # from the screen for the rest of the session.
    #
    # TWO panes, and it cannot be fewer: adoption is decided ONCE per process, at startup,
    # so each answer needs a startup of its own. Every player below is a real detached player
    # and every answer is read back from `t-play --status` in THIS shell, not from the frame.
    ADOPT_STATE=$(mktemp -d "${TMPDIR:-/tmp}/ting-adopt.XXXXXX")
    # An EMPTY config, and empty is the point: it stages nothing, it is only somewhere for the
    # pane's preference write-back to land that is not the developer's real file — the same
    # isolation UT_STATE_DIR gives the stores. YT_LANG=en beside it because three checks below
    # name an English chrome string, and a blank config would let the machine's locale decide.
    ADOPT_CFG="$UT_TEST_TMP/adopt-config"
    : >"$ADOPT_CFG"
    # TMPDIR is the suite's, deliberately and unlike UT_STATE_DIR: the players directory lives
    # under it, so this shell and the pane have to share one — that shared dir IS how the pane
    # can see a player this shell started. The EXIT trap's `--stop --all` reaps whatever any
    # check below leaves behind.
    # TS, the variable pane_has reads, is set HERE and not inside adopt_boot: the boot is
    # called as `$(adopt_boot)` for its poll result, command substitution is a SUBSHELL, and
    # an assignment made in there reaches nothing. It cost this section an afternoon of
    # "adopted but no banner" — every pane grep was reading the previous section's dead
    # session, which captures empty and so can only ever agree with `0`.
    TS="ctest-adopt-$$"
    ADOPT_TS="$TS"
    adopt_boot() {
        tmux kill-session -t "$ADOPT_TS" 2>/dev/null
        tmux new-session -d -s "$ADOPT_TS" -x 100 -y 30 \
            "cd '$PWD' && env YT_SYNC=0 UT_HISTORY=0 TMPDIR='$TMPDIR' UT_STATE_DIR='$ADOPT_STATE' TING_STATE_DIR='$ADOPT_STATE' UT_CONFIG='$ADOPT_CFG' TING_CONFIG='$ADOPT_CFG' YT_LANG=en shell/ting 'lofi hip hop'; printf 'RC=%s\n' \$?; sleep 20"
        # The header's own count word, the same first-frame marker the section above waits on:
        # the spinner line that precedes it says `searching "…"` and never `results`.
        poll_until 40 pane_has 'results'
    }
    adopt_n()       { shell/t-play --status -j 2>/dev/null | jq '.players | length'; }
    adopt_id()      { shell/t-play --status -j 2>/dev/null | jq -r '.players[0].id // ""'; }
    adopt_paused()  { [ "$(shell/t-play --status -j 2>/dev/null | jq -r '.players[0].paused')" = true ]; }
    adopt_playing() { [ "$(shell/t-play --status -j 2>/dev/null | jq -r '.players[0].paused')" = false ]; }
    adopt_swapped() { local i; i=$(adopt_id); [ -n "$i" ] && [ "$i" != "$A_ID" ]; }
    adopt_none()    { [ "$(adopt_n)" = 0 ]; }
    # PLAYING, not merely started. A detached player exists as a record the moment it forks,
    # and its title stays null until the child has resolved the URL — adopt that window and
    # the banner is legitimately blank (it fills itself on the first tick, once mpv answers
    # with a media-title). Polled, never slept: the wait is a yt-dlp call, so its length is
    # the network's to decide.
    adopt_ready()   { [ -n "$(shell/t-play --status -j 2>/dev/null | jq -r '.players[0].title // ""')" ]; }

    # The queue view's playlist (the refusal check below), written by the REAL store from the
    # REAL search envelope this suite already fetched. Nothing here is staged: t-playlist
    # produces the file it is later asked to read.
    printf '%s' "$YT_S" | UT_STATE_DIR="$ADOPT_STATE" TING_STATE_DIR="$ADOPT_STATE" shell/t-playlist --add qv-list -j >/dev/null 2>&1
    qv_len()  { shell/t-play --queue-show -j 2>/dev/null | jq -r '.len // 0'; }
    qv_urls() { shell/t-play --queue-show -j 2>/dev/null | jq -c '[.items[].url]'; }
    qv_is()   { [ "$(qv_len)" = "$1" ]; }

    # ---- one background player: adopted, controllable, and replaced on Enter ----------
    A_ID=$(shell/t-play -d -j --engine yt -- "$BARE" 2>/dev/null | jq -r '.id // ""')
    [ -n "$A_ID" ] ||
        { echo "contract.sh: the background player did not start — suite error, not a failure" >&2; exit 1; }
    report "the background player is playing before the screen opens" 1 "$(poll_until 60 adopt_ready)"
    booted=$(adopt_boot)
    report "the adoption pane boots" 1 "$booted"
    # `Playing: .+` and not `Playing:`: the label alone is what an adoption that got the id
    # and nothing else would print, and that was a real state of this code — the banner is
    # only worth its line if the TRACK reached it.
    report "a running player is on the banner of the FIRST frame" 1 "$(poll_until 10 pane_has 'Playing: .+')"
    # The banner alone could be drawn from a record read once. This is the half that cannot:
    # Space goes out as `t-play --pause --id`, and the answer is read back here from the
    # player's own state — so it proves the pane adopted the ID AND the socket of the process
    # that is actually decoding.
    tmux send-keys -t "$ADOPT_TS" Space
    report "…and its keys reach that player" 1 "$(poll_until 10 adopt_paused)"
    # Back again, and asserted rather than assumed: Space is two idempotent verbs and never a
    # toggle (ARCH-player.md「运行时 IPC」).
    tmux send-keys -t "$ADOPT_TS" Space
    report "…both ways" 1 "$(poll_until 10 adopt_playing)"

    # Enter over an adopted player: replaced, never stacked.
    tmux send-keys -t "$ADOPT_TS" j
    tmux send-keys -t "$ADOPT_TS" Enter
    # The COUNT is what the ghost bug got wrong (two mpvs in the speakers); the ID is what a
    # dropped Enter would get wrong (still the adopted one, nothing launched).
    report "Enter replaces the adopted player" 1 "$(poll_until 30 adopt_swapped)"
    report "…and does not stack a second mpv behind it" 1 "$(adopt_n)"

    # ── The queue view (key: u), the one row source that WRITES ────────────────────────
    # Here rather than in playback.sh because the claim is a MAPPING and only a real terminal
    # can drive it: from the cursor, through the row record, to the index `t-play` is asked
    # to act on. playback.sh proves the verbs themselves against a real player; what it cannot
    # reach is whether the TUI names the row the user is looking at.
    #
    # And it is the one worth a tmux round trip, because it is the only key in this file that
    # can destroy something a user cannot get back: a queue dies with its player, so `x` on
    # the wrong row is a track gone from everywhere. Asserted against the PLAYER'S OWN QUEUE
    # rather than against the frame — what the screen drew is not evidence about what was
    # removed, and the queue file is where the answer actually is.
    #
    # It rides THIS pane rather than booting its own: after Enter the screen holds exactly what
    # the section used to spend a cold start, a cold search and a resolve to reach — a player
    # launched from the list, with a queue of one.
    report "the list-launched player's queue is one track" 1 "$(poll_until 20 qv_is 1)"
    tmux send-keys -t "$TS" Down
    tmux send-keys -t "$TS" +
    report "+ queues a second track" 1 "$(poll_until 20 qv_is 2)"
    tmux send-keys -t "$TS" Down
    tmux send-keys -t "$TS" +
    report "+ queues a third" 1 "$(poll_until 20 qv_is 3)"
    # `u` opens it, and the header field is the proof: the source name IS what the first
    # line prints (`queue='…'`, the same shape as `playlist='…'`), so a view that opened
    # under the wrong LIST_SOURCE says so there and nowhere else.
    tmux send-keys -t "$TS" u
    report "u opens the queue as the row source" 1 "$(poll_until 15 pane_has "queue='")"
    # The status line's own count, not a row number: numbering is off until `#` toggles it,
    # and this says more anyway. A search row source counts "results" and only a store
    # counts "items", so `3 items` is both halves of the claim at once — the rows on screen
    # are the queue's, and there are as many of them as the player says it holds.
    report "…and the rows on screen are the queue's" 1 "$(poll_until 10 pane_has '[^0-9]3 items')"
    # THE MAPPING. The cursor opens on the playing track (index 0); two Downs put it on
    # index 2, and `x` must remove THAT one. Asserted by naming the url beforehand and
    # looking for its absence afterwards — a length check alone would pass if the wrong
    # track went.
    qv_doomed=$(shell/t-play --queue-show -j 2>/dev/null | jq -r '.items[2].url')
    qv_spared=$(shell/t-play --queue-show -j 2>/dev/null | jq -c '[.items[0].url,.items[1].url]')
    tmux send-keys -t "$TS" Down
    tmux send-keys -t "$TS" Down
    tmux send-keys -t "$TS" x
    report "x removes a waiting track" 1 "$(poll_until 20 qv_is 2)"
    report "…and it removed the one under the cursor" "$qv_spared" "$(qv_urls)"
    report "…which is not the one it was told to remove" 0 \
        "$(qv_urls | grep -c -F "$qv_doomed")"
    # x on the PLAYING row is refused, and says so instead of doing nothing visible. The
    # length not moving is the other half: a refusal that had already written would look
    # identical from the notice alone.
    qv_before=$(qv_urls)
    tmux send-keys -t "$TS" Up
    tmux send-keys -t "$TS" Up
    tmux send-keys -t "$TS" x
    report "x on the playing track says why" 1 "$(poll_until 10 pane_has 'this one is playing')"
    report "…and removed nothing" "$qv_before" "$(qv_urls)"
    # The way back, which is the whole of the row-source contract: one key in, the same
    # key out, and the search that was stashed still there. `stored_rows` not knowing
    # about this source is exactly how `u` becomes a door that only opens.
    tmux send-keys -t "$TS" u
    report "u goes back to the search" 1 "$(poll_until 10 pane_has "query='lofi")"
    # And from a STORE row source it REFUSES. There is one stash slot, so a queue opened
    # on top of a playlist would leave the search in it and send `u` back to the search
    # with the playlist gone — a list vanishing with nothing said about it. The second
    # assertion is the one that matters: the playlist is still on screen afterwards.
    tmux send-keys -t "$TS" b
    if [ "$(poll_until 10 pane_has '1\. qv-list')" = 1 ]; then
        tmux send-keys -t "$TS" 1
        tmux send-keys -t "$TS" Enter
        opened=$(poll_until 10 pane_has "playlist='qv-list'")
        report "a playlist is on screen to press u from" 1 "$opened"
        if [ "$opened" = 1 ]; then
            tmux send-keys -t "$TS" u
            report "u from a playlist says to go back first" 1 \
                "$(poll_until 10 pane_has 'back to the results')"
            report "…and the playlist is still there" 1 \
                "$(poll_until 5 pane_has "playlist='qv-list'")"
        fi
    else
        report "the queue section's playlist was listed" 1 0
    fi
    tmux send-keys -t "$ADOPT_TS" q
    report "the Enter pane quits" 1 "$(poll_until 10 pane_has 'RC=0')"
    # This player was launched from the list, so it leaves with the screen.
    report "q takes the player this session started" 1 "$(poll_until 10 adopt_none)"
    tmux kill-session -t "$ADOPT_TS" 2>/dev/null

    # ---- two: ambiguous, so the screen adopts neither and says so ----------------------
    # `t-play` answers `ambiguous` to a bare command with two live players (resolve_target).
    # The screen guesses no harder than the core does, and it must not quietly stop either one
    # on the way out — the same claim as above for a player it never took.
    A_ID=$(shell/t-play -d -j --engine yt -- "$BARE" 2>/dev/null | jq -r '.id // ""')
    B_ID=$(shell/t-play -d -j --engine yt -- "$CAPTIONED" 2>/dev/null | jq -r '.id // ""')
    [ -n "$A_ID" ] && [ -n "$B_ID" ] ||
        { echo "contract.sh: background players did not start — suite error, not a failure" >&2; exit 1; }
    report "two players are live for the ambiguous case" 2 "$(adopt_n)"
    booted=$(adopt_boot)
    report "the ambiguous pane boots" 1 "$booted"
    report "…with no banner: two players is not a guess to make" 0 \
        "$(tmux capture-pane -t "$ADOPT_TS" -p -J 2>/dev/null | grep -c 'Playing:')"
    report "…and the screen says why" 1 "$(poll_until 10 pane_has 'several background players')"
    tmux send-keys -t "$ADOPT_TS" q
    report "the ambiguous pane quits too" 1 "$(poll_until 10 pane_has 'RC=0')"
    report "…and stopped neither player" 2 "$(adopt_n)"
    shell/t-play --stop --all -j >/dev/null 2>&1
    report "q leaves players it did not adopt running" 1 "$(poll_until 10 adopt_none)"
    tmux kill-session -t "$ADOPT_TS" 2>/dev/null
    rm -rf "$ADOPT_STATE"

    # ── The parts view (key: c), on whichever installed engine HAS --parts ──────────────
    # This row source had no coverage at all. The session above drives yt, where `c` is inert
    # by capability — which is a real claim and is checked up there, but it means open_parts'
    # whole happy path (fetch, count, reshape, stash, build, label) only ever ran in a human's
    # terminal. It went unnoticed because the key LOOKS covered.
    #
    # The engine is discovered by CAPABILITY, never named: _ro_verb_has is the probe the
    # read-only verb cases above already use, and it asks the same question ting's own
    # refresh_engine_flags asks. So a fourth engine with --parts is covered the day it lands,
    # and a checkout without a --parts engine skips with a reason instead of going red.
    PARTS_ENG=""
    for n in $ENGINES; do
        _ro_verb_has "$n" "--parts" && { PARTS_ENG="$n"; break; }
    done
    if [ -z "$PARTS_ENG" ]; then
        echo "  skip  (no installed engine has --parts — no parts view to open)"
    else
        # Its own config and its own state dir, not the section's above: the `#` check up
        # there TOGGLES UT_ROW_INDEX and writes it back, so borrowing that file would make
        # the row cursor readable or not depending on which checks ran before this one.
        PTS_CFG="$UT_TEST_TMP/parts-config"
        : >"$PTS_CFG"
        PTS_STATE=$(mktemp -d "${TMPDIR:-/tmp}/ting-partsstore.XXXXXX")
        TS="ctest-parts-$$"          # the helpers above read $TS; the first session is gone
        tmux kill-session -t "$TS" 2>/dev/null
        tmux new-session -d -s "$TS" -x 100 -y 30 \
            "cd '$PWD' && env YT_SYNC=0 TMPDIR='$TMPDIR' UT_STATE_DIR='$PTS_STATE' TING_STATE_DIR='$PTS_STATE' UT_CONFIG='$PTS_CFG' TING_CONFIG='$PTS_CFG' YT_LANG=en shell/ting --engine $PARTS_ENG -n 10 'lofi hip hop'; printf 'RC=%s\n' \$?; sleep 20"
        up=$(poll_until 30 pane_has "query='")
        if [ "$up" != 1 ]; then
            report "the parts pane came up" 1 "$up"
        else
            # Enable row numbering with # and page mode with Tab via real keypresses
            tmux send-keys -t "$TS" '#'
            poll_until 5 pane_has '^[[:space:]>▶▎]*1\. ' >/dev/null
            tmux send-keys -t "$TS" Tab
            poll_until 5 pane_has 'page ' >/dev/null
            # The same walk shape the `i` block uses, and for the same reason: which of
            # today's rows is multi-part is the site's business, not this file's. Cheaper per
            # lap than that one — `--parts` is a single HTTP request, not an extraction — so
            # the bound is the WHOLE page it fetched, ten rows. Six was the bound until
            # 2026-09-13, when the day's ranking put the only multi-part rows at 7 and 9 and
            # the walk went red one row short of the door it was testing — a bound smaller
            # than the page is a bet on the ordering, and the ordering is the site's to
            # change (measured 2026-09-03 the same query answered 17, 6, 99, 1, 1, 3 on its
            # first six, so the bet looked free right up until it was not). At ten, the only
            # thing that can still fail is "no row on this page has parts", which is a
            # different claim and a visible one — the pane is dumped when it happens.
            parts_settled() { pane_has "parts='" || pane_has 'only one part'; }
            # `pallanswered` stays 1 only while EVERY lap came back with one of the two
            # answers. It is what separates "the page has no multi-part row" from "a lap sat
            # there saying nothing", and only the first of those is allowed to skip below.
            popened=0; pallanswered=1; prow=1; pwalked=1
            while :; do
                tmux send-keys -t "$TS" c
                [ "$(poll_until 12 parts_settled)" = 1 ] || pallanswered=0
                pane_has "parts='" && { popened=1; break; }
                [ $prow -ge 10 ] && break
                tmux send-keys -t "$TS" Down
                prow=$((prow + 1))
                pwalked=$(poll_until 5 pane_has "^[>▶▎] +$prow\.")
                [ "$pwalked" = 1 ] || break
            done
            # A SKIP, not a red, when every row on the page refused — and the distinction is
            # not politeness, it is which claim failed. `c` on a single-part row answering
            # "only one part" IS the door working: the key reached the engine, a real
            # `--parts` round trip came back, and the view declined for the one reason it is
            # allowed to. What is missing then is a multi-part video in bilibili's ranking
            # for this query, which this file does not get a vote on — and it moves: the same
            # query answered 17, 6, 99, 1, 1, 3 across its first six rows on 2026-09-03, and
            # on 2026-09-13 returned pages whose whole ten were single-part, twice, with the
            # ordering different on every call. A check that reds on that is reporting the
            # site's catalogue as a defect in the TUI. So the red is kept for the two things
            # that ARE this suite's business — the walk stalling (reported just below) and a
            # lap that settled on neither answer, which is the door being broken rather than
            # closed — and the pane is dumped either way, so a skip is never silent.
            if [ "$popened" = 1 ]; then
                report "c opens a multi-part row as the row source" 1 "$popened"
            elif [ "$pallanswered" = 1 ]; then
                echo "  skip  (c answered on all $prow rows; none of today's is multi-part)"
            else
                report "c opens a multi-part row as the row source" 1 "$popened"
            fi
            if [ "$popened" != 1 ]; then
                echo "  ---- pane where no row of $prow opened its parts ----" >&2
                tmux capture-pane -t "$TS" -p -J >&2 2>/dev/null
                echo "  ---- end of pane ----" >&2
            fi
            # Named apart from the door for the reason the `i` walk names its own: a walk
            # that stalled and a query that had no multi-part row are different failures, and
            # the first one is about this file. It is also the DISCRIMINATING INPUT on a
            # second, independent notice path — after a single-part row's refusal the walk's
            # next key is a plain Down with nothing fed to any reader, so a notice that still
            # ate a keypress would leave the cursor where it was.
            report "…and the walk got past every refusal on the way" 1 "$pwalked"
            if [ $popened = 1 ]; then
                # The witness that the ENVELOPE was read and not merely the rows: the
                # collection's own total duration — the number the search row was showing
                # while Enter on that row plays part one. No row of this view carries it and
                # no search screen explains it, so a parts view built without reading
                # total_duration_fmt still lists parts and fails right here.
                report "…and states the collection's total, which no row holds" 1 \
                    "$(pane_has 'total [0-9]' && echo 1 || echo 0)"
                tmux send-keys -t "$TS" c
                report "c again leaves the parts for search" 1 \
                    "$(poll_until 10 pane_back "parts='" "query='")"
            else
                echo "  skip  (no parts view was opened to read a total off or to leave)"
            fi
            # NOT reported, and this says why so the gap is not rediscovered as an oversight:
            # "a single-part row answers with a notice instead of a one-row list" cannot be
            # separated by a walk that stops at the first view it opens. A build that DID
            # open a one-row list would break the loop as "opened" and never reach a claim
            # about the refusal — so any report here would be a tautology on the branch that
            # observed it, which is the one thing this file will not print. Proving it needs
            # a row known to hold one part before the key is pressed, and today no engine's
            # search page says which row that is (the same measurement that makes `c` a
            # question key: docs/ROADMAP.md「横切规范」按下前可预期).
        fi
        tmux send-keys -t "$TS" q 2>/dev/null
        poll_until 5 pane_has 'RC=0' >/dev/null
        tmux kill-session -t "$TS" 2>/dev/null
        UT_STATE_DIR="$PTS_STATE" shell/t-play --stop --all >/dev/null 2>&1
        rm -rf "$PTS_STATE"
    fi
fi

report_real_config
report_real_state
summary
