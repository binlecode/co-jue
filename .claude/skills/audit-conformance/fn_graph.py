#!/usr/bin/env python3
"""Function graph for the ting shell suite — a manual audit aid.

NOT a gate and NOT a tests/ member: it produces candidates for a human/agent to confirm
by reading. Structural detectors are forbidden as tests in this repo (CLAUDE.md), and this
script is exactly why — it over-flags by construction.

Usage:
    python3 .claude/skills/audit-conformance/fn_graph.py > tmp/fns.txt
    awk -F'\t' '$4 ~ /DEAD/' tmp/fns.txt     # R10 candidates
    awk -F'\t' '$4 ~ /DUP/'  tmp/fns.txt     # R4 candidates (same name, two files)

Columns:  name  defined-at  callers=N  flags  first-call-sites

Known blind spots, all of which read as DEAD when they are not: a function invoked from a
`trap` string, from inside a heredoc, or through a name assembled at runtime. Confirm every
DEAD row by reading before proposing a delete.
"""

import re
import sys
import pathlib

# Globbed, not listed — the same rule .githooks/pre-push follows. A hardcoded list here
# made ting-playlist and ting-history invisible to R10/R4 for two whole features.
FILES = ([pathlib.Path(p) for p in sys.argv[1:]] or
         sorted(p for p in pathlib.Path("shell").iterdir() if p.is_file()))
DEF = re.compile(r'^\s*(?:function\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(\)\s*\{')

defs = {}
lines = {}
for f in FILES:
    src = f.read_text().splitlines()
    lines[f] = src
    for i, ln in enumerate(src, 1):
        m = DEF.match(ln)
        if m:
            defs.setdefault(m.group(1), []).append(f"{f}:{i}")

for name in sorted(defs):
    homes = defs[name]
    callers = []
    for f, src in lines.items():
        for i, ln in enumerate(src, 1):
            if f"{f}:{i}" in homes:
                continue
            code = ln.split("#", 1)[0]          # ignore comments
            if re.search(rf'(^|[\s;&|(`$])({re.escape(name)})(\s|$|;|\)|`|"|\|)', code):
                callers.append(f"{f}:{i}")
    flags = ("DUP" if len(homes) > 1 else "") + ("DEAD" if not callers else "")
    print(f"{name}\t{','.join(homes)}\tcallers={len(callers)}\t{flags}\t{';'.join(callers[:4])}")
