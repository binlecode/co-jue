---
name: audit-conformance
description: Periodic whole-suite audit of ting against the coding rules baked into this skill — surface layering (no YouTube logic in the TUI), DRY across the ten scripts, bash 3.2 portability, dead functions and one-sided variables, swallowed errors, stale prose defending retired mechanisms, contract drift in the JSON envelope / exit codes, and doc drift against docs/ARCHITECTURE.md. Inventories every violation with file:line + rule citation, then writes a scoped cleanup report. The whole-tree counterpart to reviewing a single diff. Never proposes structural or guard tests.
argument-hint: "[file-or-surface scope, default all of shell/]"
disable-model-invocation: true
---

# audit-conformance

**Invocation:** `/audit-conformance [scope]` (default scope: every script in `shell/`)

**Mission:** reviewing a diff catches what a *change* introduces; it is structurally blind to
slow accretion. This skill is the other scope: judgment-scan the whole suite against the rules
**defined in this file**, inventory every violation with `file:line` + the rule cited, and
write a scoped cleanup report.

**Why this exists here specifically.** ting has no linter, no type checker, no CI, and ten
standalone bash scripts — a player, a TUI, three engine pairs, and two durable stores
(`t-playlist`, `t-history`) — written to a frozen bash 3.2 floor. Nothing but judgment
defends its invariants, and three of them erode silently: logic creeping *up* into the TUI, a
bash-4 idiom slipping in (it runs fine on the author's shell and aborts under `/bin/bash`), and
`docs/ARCHITECTURE.md` drifting away from the code it claims to describe. Those are the accretion
classes this skill owns.

**This skill is self-contained.** Every rule is defined below. It cross-references `CLAUDE.md`
for two project facts it must honor — the bash 3.2 floor and the **functional-only** testing
mandate — and both are restated inline where they matter.

**Hard doctrine — read before producing output:**
- **Never propose a guard test, fitness function, or "conformance test."** `CLAUDE.md` mandates
  functional checks only: every check invokes a real entry point the way a caller does and
  asserts on its exit code and stdout. There is no rig layer, no screen model, no pty harness
  and no unit tier. A structural test that
  greps the source for a forbidden idiom passes against a gutted script and is itself a
  violation. Eliminating a class structurally — move the shared helper down into the player or
  the engine so the duplicate cannot exist — beats detecting it. The pre-commit hook already gates the two
  classes that *are* mechanically decidable (secrets, bash-4 idioms); do not propose a second
  detector for them.
- **Ground every finding in a read.** Each row cites `file:line` and the exact rule (Rn). A
  grep hit is a candidate, never a finding. No "looks like" rows.
- **Verify contract claims by running the command**, not by reading the emitter. R8 findings in
  particular (single-line JSON, exit codes) are cheap to execute and easy to mis-read.
- **This skill does not edit code.** It produces an inventory + a cleanup report.

**Produces:** `docs/PLAN-conformance-YYYY-MM-DD.md` + a terminal summary. Scratch scripts go in
`tmp/` (create it if absent) — never the repo root, never `tests/`.

---

## The rule set (complete, self-contained)

| # | Rule | What it means | Primary detector |
|---|------|---------------|------------------|
| R1 | **One-sided variable / flag** | A variable, flag, or env knob with only a write site or only a read site. A parsed flag that nothing consumes, an exported `YT_*` nothing reads, a state var set and never tested. | grep the bare name **and** the `${…}` form **and** `"$name"`; one side missing = candidate. Beware indirection: a var read only inside a `jq --arg`, a heredoc, or an `eval`-shaped string won't show in a bare-word grep — that is this rule's #1 false positive. #2 is arithmetic context: `((VAR == 0))` reads VAR with no `$`, so a `$`-anchored read-scan calls it write-only. |
| R2 | **Redundant same-lifecycle state** | Two variables always assigned together and cleared together (e.g. a `CURRENT_PLAY_*` pair, a "have we drawn" flag beside the value it guards) — one concept wearing two names. | read the mutation sites; look for co-set / co-cleared pairs. |
| R3 | **Logic in the wrong surface (layer back-edge)** | Three hard layering rules now. (a) **`ting` contains ZERO site logic and ZERO playback logic** — it shapes argv for `yt-search` / `t-play` and delegates; a `yt-dlp` or `mpv` invocation or an IPC command construction there is a back-edge (reading the mpv socket through the *documented* envelope field is not). (b) **player and engine do not trade knowledge**: a `yt-dlp` call, a cookie decision, a format string, or a URL pattern in `shell/t-play` is a back-edge, and so is an `mpv` call or any player-state / `players/` write in ANY `*-search` / `*-resolve`. (c) **the stores know no site and no playback**: `t-playlist` / `t-history` hold `{engine, url, …}` records and jq — a yt-dlp call, an mpv call, a host pattern, or a `players/` touch in either is a back-edge. | `grep -n 'yt-dlp\|cookies-from-browser' shell/t-play shell/ting`, `grep -n 'mpv \|--input-ipc-server\|players/' shell/*-search shell/*-resolve`, and `grep -n 'yt-dlp\|mpv \|--input-ipc-server\|players/' shell/t-playlist shell/t-history` must all be empty apart from dependency-check strings and comments — read each hit. |
| R4 | **Duplication (DRY)** | Three carve-outs first, all deliberate: (a) each script must run standalone, so `die` / `print_usage` / `require_cmd` living in more than one file is **not** a finding; (b) the engine halves each holding their own cookie block and jq prelude is **not** a finding either — nor is one engine pair duplicating another's — they are separate executables and the alternative is a shared library the split exists to avoid (a duplicate spanning *player* and *engine*, however, IS a finding: that is a boundary leak); (c) the player's IPC property reader and `ting`'s are intentionally separate and must not call each other — the TUI's is fire-and-forget, the player's confirms delivery and exits 4 (`docs/ARCH-player.md`「运行时 IPC」). Otherwise: the same logic in ≥2 homes: a second duration formatter beside the engine's `JQ_PRELUDE` `fmt_dur`, a re-implemented width/cell measurement, a copied jq filter, the same validation in two places. The governing principle is that correctness is added *down* — in the **player** if it is about playback, in the **engine** if it is about a site — so every surface inherits it; a fix in `ting` that `t-play` could have made is a bug in the wrong file. **And the carve-outs cut both ways: where permitted near-twins legitimately DIFFER, the difference must be justified in the code** — five call sites of one lock helper where four say `\|\| true` and one is bare, with no comment saying which is intended, is indistinguishable from a drift bug, and that IS a finding even though the duplication itself is sanctioned. | the function graph (Pass 0) for same-named or near-identical bodies across files; grep for duplicated jq programs and `printf` format strings. |
| R5 | **bash 3.2 violation** | `declare -A`, `mapfile`/`readarray`, `${var,,}`/`${var^^}`, `${arr[-1]}`, `&>>`, `\|&`, `${!prefix@}`; an unguarded `"${arr[@]}"` on a possibly-empty array under `set -u`; a bare `((n += w))` **as a statement** under `set -e`; treating `read -rsn1` as one character rather than one byte; `LC_ALL=C [[ … ]]` (not valid bash at all). | the forbidden-idiom greps below, then **read** each array expansion and each `((…))` to classify statement vs test. The pre-commit hook blocks these on *added* lines; this rule sweeps what predates the hook. |
| R6 | **Swallowed error** | `\|\| true`, `2>/dev/null`, or an empty branch on a path where the user must see the failure — a real fault rendered as an empty list, a `0`, or a blank field. A *deliberate* best-effort degrade is fine **if** it degrades visibly (`--:--`, `n/a`, `LIVE`) and never as a fake value. | `grep -n '|| true\|2>/dev/null' shell/*` then read every hit and ask what the user sees when it fires. The sweep returns ~150 hits and nearly all are three sanctioned shapes — `rm -f`/`rmdir` cleanup, `chmod` best-effort hardening, `kill` in teardown — so triage those on sight and spend the reading on the residue: a swallowed error on a **jq parse**, a **state write**, or a **lock** is where this rule's real findings live. |
| R7 | **Optimistic state** | State written before the operation it asserts has committed: a player record or a "playing" flag persisted before mpv is confirmed launched, a lock recorded before it is held, `TTY_ECHO_OFF=1` set before `stty` succeeded. A failure mid-op then leaves a lying record. | read the order of the write vs the op, in `detach_play`, the lock helpers, and the echo/cursor traps. |
| R8 | **Contract drift** | The frozen surface (`CLAUDE.md`): `-j` emits **one line**; the envelope's field names; the exit-code taxonomy (0 ok · 1 usage/validation · 2+ propagated tool failure · 4 didn't take effect); lifecycle semantics (idempotent stop, ambiguity → 4). Any deviation, in either direction — code that violates the doc, or a doc that overstates the code. | **run the command, never read the emitter** — Pass 0 step 5: `tests/contract.sh --offline` first, one-off invocations only for what it doesn't cover. Compare against `docs/ARCH-cli-contract.md`「数据契约」与「退出码」. |
| R9 | **Naming drift** | Env knobs outside the prefix convention (every knob is `TING_*`: a suite-wide knob is `TING_<KEY>` and an engine's own tuning is `TING_<ENGINE>_<KEY>`; old names live only in `CFG_RENAMED`; a bare-name knob, or an internal variable wearing the `TING_` prefix it does not honor, is drift); a unit-less numeric where the codebase suffixes (`_s`, `_ms`, `_pct`); a deprecated short alias (`yts`/`ytp`) used anywhere at all, or a retired TUI name (`ytt`, `yt-tui`, `ut-tui`) used where the canonical `ting` belongs (one name per command — `CLAUDE.md`); a new envelope field whose name doesn't match its siblings' style. | grep the env-read sites against ARCH-cli-contract.md「配置面」's documented list; scan user-visible strings for the wrong name form. |
| R10 | **Dead code** | A function with zero call sites (across every script in `shell/`, the two suites, and tests/drive.sh); a `case` arm for a flag no usage text mentions and nothing emits; an env knob read nowhere; a code path reachable only through a removed flag. | the function graph (Pass 0): defs minus call sites. Confirm by reading — a function called only from a heredoc or a `trap` string looks dead to a grep. |
| R11 | **Doc drift** | Each fact lives in exactly one place. The durable docs hold **why and how, never what** (`CLAUDE.md` hard rule 2) — the what is stated by the source, `usage()` and the suites — so drift now has two shapes: a doc contradicting the code (the code is right; fix the doc), and a doc **restating** what the source already states (a violation even when currently accurate — it will drift). Docs carry no section numbers: cite by filename + heading name, resolved by grep; a citation naming a heading that no longer exists is drift. A doc citing a **scratch** check by path (a `tmp/` path is a promise the checkout can't keep; the three committed `tests/` files — two suites and the TUI driver — are the stated exception and SHOULD be named), or a fact restated in the README *and* a doc so the two can disagree. | diff the env-knob read sites (**all prefixes** — `YT_*`, `UT_*`, and each engine's own, e.g. `BILI_*`) against ARCH-cli-contract.md「配置面」; diff observed exit codes against ARCH-cli-contract.md「退出码」; grep `docs/*.md` for `tmp/` paths (a `tests/` path is correct, not a finding); grep every `「…」` citation for a heading that still resolves. |
| R12 | **Terminal-ownership violation (ting only)** | The TUI owns the whole screen: every drawn line goes through the measured-width layer (`char_w`/`wrap_print` and friends) so a CJK or math-bold glyph is counted in cells; echo and the cursor are owned for the session and restored from the **same** trap; a redraw is a whole frame, never a partial that leaves a stale row. A raw `printf`/`echo` of variable-width content, or a `stty` restore that isn't in the trap, is a violation. | grep `printf\|echo` in `shell/ting` for lines carrying interpolated title/channel text; read the trap. |
| R13 | **Stale prose defending a retired mechanism** | Code comments or doc paragraphs whose subject no longer exists: a rationale for keeping a variable nothing reads, a "why we do X" for an X that was since removed, a workaround note for a path that is gone. R10 catches dead *functions* and R11 catches doc-vs-code *fact* drift; this rule catches the **residue that argues for its own retention** — it reads as deliberate, so every later audit re-litigates it (a variable kept "because a stale read is harmless" when there is no read left to be stale is the canonical case). The repo's decision-narrative comments ("Rejected: X because Y", measured trade-offs) are NOT this rule's target — those document a live decision; this rule fires only when the *subject* of the prose is gone. | for each R1/R10 candidate that turns out deliberate, read the prose defending it and ask whether its stated reason still has a referent; grep retired symbol names (from git log / the defect register) across `shell/` comments and `docs/`. |

### The suite's layer order (for R3 / R4)

```
  primitives     yt-dlp · curl (engines only)   ·   mpv · nc (player only)   ·   jq (all)
        ↑                                            ↑
  engine pairs   shell/yt-search · shell/yt-resolve     shell/t-play      (player)
                 shell/bili-search · shell/bili-resolve
                 shell/ne-search · shell/ne-resolve
                 (every site-specific fact)           (playback + lifecycle; asks an
        ↑                                              engine BY NAME, never a site;
        │                                              writes the listening row by
        │                                              calling t-history by name)
        │        shell/t-playlist · shell/t-history  (durable stores: jq only —
        │                                              no site, no playback, no players/)
        └───────────────┬──────────────────────────────────┘
                        ↑
  human surface  shell/ting                    (orchestration only; calls the verbs)
```

Every arrow points **up**. A violation is any downward reach that skips a layer: the TUI
touching a primitive, an engine writing player state, a store learning a host pattern or an
mpv flag, the player or an engine knowing anything about the TUI. The player may not read a
`YT_TUI_*`-shaped knob; the TUI may not construct yt-dlp argv. **There are no wrappers left
to hide a decision in** — the ten scripts are peers, so a misplaced fact is always in one
of ten files.

---

## Pass 0 — Scope, function graph, cheap sweeps

1. **Resolve scope** from `$ARGUMENTS` (default all of `shell/`). State it in the summary. For
   a narrow scope (say `shell/ting`) still build the whole graph, so cross-file edges into
   the scope stay visible.

2. **Pick up any open report:** `ls docs/PLAN-conformance-*.md`. If a recent one is
   unaddressed, read it and fold new findings in — do not re-list tracked violations as new.

3. **Build the function graph.** `fn_graph.py` (next to this file) lists every function
   with its definition site(s) and its call sites across every script in `shell/` (globbed,
   so a ninth script is covered the day it lands):

   ```bash
   mkdir -p tmp
   python3 .claude/skills/audit-conformance/fn_graph.py > tmp/fns.txt
   awk -F'\t' '$4 ~ /DEAD/' tmp/fns.txt      # R10 candidates
   awk -F'\t' '$4 ~ /DUP/'  tmp/fns.txt      # R4 candidates — same name in two files
   ```

   Both columns are **candidates**. A function invoked from a `trap` string, a heredoc, or a
   name assembled at runtime reads as DEAD; confirm every one by reading before proposing a
   delete. The script is a manual aid and must never become a gate or a `tests/` member —
   it over-flags by construction, which is precisely what a structural test must not do.

4. **Cheap sweeps** — each one seeds candidates that Pass 1 confirms by reading:

   ```bash
   # R3 back-edges: these MUST be empty apart from dep-check strings and comments
   grep -n 'yt-dlp\|mpv \|--input-ipc-server' shell/ting
   grep -n 'yt-dlp\|cookies-from-browser' shell/t-play
   grep -n 'mpv \|--input-ipc-server\|players/' shell/*-search shell/*-resolve
   grep -n 'yt-dlp\|mpv \|--input-ipc-server\|players/' shell/t-playlist shell/t-history

   # R5 bash-4 leaks (the hook gates added lines; this catches what predates it)
   grep -nE 'declare -A|mapfile|readarray|\$\{[A-Za-z_][A-Za-z0-9_]*(,,|\^\^)|&>>|\|&|\[-1\]' shell/*
   grep -nE '^\s*\(\([^)]*[-+*/]?=' shell/*        # (( )) as a STATEMENT under set -e — read each
   grep -n '"\${[A-Za-z_][A-Za-z0-9_]*\[@\]}"' shell/*   # unguarded empty-array expansion

   # R6 swallowed errors
   grep -n '|| true\|2>/dev/null' shell/*

   # R9/R11 config surface: every env-knob read site, ALL prefixes (an engine's own
   # prefix included — BILI_* is where an undocumented family actually hid), to diff
   # against ARCH-cli-contract.md「配置面」
   grep -ohE '\b(YT|UT|BILI)_[A-Z0-9_]+' shell/* | sort -u

   # R11: no doc may cite a SCRATCH check by path (a tests/ path is correct — the two
   # suites and the driver are the stated exception).
   grep -rn 'tmp/' docs/*.md
   ```

5. **Run the contract, don't read it** (R8). The fixed command sequence for this class already
   lives where fixed sequences belong — `tests/contract.sh` — so run it, don't restate it:

   ```bash
   tests/contract.sh --offline        # gates, exit codes, envelopes, both stores; ~16s, hermetic
   ```

   Its PASS/FAIL lines are the R8 evidence for everything it drives. A contract claim the suite
   does **not** cover (read its docstring and the checks it prints) is settled by one-off
   invocation of the real command — exit code and stdout captured as the finding's evidence —
   and if the gap is real, the *fix* is a check landed in `contract.sh`, per CLAUDE.md's
   harden-before-you-extend rule; this skill only names the gap. Store commands always run
   under a disposable `TING_STATE_DIR=$(mktemp -d)`, never the user's real one.

   Do not start a detached player as part of an audit — that is audible playback on someone's
   machine. Playback claims belong to `tests/playback.sh`, which runs its
   players in a state dir of its own and is run deliberately.

---

## Pass 1 — Rule-class audit

Read-only. The suite is ten files; a single careful sweep by the orchestrator is usually
right. For a full periodic audit, fan out to read-only subagents (`Read, Grep, Bash`; no
Edit/Write) grouped by rule cluster so each holds one mental model:

- **A — layering & surfaces:** R3, R4, R12.
- **B — subtraction:** R1, R2, R10, R13.
- **C — portability & failure:** R5, R6, R7.
- **D — contract & docs:** R8, R9, R11.

Each pass returns **only** a table — no narrative, no fixes:

| rule | file:line | what (the offending symbol / line) | evidence (a read or a command's output) | proposed source fix |
|------|-----------|-------------------------------------|------------------------------------------|---------------------|

R1/R10 systematically **over-flag** (indirect reads, heredocs, trap strings). R2/R4/R12 **under-flag**
(judgment-heavy). R8 is the one class where the evidence must be a command's actual output.
Treat every DELETE proposal as a candidate to re-confirm.

---

## Pass 2 — Dedup, prioritize, write the report

The orchestrator does this, so source-verification stays in one place.

1. **Merge + dedup.** One physical violation = one row, even when it trips two rules (note
   both). **Dead (R10) dominates:** before proposing a lifecycle fix for an R6/R7 row, count
   the callers — zero reclassifies it to R10 and deletion supersedes the remedy.

2. **Re-verify every row against source, and blind-re-read every DELETE.** Re-read the cited
   symbol with the verdict set aside, hunting for any caller, trap, heredoc, or test reference
   that disqualifies "dead". Only survivors enter the report.

3. **Prioritize.**
   - **Recurrence class first.** A class that recurs (three back-edges, four duplicated
     formatters) beats any single severe row: fixing it structurally prevents the family.
     Cross-check `git log` for the same fix repeating.
   - **Then severity:** R3/R4 layering and R8 contract drift over R9 naming.
   - **Cap the action plan at one coherent theme.** Everything else goes to a
     `## Deferred backlog` section with counts. An unscoped 40-item plan never ships.

4. **Write `docs/PLAN-conformance-YYYY-MM-DD.md`** from the template below. Each task names
   the rule + `file:line`, states a **structural** fix (move down into the player or engine / collapse /
   delete / rename), and gives a `done_when` that is observable — *"`shell/yt-search -j` emits
   one line and `grep yt-dlp shell/ting` is empty"* — **never** "a guard test passes".

5. **If a deletion would orphan a helper**, chain it into the same task.

6. **A doc-drift row (R11) is fixed in the doc, not in the code** — unless the code is what is
   wrong, in which case say which one you are proposing to move and why.

---

## Report template

```markdown
# PLAN — conformance audit (ting) · <date>

Scope: <path>   ·   Functions graphed: N   ·   Prior report folded: <file|none>

## Inventory (read-confirmed)

| rule | file:line | what | fix |
|------|-----------|------|-----|
| R3 | shell/ting:NNN | … | move … down into shell/t-play |

## This round — <one coherent theme>

- [ ] **<rule> <file:line>** — <structural fix>. done_when: <observable>.

## Deferred backlog

- R9 naming drift: N · R10 dead code: N · …
```

---

## Terminal summary

No PASS/FAIL — the output is an inventory plus a report to act on.

```
## audit-conformance — <date>

Scope: <path>            Functions graphed: N
Violations: N read-confirmed (M grep candidates dropped on read)
  R1 one-sided var:            N
  R2 redundant state:          N
  R3 wrong surface / back-edge:N   ← [list]
  R4 duplication:              N   ← [list]
  R5 bash 3.2 violation:       N
  R6 swallowed error:          N
  R7 optimistic state:         N
  R8 contract drift:           N   ← [command + observed vs documented]
  R9 naming drift:             N
  R10 dead code:               N
  R11 doc drift:               N
  R12 terminal ownership:      N
  R13 stale prose:             N

Recurring classes (git-log corroborated): [class — count]
Plan this round: <theme> — K tasks
Deferred: N
Report: docs/PLAN-conformance-<date>.md
```

**Cadence:** periodic, not per-commit — the residue accumulates between runs by design. Good
triggers: `git log` shows the same cleanup repeating, before making the repo public, before a
`UT_VERSION` bump, or after any change that touched two surfaces at once.
