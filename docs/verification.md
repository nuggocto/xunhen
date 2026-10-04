# Verification

This page maps each correctness, safety, and resource contract to the checks
that enforce it, and says how to run them. [Compatibility](compatibility.md)
covers which producer and text cases are supported, and
[Performance](performance.md) the workloads and measurements.

## Contracts and their checks

Each contract names the tests that fail when it breaks. A test counts only if
breaking the behavior it guards makes it fail; the mutations listed under
[Mutation checks](#mutation-checks) confirm that for the newer ones.

| Contract | Checks |
| --- | --- |
| Malformed input ends in an operational error, never a partial history or snapshot | `TestDecodeRejectsMalformedInput`, `TestDecodeTruncations` (every byte cut), `TestDecodeLimitBoundaries`, `TestDecodeReaderFailures`, `TestGraphValidation`, `TestReplayRejectsInvalidWork`, `TestReplayEntryBoundaries`, `FuzzDecode`, `FuzzHistory`, `FuzzReconstruct` (damaged fixtures) |
| Reconstruction matches independently recorded text | `TestReplayOracle`, `TestPreviewsMatchNeovim`, `TestCorpusThroughTheCommand`, `FuzzReconstruct` (generated histories with a model oracle) |
| A wrong base never becomes reconstruction input | `TestBaseBindingRejectsWrongInput`, `TestShowBaseAndSelectedTextPolicy`, `TestSearchSelection`, `TestCommandsLeaveInputsUnchanged` (mismatch cases), `TestReloadedSessionsShareNothing` |
| Failed or obsolete background work never replaces the current session or view | `TestWorkerKeepsOneRunningAndTheLatestPending`, `TestWorkerProtectsLoads`, `TestLateResultsAreDiscarded`, `TestEarlierLoadResultsAreDiscarded`, `TestReload`, `TestBrowserLeavesInputsUnchanged` (failed reloads) |
| Display output cannot carry terminal controls from input | `TestTerminalSafeText`, `TestDisplayText`, `TestClip`, `FuzzClip`, `TestShowDisplayEscaping`, `TestSearchDiagnosticsAreEscaped`, `TestUnknownArgumentsCannotControlTerminal`, `TestUntrustedTextCannotControlTheTerminal`, `TestUntrustedNoticesAndTimes`, `TestHostileTextReachesTheTerminalEscaped` |
| The screen measures text as the renderer does, and never with a column index built the other way | `TestClusterWidthsFollowTheRenderer`, `TestClip`, `FuzzClip` |
| No command changes its inputs | `TestCommandsLeaveInputsUnchanged`, `TestBrowserLeavesInputsUnchanged`, `TestSearchWritesNothing`, `TestExecutable`, `TestInspectReadOnlyAndSafeOutput` |
| Recovered text never reaches a diagnostic or a log | `TestCommandsLeaveInputsUnchanged`, `TestBrowserLeavesInputsUnchanged`, `TestBrowserUnderATerminal` (debugging variables) |
| Long work stops at every cancellation check, with no partial result | `TestWorkStopsAtEveryCancellationCheck`, `TestSearchStopsAtEveryCancellationCheck`, `TestDecodeReaderFailures`, `TestDecodeStopsAfterCancellation` (inside long lists of empty entries or lines), `TestHistoryBoundsAndCancellation`, `TestReplayCancellation`, `TestCancellation` (diff), `TestTreeIndexingStopsWhenCancelled`, `FuzzReconstruct` |
| Output failures end with status 1 and one diagnostic | `TestWriteFailures` (immediate and partway failures), `TestExecutable` (closed pipe, full device, interrupt while blocked) |
| Every exit from the browser restores the terminal and stops the worker | `TestBrowserUnderATerminal`, `TestWorkerShutdown` |
| Limits admit exactly their ceiling | `TestDecodeLimitBoundaries`, `TestReplayEntryBoundaries`, `TestSearchByteLimit`, `TestSearchDirectoryLimit`, `TestHistoryBoundsAndCancellation`, `TestReadFileAcceptsOnlyRegularFiles` |
| Resource use stays within the documented targets | [Performance](performance.md), `TestCacheHoldsTheLargestPair`, `TestCompareDoesNotCopyStates` |
| A source file's history is looked up only in directories the user named, with `--undo-dir` or `XUNHEN_UNDO_DIR` | `TestInputModeRules`, `TestSourceCommands` (FILE before and after the flags), `TestExecutable` (the variable, several directories, `--undo-dir` taking its place, and neither), and `tools/verify`'s walkthrough |
| The export command on screen names the same files once pasted into a shell | `TestShellQuoting`, `TestExportInstructions` (read back from the drawn screen under both width methods) |
| A release is exactly its commit, with the version its tag names | `TestResolveRefusesUnidentifiedSources`, `TestResolveIdentifiesTheCommit`, `TestEnvironmentRules`, `TestBuildEnvironmentIsExplicit`, `TestModuleRules` |
| A release, and the release tool's tests, read and write only the repository they were given, even with `GIT_DIR` or `GIT_INDEX_FILE` set by a hook or shell | `TestGitIgnoresAnotherRepositorysVariables` |
| A release reads the objects its commit ID names, even when `refs/replace` substitutes others | `TestResolveIgnoresReplacements` |
| The release tool's tests run no git hooks from the user's template or configuration | `TestRepoRunsNoHooks` |
| Release archives are reproducible and hold only regular files with fixed modes, owners, and times | `TestWriteIsDeterministic`, `TestWriteRejectsBadNames`, `TestReadRejectsUnsafeArchives`, `TestReadRejectsDamagedStreams`, `TestReadRejectsHiddenHeaders`, `tools/reproduce.sh` in CI |
| Every channel's executable passes the same artifact checks | `tools/verify` in CI, the release workflow, the Nix checks, and the AUR recipe's `check()`; `TestVerifierJudgesExecutables` and `TestVerifierJudgesArchives` confirm it fails wrong executables and damaged archives; `TestVerifierRefusesUnreadableArchives` that it refuses oversized files and FIFOs without blocking; `TestRunBoundsTheChild` that a run keeps its output and time limits when the child floods its output or leaves a process holding it open, and that no process the child started outlives the run |
| Every relative link in a document the binary archive ships leads to another file in the archive | `TestCheckDocs`; every release build, including CI's, runs the check on the commit's documents |
| The Arch package carries exactly the archive's documents, so the same links resolve | the release workflow's Arch job unpacks the built package and compares its `/usr/share/doc/xunhen` with the verified archive |

`TestCorpusThroughTheCommand`, `TestReplayOracle`, and `TestPreviewsMatchNeovim`
read the stored Neovim corpus, so every `go test ./...` runs the oracle
corpus. None of them starts Neovim.

The cancellation tests use `synth.Countdown`, a context that reports
cancellation from a chosen check onward. Each operation runs once to count
its checks, then again cancelled at the first, a middle, and the last one,
with no timing involved. How long cancellation takes is a measurement, not a
test; [Performance](performance.md) records it.

## Mutation checks

A new test earns its place by failing when the behavior it guards breaks.
Each of these changes was made to the code, the named test failed, and the
change was reverted:

| Change | Failing test |
| --- | --- |
| The decoder admits one more than a count limit | `TestDecodeLimitBoundaries`, 11 cases |
| Replay accepts a bottom two past the last line | `TestReplayEntryBoundaries` |
| Replay allows one line more than the state-line limit | `TestReplayEntryBoundaries`, including an intermediate state over the limit |
| Replay rejects a state exactly at the byte limit | `TestReplayEntryBoundaries` |
| Replay keeps an empty buffer at zero lines | `TestReplayEntryBoundaries` |
| Replay walks down to the target in the wrong order | `FuzzReconstruct`, within one second |
| Replay stops one change before the shared ancestor | `FuzzReconstruct`, within one second |
| A command writes a file beside its base | `TestCommandsLeaveInputsUnchanged` |
| A base-loading error quotes a line of the base | `TestCommandsLeaveInputsUnchanged`, three cases |
| Escapes are drawn as the raw bytes they stand for | `TestHostileTextReachesTheTerminalEscaped` |
| The browser passes `TEA_TRACE` to Bubble Tea | `TestBrowserUnderATerminal` |
| Long-line indexing ignores cancellation | `TestWorkStopsAtEveryCancellationCheck` |
| Raw export swaps the two final-newline policies | `TestCorpusThroughTheCommand`, all 47 states |
| A width report keeps drawing the state measured the old way | `TestClusterWidthsFollowTheRenderer`, the case with a state on screen |
| The content pane draws no lines | `TestClusterWidthsFollowTheRenderer`, both cases |
| The diff copies both states' line arrays again | `TestCompareDoesNotCopyStates`, both cases |
| Anchoring's one-byte line counts wrap past 255 copies | `TestRepeatedLinesAreNotAnchors`, all three cases |
| An output limit embeds `bytes.Buffer`, so `io.Copy` bypasses it | `TestRunBoundsTheChild` and `TestLimitedBufferThroughCopy`, the cases past the limit |
| A verifier run waits for a descendant holding its output | `TestRunBoundsTheChild`, the case of a descendant holding the output |
| A verifier run leaves the child's descendants running | `TestRunBoundsTheChild`, both descendant cases |
| The decoder checks cancellation only between records and line text | `TestDecodeStopsAfterCancellation`, both cases |
| A multi-line usage hint keeps status 2 when stderr fails | `TestWriteFailures`, the usage hint case |
| Page down moves by one line, or by one line less or more than a screen | `TestContentPaging`, the page down case |

## Running the checks

```sh
mise run check          # formatting, tests, vet, build
mise run race           # the tests under the race detector
mise run fuzz           # every fuzz target for about 10 seconds
mise run fuzz-extended  # every fuzz target for about 15 minutes
mise run vulncheck      # reachable known vulnerabilities
mise run notices        # THIRD_PARTY_NOTICES.txt matches the linked modules
mise run reproduce      # two clean release builds, compared byte for byte
mise run measure        # the resource workloads
```

CI runs these jobs on every push and pull request: correctness, which also
runs `tools/verify` against the build and checks the notices; race; fuzz
smoke; vulnerabilities; a reproducible release build, whose archive
`tools/verify` then checks; and `nix flake check`, whose sandboxed build
runs the whole suite before `tools/verify` checks the packaged executable
and a NixOS machine installs it. The
extended fuzz campaign and the measurements run locally, before a release.
[releasing.md](releasing.md) describes the release workflow's checks.

None of these checks skips when a tool is missing. The terminal tests fail
without a pseudo-terminal, the quoting tests without bash, the release-tool
tests without git, and the artifact verifier's tests without `/bin/sh`. Two
kinds of test
skip: the permission cases, when run as root, whom permission bits do not
restrict, and `TestMeasureWorkload`, which measures rather than checks and
runs only when `tools/measure.sh` points it at a workload.

### Fuzz campaigns

`tools/fuzz.sh` runs the eight targets one after another:

| Campaign | Per target | Workers | Minimization | Target timeout | Deadline | Data limit per process |
| --- | --- | --- | --- | --- | --- | --- |
| smoke | 10 s | 2 | 10 s | 2 min | 20 min | 2 GiB |
| extended | 15 min | 4 | 60 s | 20 min | 3 h | 4 GiB |

The data limit is `ulimit -d`, which Linux applies to a Go heap's writable
mappings. The fuzzing coordinator and each worker are separate processes, so
a campaign holds at most one limit per process. A failing input stays in the
target package's `testdata/fuzz` directory, where every later `go test`
replays it, and CI uploads it. The script continues past a failing target and
exits nonzero after listing every retained input.

### Campaign results

On 2026-09-26, on the reference machine in [Performance](performance.md),
both campaigns ran the uncommitted hardening changes and found no failure.
The smoke campaign took 83 s, and took 83 s again with no failure on
2026-09-27, after the fixes from review. The extended campaign took 2 h 0 min
of its 3 h deadline:

| Target | Inputs run in 15 min | Interesting inputs kept |
| --- | ---: | ---: |
| `FuzzDecode` | 208,559,322 | 108 |
| `FuzzHistory` | 112,947,096 | 66 |
| `FuzzText` | 110,244,401 | 157 |
| `FuzzReconstruct` | 99,258,610 | 217 |
| `FuzzLines` | 136,322,728 | 294 |
| `FuzzNames` | 209,271,702 | 77 |
| `FuzzChoose` | 210,531,220 | 59 |
| `FuzzClip` | 7,880,799 | 497 |

`FuzzClip` runs fewer inputs because each one measures and clips a line of
up to 16 KiB twice, with and without an index, and it spends stretches of
the campaign minimizing new inputs. The earlier browser work found two
defects this way, a width mismatch and a loop that stopped advancing; both
are fixed and kept as regressions: a stored input in
`internal/termtext/testdata/fuzz` and a case in `TestClip`.

## Corpus provenance

On 2026-09-26 the corpus was regenerated with the pinned producer, whose
executable matched the recorded SHA-256, and compared with the stored one.
All 20 fixtures matched in meaning: the producer record, every state Neovim
read back, the tree shapes without their timestamps, and the base and
initial texts. Only the undo files' bytes differed, because they hold the
time of each change. The built command then inspected every regenerated
history and exported all 47 distinct states of the 16 fixtures whose base
it supports, each identical to Neovim's reading. [Compatibility](compatibility.md#checking-the-corpus-against-the-producer)
has the commands.

## Audits

### Writes and file access

Every file the command opens goes through `internal/input`, with
`O_RDONLY`, `O_PATH`, or `O_DIRECTORY` and never a flag that can write,
create, or truncate. No production code calls a function that writes,
renames, removes, or changes the permissions of a file. The command writes
only to its standard output and error.

### Private content

Recovered text reaches only standard output, when a command was asked to show
it, and the browser's screen. Diagnostics name paths, fields, offsets, and
counts, never line content. Every panic message in production code carries
only IDs and counts.

Bubble Tea writes everything it draws to the file `TEA_TRACE` names, and
`TEA_DEBUG` makes it write panic logs into the working directory. The browser
removes both variables before Bubble Tea starts, so neither can copy
recovered text to a file; `TestBrowserUnderATerminal` checks that no trace
file appears.

## Dependencies

The command links the standard library and these modules, all MIT or
BSD-style licensed:

| Module | Version | License | Why |
| --- | --- | --- | --- |
| charm.land/bubbletea/v2 | v2.0.10 | MIT | Terminal lifecycle and input for the browser |
| github.com/charmbracelet/x/ansi | v0.11.7 | MIT | Cell widths measured as the renderer measures them |
| github.com/charmbracelet/ultraviolet | v0.0.0-20260703014108-f5a850f9c2b7 | MIT | Bubble Tea's renderer |
| github.com/charmbracelet/colorprofile | v0.4.3 | MIT | Bubble Tea |
| github.com/charmbracelet/x/term, termios, windows | v0.2.2, v0.1.1, v0.2.2 | MIT | Bubble Tea |
| github.com/clipperhouse/displaywidth | v0.11.0 | MIT | x/ansi |
| github.com/clipperhouse/uax29/v2 | v2.7.0 | MIT | x/ansi grapheme clusters |
| github.com/lucasb-eyer/go-colorful | v1.4.0 | MIT | Bubble Tea |
| github.com/mattn/go-runewidth | v0.0.23 | MIT | x/ansi |
| github.com/muesli/cancelreader | v0.2.2 | MIT | Bubble Tea input |
| github.com/rivo/uniseg | v0.4.7 | MIT | Bubble Tea |
| github.com/xo/terminfo | v0.0.0-20220910002029-abceb7e1c41e | MIT | Bubble Tea |
| golang.org/x/sync, golang.org/x/sys | v0.21.0, v0.46.0 | BSD-style | Bubble Tea |

`THIRD_PARTY_NOTICES.txt` reproduces each of these modules' license files and
the Go distribution's. `tools/release.sh notices` generates it from the
modules linked into the built executable, so a module only a test or tool
imports never appears; CI and every release build fail when it is stale.
The release archive and the Nix package, under `share/doc/xunhen`, install
it beside `LICENSE`; the Nix checks fail if either file is missing. The Arch
package installs both under `/usr/share/licenses/xunhen` and links them from
`/usr/share/doc/xunhen`.
`go mod verify` checks every module against `go.sum` in CI.

`tools/vulncheck` is a separate module that pins govulncheck v1.8.0 and its
dependencies by checksum, so the vulnerability check never downloads an
unreviewed version. It analyzes the source for the release configuration,
Linux/amd64 without cgo, and reports only vulnerabilities in code the command
can reach.

Go 1.27.1 was the newest release of the 1.27 line when these checks ran.
Before packaging, check for a newer patch release and update `go.mod`,
`mise.toml`, and CI together.
