---
name: capture-pane
description: Refresh the terminal frames in README.md / docs/ARCHITECTURE.md from REAL ting panes instead of hand-drawing them. Covers picking the geometry per view, waiting for the ready marker so the frame is not a spinner, cleaning the capture with clean_capture.py, proving it with this skill's assert_pane.py BEFORE it enters a doc, and splicing it in with a Python replace rather than hand-transcribing box glyphs. Use when a layout change has made a doc frame stale.
---

# capture-pane

Every terminal frame in this repo's docs must come from a real pane. A hand-drawn mockup
drifts from the code (invented labels, a rail that doesn't line up, a version that no longer
exists) and cannot reproduce the alignment that *is* the claim being made — the right-flush
duration rail, the shared title column, the CJK title that occupies two cells per character.

For session setup, keys, ready markers, and player cleanup, see `tests/drive.sh` — the driver
that already gets all four right. This one is capture → clean → **prove** → splice.

## 1. Pick the view and geometry

One capture = one tmux session at one declared size. Layout in this TUI is *width-conditional*
(the reflow pays for the details block out of the row budget, and the nav block drops entirely
when it doesn't fit), so a frame without its geometry stated is not a claim anyone can check.

| View | How | Geometry | Why this size |
|---|---|---|---|
| list (canonical) | default | `-x 100 -y 30` | wide enough for a full nav block and a 10-row page |
| list (narrow / degraded) | default | `-x 62 -y 12` | the reflow floor — the details block DROPS rather than overflowing |
| playing | `Enter`, wait for the banner | `-x 100 -y 30` | banner + progress bar + the playing row's ground; needs a `-pe` capture too |
| page mode | `Tab` | `-x 100 -y 30` | the other windowing: a fixed page, and a `page N/M` segment |
| filter open | `/` then text | `-x 100 -y 30` | caret + narrowed page + bottom input row |
| ASCII fallback | `TING_ASCII=1` | any | the whole glyph set in its ASCII form |
| zh chrome | `TING_LANG=zh` | any | translated chrome at the same measured width |

Keep every capture destined for one doc section at the **same width** so the blocks line up
visually, and put that width in the surrounding prose.

## 2. Capture — after the ready marker, never after a sleep

`TING_CONFIG` is not optional here, and it points at a file of this capture's OWN. Two reasons,
and the second is the one that bites: a frame taken against the developer's config is a
picture of their theme and their chrome language rather than of the shipped defaults — and
`ting` WRITES its preference keys back to whatever `TING_CONFIG` names (the live set is `PREF_KEYS` in `shell/ting` — grep it, don't trust a remembered count), so `t`, `l`, `v`,
`o`, `e` and the count edges would edit that file as a side effect of photographing it.

```bash
S=cap
CAPCFG=$(mktemp "${TMPDIR:-/tmp}/ting-capture.XXXXXX")
tmux kill-session -t $S 2>/dev/null
tmux new-session -d -s $S -x 100 -y 30 "cd $PWD && TING_LANG=en TING_CONFIG=$CAPCFG shell/ting 'lofi hip hop'"
timeout 30 bash -c "until tmux capture-pane -t $S -p | grep -q \"query='\"; do sleep 0.5; done"
tmux capture-pane -t $S -p > tmp/raw-list.txt
```

The marker is the TITLE line's source field (`query='`, `chapters='`, `playlist='`), not the
status line: the status is segments now and spells no `key=` at all. It also names what the
screen IS rather than how many rows it holds, which is what a marker should do.

For a keypress-driven frame, send the key and wait for the state to settle before capturing —
a re-fetch (`n`, `o`, `→` past the last page) needs the marker again, and a mode switch
needs ~1 s:

```bash
tmux send-keys -t $S Tab; sleep 1.2
tmux capture-pane -t $S -p > tmp/raw-page-mode.txt
tmux send-keys -t $S '/'; sleep 0.5; tmux send-keys -t $S -l 'radio'; sleep 1
tmux capture-pane -t $S -p > tmp/raw-filter.txt
tmux kill-session -t $S 2>/dev/null
rm -f "$CAPCFG"
```

If the frame will show playback, kill the player the way `tests/drive.sh`'s EXIT trap does —
`shell/t-play --stop --all` — the player survives the session kill.

## 3. Clean

```bash
python3 .claude/skills/capture-pane/clean_capture.py tmp/raw-list.txt > tmp/list.txt
```

It right-trims tmux's row padding, drops the empty bottom of the pane, drops a leading
`searching "…"…` line, and **exits 2 on a frame that still looks mid-fetch** — that guard is
the whole point, because a spinner frame is a picture of the loading state that reads as a
picture of the layout.

It never removes an interior row. Every row in a list frame is mutually aligned with the
others, so a "compressed" frame is a torn frame — paste frames verbatim.

## 4. Prove the capture BEFORE it enters a doc

A capture is evidence only once it has been measured. This is the step that separates this
skill from screenshotting:

```bash
python3 .claude/skills/capture-pane/assert_pane.py tmp/list.txt 100 list   # PASS + rail/index/gutter columns reported
```

**A frame with a player in it needs a second capture.** `capture-pane -p` throws every SGR
away, and the playing row is drawn as a ground rather than as text — so a row highlighted
only as far as its title looks exactly like a correct one in a `-p` capture. Take the same
pane twice and hand the `-e` copy over:

```bash
tmux capture-pane -t "$S" -p  > tmp/play.txt
tmux capture-pane -t "$S" -pe > tmp/play.e.txt
python3 .claude/skills/capture-pane/assert_pane.py tmp/play.txt 100 list --sgr tmp/play.e.txt
```

If the window has scrolled away from the playing row (the cursor is the user's and the
window follows the cursor), add `--off-window`: the check then asserts that NOTHING is
reversed, rather than switching itself off.

If `assert_pane.py` fails, **you found a layout bug — do not paste the frame.** Fix the
renderer, re-capture. Width measurement lives only in `assert_pane.py`; do not reimplement cell
counting in this skill's script or in a doc-splicing snippet.

## 5. Splice — Python replace between stable markers, never by hand

Box glyphs, the right-flush rail, and CJK column alignment make manual transcription
lossy in a way that is invisible in review.

```bash
python3 - <<'PY'
import pathlib
DOC = pathlib.Path("README.md")
frame = pathlib.Path("tmp/list.txt").read_text().rstrip("\n")
t = DOC.read_text()
start = t.index("<!-- pane:list-100 -->")          # stable markers, added once
end = t.index("<!-- /pane:list-100 -->")
block = f"<!-- pane:list-100 -->\n\n```\n{frame}\n```\n\n"
DOC.write_text(t[:start] + block + t[end:])
PY

grep -c '^```' README.md          # must be EVEN
grep -n '^## ' README.md          # headings intact
```

Prefer HTML-comment markers over "the heading after it" indices: a heading rename silently
relocates an index-based splice, and the diff looks like a doc rewrite.

## Gotchas

- **A spinner frame is the #1 mistake.** Wait for `query='`; the cleaner blocks it, but only
  if you run the cleaner.
- **State the geometry in the prose.** A 100-col frame under a claim about 62 cols is worse
  than no frame.
- **`send-keys -l`** for literal filter text, or `n` becomes the new-search key.
- **Version in a captured frame** comes from the `VERSION` file at the repo root (each entry
  point reads it into its own `TING_VERSION`) — after a bump, re-capture anything showing it
  rather than editing the number in the fence.
- Scratch stays in `tmp/`. A cleaned frame is scratch too — the doc is where it lands.
