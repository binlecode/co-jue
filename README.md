# ting

**ting / 听** — an agent-first multi-source media engine with a terminal face.

Search a source, play it through mpv detached from your terminal, and keep controlling it — from
a TUI if you are a human, from a single-line JSON contract if you are a program. Three sources
ship (YouTube, Bilibili, NetEase Cloud Music); a fourth is a new pair of scripts and no change
anywhere else — the third one was.

```sh
ting                                   # interactive: search, browse, play, control
yt-search -j -n 25 -- "lofi hip hop"   # machine: one line of JSON out
bili-search -j -n 25 -- "周杰伦"        # machine: the second source, the same envelope
ne-search -j -n 25 -- "钢琴"            # machine: the third; rows carry a real `access`
t-play -d -j -- "<url>"                # machine: launch detached, get {id, pid, sock}
t-play -d --start 601 -- "<url>"       # machine: open at 601s (a link's own &t= does this too)
yt-resolve --transcript -j -- "<url>"  # machine: captions as clean text + timed segments
t-play --status -j                     # machine: what is playing, where, how loud
t-play --pause --id <id> -j            # machine: also --resume, --seek ±N, --seek-to N
t-playlist --show chill -j | t-play -d --queue -     # machine: play a list, one player
t-play --enqueue - --id <id> -j        # machine: append to it; --next skips a track
t-play --set-loop one --id <id> -j     # machine: repeat this track (off|one; a LIST is --queue)
t-play --stop --id <id> -j             # machine: stop it
t-history --ls -n 20 -j                # machine: what was played, when, for how long
```

## Status

**Reference implementation.** This is a working shell suite that its author uses daily, published
together with a design document that is longer than most of the code it describes. Since `v0.8.0`
it is also tagged and installable from a tap (`brew install binlecode/actop/ting`) — what ships
is these scripts, not a binary, so the Go rewrite stays ruled out
(see [`docs/ROADMAP.md`](docs/ROADMAP.md) for why).

The document may be the more useful artifact. `docs/ARCHITECTURE.md` records things that are usually
learned and then forgotten: East-Asian-width handling in a terminal renderer, DCS frame
synchronisation, correlating mpv IPC replies by `request_id`, and the `set -e` traps that bash 3.2
sets for you.

## What it is

- **`t-play`** — the player. Source-agnostic: it drives mpv, owns the detached player lifecycle
  (id / pid / socket / lock / state dir / reap) and the **queue** a player consumes — a lone
  handle is a queue of one, each item resolved when it is reached because a stream URL expires —
  and defines the contract. It EXECUTES a start offset (`--start SEC`, or the one a link carried)
  without knowing any site's spelling for it: `?t=601s` is YouTube grammar, and reading it is the
  engine's job — the player only ever sees a number. It never searches and
  never extracts, so it knows nothing about YouTube. Deliberately single-purpose, with mutually
  exclusive flags rejected up front and a flag that moved to an engine answered by naming that
  engine — because that is what makes it safe for a small model to call.
- **`yt-search` + `yt-resolve`** — the YouTube *engine*, a pair. Search turns a query into results;
  resolve turns a result id (or a URL) into a direct stream URL plus the HTTP headers it must be
  fetched with, and also answers `--info` and `--transcript`. Everything site-specific lives here:
  the yt-dlp calls, the cookie decision, the format-per-mode table, and the ten spellings of a
  timestamp that all become one `start_seconds`. Adding a source is adding a pair.
- **`bili-search` + `bili-resolve`** — the Bilibili *engine*, the second pair, and the proof that
  the sentence above is true: neither the player nor the TUI changed a line to admit it. Its two
  halves use **different primitives** — search talks HTTP through `curl` because yt-dlp's Bilibili
  search returns no metadata at all, resolve shells out to `yt-dlp` because reimplementing this
  site's request signing and stream selection would be a thousand lines to redo what a dependency
  already maintains. The seam between an engine and the player is the **envelope**, never the tool
  behind it. There is no `--transcript` here: the site has no captions, and an engine says what it
  cannot do by not having the verb.
- **`ne-search` + `ne-resolve`** — the NetEase Cloud Music *engine*, the third pair, and the one
  that pays the sentence off twice. Nothing outside these two files changed to admit it: the
  player found it by name, the TUI's `e` key offered it, and the test suite's cross-engine
  invariants covered it the moment the pair landed. And it is the first engine whose search rows
  carry a **real `access`** — this site publishes a `fee` per track, so a row says whether it is
  fully playable, a 30-second sample, or an album purchase, in the page already fetched. The two
  halves use different primitives again, and for a blunter reason than Bilibili's: the site's
  plaintext search endpoint is gone, so `ne-search` speaks the browser's encrypted `weapi` — two
  AES passes through `openssl`, which is a dependency of that **one file** and of nothing else in
  the suite. Its `--transcript` is the song's lyrics.
- **`t-playlist`** — the playlist store, and the first piece of state the suite keeps *after* a
  reboot. Durable, user-level, engine-agnostic: it holds `{engine, url, title, …}` records under
  `${XDG_STATE_HOME:-~/.local/state}/ting/playlists/`, one file per list, written atomically
  under a lock. It knows no site and no playback — `engine` + `url` are exactly the two arguments
  of `t-play`, so a stored record is a call rather than a reference. Optional: without it the
  rest of the suite is unchanged.
- **`t-history`** — the listening log, and the other half of that store: one line of
  `history/<YYYY-MM>.jsonl` per track, written by the player itself as each track ends —
  whether it ended on its own, was skipped, or was stopped. Append-only and lock-free, which
  is why every line is kept under 4 KB. A row is the same record a playlist holds plus the
  four fields a listening has (`played_at`, `ended_at`, `seconds`, `reason`), so
  `t-history --ls -j` pipes straight into `t-playlist --add` or `t-play -d --queue -`.
  `UT_HISTORY=0` turns the writing off; optional, like the playlist store.
- **`ting`** — the human face. One self-rendered list, live filter, pagination that
  reflows against the measured chrome, three playback states, en/zh chrome, ASCII fallback, themes.
  No TUI framework, no fzf.

The pieces a program depends on: a **single-line JSON envelope**, an **exit-code taxonomy**
(1 usage / 2+ propagated tool failure / 4 didn't take effect), and a **player lifecycle** that
survives the terminal that started it.

## What it is not

Not a general-purpose terminal music player: that layer is full and well maintained — cmus,
ncmpcpp, rmpc, musikcube, kew, termusic — and this is not a replacement for it. What plays here
comes from an engine, not from `~/Music`.

**All three listening features have landed** (`docs/ARCHITECTURE.md`「两个存储」): playlist
management (`t-playlist`, the `a` and `b` keys), the queue (`t-play
--queue/--enqueue/--next`, `+` and `>`), and the listening history (`t-history`, the `h` key).
Each shipped with the rule they all carry: an agent surface — a verb and a `-j` envelope —
alongside its keybinding, or it is not done. **Favourites is deliberately not a feature**: it is
a playlist with a fixed name. A downloader and channel subscriptions are unscheduled.

## Requirements

- **macOS first.** On Linux the mpv IPC path needs a `-U`-capable netcat: the suite probes for
  one and accepts either `nc` with `-U` (Debian/Ubuntu's `netcat-openbsd`) or `ncat` (Fedora's
  nmap netcat). Only `netcat-traditional`/busybox-only environments fall short — install a
  capable variant; without one, playback and the queue still work and only the socket verbs
  refuse.
- `yt-dlp`, `jq`, `mpv`, a unix-socket netcat (BSD `nc` ships with macOS), `curl`.
  They are not all needed by all of it: `yt-dlp` belongs to the engines, `mpv` and the netcat
  to the player, `jq` to both. **`curl` is required by `bili-search` and by the netease pair** —
  it IS their transport — and optional everywhere else (the YouTube engine's play-time client
  probe). **`openssl` is needed by `ne-search` alone**, for the encrypted search payload that
  site now insists on; without it that one command refuses and the other nine are unaffected.
- bash 3.2 — the version macOS ships. The suite is written to that floor on purpose; see
  `docs/ARCHITECTURE.md`「可移植性契约」.

Nothing is vendored. Install yt-dlp and mpv however you normally would.

## Configuration

Every default value in the suite lives in one tracked file at the root of the checkout,
`config`, declared once for all ten entry points. It is **not optional** — a checkout
without it exits 2 and says so, rather than letting an unset variable surface 100 lines
later.

To change settings for yourself, don't edit that file. Write only the keys you want in
`${XDG_CONFIG_HOME:-~/.config}/ting/config`:

```sh
mkdir -p ~/.config/ting
cat >> ~/.config/ting/config <<'EOF'
UT_DEFAULT_ENGINE=bili        # search Bilibili unless --engine says otherwise
UT_SEARCH_RESULTS=40          # fetch 40 rows per query instead of 20
YT_THEME=nord
UT_COOKIE_BROWSER=safari      # read site logins from Safari instead of Chrome
EOF
```

Four layers, and each one wins over the next:

```
flag (per call)  >  environment  >  your config  >  the shipped defaults
```

`KEY=value`, one per line, `#` to end of line is a comment, a leading `~/` expands.
Extensionless and flat, the same spelling `yt-dlp` uses for `~/.config/yt-dlp/config` — a
`.toml` or `.yml` would promise structure this suite cannot parse without taking a runtime
dependency it refuses.

Both files are **read as data, never sourced**, so `UT_X=$(cmd)` stores those characters
instead of running anything, and only `UT_`/`YT_`/`BILI_`/`NE_` keys are read — no config can
reach `PATH`, `TMPDIR` or `LD_PRELOAD`. `UT_ENGINE_DIR` wears an allowed prefix and would be
the hole in that: it names a directory of programs the suite runs, so it is refused from both
files and read from the environment only.

The shipped `config` is **never written by any command**. Your own file is: `ting` writes
eleven preference keys back to it as you change them at runtime — the engine, sort field,
play mode, quality tier, theme, language, result count, key-hint tier, row numbers, list
mode and loop mode, each behind the key that changes it. The edit is in place, one pass and one rename, so your
comments and layout survive; a value that would not read back unchanged is not written at
all, and a key already pinned in your environment is left alone with a note rather than
written to a file that could never win against it.

`UT_CONFIG` relocates your file, from the environment only. Three knobs are deliberately
absent from the shipped defaults because their unset state *is* an auto-detection that a
value would defeat: `YT_LANG` (zh under a zh\* locale), `YT_ASCII` (on under a non-UTF-8
locale) and `UT_STATE_DIR` (its default chains through `XDG_STATE_HOME`). Set those in your
own config or the environment.

**A source that does not ship with the suite** is still just a `<name>-search` +
`<name>-resolve` pair. Three places are scanned for one, in this order: next to the suite's
own scripts, then `$UT_ENGINE_DIR` (default
`${XDG_DATA_HOME:-~/.local/share}/ting/engines`), then `PATH`. A name already installed
beside the suite wins, so a pair you drop in that directory can add a source but never
replace a built-in one. `docs/ARCH-cli-contract.md`「加一个引擎 —— 清单」 is what such a pair
has to satisfy.

The shipped `config` enumerates every key, with its default and a comment saying what it
does — read that file to see them all. `docs/ARCH-cli-contract.md`「配置面」 explains the
chain, the write-back and the rules that span files, rather than restating the list.

## Try it

```sh
git clone git@github.com:binlecode/ting.git
cd ting
./shell/ting "lofi hip hop"      # or: ./shell/ting  and type a query
```

Or install the released version:

```sh
brew install binlecode/actop/ting
```

For daily use from a checkout, symlink onto your PATH. Each command goes under its own name —
the suite ships no second spelling for anything:

```sh
ln -s "$PWD/shell/ting"         ~/bin/ting
ln -s "$PWD/shell/t-play"       ~/bin/t-play
ln -s "$PWD/shell/yt-search"    ~/bin/yt-search
ln -s "$PWD/shell/yt-resolve"   ~/bin/yt-resolve
ln -s "$PWD/shell/bili-search"  ~/bin/bili-search
ln -s "$PWD/shell/bili-resolve" ~/bin/bili-resolve
ln -s "$PWD/shell/ne-search"    ~/bin/ne-search
ln -s "$PWD/shell/ne-resolve"   ~/bin/ne-resolve
ln -s "$PWD/shell/t-playlist"   ~/bin/t-playlist
ln -s "$PWD/shell/t-history"    ~/bin/t-history
```

Four of these verbs were spelled `uting`, `ut-play`, `ut-playlist` and `ut-history` up to
v0.9.0, and v0.9.0 shipped a symlink for each. v0.10.0 drops them: one name per command, and
nothing on disk claiming otherwise. Your own data does NOT move with them — a config at
`~/.config/uting/config` and a store at `~/.local/state/uting` are still read where they are.

Only `ting` is strictly required: every command resolves its siblings from its own location,
so a single symlink is enough to use the whole suite by hand. The rest are for calling the verbs
directly — which is what an agent does.

The human face carries the project's own name, so `~/bin/ting` is a plain symlink to
`shell/ting` — same word at both ends, no alias in between. Want something shorter to type?
Make one — `alias ut=ting`, or a symlink of your own. Nothing reads its own `argv[0]`, so any
name works. The suite ships no short form itself, because a second official spelling is a second
thing to keep in sync (`docs/ARCHITECTURE.md`「平级动词，没有内核」).

`ting --version` (or `-V`) answers before any dependency check, so it works on a machine that
has not installed yt-dlp or mpv yet — which is exactly when you want to know what you have. Every
entry point reports the same number: it is declared once, in `VERSION`.

That number is **semver over the CLI contract, not over the code**: the command names, their
flags, the exit-code table, the JSON envelopes, and the player lifecycle are the public API — a
renderer or a comment is not. While the suite is `0.y.z`, a breaking change bumps `y` and an
addition bumps `z`; `1.0.0` is a freeze promise this reference implementation does not make yet.
It is not a distribution milestone — being installable says nothing about whether the contract has
stopped moving, and the two were unhitched deliberately when the packaging decision reversed.

## Keys

`↑/↓` (or `j`/`k`) select · `<digits>j` jump to that row · `←/→` page · `Enter` play · `/` filter ·
`?` more / fewer keys · `n` new search ·
`o` sort · `v` playback mode · `f` quality tier · `e` switch source · `a` add to playlist ·
`b` open a playlist · `d` remove from the playlist on screen · `h` listening history ·
`c` the focused row's parts · `i` the focused row's chapters · `+` add to the queue · `>` next track ·
`u` the running player's queue — and inside it, `Enter` play that one now · `x` drop it ·
`p`/`P` move it earlier/later · `X` drop everything still waiting ·
`Space` pause · `-`/`=` volume · `#` row numbers (the jump's partner: it prints the
number `Nj` takes) · `Tab` list mode ·
`[`/`]` seek · `r` loop mode · `s` stop · `l` language · `t` theme · `z` undo · `q` quit

**Nothing asks "are you sure".** `d`, `D` (delete the playlist on screen) and `X` act on one
key, and so do `a`, `R`, `+` and `x`; for three seconds after any of them the notice line says
`z to undo`, and `z` puts it back exactly — the row in its own slot, the list under its old
name, the queue as it was. A confirmation guards nothing once `d y` has become one gesture; a
cheap way back does. Deleting the track that is playing does not stop it until the undo has
closed, so `z` inside the window never interrupts the sound. The copy is kept by the store
that was written, for this `ting` only: `t-playlist` and `t-play` take `--owner PID` and
`--undo` for any caller that wants the same (`docs/ARCH-player.md`「撤销副本」).

**The hint block has three tiers and `?` cycles them.** `core` — the shipped default — prints
this view's own job (move, act on the row, get back out, quit, and `?` itself) and fits one
line at 80 columns; `full` prints every key above; `hidden` suppresses the hint block entirely
for maximum content rows. It hides HINTS, never keys: everything in `full` is still pressable
under `core` and `hidden`. The tier is a preference like the other ten, written back to your own
config as `UT_KEYS=core|full|hidden`, and `？` is bound with it — a zh input method's
shift-/ is a full-width question mark. `j`/`k` are list-view only: with `/` open they are text
you are typing.

There is ONE view. `b`, `h`, `c`, `i` and `u` each replace the rows in it and are pressed again
to come back — a playlist, the listening log, a video's parts, a video's chapters, the queue
the running player is working through. Nothing toggles a second renderer, so `Tab` is unbound
and prints nowhere.

`u` is the half of `+` that lets you see what you queued. Until it existed the queue was a
number on the status line: you could add to it blind and had no way to look, reorder, or take
back a mistake. It puts the WHOLE queue on screen — played rows included, so the row numbers
and the indexes the CLI takes are one set of numbers rather than two — with the track being
heard on a ground of its own. Its four write keys only exist while it is open, and each one is
a `t-play --queue-*` call: the TUI never edits the queue file, because the player's own child
is writing to it too, at every track boundary. `x` on the track that is playing says so rather
than doing it — `s` stops it and `>` skips it. `u` opens from the search rows only (there is
one stash slot, so a queue opened on top of a playlist would lose the way back to it), and only
when the queue holds more than the one track being played.

`e` is drawn only when a second engine is installed — the TUI discovers engines by looking for
`<name>-search` and `<name>-resolve` pairs, so it holds no list of sources. `a` and `b` are
drawn only when `t-playlist` is installed and `h` only when `t-history` is, by the same rule.
`c` and `i` follow the same rule off a CAPABILITY rather than an install: `c` is drawn only
when the session's engine has `--parts`, which YouTube never will — one id there is one file —
and `i` only when it has `--info`. Three of them go one step further and read the VIEW as well:
`o`, `e` and `c` re-sort or re-fetch a SEARCH, so on a playlist, the history or a parts list
their only effect would be a notice saying they do not apply — and a measured block does not
spend a cell to say no.

`i` puts the focused row's CHAPTERS on screen as rows, paid for by one `--info` (the upload
date and the like count land on the status line — the two facts a row cannot hold). A chapter
row is a call, not a reference: its url carries `t=<second>`, which both engines already read
into the resolve envelope's `start_seconds`, so `Enter` plays from that chapter, `+` queues
from it and `a` stores it. `Enter` on a chapter of the track already playing SEEKS instead —
an offset inside the file that is open needs no second resolve. An item with no chapters says
so and stays where it is. A chapter is a SPAN, so its rail prints one — `0:00 → 2:30`, in the
column a search row uses for how long it is; the length that implies is also what `a` stores
and `+` queues. Both times are right-aligned in a field sized over the whole table, so the
arrow is a column and stays one as you page. While that item is playing, the two rails read
two different clocks: the one above the rows stays the ITEM's timeline, and the one under them
becomes the SELECTED chapter's own progress — filled for a chapter the playhead has passed,
empty for one it has not reached. The view opens on the chapter that is playing and the cursor
follows it over each boundary, so the bar under the rows is always the chapter you are hearing
— until you move the cursor yourself, and then it stays where you put it. The header names the
source, so it reads `chapters='<the item>'`.
`h` asks for no name — the log is one thing — and shows the 50 newest listenings. `d` is the
mirror: it is drawn only with a **playlist** on screen, because a search result is a row of
nothing and the log has no per-row removal to call. Taking out the last track leaves the list
there, empty, for `D` to delete if that is what you meant.
The row count lives on the two page EDGES rather than on a key of its own: `→` past the last
page fetches one batch more, and `←` on page 1 drops one again — a local truncation, no
re-fetch, with a screenful as the floor. Ten of these keys — engine, sort, mode, quality
tier, theme, language, that count, the hint tier, the row numbers and the list mode — are written back to your own config file, in place, so the next
session opens where this one left off (`docs/ARCH-cli-contract.md`「配置面」). With a
playlist or the log on screen the two keys that re-fetch a query — `o`, `e` — say so and
do nothing, and the two edges are a silent no-op there; both can mix sources, and each row plays under the engine that produced it. `+` and `>` appear only while
something is playing: the queue belongs to the player, so with no player there is nothing to
append to.

## Tests

**Functional tests only.** A command-line tool is tested by running it and reading its exit
code and its stdout, which is all these do — no rig layer, no screen model, no pty harness,
no unit tier, and **no fixture, mock, fake, stub or stand-in of any kind** — all tests
are real functional tests. Tests drive real entry points, send real network requests, parse
real envelopes, and manage real processes; no synthetic state files, no pre-baked fixture
responses, and no staged configuration keys. Anything that would run in place of a component
or fake its output is out, peers included — a claim needing a real peer or external service is
proved against the real one, or it is not claimed. Each file's header says what it proves; run
either one directly.

| Suite | What it is for |
|---|---|
| `tests/contract.sh` | The CLI contract, asserted by running it: the search and resolve envelopes, the player's engine seam (an unknown engine is usage, a dead media id is a propagated failure that still carries a reason), every documented rejection, the host gate stated as an invariant over every **discovered** engine (a real URL is claimed by exactly one; a confusable is refused by all), `--transcript` both ways, the idle lifecycle verbs (including the queue verbs, where a
payload this process cannot use is a usage error and a well-formed one with nothing playing is
"did not take effect"), the tombstone record for a player that died unasked, the exit-code taxonomy, the playlist store (driven under a disposable `UT_STATE_DIR`, including eight concurrent writers against the lock), the listening log's own contract in the same disposable store (an 8 KB title truncated and MEASURED, because "every line under 4096 bytes" is the premise its lock-free append rests on), and the TUI booting / surviving a resize / leaving on `q` under tmux — and leaving no player behind when it goes, because `ting` stops its playback on exit, so a TUI that did not leave is a TUI still holding one. It also runs three of the four pipelines `docs/ARCH-cli-contract.md`「调用面」 prints, rather than leaving them as prose nothing executes — the fourth launches a player and belongs below. Under two minutes in full; **`--offline` runs the hermetic prefix** — every gate, both stores, the lifecycle and the death record, in ~30s with no packet sent, which is what makes "run it before every commit" a rule and not a wish. The check total is deliberately not quoted here: it moves with every check that lands, it was already stale in this sentence twice over, and the suite prints its own — **`0 failed` is the number that means passing**.  |
| `tests/playback.sh` | The detached-player lifecycle, whose bugs are **processes**: detach returns before mpv is up, two players, an ambiguous mutation → exit 4 *and* `status:"ambiguous"` (4 alone is also what an idle call answers, so the field is the half that separates them), a targeted one moves only its target, and zero orphan mpv at the end. It also owns the **live read** — the `--status` fields off a real mpv socket, `paused:false` distinguished from `paused:null`, and a really-running player whose socket is really removed degrading to nulls with volume off the record — because the peer has no stand-in and never will. It drives a **queue** end to end for the same reason — a mock engine would skip the
resolve between two tracks, which is the thing most likely to break: `--queue` launches —
from a real `t-playlist --show -j` envelope, so the documented pipeline is what starts the
player rather than an array written to look like one —
`--enqueue` appends (six concurrent writers, no lost update), `--next` moves the position and
the player follows, and a track reaching its own end starts the next. It also plays a real
Bilibili track — the one check that proves the player *applies* an engine's `http_headers` rather than merely receiving them, because that site's CDN answers 403 without them while YouTube would keep working. And it owns the **listening log's wiring**, since only here does a real track really end: a 19-second handle is played out, and the row that appears for it carries no reason — which is what separates a history from a death record. Starts real players at `--volume 0` in a state dir of its own, and points `UT_STATE_DIR` somewhere disposable too, so it never touches what you are listening to nor what you listened to. It needs the network, and its time is real engine resolves plus one 19-second track played out to its own end — what is left is not waiting. |

One more file in `tests/` is not a suite and asserts nothing. `tests/drive.sh` is a **driver** for the TUI, which needs a real tty and so cannot be run from
a pipe. It launches tmux at a declared geometry, waits on the ready marker, optionally sends
keys, dumps the frame, and **always reaps the detached player** — `Enter` starts mpv in its own
process group, and killing the tmux session does not stop it. Like the two suites it drives a
state dir of its own, which is what lets that reap be unconditional: with your real one it would
reach the player *you* are listening to, so the reap used to be skipped unless the keys contained
`Enter` — and `-i`, the one mode where a human presses it, was therefore never cleaned up at all.

```sh
tests/drive.sh -x 62 -y 20              # the reflow floor, frame dumped
tests/drive.sh -k Enter -w Playing      # a real detached play, then cleaned up (en chrome)
tests/drive.sh -k Enter -w 播放中       # the same marker under a zh chrome
tests/drive.sh -i                       # attach and drive it by hand
```

`contract.sh` needs `tmux` for its TUI boot check and nothing else — `tests/` is shell all the
way down, and `.githooks/pre-commit` refuses a non-`.sh` file there to keep it that way. Nothing
here is needed at runtime.

**Layout is proved outside the suite.** Cell grids, column alignment and glyph widths are
checked when a frame enters a doc — that is what `.claude/skills/capture-pane` is for, and its
`assert_pane.py` refuses a frame that would wrap. The suite asserts *survival*, not shape.

Lessons these paid for, every one of which produced a wrong result first:

- **Wait on a ready marker, never on a sleep.** A captured spinner frame is a picture of the
  loading state, not of the layout.
- **A pty starts at 0×0, and `LINES`/`COLUMNS` do not fix it.** The TUI reads `stty size`
  through `/dev/tty`, so without `TIOCSWINSZ` the reflow has no rows and draws a one-row list
  whose frames look plausible enough to trust. tmux is the terminal here for that reason.
- **A piped `while read` loop body runs in a subshell**, so every failure it counted was
  discarded and the run reported green regardless.
- **Match multibyte glyphs without `LC_ALL=C`.** Under the C locale a bracket expression of
  multibyte characters is a set of *bytes*: it matches fragments, reports one glyph where four
  turned, and reads as a spinner that never advanced.
- **A check nobody has watched fail is untested.** Break the thing it guards first.

## Documentation

**Everything under `docs/` is written in Chinese**; this README and `CLAUDE.md` are the English
surface.

- [`docs/USER_MANUAL.md`](docs/USER_MANUAL.md) — the complete user manual (Chinese): full interactive
  TUI keys, multi-row views, CLI agent pipelines, detached playback lifecycle, configuration,
  and troubleshooting.
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — the umbrella: what this suite is positioned to
  be and what it deliberately is not, topology, seams, the control-flow diagrams, every
  non-obvious decision, and the risk register — the known ways this can go wrong and what
  defends each one. Diagrams, flows and decisions; the detail is in the per-scope docs below.
- [`docs/ARCH-cli-contract.md`](docs/ARCH-cli-contract.md) — the frozen CLI contract: envelopes, exit codes,
  lifecycle semantics, and the checklist for adding an engine.
- [`docs/ARCH-engine.md`](docs/ARCH-engine.md) — the site half: query shaping, the Bilibili
  transport, the login / PO-token probe, handle grammar, `--info` / `--transcript`.
- [`docs/ARCH-player.md`](docs/ARCH-player.md) — the player, the queue and the two durable
  stores: the detached lifecycle, runtime IPC, `t-playlist` and `t-history`.
- [`docs/ARCH-tui.md`](docs/ARCH-tui.md) — the human face: one view with five row sources, in-place rendering,
  the width layer, the reflow and the three play states.
- [`docs/ROADMAP.md`](docs/ROADMAP.md) — the recorded NOs with their reopen conditions, the
  reopen triggers for settled decisions, and what is not built yet. No changelog, no survey
  data, and no landed decisions — those live in the architecture doc's decisions chapter; positioning is its opening chapter.
- [`docs/RESEARCH-tui-player.md`](docs/RESEARCH-tui-player.md) — the survey those decisions rest
  on, kept separate so the decision records hold decisions rather than data. Two halves, and it opens
  by saying which is which: **one half is measured** — the name screening, what comparable
  projects do by GitHub API, what publishing this shell version costs, and how ready it is;
  **the other started as read, not run** — how other terminal players are built along five
  orthogonal design axes, the five playback architectures they pick from, and the four routes
  to a playable URL on the Chinese side. The 2026-09-03 pass measured the parts that carry
  weight (how long a signed stream URL lives on each of the three sites, and how two of these
  players really talk to their backends) and says which claims are still only read. It labels
  its own gaps.

## License

MIT
