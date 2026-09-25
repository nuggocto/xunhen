# Browsing a history in the terminal

`xunhen browse` shows an undo history as a tree beside the text of the
selected state. You can walk the branches, open any retained state, and diff
two of them without typing node IDs. It reads the same inputs as `show` and
`diff`:

```sh
xunhen browse --undo history.undo --base retry.go
xunhen browse --source retry.go --undo-dir ~/.local/state/nvim/undo
```

Like `show` and `diff`, the browser needs a base that verifies against the
history. Without one it has nothing to reconstruct, so the load fails and
the command exits with the same diagnostic `show` would print.

## The screen

The top row names the undo file, the base, and the reference node. On a
terminal at least 80 columns wide, the tree takes a third of the width, from
28 to 44 columns, and the selected state takes the rest. On a narrower terminal only one pane shows at a time,
and tab switches between them. The last two rows show what the browser is
doing and the main keys.

Each tree row is one retained state, in the recorded branch order. A node's
preferred child continues on the same level below it, so a long linear
history stays flat. Every other child starts a branch one level deeper and is
marked with `` `- ``. Other marks:

- `[ref]` is the reference: the buffer when the undo file was written, which
  the base verified against. The browser starts here.
- `[from]` is the pinned left side of comparisons. It starts at the
  reference.
- `[+N]` counts the rows a fold hides.
- `<N>` replaces indentation deeper than a third of the pane, so the ID and
  time of a node 10,000 branches deep still fit.

Times are the recorded event times in local time. They label changes; they
are not a complete editing timeline, and the root has none.

The text pane shows the selected state with line numbers, or a unified diff
when comparing. The header says which state or pair is on screen. While the
next state is being reconstructed, the header names it and says which earlier
result is still showing, so an old state is never shown as the new one. A
comparison of two identical states says so rather than showing nothing.

## Keys

| Key | In the tree | In the text |
| --- | --- | --- |
| up/down, j/k | select the previous or next row | scroll one line |
| left/right, h/l | fold or unfold; left on a folded or childless node selects its parent | scroll sideways |
| page up/down | move by a screen | scroll by a screen |
| home/end | first or last row | top or bottom |
| 0 and $ | | scroll to the start, or to the end of the longest line on screen |

| Key | Anywhere |
| --- | --- |
| tab | move between the tree and the text |
| p | preview the selected node |
| d | compare the pinned node (`-` lines) with the selected node (`+` lines) |
| space | pin the selected node as the left side of comparisons |
| g | go to a node by ID; hidden nodes are unfolded |
| e | show the command that saves the selected state to a file |
| r | reload the inputs |
| ? | help; esc closes it |
| q | quit |
| ctrl+c | interrupt and exit with status 130 |
| ctrl+z | suspend; `fg` in the shell resumes |

The jump prompt accepts decimal digits only. An unknown ID leaves the
selection where it was and says so.

## Saving a state

The browser never writes files. Press `e` for the command that saves the
selected state, built from the inputs that loaded the history:

```sh
xunhen show \
  --undo history.undo \
  --base retry.go \
  --node 42 --raw --final-newline=include > recovered.go
```

Each input flag gets its own line, so a search through 32 undo directories
stays readable, and sideways scrolling reaches the end of the widest line.

`--final-newline=include` ends the file with a newline after the last line,
as most source files do; `omit` leaves it off. The undo file does not record
which one the original had. `--raw` refuses to write to a terminal and
refuses states that hold invalid UTF-8 or NUL, as `show` documents.

Paths in the command are quoted for a POSIX shell. A path containing control
characters or invalid UTF-8 uses bash and zsh `$'...'` quoting, which shows
every byte as a printable escape and gives the shell the original bytes.

## Reloading

`r` loads the inputs again the same way as at startup: the same `--undo` and
`--base`, or the same discovery search. The new load is validated in full
before anything changes. When it succeeds, the old history, its cached
states, and every reference into it are dropped. The selection keeps its node
ID if that ID exists in the new history and otherwise returns to the
reference, and the pin returns to the new reference. When the reload fails,
the earlier load stays in use and the status row says why.

Before a reload starts, the browser releases the states it has cached and
the result on screen, so the old and new loads overlap by their histories
alone.

## Terminal requirements and exits

The browser needs a terminal on stdin and stdout and a `TERM` other than
`dumb`. Otherwise it exits with status 1 before opening any input and
suggests `inspect`, `show`, and `diff`. It never writes screen control
sequences to a pipe or file.

Every way out restores the terminal settings, leaves the alternate screen,
and stops the background worker before the process exits:

| How it ends | Exit status |
| --- | --- |
| q | 0 |
| ctrl+c or SIGINT | 130 |
| SIGTERM | 143 |
| SIGHUP, including a dropped SSH connection | 129 |
| The first load fails | 1, with the same diagnostic as `show` |

Only the browser catches these signals, and only while it runs. The other
commands keep the default disposition, so an interrupt still ends a blocked
write.

ctrl+z restores the terminal before the process stops and takes it back on
`fg`, redrawing at the current size. A stop signal sent from outside, such
as `kill -TSTP`, stops the process without that cleanup, and the terminal
stays in raw mode until `fg`.

## Text display

Recovered text, file names, and error messages are untrusted. Terminal
controls, format characters such as bidirectional overrides, and invalid
bytes are shown as the same escapes `show` prints, such as `\x1b`. Tabs
expand to stops every 8 columns. Wide characters take two cells. A combining
mark with no base, or one that would merge with an escape beside it, is
escaped too, so every character's width is known.

Widths follow the renderer. Bubble Tea measures each character by its first
rune until the terminal reports Unicode core mode (2027), and by its whole
grapheme cluster after that; the browser switches with it and prepares the
shown state again. A terminal that measures some emoji sequences differently
from both methods may still shift the rest of such a line. The browser needs
no patched fonts: its marks are ASCII.

With `NO_COLOR` set to any non-empty value, the browser uses no color. The
selection stays marked by `>` and reverse video.

## How the browser stays responsive

Drawing never reads, replays, or compares. One background worker does that
work, one request at a time:

- At most one request runs and at most one waits. A new request replaces the
  waiting one and cancels the running one, which is obsolete by then.
- A waiting reload is never replaced by a selection change, and a running
  reload is cancelled only by another reload.
- Every request carries a load generation and a selection generation. The
  browser accepts a result, success or failure, only while both still match,
  so a late result can never appear as the current state.
- The worker delivers each result through one waiting reader and stops
  delivering when the browser closes.

Moving the selection quickly therefore costs one cancelled replay per key at
most, and the text shown always belongs to the node the header names.

Long lines get a column index when they are prepared, one checkpoint per
4 KiB, so drawing any part of a 16 MiB line reads a few kilobytes. The tree
is indexed once per load; folding and scrolling visit only the rows on
screen.

## Memory

Prepared states are cached in least-recently-used order, up to 128 MiB for
one load. The cache charges what a state owns:

- its line array, 16 bytes per line;
- its long-line column indexes;
- 256 bytes of fixed fields, plus 256 bytes of bookkeeping per entry.

The line bytes themselves belong to the load: every line of every state is a
string from the decoded undo file or the base, which states share rather than
copy. The largest state the limits allow, 4,000,000 lines with as many long
lines as the remaining bytes can hold, charges 66 MB, so two of them fit.
`TestCacheHoldsTheLargestPair` builds that state and checks it.

Evicting a state only drops the cache's reference. States never change after
they are prepared, so a state still on screen stays intact until the view
lets go of it.

The cache bounds only what it keeps. A comparison on screen also holds its two
states and the diff's own copies of their line arrays, and a reconstruction
needs its workspace while it runs. These are the peaks measured with the
built binary on the development machine, Linux/amd64 with Go 1.27.1, read
from `VmHWM`:

| Input | Actions | Peak memory |
| --- | --- | --- |
| Two 4,000,000-line states, one a shuffle of the other | preview both, compare | 837 MiB |
| The same | compare, then reload twice while comparing | 874 MiB |
| 550,000 changes in a 223 MiB undo file | jump to both ends, reload, compare | 566 MiB |
| Three 16 MiB lines | preview, scroll to the end, compare | 121 MiB |

The shuffled comparison is the costliest input `diff` has, as for the
command, and it stays within the 1 GiB target. These are measurements of
these inputs, not a guarantee for every input.

The same runs measured how long work takes to stop. Cancelling a load, a
replay, a comparison, or the indexing of a 16 MiB line returned within 9 ms.
Quitting while the shuffled comparison ran ended the process in about 20 ms.
