# Line diff

`xunhen diff` compares two reconstructed states of one history. This page
describes the algorithm in `internal/diff`, how it bounds its work, and the
output format. The replay that produces each state is described in
[the format notes](undo-format.md).

## What is compared

Each side is a list of buffer lines, as Neovim holds them. Lines are equal
only when their bytes are equal. Terminal escaping happens after the
comparison, when the command prints a line, so a line holding ESC and a line
holding the four characters `\x1b` count as different even though they print
the same.

The undo file records neither state's final newline, file encoding, or
fileformat. The diff therefore never prints `\ No newline at end of file`.
An empty buffer is one empty line, so going from an empty buffer to `a`
deletes the empty line and inserts `a`.

## Algorithm

The exact search is the linear-space variant from Eugene W. Myers, "An O(ND)
Difference Algorithm and Its Variations", *Algorithmica* 1 (1986), section 4b.
It finds an edit script with the fewest deletions plus insertions. The
comparison runs in these steps:

1. **Trim.** Drop the common prefix and suffix. Most comparisons between
   branches of one file differ in a small region, and this step leaves only
   that region.
2. **Identify.** Give each distinct line in the region a small integer, so
   the search compares integers instead of strings.
3. **Filter.** Remove lines that occur on only one side. They cannot belong to
   any common subsequence, so removing them does not change the result, and a
   rewrite that introduces fresh text costs almost nothing.
4. **Search.** Take a region, trim it again, and split it. The exact search
   runs Myers' search from both corners at once until the two paths meet, which
   gives a point on a shortest edit path; the parts on either side of that
   point become new regions. Two frontiers of `n + m` entries replace the
   stored rounds of the textbook version, so memory stays linear in the region
   size. Regions wait on an explicit stack rather than in recursive calls, so
   no input can exhaust the stack.
5. **Map back.** Matched lines map back to their original positions.

Within each stretch of changes, every deleted line comes before every
inserted line, whichever order the search took. Equal inputs always produce
the same script.

### When states are far apart

The exact search costs about `(n + m) × D` for a region of `n + m` lines that
differs by `D` edits, so two very different states could keep it busy for
minutes. Instead of failing, the comparison counts its search work as effort
and changes strategy as the effort grows:

| Effort spent | Each region is |
| --- | --- |
| Under 64,000,000 | Split by the exact search. |
| Under 256,000,000 | Split at anchors: lines that occur exactly once on each side, keeping the longest chain that stays in order on both sides, as patience diff does. Regions of at most 512 lines, both sides together, still get the exact search. |
| After that | Left as a plain change: every line deleted, then every line inserted. |

A unit of effort is one diagonal visited, one matched line followed, or one
frontier entry reset by the exact search. Anchoring charges three units per
line it counts and sixteen per candidate anchor, which makes a unit cost 1 to
1.5 ns on the development machine either way. The search therefore stops after
about 0.4 s, however far apart the states are.

Every diff is complete: applying its hunks to the left state gives the right
one. Only minimality is given up, and only after the exact effort runs out.

Measured on the development machine (AMD Ryzen AI Max+ 395, Go 1.27.1, five
runs each; the runs agreed within 3%):

| Comparison | Time |
| --- | --- |
| One changed line in a 1,000,000-line state | 6.4 ms |
| 3,000 interleaved moves, minimal diff | 11 ms |
| 100,000 interleaved moves | 143 ms |
| 200,000 unique lines in shuffled order | 182 ms |
| 200,000 lines drawn from four repeated values | 132 ms |

`go test ./internal/diff -bench BenchmarkCompare` reproduces these cases.

## Limits

A comparison fails only when it is cancelled or when either state has more
lines than the state line limit (4,000,000). Search work is bounded by effort,
and every other cost is linear in the size of the two states: trimming and
identifying read each line at most once, and the frontiers, identifiers,
anchoring counts, and script all hold one entry per line or less. The line
lookup table is an open-addressed array of line indexes, each slot tagged
with 32 bits of the line's hash, rather than a map of strings, so it gives the
garbage collector nothing to scan. Once the left-hand lines are in the table,
it no longer changes, and more than 131,072 right-hand lines are looked up on
up to eight goroutines, each owning one contiguous range of the result. Two
4,000,000-line states with the same lines in shuffled order compare in 1.5 s,
with a peak of 766 MiB for the whole command.

Cancellation is checked before each round of the exact search, before each
region, and every 1,024 lines while trimming and identifying.

## Output

The command prints a unified diff with three lines of context:

```text
--- node 2
+++ node 3
@@ -1,3 +1,3 @@
 package sample
 
-func experiment() int { return 42 }
+func chosen() int { return 1 }
```

- Headers name the node selectors, not file paths.
- Hunk ranges follow GNU `diff -u`. Line numbers are one-based, a one-line
  range omits its count, and an empty range names the line before it.
- Two changes separated by six or fewer unchanged lines share a hunk.
- Identical states print nothing. The exit status is 0 whether or not the
  states differ.
- Each line is escaped like `show` output: tabs and printable Unicode stay
  as they are, while controls and invalid bytes become Go-style escapes.
  Escaping never produces a newline, so recovered text cannot forge hunk
  structure.

The comparison finishes before anything is written, and the diff then
streams to stdout as it is rendered. Output is terminal-safe rather than
byte-exact, so it is not meant as input for `patch`. For exact bytes, export
each state with `show --raw`.
