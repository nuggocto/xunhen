# Compatibility

xunhen supports undo files written by one producer, the one its fixture
corpus was generated with. Every claim below names the tests that back it,
and those tests read checked-in fixtures, so `go test ./...` rechecks the
whole matrix without Neovim installed.

## The supported producer

| Field | Value |
| --- | --- |
| Executable | Arch Linux `neovim 0.12.5-1`, x86_64 |
| Executable SHA-256 | `5c2cd28eb9b608fff5ca07cd5ee0c31011a15b137b0e63513e253c2c3529bd7f` |
| Upstream source | [`5885a30e1e1225349079e7a1c4a3848aa8e43e42`][revision] (tag v0.12.5) |
| Arch packaging | `583b707757678d79349f2f6d7497a288aec5e61e`, a `RelWithDebInfo` build with no source patches |
| Profile | Undo format 3, Linux/amd64, little-endian LP64 |

[`testdata/undo/corpus.json`](../testdata/undo/corpus.json) records these
values and the SHA-256 of every fixture file. The fixture generator refuses
any other executable (`TestProducerPin`), and `TestReferenceCorpus` fails if
a stored fixture's bytes change.

### What this does not cover

Another build of Neovim 0.12.5 is a different producer until its own corpus
passes. Sharing a version string does not make a build compatible:
compilers, flags, and platforms change the native records that format 3
embeds. The same goes for other Neovim releases, other architectures, and
other operating systems. Vim's undo files, format 2, and encrypted undo
files are rejected outright.

An undo file does not identify its producer or ABI. xunhen reads every
format 3 file with the profile above and cannot tell a foreign build's file
from a supported one, so a foreign file may be rejected as malformed, or it
may decode. Base verification still applies: text that matches the file's
reference hash verifies, whatever produced the file. Recognizing the format
does not authenticate the file.

## Capabilities

| Capability | What it needs | Evidence |
| --- | --- | --- |
| Decode and inspect | Envelope, records, and native extmark records decode; the tree matches what Neovim's `undotree()` reported; malformed input fails with its offset and no partial history | `TestReferenceGraphs`, `TestDecodeRejectsMalformedInput`, `TestDecodeTruncations`, `TestDecodeLimitBoundaries`, `TestGraphValidation`, `FuzzDecode`, `FuzzHistory` |
| Load a base | The supported text profile below; the reference hash and line count match exactly | `TestFixtureBaseLoading` (every fixture), `TestBaseTextProfile`, `TestBaseBindingRejectsWrongInput`, `TestEmptyBaseHash` |
| Reconstruct | Every state Neovim recorded, line for line, from the verified reference | `TestReplayOracle` (every recorded state of every fixture), `TestReplayEntryBoundaries`, `FuzzReconstruct` |
| Preview and diff | The same states drawn with terminal controls escaped; diffs that rebuild the right-hand state | `TestPreviewsMatchNeovim`, `TestDiffRecovery`, `TestShowDisplayEscaping`, `TestHostileTextReachesTheTerminalEscaped`, `FuzzLines`, `FuzzClip` |
| Raw export | The recorded bytes of every exportable state, under the chosen final-newline policy | `TestCorpusThroughTheCommand` (every state of every supported fixture, both policies), `TestShowBaseAndSelectedTextPolicy`, `TestFinalNewlineOracle` |
| Discovery | Undo file names as Neovim derives them; a candidate counts only after its history verifies against the source | `TestNamesMatchNeovim` (19 recorded cases), `TestNamingCorpus`, `TestSearchSelection`, `FuzzNames`, `FuzzChoose` |

Replay runs below text policy. The replay tests check every fixture,
including those whose base the command refuses, because they feed Neovim's
recorded reference lines straight to the reconstructor. That shows the
format is understood, not that the command can load or export that text.
The text cases below say what the command does.

## Text and history cases

Supported means the command loads the base, reconstructs every recorded
state, and exports each state whose text is exportable. Rejected means the
command refuses with a named reason and writes nothing.

| Case | Fixture | Status | Behavior |
| --- | --- | --- | --- |
| Branching: an abandoned experiment | `abandoned-branch` | Supported | The experiment is recovered after another branch was saved |
| Branching at the retained root | `root-branches` | Supported | |
| Reference on an intermediate branch | `intermediate-branch` | Supported | The next-redo header's parent locates the reference |
| Current state is not the newest leaf | `undone-anchor` | Supported | |
| Pruned history | `pruned` | Supported | Node 0 is the oldest retained state, not the original file |
| Joined edits | `joined-edits` | Supported | |
| Line moves with extmark records | `move-lines` | Supported | Extmark records decode and are otherwise ignored |
| Repeated lines | `repeated-lines` | Supported | |
| Linear history | `linear` | Supported | |
| Save and reopen | `save-reopen` | Supported | |
| Empty buffer | `empty` | Supported | One empty line; `include` exports one LF, `omit` exports nothing |
| Explicit `:wundo` of an empty buffer | `empty-wundo` | Supported | Hashed with a NUL terminator, unlike the automatic writer |
| Explicit `:wundo` of unsaved text | `unsaved-wundo` | Supported | The base is the unsaved buffer text, not the file on disk |
| No final newline in the base | `no-final-newline` | Supported | The final newline is not recorded; export takes a policy |
| `'endofline'` changed during editing | `eol-option-change` | Supported | Undo restores lines, not the option; export takes a policy |
| Terminal controls in the text | `terminal-controls` | Supported | Display escapes them; raw export to a file writes them as recorded; raw export to a terminal is refused |
| Lone CR inside a line | synthetic | Supported | A line's content, exported as is |
| CRLF base | `crlf` | Rejected | `unsupported input: base text: CRLF base text`; inspect still works |
| UTF-8 BOM in the base | synthetic | Rejected | `unsupported input: base text: UTF-8 BOM` |
| Latin-1 base | `latin1` | Rejected | `unsupported input: base text: invalid UTF-8`; the undo file does not record the encoding |
| Invalid UTF-8 base | `invalid-utf8` | Rejected | `unsupported input: base text: invalid UTF-8` |
| Invalid UTF-8 in a reconstructed state | synthetic | Display only | Shown escaped; raw export refuses the state |
| Embedded NUL in the base | `embedded-nul` | Rejected | `unsupported input: base text: embedded NUL` |
| NUL in a reconstructed state | synthetic | Display only | Shown escaped; raw export refuses the state |

Synthetic cases edit a fixture or the base in a test, as
`TestBaseTextProfile` and `TestShowBaseAndSelectedTextPolicy` do.

### Unverified

Anything this page does not list is unverified. That includes every other
producer build, histories with more than one text encoding, and `'fileformat'`
values other than `unix`. Neovim does not record `'fileencoding'`,
`'fileformat'`, or `'endofline'` in the undo file, so a recovered state is
exact buffer lines, never the original file's bytes.

## Stability within v1

From version 1.0.0 on, these stay compatible for every 1.x release:

- The command names, their flags, and the two input forms: `--undo` with
  `--base`, and `--source` with `--undo-dir`.
- The exit statuses in [usage.md](usage.md#exit-status).
- The bytes `show --raw` writes for a given state and final-newline policy.
- The format of `diff`: unified hunks with three lines of context, headed
  `--- node FROM` and `+++ node TO`.
- The `node ID:` lines of `inspect` and the names of their fields. Other
  `inspect` lines are for reading; a later release may add lines or fields.
- Node IDs: the same unchanged undo file gives the same IDs.
- Refusing unsupported input, damaged input, and a mismatched base, rather
  than guessing.

A 1.x release may add commands, flags, supported producers, and text cases,
and may reword diagnostics; scripts should rely on the exit status, not on
diagnostic text. The browser's layout and keys may change between minor
releases. A change that breaks any listed contract needs version 2.0.0.

xunhen is a command, not a library. Every Go package is under `internal/`,
`cmd/`, or `tools/`, none of it is a public API, and it may change in any
release.

## Checking the corpus against the producer

On a machine with the pinned executable, regenerate the corpus and compare
it with the stored one:

```sh
sha256sum /usr/bin/nvim   # must print the executable SHA-256 above
go run ./tools/fixtures -nvim /usr/bin/nvim -out /tmp/xunhen-corpus
```

The generator checks the executable's hash and ABI, runs each case in a
private editor, and fails unless Neovim's own reading of each history
matches the expected text and ancestry. Undo files hold real timestamps, so
regenerated bytes differ from the stored ones; the oracle states and tree
shapes must not. Neovim is only the reference here. The tests replay the
stored files without it.

## Adding a producer

A new producer needs its own corpus: generate it with that executable, record
its build provenance, pass the whole matrix, and list it here. Until then it
stays unverified, even when its files happen to decode.

[revision]: https://github.com/neovim/neovim/tree/5885a30e1e1225349079e7a1c4a3848aa8e43e42
