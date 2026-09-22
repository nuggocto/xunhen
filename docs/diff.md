# Line diff

`xunhen diff` compares two reconstructed states of one history. This page
describes the algorithm in `internal/diff`, what its budgets count, and the
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

The search is the greedy forward algorithm from Eugene W. Myers, "An O(ND)
Difference Algorithm and Its Variations", *Algorithmica* 1 (1986). It finds
an edit script with the fewest deletions plus insertions. Five steps run in
order:

1. **Trim.** Drop the common prefix and suffix. Most comparisons between
   branches of one file differ in a small region, and this step leaves only
   that region.
2. **Identify.** Give each distinct line in the region a small integer, so
   the search compares integers instead of strings.
3. **Filter.** Remove lines that occur on only one side. They cannot belong to
   any common subsequence, so removing them does not change the result, and a
   rewrite that introduces fresh text costs almost nothing.
4. **Search.** Round `d` records the furthest point each diagonal `k` in
   `[-d, d]` reaches with `d` edits. Only points inside the edit grid count.
   When two moves reach the same point, the insertion wins, as in the paper.
   The search ends when the diagonal `n - m` reaches the corner.
5. **Backtrack.** The stored rounds give the path back from the corner without
   a second search. Matched lines map back to their original positions.

Within each stretch of changes, every deleted line comes before every
inserted line, whichever order the search took. Equal inputs always produce
the same script.

The search keeps every round, so its memory grows with the square of the edit
distance `D`: round `d` stores `2d + 1` four-byte entries, `4(D + 1)²` bytes in
all. The workspace budget therefore stops the search at about 2,900 edits,
counted after the trim and filter steps. The step budget, about `D²/2`
diagonal visits, would allow about 4,400. Myers' linear-space variant would
drop the stored rounds and reach that higher ceiling, at the cost of a
recursive middle-snake search that is harder to verify. Two branches of one
source file rarely differ by thousands of lines, so the simpler version comes
first. Revisit it if measured histories hit the workspace budget.

The lookup table sets a second ceiling. At 64 bytes per distinct left-hand
line, a changed region with more than about 450,000 distinct lines exceeds the
workspace budget, even when the filter would leave nothing to search. A small
change inside a large state is unaffected, because trimming removes the
unchanged lines before the table is built. On the development machine, a
2,800-edit comparison took 12 ms and one change in a million-line state took
3 ms.

## Budgets

Every budget is charged before its work, and an exhausted budget returns an
error naming it. No partial diff is ever returned.

| Budget | Default | What it counts |
| --- | --- | --- |
| `diff steps` | 10,000,000 | Diagonals visited plus matched lines followed during the search |
| `diff workspace bytes` | 32 MiB | Line identifiers, kept-line indexes, stored rounds, script runs, and an estimated 64 bytes per distinct line for the lookup table |
| `diff compared bytes` | 256 MiB | Line bytes read by prefix and suffix comparisons and by identification |
| `state lines` | 1,000,000 | Lines on either side |

Each line is identified once, and trimming reads each line at most once, so
two 16 MiB states read at most about 64 MiB. The compared-bytes budget only
binds when a caller lowers it.

Cancellation is checked before each search round and every 1,024 lines while
identifying.

The workspace budget does not cap process memory. Go's map, slice, and
garbage-collector overhead come on top of it.

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

The whole diff is rendered before anything is written, within the 16 MiB
output budget. Output is terminal-safe rather than byte-exact, so it is not
meant as input for `patch`. For exact bytes, export each state with
`show --raw`.
