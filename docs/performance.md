# Performance

These are measurements of xunhen on synthetic workloads, taken on one named
machine, and the targets they are held to. They are project targets for
that machine, not guarantees for every input or computer. The raw samples
are in [`measurements/2026-09-27`](measurements/2026-09-27), with the
environment they ran in and a summary of every operation.

## Reference machine

| | |
| --- | --- |
| CPU | AMD Ryzen AI Max+ 395, 32 threads, `performance` governor and platform profile, on mains power |
| Memory | 62.1 GiB |
| System | Omarchy 4.0.4, Linux 7.2.5 x86_64 |
| Toolchain | Go 1.27.1 |
| Build | `GOOS=linux GOARCH=amd64 GOAMD64=v1 CGO_ENABLED=0 go build -trimpath`, as releases are built |
| Executable | SHA-256 `a1f0a93a9eaf6eb03aaa369e8f8a3c95b757d4fb3cc556c8464cd3ac424c54b4` |

## Workloads

`tools/workload` generates every input from a versioned recipe and a seed,
and writes a manifest with the counts below, the SHA-256 of the undo file
and the base, and the digest of several states whose text the generator
knows by construction. Before anything is timed, the runner exports each of
those states with the built command and compares the digests, so a fast
wrong answer fails the run. These histories test cost, not compatibility:
only the Neovim corpus establishes what a real producer writes.

| Recipe | Version | Seed | Nodes | Entries | Stored lines | Reference lines | Longest line | Undo file |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `small` | 1 | 1 | 41 | 40 | 72 | 219 | 66 B | 20 KiB |
| `ordinary` | 1 | 2 | 1,001 | 1,000 | 3,089 | 3,004 | 69 B | 0.5 MiB |
| `deep` | 1 | 3 | 150,001 | 150,000 | 149,213 | 743 | 74 B | 66.6 MiB |
| `wide` | 1 | 4 | 60,001 | 60,000 | 60,253 | 31 | 72 B | 26.6 MiB |
| `shuffled` | 1 | 5 | 2 | 1 | 4,000,000 | 4,000,000 | 12 B | 61.0 MiB |
| `repeated` | 1 | 6 | 2 | 1 | 1,000,000 | 1,000,000 | 7 B | 10.5 MiB |
| `replaced` | 1 | 7 | 2 | 1 | 1,000,000 | 1,000,000 | 14 B | 15.3 MiB |
| `changes-limit` | 1 | 8 | 560,001 | 560,000 | 560,000 | 100 | 76 B | 249.6 MiB |
| `entries-limit` | 1 | 9 | 2 | 1,000,000 | 0 | 4,000,000 | 12 B | 17.2 MiB |
| `lines-limit` | 1 | 10 | 2 | 1 | 1 | 3 | 16 MiB | 16.0 MiB |

`small` and `ordinary` are the everyday cases: a short session, and a
thousand changes to a 3,000-line file with occasional branches. `deep` is
100,000 changes in a row followed by 50,000 alternatives nested one inside
the next, and `wide` is 50,000 alternative first changes. `shuffled`,
`repeated`, and `replaced` are the diff's hard cases: every line moved, few
unique lines to anchor on, and change everywhere. The `-limit` recipes each
put one ceiling at or near its limit while respecting all the others:
`changes-limit` fills 97.5% of the 256 MiB undo input, `entries-limit`
reaches both the entry limit and the 4,000,000-line state limit, and
`lines-limit` holds three lines of the maximum 16 MiB.

## Method

`tools/measure.sh` builds the executables before any timing, records the
environment, regenerates the workloads, and runs two kinds of measurement:

- **The built command, end to end.** `inspect`, `show`, and `diff` run as a
  script would, output discarded. The browser runs on a pseudo-terminal:
  time from start until the first state is drawn, the peak resident memory
  over a comparison and two reloads, and the time to exit after `q` while a
  comparison runs. The runner sends `q` once the pending comparison is on
  screen, and counts the sample as an exit during work only if the finished
  comparison never reached the screen; Bubble Tea draws the last view again
  as it exits, so a comparison that finished first would show there. On the
  four branching workloads the comparison always finishes first, so they
  have no such samples.
- **The browser's operations in-process.** A test binary built with the same
  flags runs the worker's operations directly and records wall time and
  allocation: loading, previews from a cold and a warm cache, comparisons,
  and how long an operation takes to return when cancelled halfway through
  it. It also records the live heap after a comparison and during a reload.

"Cold file cache" samples first evict the inputs from the page cache with
`posix_fadvise`; the others read them from memory. "Uncached" means a cold
application cache, which is separate. The four branching workloads get 20
samples an operation, enough for a nearest-rank 95th percentile; the others
get 5, reported as median and slowest only.

Peak memory is exact for the browser, read from `VmHWM` in `/proc` while it
runs. For the other commands it comes from `wait4`, which on Linux cannot
report less than the runner's own peak, about 12 to 16 MiB, because a child
begins as a copy of its parent. Values at that floor mean "at most".

## Targets

| Operation | Target | Measured on `ordinary` | Status |
| --- | --- | --- | --- |
| Load, start to first drawn state | p95 under 500 ms | 20.0 ms end to end; 1.7 ms of it is loading | Met |
| Uncached preview | p95 under 100 ms | 50 µs | Met |
| Cached selection | p95 under 50 ms | 1 µs, plus at most one 16 ms frame to draw | Met |
| Comparison, displayed result prepared | p95 under 250 ms | 237 µs | Met |
| Cancellation of active work | under 100 ms | 6 µs; 28.5 ms slowest of any workload | Met |
| Exit during active work | under 250 ms | none on `ordinary`, whose comparison ends first; 17.8 ms slowest of any workload | Met |
| Peak resident memory | under 1 GiB for every workload | 820 MiB worst, on `shuffled` | Met |

The first four targets are interaction budgets. A browser that answers a key
within about 100 ms feels immediate, and 500 ms is the longest a user should
wait for a history to open. The ordinary workload is well inside them, so
these targets carry little risk; they are there to catch a regression by an
order of magnitude.

The near-limit workloads have their own targets, because a single operation
on them is large by construction. Each must still stop within 100 ms when
cancelled and exit within 250 ms, so the browser stays responsive however
long a request would take.

| Workload | Target | Measured |
| --- | --- | --- |
| Any near-limit workload | first state drawn within 1 s | 487 ms slowest (`changes-limit`, cold file cache) |
| Any near-limit workload | uncached preview within 500 ms | 145 ms slowest (`entries-limit`) |
| Any near-limit workload | comparison within 2 s | 835 ms slowest (`shuffled`) |
| Every workload | peak resident memory under 1 GiB | 820 MiB (`shuffled`), 762 MiB (`changes-limit`), 726 MiB (`entries-limit`) |

## Results

| Workload | First state | Uncached preview | Comparison | Cancelled halfway | Exit while comparing | Browser peak |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `small` | 19.8 ms | 14 µs | 54 µs | 2 µs | none | 17 MiB |
| `ordinary` | 19.7 ms | 26 µs | 172 µs | 3 µs | none | 21 MiB |
| `deep` | 120.1 ms | 7.8 ms | 8.9 ms | 7 µs | none | 232 MiB |
| `wide` | 53.3 ms | 15 µs | 44 µs | 4 µs | none | 110 MiB |
| `shuffled` | 387.0 ms | 48.3 ms | 817.2 ms | 24.5 ms | 7.7 ms | 820 MiB |
| `repeated` | 119.3 ms | 12.0 ms | 158.5 ms | 16 µs | 2.1 ms | 282 MiB |
| `replaced` | 119.6 ms | 14.2 ms | 89.6 ms | 14 µs | 2.1 ms | 427 MiB |
| `changes-limit` | 437.3 ms | 101.1 ms | 114.0 ms | 27 µs | 3.0 ms | 762 MiB |
| `entries-limit` | 253.3 ms | 135.0 ms | 198.7 ms | 6 µs | 2.9 ms | 726 MiB |
| `lines-limit` | 169.9 ms | 109.2 ms | 217.2 ms | 221 µs | 3.0 ms | 366 MiB |

Medians, from a warm file cache. "First state" is end to end: start, load,
first preview, and drawing. Preview and comparison times are the worker's,
from a cold application cache; a cached preview took at most 5 µs on every
workload. "None" under exit while comparing means every comparison finished
before the quit took effect, and those exits, which had no work to stop,
took at most 2.8 ms. The [summary](measurements/2026-09-27/summary.md) has every
operation with its sample count, 95th percentile where there are enough
samples, slowest sample, and allocations, and the command timings.

The command-line tools follow the same pattern. `inspect` of the 250 MiB
`changes-limit` file takes 542 ms and 338 MiB, and `diff` of the shuffled
pair takes 1.32 s and 785 MiB.

## Where the memory goes

The peak comes from comparing the two shuffled 4,000,000-line states and
then reloading, and every part of it has a place:

| Holder | Size | What it is |
| --- | ---: | --- |
| The load | about 250 MiB | 4,000,000 decoded lines, each its own allocation with a 16-byte header, and the base: its file read once and split into 4,000,000 more headers that share it |
| Graph and tree index | under 1 MiB | Two nodes here; 24 bytes per node in general |
| Cache | about 122 MiB | Both states' line arrays, 16 bytes per line, charged against the 128 MiB budget |
| Comparison on screen | a few MiB | Its hunks, which refer to the cached states' line arrays rather than copies of them |
| Diff workspace, while it runs | about 150 MiB | While matching, a hash table of 8,388,608 slots and identifier arrays over both sides; then, with the table released, the two search frontiers, index arrays, and a count per distinct line |
| Replay workspace, while it runs | about 120 MiB | A copy of the base's line headers in chunks, and the finished state's array |
| A reload | about 250 MiB more | The old load stays until the new one validates; the browser drops the cache and the displayed comparison first |
| Queued work | under 1 KiB | At most one pending request, a few words; a finished result waiting for the view holds a state or comparison already counted above |

The in-process measurement found 360 MiB live after the comparison and 596
MiB live with a second load held as well. The rest of the 820 MiB peak is
garbage not yet collected and runtime overhead: the command sets a 768 MiB
soft limit on the Go heap, which is not a cap on resident memory. The cache
itself stays within its budget; it is one of the smaller holders.

Fewer cores raise the peak. The collector gets less time beside the work,
so garbage piles up further before it is swept. Until 2026-09-27 the diff
copied both line arrays, 122 MiB at the limit, and grew its matching arrays
by doubling, leaving about 750 MiB live against the 768 MiB soft limit. That
peaked at 923 MiB here and at 1085 MiB on a four-core CI runner, over the
target. The diff now reads the states' own arrays and sizes its workspace
once, and with this machine limited to four cores the same session peaks at
783 to 830 MiB.

The largest remaining holders are the two loads a reload keeps side by
side, which a failed reload needs, and the replay's copy of the base, which
replay rewrites in place. Those are the places to look if a workload ever
approaches the target again.

## Reproducing

```sh
mise run measure                         # everything, into .local/measurements/DATE
go run ./tools/workload list             # recipes and what each is for
go run ./tools/workload generate -out DIR shuffled
go run ./tools/workload run -bin PATH -dir DIR/shuffled -samples 5 -out results.jsonl
go run ./tools/workload summarize results.jsonl
```

Each process in a measurement run may grow its data segment to 1.5 GiB, so a
regression past the target fails the run instead of exhausting the machine.
Compare timings only against this machine; another computer's numbers show
whether something broke, not whether a target is met.
