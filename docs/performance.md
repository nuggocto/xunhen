# Performance

These measurements use synthetic workloads on one development machine.
They are a regression baseline, not guarantees for every computer or input.
The [raw samples, environment, and full summary](measurements/2026-10-05)
record the run.

## Environment and method

- AMD Ryzen AI Max+ 395, 32 threads, 62.1 GiB RAM, on mains power with the
  `performance` governor and platform profile.
- Omarchy 4.0.4, Linux 7.2.5 x86-64, Go 1.27.1.
- The `v1.0.0-rc.1` binary archive's executable, SHA-256
  `2315888b811f6c1fa300207a47829544953741f2a87832ba4c9f36696a510c8d`,
  built for Linux/amd64 with `GOAMD64=v1`, cgo off, and `-trimpath`.

`tools/workload` generates inputs from versioned recipes and seeds. Before
timing, the runner exports known states and compares their digests. These
workloads measure cost; the Neovim fixture corpus establishes compatibility.

Command timings include startup, loading, and output (discarded). The
browser runs on a pseudo-terminal; its first-state timing includes drawing.
Worker previews and comparisons are also measured in-process, with a cold
application cache. The four branching workloads have 20 samples per operation;
the others have five, so only medians and slowest samples are reported for them.
Cold file-cache runs evict the inputs with `posix_fadvise`.

Browser peak RSS comes from `/proc`'s `VmHWM`. Command RSS comes from `wait4`,
which has a floor of about 12–16 MiB inherited from the runner. Cancellation
is measured halfway through work. Exit-during-work samples count only when
the comparison has not finished before quit takes effect.

## Results

Medians from a warm file cache. Preview and comparison use a cold application
cache. "None" means every comparison finished before quit took effect.

| Workload | First state | Uncached preview | Comparison | Cancelled halfway | Exit while comparing | Browser peak |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `small` | 20.0 ms | 10 µs | 34 µs | 2 µs | none | 17 MiB |
| `ordinary` | 19.8 ms | 32 µs | 224 µs | 3 µs | none | 22 MiB |
| `deep` | 120.3 ms | 7.8 ms | 10.1 ms | 9 µs | none | 187 MiB |
| `wide` | 53.3 ms | 13 µs | 30 µs | 3 µs | none | 105 MiB |
| `shuffled` | 453.5 ms | 48.6 ms | 835.4 ms | 196 µs | 7.9 ms | 818 MiB |
| `repeated` | 119.0 ms | 11.8 ms | 165.7 ms | 20 µs | 2.2 ms | 278 MiB |
| `replaced` | 119.8 ms | 12.3 ms | 98.8 ms | 41 µs | 1.7 ms | 423 MiB |
| `changes-limit` | 468.9 ms | 137.3 ms | 160.1 ms | 47 µs | 1.9 ms | 739 MiB |
| `entries-limit` | 253.0 ms | 135.6 ms | 206.9 ms | 7 µs | 15.1 ms | 759 MiB |
| `lines-limit` | 185.9 ms | 116.7 ms | 228.0 ms | 443 µs | 2.1 ms | 313 MiB |
| `empty-lines` | 136.6 ms | 24.0 ms | 51.9 ms | 4.8 ms | 2.8 ms | 416 MiB |

`ordinary` has 1,000 changes to a 3,000-line file. `deep` and `wide` stress
nested and sibling branches. `shuffled` compares two 4,000,000-line states;
`repeated` and `replaced` stress repeated values and full rewrites. The
`-limit` recipes approach individual input ceilings. `empty-lines` has
4,000,000 stored empty lines. The summary records counts, seeds, allocations,
sample counts, and percentiles for each operation.

The ordinary-workload p95 targets are 500 ms to first state, 100 ms for an
uncached preview, 50 ms for cached selection, and 250 ms for comparison.
Near-limit targets are 1 s to first state, 500 ms for preview, and 2 s for
comparison. Every workload must cancel within 100 ms, quit during work
within 250 ms, and stay below 1 GiB RSS. This run met those targets;
[summary.md](measurements/2026-10-05/summary.md) has the supporting samples.

## Memory

The largest peak, 818 MiB, comes from comparing shuffled states and reloading.
Each load holds about 250 MiB of decoded lines, base bytes, and line headers.
The cache holds about 122 MiB for the pair's line arrays, within its 128 MiB
budget. Diff workspace needs about 150 MiB; replay needs about 120 MiB.
Reload keeps the old load until the new one validates, but releases cached
states and the displayed comparison first.

Live heap was 360 MiB after comparison and 596 MiB with a second load held.
Garbage awaiting collection and runtime overhead account for the rest. The
768 MiB Go heap soft limit does not cap RSS. With this machine limited to
four cores, the same session peaked at 783–830 MiB.

## Reproduce

```sh
mise run measure                         # writes .local/measurements/DATE
go run ./tools/workload list
go run ./tools/workload generate -out DIR shuffled
go run ./tools/workload run -bin PATH -dir DIR/shuffled -samples 5 -out results.jsonl
go run ./tools/workload summarize results.jsonl
```

The runner bounds each process's data segment at 1.5 GiB. Compare timings
against runs on the same machine; measurements elsewhere need their own baseline.
