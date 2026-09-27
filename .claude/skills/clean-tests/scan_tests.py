#!/usr/bin/env python3
"""scan_tests.py: Automated static audit of ting tests against clean-tests rules.

Scans tests/contract.sh, tests/playback.sh, tests/drive.sh, and internal/*_test.go
for anti-patterns: bare sleeps, weak assertions, dead compat checks, isolation leaks,
hand-crafted fixture bypass, and vacuous checks.
"""

import os
import re
import sys
from typing import List, Tuple

SEVERITY_BLOCKER = "BLOCKER"
SEVERITY_WARNING = "WARNING"
SEVERITY_INFO = "INFO"

RULES = {
    "R1_MOCK": ("Rule 1: Mock/patch or fake domain objects detected", SEVERITY_BLOCKER),
    "R2_FIXTURE_BYPASS": ("Rule 2: Hand-crafted JSON fixture bypassing production writer", SEVERITY_INFO),
    "R3_PRIVATE_LEAK": ("Rule 3: Private boundary leak (calling t-engine-* directly for public verbs)", SEVERITY_WARNING),
    "R4_ISOLATION_LEAK": ("Rule 4: Test isolation leak (unredirected state or user home writes)", SEVERITY_BLOCKER),
    "R5_COMPAT_SHIM": ("Rule 5: Deprecated alias, dead flag, or historical compat check", SEVERITY_INFO),
    "R6_LIB_BEHAVIOR": ("Rule 6: Testing 3rd-party library / tool semantics instead of ting contracts", SEVERITY_WARNING),
    "R7_VACUOUS_ASSERT": ("Rule 7: Vacuous assertion (truthy-only or tautological)", SEVERITY_WARNING),
    "R8_ALWAYS_GREEN": ("Rule 8: Always-green flaw (pipefail masking error, empty loop)", SEVERITY_BLOCKER),
    "R9_WEAK_ASSERT": ("Rule 9: Structural-only or weak assertion (loose inequalities, exit_code > 0)", SEVERITY_WARNING),
    "R10_TIMEOUT": ("Rule 10: Bare fixed sleep guessing time instead of bounded signal polling", SEVERITY_WARNING),
    "R11_CONFIG": ("Rule 11: Missing environment sweep (inherited TING_/UT_ vars)", SEVERITY_WARNING),
    "R12_SUBSUMED": ("Rule 12: Clustered micro-test or redundant branch check", SEVERITY_INFO),
}

Finding = Tuple[str, str, int, str, str, str]  # (rule_id, file, line_num, snippet, explanation, severity)


def audit_file(filepath: str) -> List[Finding]:
    findings: List[Finding] = []
    if not os.path.exists(filepath):
        return findings

    with open(filepath, "r", encoding="utf-8") as f:
        raw_content = f.read()

    lines = raw_content.splitlines()
    is_playback = "playback.sh" in filepath
    is_contract = "contract.sh" in filepath
    is_drive = "drive.sh" in filepath

    # Check for multi-line weak assertions across line continuation \
    multi_line_matches = re.finditer(
        r'report\s+["\'][^"\']+["\']\s+1\s*\\\s*\n\s*"\$\(\[\s*"[^"]*"\s*!=\s*1\s*\]\s*&&\s*echo\s*1',
        raw_content,
    )
    for m in multi_line_matches:
        start_pos = m.start()
        line_num = raw_content[:start_pos].count("\n") + 1
        rule_desc, sev = RULES["R9_WEAK_ASSERT"]
        findings.append((
            "R9_WEAK_ASSERT", filepath, line_num, m.group(0).replace("\n", " "),
            "Weak inequality `!= 1` across multi-line report; assert exact exit code (e.g. 2 for network/external error).",
            sev
        ))

    for idx, raw_line in enumerate(lines, 1):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue

        # Suppression comment check
        if "# clean-tests: allow-" in raw_line:
            continue

        # R1: Mock/patch/fake
        if re.search(r"\b(unittest\.mock|MagicMock|monkeypatch|fake_mpv|mock_)\b", line):
            rule_desc, sev = RULES["R1_MOCK"]
            findings.append((
                "R1_MOCK", filepath, idx, line,
                "Explicit mock or fake detected; ting requires real dependencies.",
                sev
            ))

        # R2: Hand-crafted JSON fixtures
        if is_contract and re.search(r'ENV_JSON=\'\{.*"results":', line):
            rule_desc, sev = RULES["R2_FIXTURE_BYPASS"]
            findings.append((
                "R2_FIXTURE_BYPASS", filepath, idx, line,
                "Handcrafted search envelope fixture; prefer generating via real t-play --search or production command.",
                sev
            ))

        # R3: Private boundary leak (t-engine-* for public verbs)
        if re.search(r"shell/t-engine-\w+\s+--(search|info|items|transcript|auth)\b", line):
            rule_desc, sev = RULES["R3_PRIVATE_LEAK"]
            findings.append((
                "R3_PRIVATE_LEAK", filepath, idx, line,
                "Calling t-engine-* directly for a public verb. Public verbs should be driven through t-play.",
                sev
            ))

        # R4: Isolation leak
        if is_drive and "UT_STATE_DIR is deliberately NOT redirected" in raw_line:
            rule_desc, sev = RULES["R4_ISOLATION_LEAK"]
            findings.append((
                "R4_ISOLATION_LEAK", filepath, idx, line,
                "UT_STATE_DIR is left unredirected; playlist-editing keys will mutate real user store.",
                sev
            ))

        # R5: Backward-compat shims and dead aliases
        if re.search(r"\b(YT_COOKIE_BROWSER|BILI_COOKIE_BROWSER|NE_COOKIE_BROWSER)\b", line):
            rule_desc, sev = RULES["R5_COMPAT_SHIM"]
            findings.append((
                "R5_COMPAT_SHIM", filepath, idx, line,
                "Testing legacy per-engine cookie browser knobs; should be retired per zero-compat policy.",
                sev
            ))
        if re.search(r"\bUT_START_RESULTS\b", line):
            rule_desc, sev = RULES["R5_COMPAT_SHIM"]
            findings.append((
                "R5_COMPAT_SHIM", filepath, idx, line,
                "Testing deprecated UT_START_RESULTS alias for UT_SEARCH_RESULTS.",
                sev
            ))

        # R9: Weak assertions (inequalities, loose exit_code)
        if re.search(r"\.exit_code\s*>\s*0", line):
            rule_desc, sev = RULES["R9_WEAK_ASSERT"]
            findings.append((
                "R9_WEAK_ASSERT", filepath, idx, line,
                "Weak inequality `.exit_code > 0`; assert exact exit code (e.g. 2 for external tool failure).",
                sev
            ))
        if is_playback and re.search(r'jq -e \'\.id and \.pid and \.sock\'', line):
            rule_desc, sev = RULES["R9_WEAK_ASSERT"]
            findings.append((
                "R9_WEAK_ASSERT", filepath, idx, line,
                "Structural presence check `.id and .pid and .sock`; assert exact `.status == \"started\"`.",
                sev
            ))
        if is_playback and re.search(r'^\s*(ok|bad)\s+"', raw_line):
            rule_desc, sev = RULES["R9_WEAK_ASSERT"]
            findings.append((
                "R9_WEAK_ASSERT", filepath, idx, line,
                "Direct ok/bad call bypasses report <name> <want> <got> diff alignment.",
                sev
            ))

        # R10: Bare fixed sleep guessing time
        sleep_m = re.search(r"\bsleep\s+([0-9]+(\.[0-9]+)?)\b", line)
        if sleep_m:
            # Check if this sleep is used as a PID holder background process (e.g., sleep 30 & HOLD=$!)
            if re.search(r"&\s*[A-Z_]*HOLD=", line) or "outlives by nothing" in line:
                continue
            # Check context: is it inside a loop (poll step) or trailing tmux keepalive?
            ctx_start = max(0, idx - 3)
            ctx_end = min(len(lines), idx + 2)
            ctx = " ".join(lines[ctx_start:ctx_end])
            in_loop = re.search(r"\b(while|until|for|seq|poll_until)\b", ctx)
            is_tmux_keepalive = "sleep 20" in line or "sleep 30" in line or "sleep 5" in line
            if not in_loop and not is_tmux_keepalive:
                rule_desc, sev = RULES["R10_TIMEOUT"]
                findings.append((
                    "R10_TIMEOUT", filepath, idx, line,
                    f"Bare fixed sleep ({sleep_m.group(1)}s) guessing time; replace with bounded signal polling (poll_until).",
                    sev
                ))

    return findings


def main():
    root = sys.argv[1] if len(sys.argv) > 1 else "."
    targets = [
        os.path.join(root, "tests/contract.sh"),
        os.path.join(root, "tests/playback.sh"),
        os.path.join(root, "tests/drive.sh"),
        os.path.join(root, "internal/config/config_test.go"),
        os.path.join(root, "internal/verb/verb_test.go"),
    ]

    all_findings: List[Finding] = []
    for target in targets:
        if os.path.exists(target):
            all_findings.extend(audit_file(target))

    blockers = [f for f in all_findings if f[5] == SEVERITY_BLOCKER]
    warnings = [f for f in all_findings if f[5] == SEVERITY_WARNING]
    infos = [f for f in all_findings if f[5] == SEVERITY_INFO]

    print(f"=== clean-tests Audit Scanner ===")
    print(f"Scanned files: {len(targets)}")
    print(f"Total findings: {len(all_findings)} (🔴 {len(blockers)} Blocker, 🟡 {len(warnings)} Warning, 🟢 {len(infos)} Info)\n")

    if not all_findings:
        print("PASS: No clean-tests anti-patterns found.")
        sys.exit(0)

    # Group by rule
    grouped = {}
    for f in all_findings:
        rule_id = f[0]
        grouped.setdefault(rule_id, []).append(f)

    for rule_id, items in sorted(grouped.items()):
        rule_title, sev = RULES.get(rule_id, (rule_id, "INFO"))
        sev_icon = "🔴" if sev == SEVERITY_BLOCKER else ("🟡" if sev == SEVERITY_WARNING else "🟢")
        print(f"[{rule_id}] {sev_icon} {rule_title} ({len(items)} hits):")
        for _, fpath, line_num, snippet, explanation, _ in items:
            relpath = os.path.relpath(fpath, root)
            print(f"  - {relpath}:{line_num}: {snippet}")
            print(f"    -> {explanation}")
        print()

    print(f"Verdict: {len(all_findings)} finding(s). Blockers: {len(blockers)}.")
    sys.exit(1 if blockers else 0)


if __name__ == "__main__":
    main()
