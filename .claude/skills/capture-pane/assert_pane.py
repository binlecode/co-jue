#!/usr/bin/env python3
"""Assert ting's layout invariants against a captured pane.

Several rigs merged into one, because they all read the same capture and all failed the same
way when kept apart — you fix a renderer, remember to re-run two of the three, and the third
goes stale. There is one view left to check: the `card` branch went when the second renderer
did, and a mode nothing can produce is a mode that quietly passes. The checks:

  pane width   No drawn line exceeds the pane. This is the one that catches the width layer
               getting something wrong, and it is measured in CELLS (east_asian_width), not
               characters, because a CJK title is two cells and a naive len() would pass a
               line that visibly wraps.

  index column Every result row's title starts on the SAME cell, whether the row is selected
               ("> ") or not, 1-digit index or 2-digit. The prefixes used to be per-row
               literals, so a page that reached two digits started every 1-digit title one
               cell to the left — ragged in the one column the eye scans down.

  rail column  Every result row's duration rail is right-flush at one column, and the
               progress bar ends on the same one. That column is the pane width minus the
               scrollbar gutter and its cell of air, so it is MEASURED rather than assumed —
               the renderer derives every right-hand flush from one expression, and this
               asserts they agree with each other, which stays true when the gutter's width
               changes (█ and │ are Ambiguous: two cells under TING_AMBIG_WIDE).

  gutter       Every result row ends with a scrollbar cell, all in the same column, and a
               list longer than its window shows BOTH glyphs — all thumb or all track means
               the thumb was never sized.
               A capture is the terminal's own cell grid, so this measures where the rail was
               actually DRAWN — which is the whole point: char_w over-counts on purpose, so a
               rail placed by computed padding lands early on rows the terminal drew narrower
               than we measured. Placing it with CHA is what makes this pass.


  playing row  Given a SECOND capture taken with `tmux capture-pane -pe` (--sgr <file>), the
               row the player is on carries the reverse attribute from cell 1 to the pane
               width — the whole line, including the stretch the rail's CHA jumps over
               without writing. This is the one invariant a plain `-p` capture is
               structurally blind to: `-p` throws every SGR away, so a row highlighted only
               as far as its title looks identical to a correct one. The check is here and
               not in tests/ for the reason every other check here is: this is layout, and
               the suites assert survival, not shape.

               A window the playing row has scrolled out of is a real frame too — the cursor
               is the user's and the window follows the cursor, so nothing is highlighted and
               nothing scrolls back. Say so with --off-window, and the check REVERSES rather
               than switching off: it then asserts that no row is reversed. Both states are
               asserted; neither flag turns the check into a no-op.

Captures come from `tmux capture-pane -p` — which is what the capture-pane skill feeds this, and the
only source now that the pty rigs are gone. Ambiguous-width characters count as ONE cell, matching the suite's default
(TING_AMBIG_WIDE unset); pass --ambig-wide to match the other setting.

usage: assert_pane.py <capture.txt> <pane_width> [list] [--rows N] [--ambig-wide]
                      [--sgr <capture-pe.txt> [--off-window]]
exit:  0 = every applicable invariant held, 1 = at least one failed
"""
import re
import sys
import unicodedata

ZERO_WIDTH = "‍︎️"


def cells(s, ambig_wide=False):
    n = 0
    for ch in s:
        if unicodedata.combining(ch) or ch in ZERO_WIDTH:
            continue
        ea = unicodedata.east_asian_width(ch)
        if ea in ("W", "F"):
            n += 2
        elif ea == "A":
            n += 2 if ambig_wide else 1
        else:
            n += 1
    return n


# A row is its 2-cell cursor slot, then the ordinal field ONLY when the row numbers are on
# (the # key / UT_ROW_INDEX), then the title. The marker slot is the constant: it is two
# cells whether it holds the cursor glyph or nothing, which is what keeps the title column
# still as the cursor moves.
#
# The ordinal being optional costs this pattern its own discriminator — "  " then anything
# also describes the details block's metadata line and the hint block. So a row is this
# prefix AND a duration rail at the end of the line; neither half identifies one alone.
# `▎ ` is the cursor as of the accent-bar refresh (137e76b); `> ` is what ASCII mode
# narrows it to, and `▶ ` is the shape it had before that commit — kept so an older
# captured frame still parses. GL_CURSOR in shell/ting is the authority.
ROW = re.compile(r"^(▎ |▶ |> |  )( *\d+\. )?(?=\S)")
# The rail, and after it the scrollbar cell that now closes every row. The gutter is part of
# the match rather than stripped beforehand so that one regex still identifies a row — and
# its group is what lets the rail's own end column be measured without it.
RAIL = re.compile(r"(LIVE|--:--|\d+:\d\d(?::\d\d)?)( [█│#|])?$")
CSI = re.compile(r"\x1b\[([0-9;]*)([@-~])")
# Every spelling of the wordmark. en is `ting`; zh is the seal 【 听 】, which ASCII mode
# narrows to `[ 听 ]` — both carry 听, so the bare glyph covers the pair.
BRAND = re.compile(r"ting|听")


def reverse_span(line, ambig_wide=False):
    """First and last CELL (1-based, inclusive) carrying SGR 7 on this line, or (None, None).

    tmux -e re-emits the attributes it recorded per cell, so this reads the terminal's own
    idea of the row rather than the escape sequence ting wrote — which is the point: the
    stretch between the title and the rail is never written by the renderer at all, it is
    filled by a back-colour erase, and only the grid knows whether that worked.
    """
    rev, col, first, last, i = False, 0, None, None, 0
    while i < len(line):
        m = CSI.match(line, i)
        if m:
            if m.group(2) == "m":
                for part in (m.group(1) or "0").split(";"):
                    part = part or "0"
                    if part == "7":
                        rev = True
                    elif part in ("0", "27"):
                        rev = False
            i = m.end()
            continue
        if line[i] == "\x1b":          # any other escape: skip its final byte and move on
            j = i + 1
            while j < len(line) and not ("@" <= line[j] <= "~"):
                j += 1
            i = j + 1
            continue
        cw = cells(line[i], ambig_wide)
        if rev and cw:
            if first is None:
                first = col + 1
            last = col + cw
        col += cw
        i += 1
    return first, last


def main(argv):
    ambig_wide = "--ambig-wide" in argv
    argv = [a for a in argv if a != "--ambig-wide"]
    off_window = "--off-window" in argv
    argv = [a for a in argv if a != "--off-window"]
    sgr_path = None
    if "--sgr" in argv:
        i = argv.index("--sgr")
        sgr_path = argv[i + 1]
        del argv[i:i + 2]
    expect_rows = None
    if "--rows" in argv:
        i = argv.index("--rows")
        expect_rows = int(argv[i + 1])
        del argv[i:i + 2]
    path, width = argv[0], int(argv[1])
    view = argv[2] if len(argv) > 2 else "list"

    lines = open(path, encoding="utf-8").read().split("\n")
    while lines and lines[-1].strip() == "":   # captures keep trailing blanks
        lines.pop()

    def w(s):
        return cells(s, ambig_wide)

    fails, notes = [], []

    for i, line in enumerate(lines):
        if w(line) > width:
            fails.append("line %d is %d cells > pane %d: %r" % (i + 1, w(line), width, line[:70]))

    body = "\n".join(lines)

    if view == "list":
        # The wordmark is language-dependent (TING_LANG=zh draws the seal 【 听 】), so this
        # asks for either spelling rather than the English one — a frame captured on a zh config is
        # not a scrolled header.
        if not lines or not BRAND.search(lines[0]):
            fails.append("header not on line 1 (scrolled off?): %r" % (lines[0][:60] if lines else ""))
        # Detect the hint block by its LAST item (q quit), not by a label: the block carried a
        # "Navigation:" prefix once and no longer does, and a rig that keys off chrome wording
        # fails the day the wording is tuned. The last item is structural — the block is packed
        # in order and q is always last.
        if not re.search(r"\bq\s+(quit|退出)", body):
            # Dropped on purpose when the pane cannot pay for it (nav_ok). This was a flat
            # "len(lines) >= 12", which only held at >= 40 columns — the chrome packs to the
            # width, so at 30 columns the same hints cost three more lines and the gate
            # fires in a 14-row pane. Ask instead whether the drop bought anything: a frame
            # with rows to spare at the bottom dropped the block for nothing.
            spare = len(lines) - 1 - max([i for i, l in enumerate(lines) if l.strip()] or [-1])
            if spare > 1:
                fails.append("navigation hint block missing, %d rows spare" % spare)
            else:
                notes.append("hint block dropped (short terminal) — expected")

        # ROW + RAIL still catches one thing that is not a row: the Now-Playing banner. It
        # opens with the play glyph in the same 2-cell slot, and at a narrow width its tail
        # elides down to "elapsed / total", which is a duration rail as far as any pattern
        # can tell (seen at 40 columns). Localised state labels are not the way out of that —
        # "播放中" / "Playing" / "Paused" would put chrome wording into a layout rig, which is
        # the thing this file's own comments already refuse to do twice.
        #
        # So use the STRUCTURE instead: result rows are CONTIGUOUS, and they are the last such
        # block on the screen — the banner sits above the key block, and everything under the
        # rows (the details metadata, the description, the page counter) fails RAIL. Take the
        # last run and say how many lines were dropped, so a misdetection is visible rather
        # than silent.
        hits = [i for i, l in enumerate(lines) if ROW.match(l) and RAIL.search(l)]
        runs, cur = [], []
        for i in hits:
            if cur and i != cur[-1] + 1:
                runs.append(cur)
                cur = []
            cur.append(i)
        if cur:
            runs.append(cur)
        rows = [lines[i] for i in runs[-1]] if runs else []
        if len(runs) > 1:
            notes.append("ignored %d line(s) above the row block (the banner elides to a "
                         "duration at narrow widths)" % sum(len(r) for r in runs[:-1]))
        notes.append("result rows: %d" % len(rows))
        if expect_rows is not None and len(rows) != expect_rows:
            fails.append("expected %d result rows, found %d" % (expect_rows, len(rows)))

        # index column: one start cell for every row, and the marker must appear somewhere
        starts, marked, digits = {}, 0, set()
        for l in rows:
            m = ROW.match(l)
            if m.group(1) != "  ":
                marked += 1
            if m.group(2):
                digits.add(len(m.group(2).strip()) - 1)
            starts.setdefault(m.end(), []).append(m.group(0))
        if rows and len(starts) > 1:
            fails.append("titles start in %d different columns: %r" % (len(starts), starts))
        if rows and marked == 0:
            fails.append("no selected row in this capture (cursor marker never rendered)")
        if rows and marked > 1:
            fails.append("%d rows carry the cursor marker; there is one cursor" % marked)
        if rows:
            notes.append("index: title col=%s digits=%s"
                         % (sorted(starts), sorted(digits) or "off"))

        # rail column: one column for every row, and it is the pane width less whatever the
        # scrollbar gutter takes. Both are measured off the capture — the renderer derives
        # them from one expression, so what is worth asserting is that they still agree.
        ends, guts = {}, {}
        for l in rows:
            m = RAIL.search(l)
            if not m:
                fails.append("row has no duration rail: %r" % l[-30:])
                continue
            g = m.group(2) or ""
            ends.setdefault(w(l) - w(g), []).append(m.group(1))
            guts.setdefault(w(l), []).append(g.strip())
        if len(ends) > 1:
            fails.append("rails end in %d different columns: %r" % (len(ends), dict(ends)))
        if ends:
            notes.append("rail: end col=%s" % sorted(ends))
        # The gutter: present on every row, in one column, and both glyphs on a list that is
        # longer than its window. A single glyph everywhere means the thumb was never sized —
        # which is a correct-looking frame and a scrollbar that says nothing.
        marks = [g for v in guts.values() for g in v]
        if rows and not all(marks):
            fails.append("%d row(s) end without a scrollbar cell" % marks.count(""))
        elif marks:
            if len(guts) > 1:
                fails.append("scrollbar cells end in %d different columns: %r"
                             % (len(guts), sorted(guts)))
            if expect_rows is None or expect_rows >= len(rows):
                notes.append("gutter: col=%s glyphs=%s"
                             % (sorted(guts), "".join(sorted(set(marks)))))

        # The progress bar. Not a boundary any more and not pane-wide: it is indented into
        # the rows' own two-cell gutter and ends on THE SAME COLUMN the duration rail lands
        # on. That shared column is the invariant, not any particular number — the renderer
        # derives both from one expression, and a scrollbar gutter moves them together. So
        # this asserts against the rail column measured just above rather than against a
        # literal, and stays true on both sides of that change. It prints only while
        # something plays; an idle frame has no bar and that is not a failure.
        bar = [l for l in lines
               if re.match(r"^  [━─╸\[\]]{10,}$", l.rstrip())
               or re.match(r"^  [=\->\[\]]{10,}$", l.rstrip())]
        if bar:
            bar_right = sorted(set(2 + w(l.strip()) for l in bar))
            if len(bar_right) > 1:
                fails.append("progress bars end in %d different columns: %r"
                             % (len(bar_right), bar_right))
            elif ends and bar_right != sorted(ends):
                fails.append("progress bar ends at column %s but the duration rail ends at "
                             "%s; both flush to one right edge" % (bar_right, sorted(ends)))
            notes.append("progress bar: right edge %s" % bar_right)

        # THE PLAYING ROW, from the -e capture. Nothing else on the screen is reversed, so
        # "the reversed line" identifies it without knowing which row is playing.
        if sgr_path:
            elines = open(sgr_path, encoding="utf-8").read().split("\n")
            spans = [(i + 1, reverse_span(l, ambig_wide)) for i, l in enumerate(elines)]
            spans = [(i, s) for i, s in spans if s[0] is not None]
            if off_window:
                if spans:
                    fails.append("--off-window, but %d row(s) are reversed (lines %s): the "
                                 "playing row is not in this window and must not be drawn"
                                 % (len(spans), [i for i, _ in spans]))
                else:
                    notes.append("playing row off this window, nothing reversed — expected")
            elif not spans:
                fails.append("no row carries the reverse attribute in %s (nothing playing, "
                             "or the ground was never drawn). If the window has scrolled off "
                             "the playing row, say --off-window" % sgr_path.split("/")[-1])
            # The ground runs from cell 1 to the RAIL's column — not to the pane's edge:
            # the scrollbar sits outside it deliberately, because a reversed thumb is a hole
            # exactly on the row a reader is most likely to be looking at. So the expectation
            # is measured off the same capture rather than written down, and it is still one
            # rule when there is no gutter at all (the rail ends at the pane width then).
            want = sorted(ends)[0] if ends else width
            for ln, (a, b) in spans:
                if a != 1 or b != want:
                    fails.append("playing row on line %d is reversed over cells %d-%s, "
                                 "expected 1-%d (a hole where CHA jumped?)" % (ln, a, b, want))
            if spans and not off_window:
                notes.append("playing row: line %s reversed %s-%s"
                             % (spans[0][0], spans[0][1][0], spans[0][1][1]))

    print("%-26s %-5s pane=%-4d lines=%-3d max_cell=%-4d %s"
          % (path.split("/")[-1], view, width, len(lines),
             max([w(l) for l in lines] or [0]), "PASS" if not fails else "FAIL"))
    for n in notes:
        print("      . " + n)
    for f in fails:
        print("      ! " + f)
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
