---
name: clean-tests
description: Prune, consolidate, and harden ting's test suites against CLAUDE.md functional boundary rules — eliminating mock/fixture sprawl, bare sleeps, weak assertions, dead compat checks, and isolation leaks. Callable any time.
argument-hint: "[scope, default tests/]"
---

# clean-tests

**Invocation:** `/clean-tests [path]` (default scope: `tests/` and `internal/`)

**First Principle & Mission:** Tests exist solely to prove that components meet their **contracts** across **functional boundaries** (CLI exit codes `0/1/2/4`, JSON envelope schemas, Unix socket IPC) and **delegation chains** under real conditions (real `mpv`, real `nc`, real `curl`, real `jq`, real `yt-dlp`). Every surviving test must catch a real regression in ting's system functionality or agent behavior. Everything else is noise.

**The rules are `CLAUDE.md` (Engineering Rules)** — enforced repository doctrine, the single source of truth. Read it first. This skill is the *operational procedure* for simplifying, consolidating, and hardening an existing suite; where the two overlap, `CLAUDE.md` wins.

---

## The Three Pillars in ting: Boundaries, Delegations, Contracts

KISS governs test suite health: a lean suite verifying public boundary contracts with real dependencies catches more regressions and incurs zero phantom maintenance compared to bloated suites of mocked micro-tests.

```
       [ Upstream / Human / Agent Caller ]
                        │
   ═════════════════════▼═════════════════════  1. Functional Boundary
       [ Public Interface: ting-play / ting ]      (Test public entry points; isolate environment;
                        │                        zero backward-compat shims; strict exit codes)
                        ▼  2. Delegations
       [ Real Collaborators: mpv / IPC / IO ]   (Real mpv, real nc, real SQLite/JSONL, real curl;
                        │                        zero mock/fake; boundary data via real writers;
                        │                        bounded signal polling, zero guessing sleep)
   ═════════════════════▼═════════════════════  3. Contract & Invariants
       [ Observable Outcome: Envelopes & RC ]   (Exact exit codes 0/1/2/4, exact reason enums;
                                                 falsifiable: must fail when contract is broken)
```

### Target Test Hierarchy (三层测试金字塔)

To prevent boilerplate inflation and over-engineering, every test in `ting` must live at its proper layer:

```
                      ▲
                     / \     L3. Scenario Flows (~15–20 across repo)
                    /   \    Full playback lifecycle under tests/playback.sh,
                   /-----\   real tmux TUI key & frame flows under tests/contract.sh.
                  /       \   L2. Component Boundary Contracts (~80–120 across repo)
                 /         \  Public CLI entry point (ting-play, ting-playlist, ting-history),
                /-----------\ 4-tier exit codes, JSON envelopes, socket IPC mutations.
               /             \ L1. Pure Logic & Algorithms (~30–50 across repo)
              /               \Pure functions, table-driven tests (internal/config/config_test.go),
             -------------------argument parsing and format conversions without process orchestration.
```

1. **Functional Boundaries (功能边界 —— 测对外接口，不测内部装配)**
   - **Public contract entry points**: Test components at their defined boundary (`docs/ARCH-cli-contract.md`). Drive public actions through `ting-play`, not by calling internal engine scripts (`shell/ting-engine-*`) directly for public verbs.
   - **Clean environment boundary**: All filesystem writes strictly under disposable `TMPDIR` / scratch `TING_STATE_DIR`. Verify real `~/.config` and `~/.local/state` directories remain untouched via checksums. Never run unisolated TUI automation.
   - **Distinct domain boundary**: Test `ting` domain contracts, not third-party tool semantics (`jq`, `curl`, `awk`). Zero backward-compat shims for deleted flags or expired migration names.

2. **Delegation Integrity (委托协同 —— 测真实联动，不测隔离幻觉)**
   - **Real dependencies only**: Real `mpv`, real `nc -U`, real `curl`, real `jq`, real `yt-dlp`. Zero fixture / zero mock / zero stub. Dead-proxy isolation (`http_proxy=$NOPROXY`) is used to test network failures, never synthetic fake tools on `PATH`.
   - **Boundary data format**: All serialized fixtures must be produced via real production commands (`ting-play --search -j`, `ting-playlist --show -j`, `ting-history --ls -j`), never hand-crafted JSON strings (`ENV_JSON`).
   - **Bounded async polling**: Every asynchronous wait is a bounded loop (`poll_until`) on a `0.05s` / `0.25s` tick polling real state signals (socket appearance, process exit via `kill -0`, frame ready marker, queue position change). Zero guessing `sleep`.

3. **Contracts & Invariants (契约与不变量 —— 测可证伪的确切结果，不测模糊形态)**
   - **Behavior over structure**: Assert observable contract outcomes (exact exit codes `0/1/2/4`, exact reason enum, exact state changes). Never assert loose inequalities (`!= 1`, `exit_code > 0`), mere field presence (`.id and .sock`), or truthy booleans.
   - **Falsifiability (当场验红)**: Every assertion must be verified to turn red when the contract is broken. Eliminate always-green traps (`set -o pipefail` swallowing grep errors, zero-denominator empty loops).
   - **Consolidation around scenarios**: Consolidate fragmented micro-tests into unified behavioral flows. Use **Graft-then-delete**: never drop a unique contract check when removing duplicate tests.

**Produces:** Terminal summary + findings report. **Verdict:** PASS / FAIL.

---

## The 12 Blocking Rules in ting (with Severities)

| # | Sev | Pillar | Rule | Description | Action |
|---|-----|--------|------|-------------|--------|
| 1 | 🔴 | **Delegation** | **Mock / patch / fake** | Any fake tools on `PATH`, wrapper scripts, or mock objects. | Reroute to real dependency (`mpv`, `nc`, `curl`, `jq`) or clean scratch environment |
| 2 | 🟢 | **Delegation** | **Fixture bypassing writer** | Hand-crafted JSON string literals (e.g. `ENV_JSON='{"status":"ok",...}'`) fed into stores or player queues. | Rebuild fixture by piping output from real production writer (`ting-play`, `ting-playlist`, `ting-history`) |
| 3 | 🟡 | **Boundary** | **Private boundary leak** | Calling `shell/ting-engine-*` directly for public verbs (`--search`, `--info`, `--items`, `--transcript`, `--auth`) instead of `ting-play`. | Reroute call through `shell/ting-play` public interface |
| 4 | 🔴 | **Boundary** | **Test isolation leak** | Writes to real `~/.config/ting`, `~/.local/state/ting` outside `TMPDIR`; unisolated `drive.sh` mutating user playlists. | Enforce scratch `TMPDIR`, `TING_CONFIG`, and `TING_STATE_DIR` on all test and driver runs |
| 5 | 🟢 | **Boundary** | **Backward-compat shim** | Testing that deleted flags stay removed (`-J`, `-l`, `-S`), or testing expired migration aliases (`YT_COOKIE_BROWSER`, any pre-`TING_` key name). | Delete negative checks per zero-compat policy |
| 6 | 🟡 | **Boundary** | **Library / tool behavior** | Tests third-party tool semantics (e.g. `jq` filter syntax, `curl` HTTP codes, `bash` features) rather than `ting` contracts. | Delete or refocus on `ting` domain contract |
| 7 | 🟡 | **Contract** | **Vacuous assertion** | Asserting exit code alone without checking stdout/stderr when multiple different errors share exit code 1 or 4. | Strengthen with `err_has` or envelope `.reason` check |
| 8 | 🔴 | **Contract** | **Always-green flaws** | Direct piping into `grep` under `set -o pipefail` where a failing command masks the grep result; empty engine loops. | Use `err_has` helper; enforce `[ "$NENG" -ge 2 ]` and count-based report |
| 9 | 🟡 | **Contract** | **Structural-only / weak** | Loose inequalities (`!= 1`, `.exit_code > 0`), mere field presence (`.id and .sock`), or `ok`/`bad` bypassing `<want> <got>`. | Strengthen to exact exit code (e.g. `2`), exact `.status == "started"`, and `report` diffs |
| 10 | 🟡 | **Delegation** | **Timeout discipline** | Bare fixed `sleep` (e.g. `sleep 0.6`, `sleep 0.5`) guessing timing instead of polling real signals. | Replace with bounded `poll_until` loop checking concrete signals |
| 11 | 🟡 | **Delegation** | **Config discipline** | Tests inheriting developer's exported `TING_*` environment variables, altering execution silently. | Sweep and unset `compgen -v | grep '^TING_'` in test suite prelude |
| 12 | 🟢 | **Contract** | **Subsumed & fragmented** | Spawning separate subshells for 10 identical scalar invalid inputs that hit the same regex/validator branch. | Consolidate into concise parameterized or scenario flow via Graft-then-delete |

> **Suppression Annotation:** A deliberate exception (e.g. background PID-holder process) must carry `# clean-tests: allow-<rule> (<reason>)` inline so the scanner skips it without false alerts.

---

## Pass 0 — Triage, Automated Scanning, and Smell Discovery

Run the automated scanner script:
```bash
python3 .claude/skills/clean-tests/scan_tests.py
```

Or perform targeted regex scans across `tests/`:

```bash
# 1. Bare fixed sleeps guessing time (Rule 10)
grep -rnE '^[[:space:]]*sleep[[:space:]]+[0-9]+' tests/

# 2. Weak inequalities and structural-only assertions (Rule 9)
grep -rnE '\.exit_code[[:space:]]*>[[:space:]]*0' tests/
grep -rnE '!= 1 \]\s*&&\s*echo 1' tests/
grep -rnE '^[[:space:]]*(ok|bad)[[:space:]]+"' tests/

# 3. Hand-crafted JSON fixtures bypassing writers (Rule 2)
grep -rn 'ENV_JSON=' tests/

# 4. Private boundary leaks (Rule 3)
grep -rnE 'shell/ting-engine-\w+[[:space:]]+--(search|info|items|transcript|auth)' tests/

# 5. Backward-compat shims and dead aliases (Rule 5)
grep -rnE '(YT_COOKIE_BROWSER|BILI_COOKIE_BROWSER|NE_COOKIE_BROWSER)' tests/

# 6. Test isolation leaks (Rule 4)
grep -rn 'TING_STATE_DIR is deliberately NOT redirected' tests/
```

---

## Pass 1 — Targeted Audit & Evaluation

Evaluate suspicious tests against the **Deletion Test** and **Same-Branch Proof** before flagging any rule:

### Deletion test — apply before flagging any rule
> *If deleted, will a real regression in system functionality, delegation, or contract go undetected by every remaining test?*
- **Yes** → keep, naming the concrete failure mode ("guards 4-tier failure taxonomy on network drops", "prevents orphan mpv leak on SIGINT").
- **No** → candidate for consolidation or deletion.

### Same-branch proof — required before merging or deleting tests
Subsumption is a claim about the **production code path**, not just test similarity:
- One regex / dispatch handling several inputs through a single branch = same branch → candidate for deduplication and consolidation.
- A function that branches on its input (mode dispatch, type switch, error branches) = distinct paths → keep both.
- Separate engines (`ting-engine-yt`, `ting-engine-bili`, `ting-engine-ne`) with independent implementations are distinct branches → keep cross-engine matrix.
- An offline gate test does not subsume a live network resolve test.

---

## Pass 2 — Transformation Recipes (Before vs After)

### Recipe A: Replace bare sleep with bounded signal polling (Rule 10)
*Before:*
```bash
tmux send-keys -t "$PS_TS" Escape 2>/dev/null
sleep 0.6
report "Esc closes the prompt inside 600ms" 1 \
    "$(tmux capture-pane -t "$PS_TS" -p -J 2>/dev/null | grep -c '__GONE__' | awk '{print ($1 > 0) ? 1 : 0}')"
```
*After:*
```bash
tmux send-keys -t "$PS_TS" Escape 2>/dev/null
prompt_closed() { tmux capture-pane -t "$PS_TS" -p -J 2>/dev/null | grep -q '__GONE__'; }
report "Esc closes the prompt" 1 "$(poll_until 12 prompt_closed)"  # 12 ticks * 0.05s = 600ms bound
```

### Recipe B: Strengthen loose inequality to exact error code (Rule 9)
*Before:*
```bash
report "ting-engine-yt still takes youtu.be" 1 \
 "$([ "$(http_proxy=$NOPROXY https_proxy=$NOPROXY rc shell/ting-engine-yt --stream -j -- https://youtu.be/$MEDIA_ID)" != 1 ] && echo 1 || echo 0)"
```
*After (under dead proxy, transport/network failure is exactly exit code 2 in ting's 4-tier taxonomy):*
```bash
report "ting-engine-yt still takes youtu.be" 2 \
    "$(http_proxy=$NOPROXY https_proxy=$NOPROXY rc shell/ting-play --engine yt --stream -j -- https://youtu.be/$MEDIA_ID)"
```

### Recipe C: Strengthen death record exit code (Rule 9)
*Before:*
```bash
report "death records non-zero exit code" 0 \
 "$(shell/ting-play --status -j | jq -e --arg i "$f_id" '.failed[]|select(.id==$i)|.exit_code > 0' >/dev/null 2>&1; echo $?)"
```
*After:*
```bash
report "death records external tool failure (exit 2)" 2 \
 "$(shell/ting-play --status -j | jq -r --arg i "$f_id" '.failed[]|select(.id==$i)|.exit_code // empty')"
```

### Recipe D: Unify reporting helper (Rule 9)
*Before:*
```bash
if [ -n "$pos" ]; then ok "position came off the socket (${pos}s)"
else bad "player 1 never reported a position"; fi
```
*After:*
```bash
report "position came off the socket" true \
    "$([ -n "$pos" ] && awk -v p="$pos" 'BEGIN{exit !(p > 0)}' && echo true || echo false)"
```

---

## Pass 3 — Verify & Quality Gate

Run all verification gates to ensure zero regressions:

1. **Syntax & Portability Check**:
   ```bash
   bash -n shell/*
   ```

2. **Hermetic Contract Suite**:
   ```bash
   tests/contract.sh --offline
   ```

3. **Go Unit & Boundary Suite**:
   ```bash
   go test ./...
   ```

4. **Verify Host State Integrity**:
   Confirm that `tests/contract.sh` reports:
   - `your own config is untouched`
   - `your own store is untouched`

5. **Assertion Falsifiability (当场验红)**:
   For every newly strengthened assertion, verify that introducing an intentional bug (e.g. changing exit code or returned status) causes the test to fail red.

---

## Output Summary Template

```markdown
## clean-tests Summary

- **Files scanned**: N
- **Violations found**: N (🔴 Blocker: N, 🟡 Warning: N, 🟢 Info: N)
- **Tests consolidated / merged**: N
- **Tests deleted (dead compat / duplicate)**: N
- **Assertions strengthened & verified red**: N
- **Bare sleeps converted to bounded polls**: N
- **State isolation audit**: PASS (host config & state untouched)
- **Verification status**:
  - bash -n shell/*: PASS
  - tests/contract.sh --offline: N ok, 0 failed
  - go test ./...: PASS
  - tests/playback.sh (if applicable): N ok, 0 failed

**Verdict**: PASS / FAIL
```
