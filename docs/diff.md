# Line diff

`internal/diff` compares buffer lines by their bytes, before terminal
escaping. It uses the linear-space search in Eugene W. Myers, "An O(ND)
Difference Algorithm and Its Variations", *Algorithmica* 1 (1986), section 4b.
Replay is described in [undo-format.md](undo-format.md).

## Search

The comparison trims equal prefixes and suffixes, assigns integer identities
to distinct lines, and filters lines that appear on only one side. Myers'
search then splits the remaining regions using two frontiers. An explicit
stack replaces recursion. Matched lines map back to their original positions;
within a change, deletions precede insertions. Equal inputs give the same result.

Very different states make exact search expensive, so work is bounded:

| Effort spent | Strategy |
| --- | --- |
| Under 64,000,000 | Exact search for a shortest edit script |
| Under 256,000,000 | Patience-style anchors: lines unique on each side, in their longest shared order; regions of at most 512 lines still use exact search |
| After that | Delete and insert the remaining region |

Exact search charges each diagonal visited, matching line followed, and
frontier entry reset. Anchoring charges three units per line counted and
sixteen per candidate anchor. Every result remains complete: applying its
edits to the left state gives the right state. Only minimality is given up.

Each state is limited to 4,000,000 lines. The remaining work and workspace
are linear in the input size. The line lookup table stores indexes and
32-bit hash tags instead of strings; large right-hand inputs are looked up
on at most eight goroutines. Cancellation is checked between search rounds,
between regions, and every 1,024 lines while trimming and identifying.

## Output

The command writes unified hunks with three lines of context and headers
`--- node FROM` / `+++ node TO`. Ranges follow GNU `diff -u`; changes separated
by six or fewer unchanged lines share a hunk. Identical states print nothing,
and differing states still exit 0.

Final-newline presence is unknown and never reported. An empty buffer is one
empty line. Output escapes controls and invalid bytes, so it is for reading,
not for `patch`; export each state with `show --raw` for exact buffer text.
Comparison finishes before output starts, then rendering streams to stdout.

`go test ./internal/diff -bench BenchmarkCompare` exercises the search.
[performance.md](performance.md) records whole-command measurements.
