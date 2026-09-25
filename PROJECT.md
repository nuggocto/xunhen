# Xunhen 

**Seek traces.** Browse the editing history that survived in Neovim undo files.

Neovim can save undo history between editing sessions. That history branches:
undo a change, type something different, and the abandoned work can remain on
another branch. An undo file may therefore contain code that was never saved
as source text and never committed to git.

xunhen will make that history inspectable from a standalone Go program. Start
with commands that describe a history, reconstruct a selected state, and diff
two states. Then add a terminal interface for exploring the tree.

This document covers the build from an empty Go module through a Linux v1.0.0
release, followed by a separate post-v1 roadmap. It is a plan, not a description
of implemented functionality. Command examples and package names are proposed
contracts until their implementation and verification tasks are complete.

Jump to the [v1 contract](#what-counts-as-v1),
[architecture](#architecture-and-data-flow),
[domain types](#core-domain-types-and-representations),
[build and release checklists](#build-and-release-plan), or
[post-v1 roadmap](#after-v1).

## What can actually survive

The relevant history must have reached an undo file, either through automatic
persistence or an explicit undo-file write. Automatic persistence requires
`'undofile'` and happens when the source file is written. Undo history has
retention limits; an edit made after the last undo-file write may exist only
in the running editor. Undo blocks also group changes rather than recording an
independently accessible state for every keystroke.

The promise is access to **retained, persisted undo history**, not every state
that ever existed. Timestamps describe recorded history events; they are not
proof of a complete editing timeline.

The undo directory is configurable through `'undodir'`.
`~/.local/state/nvim/undo/` is a common location, not a universal path. The first
CLI takes an explicit undo-file path. By v1, discovery also accepts explicitly
supplied undo directories; neither workflow guesses the user's editor setup.

### Something git cannot show

Suppose `retry.go` contains the version committed yesterday:

1. You write an experimental backoff implementation in Neovim.
2. You undo that experiment without saving it, staging it, or stashing it.
3. You write a smaller fix on the new branch and save the file.
4. Neovim persists an undo tree that still includes the abandoned experiment.

Git can show yesterday's commit and, after a commit, the smaller fix. It has no
record of the experiment in this example: the text never entered git's object
database. xunhen should let you select the abandoned branch, inspect the
experiment, and compare it with the smaller fix.

That recovery depends on the branch surviving and on enough information being
available to reconstruct it. An undo file is not assumed to be a self-contained
archive of complete buffer snapshots.

## Scope and technical choices

The v1 product handles one history at a time, for ordinary source files.
Its operations are inspection, reconstruction, line-based diffing, terminal
navigation, and locating a history in explicitly supplied undo directories.
Cross-referencing with git and searching many histories come after v1.

Decisions made for the starting design:

| Area | Decision |
| --- | --- |
| Language | Go; standard library first. |
| Initial environment | Linux/amd64, with local Go 1.27.1 and Neovim 0.12.5 as the observed development baseline. |
| Release platforms | Linux only. The v1 binary target is Linux/amd64; Linux/arm64 is a post-v1 candidate. No macOS or Windows releases are planned. |
| Distribution channels | GitHub release archives, a tagged Nix package usable on NixOS, and an AUR package are required for v1. |
| Public website | A separate static Astro site in `../xunhen-front`, hosted on Cloudflare Pages at `https://xunhen.org`. |
| Input | An explicit undo-file path and, when needed, an explicit base-text path. |
| Access | Read inputs only. The parser receives a reader, not filesystem write capabilities. |
| First interface | `inspect`, `show`, and `diff` commands. |
| TUI | Bubble Tea, after the CLI can recover and compare states. |
| Diff | A small internal line-diff implementation with bounded work and memory. |
| Damaged input | Strict reconstruction. Report errors; do not guess missing records or relationships. |
| Storage | In-memory histories and bounded caches; no database or persistent index initially. |
| Development fixtures | Opt-in headless Neovim generation in an isolated temporary environment. Ordinary tests consume checked-in synthetic fixtures. |

The installed version strings identify a starting environment, not a verified
compatibility matrix. Record the actual Neovim build provenance when generating
fixtures. The executable uses the standard library, Bubble Tea v2.0.10 for the
terminal browser, and Bubble Tea's own x/ansi package to measure terminal cells
the way its renderer does. The selected Go minimum and CI toolchain are 1.27.1,
recorded in `go.mod`.

Bubble Tea is justified by terminal lifecycle handling, input processing, and
its model/update/view structure. Its transitive dependencies still count toward
the dependency budget. Additional component or styling packages need a named
use. The initial core should not need a CLI framework, database driver, git
library, or diff dependency.

xunhen does not modify undo files or source files, restore changes into an
editor, provide an editor plugin, or require a running Neovim instance. Reading
an explicitly supplied source file or copy as reconstruction input is allowed.
The shipped program never launches Neovim.

## What counts as v1

v1 is a usable, documented Linux release with a tested recovery path. A working
parser or an attractive TUI alone is not enough. The release must satisfy this
contract:

| Area | Required for v1 |
| --- | --- |
| Recovery | Inspect a persisted branching history, bind a matching base, reconstruct retained states on different branches, and export verified text to redirected stdout. |
| Interfaces | Working `inspect`, `show`, `diff`, and `browse` commands, with consistent help, diagnostics, and exit codes. |
| Discovery | Resolve a source file against explicitly supplied undo directories with bounded search, visible ambiguity, and an explicit-path fallback. |
| TUI | Keyboard tree navigation, state preview, choosing two states to compare, scrolling, help, cancellation, and clean terminal restoration. |
| Compatibility | At least the source-verified initial Neovim producer/build, with every advertised format and feature covered by fixtures. |
| Text | Exact buffer-line reconstruction for the supported UTF-8/LF profile, including empty buffers. Export uses an explicit final-newline policy; historical encoding/newline options are not recoverable from the undo file. Publish rejected and unverified text cases. |
| Reliability | Bounded input and work, errors instead of input-triggered panics, strict base matching, safe terminal output, and unchanged input contents. |
| Distribution | A tested Linux/amd64 archive, a NixOS-compatible Nix package, and a maintained AUR recipe, with checksums, build provenance, license material, release notes, and installation instructions for each channel. |
| Website | A live, accessible landing page and an in-site Changelog tab at `https://xunhen.org`, sharing the same visual design, with complete release notes and verified Linux download, NixOS, and AUR installation links. |
| Maintenance | Repeatable CI and release commands, a compatibility policy, a regression corpus, and instructions for reporting problems without sharing private undo history. |

If source research shows that the minimum recovery or text contract cannot be
met, stop and revise the product contract before implementation proceeds. Do
not quietly replace full-state recovery with fragments and call it v1.

### Linux support and distribution

The mandatory v1 target is `GOOS=linux`, `GOARCH=amd64`, and `GOAMD64=v1`.
Use `CGO_ENABLED=0` for the release build and verify the resulting binary has
no unexpected dynamic-loader or shared-library requirement. The archive should
run without Go, Neovim, git, or extra application data installed.

Validate on pinned Debian stable and Ubuntu LTS environments, plus a pinned
Alpine environment to catch accidental libc dependencies. Record the exact
distro, kernel, and terminal versions tested. Containers test userland
compatibility but share the host kernel; use a VM or native system for the
oldest claimed kernel baseline. The development machine provides an additional
rolling-Linux check, not a substitute for release testing.

NixOS and Arch Linux are also required release-test environments. Test the Nix
package in a pinned NixOS VM and build/install the AUR package through a clean
Arch packaging environment. A passing generic Linux archive test does not
verify either package-manager integration.

Publish through GitHub Releases for `nuggocto/xunhen`. The initial asset is
`xunhen_1.0.0_linux_amd64.tar.gz`, accompanied by `SHA256SUMS.txt`, release notes,
and build/dependency provenance. Installation is extraction plus placing the
binary on `PATH`, with a user-local option that does not require root. Document
verification, upgrade, downgrade, and uninstall. Source installation uses the
tagged `github.com/nuggocto/xunhen/cmd/xunhen` package.

For NixOS, ship a source-built `buildGoModule` derivation in `nix/package.nix`
and a repository `flake.nix` with committed `flake.lock`. Expose the executable
as `packages.x86_64-linux.xunhen` and the default package, with matching runnable
app outputs. Pin the nixpkgs input and Go dependency hash, and choose a builder
whose Go toolchain satisfies the module's supported version. The tagged flake
must support a sandboxed build, running the command, and installation through
NixOS `environment.systemPackages`. Document flake prerequisites, user-profile
installation, system installation, updates, rollback, and removal. A CLI
package does not need a NixOS service module. Inclusion in upstream nixpkgs is
a possible later contribution; v1 support is delivered by the project flake.

For the AUR, the planned v1 package is `xunhen`, built from a checksummed stable
source archive for Arch `x86_64`. Check package-name availability and coordinate
with an existing maintainer if necessary. Keep the recipe template under
`packaging/aur/`, then publish a resolved `PKGBUILD` and generated `.SRCINFO` to
the separate AUR packaging repository. AUR hosts build recipes, not uploaded
application binaries. A later recipe that installs the prebuilt GitHub binary
must use the `xunhen-bin` name rather than masquerade as the source package.

The Nix and AUR builds must identify the same application release and pass the
same behavioral corpus as the archive. Their package-manager build inputs may
differ, so record their toolchains and provenance separately; matching binary
hashes across those channels are not the compatibility test. Each channel needs
a working install/update/remove path before v1 is complete.

Other distro packages, automatic updates, and additional CPU architectures
are not prerequisites for v1. macOS and Windows are outside the release roadmap,
including the post-v1 work at the end of this file.

### Public website

`../xunhen-front` is a separate repository for the public Astro site. It
currently contains a README and license; application setup is still work to
do. Keep its Git history, license, and `shrek` default branch intact. This
project plan tracks the coordinated work, while the site repository owns its
source, build configuration, and deployment instructions.

The v1 site has two main views, reached through **Home** and **Changelog**
navigation tabs:

- `/`: explain xunhen, show the abandoned-code example with a synthetic TUI
  image or recording, state recovery limitations, and provide Linux archive,
  NixOS, AUR, source, and documentation links.
- `/changelog/`: complete release notes rendered on the site, with version and
  release-date headings, newest releases first, and stable links to individual
  release entries. Group notes under **Added (what's new)**, **Changed**,
  **Fixed**, **Removed**, **Deprecated**, and **Security**, showing only the
  categories that contain entries. Include compatibility or migration notes
  when relevant. GitHub release links are secondary actions for source details
  and downloads; readers can understand every listed change without leaving
  the site.

Both views use the landing page's shared layout, typography, colors, spacing,
header, and footer. Use tab-styled navigation links to the two static routes,
with a clear active state and accessible current-page indication. Switching
views, reading release notes, and opening a release anchor must work without
client-side JavaScript, including normal browser back/forward navigation.

Use Astro's static output, shared styles/components, and repository-owned
Markdown release content. Add client-side JavaScript only for a small
interaction such as copying an install command. The website remains a project
landing page and changelog; it never becomes the working TUI or accepts users'
undo files.

Cloudflare Pages builds the frontend repository with the selected package
manager and publishes `dist/`. With npm, the build command is `npm run build`;
the repository must commit its lockfile and pin a supported Node/Astro setup.
Production tracks the frontend's `shrek` branch, with separate preview
deployments for review. Static hosting needs no Astro server adapter or Pages
Functions.

The domain `xunhen.org` is already owned. DNS delegation, the Cloudflare zone,
Pages custom-domain association, and HTTPS issuance still need verification.
For the apex domain, Cloudflare Pages requires an active zone in the same
account and Cloudflare nameservers. Attach the domain through Pages before
relying on the DNS record, preserving unrelated existing DNS records.

Published application tags and release notes are the authority for release
content. Update the site's checked-in content from those releases and record
the linked version; do not fetch GitHub release data in the visitor's browser.
Before v1 is available, the site must describe the current development status
without offering nonexistent downloads. Publishing v1 includes deploying its
real installation links and changelog entry to the site.

## User-facing workflow

Proposed commands:

```sh
xunhen inspect --undo /path/to/undo-file
xunhen show --undo /path/to/undo-file --base ./retry.go --node 42
xunhen diff --undo /path/to/undo-file --base ./retry.go --from 17 --to 42
xunhen browse --undo /path/to/undo-file --base ./retry.go
xunhen browse --source ./retry.go --undo-dir /path/to/undo-directory
```

`inspect` should report the recognized format, validated history relationships,
available event metadata, and whether reconstruction needs additional input.
It is useful even when the matching source text is unavailable. If decoding
fails, any displayed header information must be labeled as header-only; a
partially decoded graph is not a valid history.

Node selectors identify nodes within the supplied history. They are not byte
offsets or timestamps, and they are not global identifiers. Whether an on-disk
sequence number can be exposed directly needs source verification. Selection
must remain deterministic when reopening the same unchanged inputs.

`show` displays reconstructed buffer lines. Raw output serializes supported
text as UTF-8/LF and requires `--final-newline=include|omit`; the undo file does
not preserve the historical value of that option. Raw output must not send
arbitrary control bytes to a terminal. File export is an explicit shell
operation, for example `show ... --raw --final-newline=include > recovered.go`.
This is a declared serialization policy, not a claim of historically exact
source-file bytes. xunhen does not overwrite the source.

`diff` produces a unified, line-based comparison of two reconstructed states.
It compares the underlying text before terminal escaping. Far-apart states
still get a complete diff, possibly longer than the minimal one, rather than an
error.

The TUI starts with a branch list, a selected-state preview, and a comparison
view. Selecting a second node sets the comparison target. Tree relationships
determine navigation; timestamps are labels and optional sort keys, not a
substitute for ancestry. Syntax highlighting and word-level diffs can wait.

### CLI contract

The explicit `--undo` mode remains available for copied or orphaned histories.
The convenience `--source` mode uses only explicitly supplied `--undo-dir`
locations, which may be repeated. Do not read editor configuration or launch
Neovim to discover directories. Conflicting input modes are usage errors.
Discovery must report zero or multiple matches rather than choose silently.

Use stdout for command results and stderr for diagnostics. Add `--help` to
each command and `--version` to the executable. Version output includes the
release version and available commit/toolchain information, with a truthful
development fallback. Default output is terminal-safe; redirected raw output
is a distinct mode. `browse` requires a usable terminal and should suggest the
CLI commands when one is unavailable.

Reserve exit status `0` for success, `1` for operational failure, and `2` for
invalid arguments. Cancellation through an interrupt uses the conventional
status `130` after cleanup. A completed `diff` exits `0` whether inputs differ
or not; the diff output expresses the difference. Document broken-pipe and
other output-write failures and never print a panic trace for them.

Compute and validate a raw snapshot or diff before starting output. Output I/O
can still fail partway through a write, so a nonzero exit means the caller must
discard any partial output. TUI escape sequences, progress indicators, and
color must never leak into machine-consumed output.

## Architecture and data flow

Keep the first packages small and internal to the application:

| Package | Owns | Boundary |
| --- | --- | --- |
| `cmd/xunhen` | Argument parsing, the load coordinator for explicit and source-based inputs, command dispatch, limits, cancellation, exit status. | Coordinates packages; contains no binary-layout knowledge. |
| `internal/input` | Read-only, nonblocking opens of regular files and held directories, file identity, and change detection during a load. | Knows nothing about the undo format; callers pass the function that consumes an open file. |
| `internal/discover` | Bounded lookup in supplied undo directories, source-path matching, and candidate reporting. | Uses verified filename rules and decoder metadata; never treats a filename guess as proof of a matching base. |
| `internal/undofile` | Header recognition, format/ABI-profile dispatch, bounded binary decoding, normalized records, and base-matching rules. | Only package that knows on-disk layout and record encoding. |
| `internal/history` | Graph validation, node identity, traversal, base association, and reconstruction. The later TUI owns any state cache. | Consumes normalized decoder output; never switches on an undo-format version. |
| `internal/diff` | Line comparison and structured diff hunks. | Consumes reconstructed text, not undo records or UI state. |
| `internal/tui` | Bubble Tea model, user selection, background-operation requests, and rendered views. | Consumes history and diff operations; owns no reconstruction rules. |
| `internal/termtext` | Safe display of untrusted text, filenames, and diagnostics. | Escapes terminal controls at presentation boundaries without changing stored text. |

```text
explicit undo path
        |
        v
read-only open -> bounded reader -> format decoder
                                         |
                                         v
                                 normalized records
                                         |
                                         v
                                graph validation
                                         |
                                         v
                                  immutable history -----> inspect
                                         |
explicit base path -> read-only load -> verified match
                                         |
                                         v
                                  reconstruction
                                         |
                                         v
                                  selected snapshots
                                     /         \
                                  show          diff hunks
                                     \         /
                                 terminal-safe display
                                         |
                                   CLI or TUI
```

`history` may consume the stable normalized types exposed by `undofile`;
version-specific wire types remain private to `undofile`. `diff` and `tui`
depend on history operations, not decoder internals. Nothing in the core
imports the TUI. Avoid adding service/repository layers or an interface for
every concrete type.

### Decoding and compatibility

Recognize the file envelope, identify its declared format and relevant flags,
then select an explicitly supported decoder. Dispatch is based on the file's
verified format markers, not on the installed Neovim executable. Multiple
Neovim releases may share a format; a release number does not imply a new
decoder.

Version-specific decoding can begin in separate files within `undofile`.
Each decoder translates its wire representation into the same documented
record meanings. Do not scatter version checks through reconstruction or UI
code. If two formats cannot share those meanings, revise the normalized
contract explicitly rather than hide incompatible behavior behind flags.

The decoder validates lengths, counts, arithmetic, record boundaries, and
supported encodings before allocating or exposing data. The history builder
then checks identities, references, ancestry, and replay prerequisites.
Reconstruction validates every edit range against the current text.

The initial wire layout and replay semantics are specified in
[docs/undo-format.md](docs/undo-format.md), with pinned source references and
an independent Neovim fixture corpus. Format 3 includes native-layout extmark
payloads; version recognition alone cannot establish producer-ABI compatibility.
A serialized undo record is a direction-dependent line-range swap, not a
complete snapshot or an immutable forward patch.

### Reconstruction and ownership

Require matching base text under the selected normalization profile. The
initial format hashes internal lines with NUL terminators, with a separate
automatic-save case for empty buffers. The reference is the buffer when the
undo file was written, which can differ from the current source on disk. A
hash match establishes that content relationship, not the authenticity or
safety of the undo file.

First load and validate the history. Then match explicitly supplied base text
using that decoder's verified rules and associate it with the correct history
state. Only then construct a reconstructor. A missing or mismatched base keeps
metadata inspection available but blocks complete-state claims.

Decoded records and the validated history become immutable after loading.
Each reconstruction request owns a fresh workspace starting with the verified
reference lines. Snapshots do not expose writable backing slices. No package
returns internal mutable slices for a caller to modify.

Navigation between branches replays through their shared ancestor. Headers
above it on the reference path contain undo text; headers below it on the
target path contain redo text. A fresh request applies each persisted header
once in its stored entry order. Inverse capture and entry reversal are needed
only if a future worker keeps a mutable workspace across requests.

The later TUI may cache completed immutable snapshots by history/base and node
identity. A cached text snapshot is an output, not a replay checkpoint. Evict
by retained bytes and discard the cache on a new history/base load.

The TUI owns selection and viewport state. Use one bounded background worker
for reconstruction and diff requests, with at most one running request and one
pending latest request. Cancel obsolete work and tag results with a load and
selection generation so late results cannot replace the current preview.
Join the worker when the UI closes. CLI commands can execute synchronously.

## Core domain types and representations

These are intended responsibilities and invariants. Exact exported signatures
follow the format study; the type names are not claims about Neovim structs.

| Type | Meaning and invariants |
| --- | --- |
| `FormatInfo` | Recognized format identity and supported feature flags. Preserve unknown identifiers for diagnostics; recognition alone does not mean support. |
| `DecodedFile` | Bounded, syntactically decoded records and base-match metadata. It is not yet proof of a valid history graph. |
| `NodeID` | A distinct identifier within one history. Do not interchange it with sequence numbers, array indexes, byte offsets, or timestamps. |
| `NodeRef` | A looked-up node bound to its owning history. Core operations use this reference when a bare ID could refer to the wrong history. |
| `History` | Immutable validated nodes, replay records, and navigation indexes. No duplicate identities, dangling references, cycles in logical ancestry, or unexplained disconnected nodes. |
| `Node` | Event identity, logical relationships, available metadata, and references to replay data. A node is not itself a full-text snapshot. |
| `EventTime` | A recorded timestamp when one is available. Missing time is explicit; equal or non-monotonic times do not invalidate otherwise valid ancestry. |
| `VerifiedBase` | Immutable logical buffer lines checked against one decoded file's reference hash and line count. The source label is used for diagnostics; historical file options remain unknown. |
| `Reconstructor` | A validated history bound to matching base text and its reference state. Owns the state limits and gives each request a fresh workspace. |
| `Snapshot` | Successfully reconstructed buffer lines and their history/node identity. Historical encoding/newline settings stay unknown; a chosen export policy is separate. Never uses empty text to stand for failed reconstruction. |
| `Diff` | A completed comparison with ordered, valid hunks and identified inputs. Cancellation does not produce an ordinary completed diff; far-apart states produce a complete one that may not be minimal. |
| `Limits` | Validated input and state ceilings. Reject zero/negative or overflowing settings instead of interpreting them as unlimited. |
| `InputError` | An operational failure with a category, source label, and byte offset or node context when known. Examples include truncation, unsupported format, broken reference, and base mismatch. |

Use private fields and checked constructors for `History`, `Reconstructor`,
and `Snapshot`. Keep decoded input separate from validated domain objects.
Avoid a single struct with combinations such as `Valid`, `Partial`, `HasBase`,
and `Reconstructed` booleans; those combinations invite callers to use states
that should never have existed.

Go cannot prove graph connectivity, correct base matching, or slice bounds
from type declarations. Named types prevent accidental mixing; constructors
and operations establish the remaining invariants. Zero values and invalid
public selectors still need safe handling. A foreign-history node reference
must be rejected rather than interpreted against whichever history is open.

### Why this representation

- **Workload:** load once, look up selected nodes repeatedly, walk parents and
  branches, and reconstruct a small number of states at a time. Start with one
  history and source text normally below a few MiB.
- **Representation:** a private `[]node`, an immutable `map[NodeID]int` for
  lookup, and navigation indexes built once from validated relationships.
  Keep node metadata separate from variable-sized edit payloads. Logical
  relationships have one canonical representation; child-order indexes are
  derived, checked during construction, and discarded with the history.
- **Alternative:** a mutable pointer tree makes local traversal convenient but
  leaves ownership, aliasing, and malformed-reference validation harder to
  audit. Full snapshots at every node avoid replay but multiply storage by
  both node count and text size. Neither is the starting choice.
- **Verification:** test relationship validation and compare reconstructed
  states against independently produced Neovim results. A successful parse
  alone does not establish that the representation has the right semantics.

Reconstructed text is a private slice of byte-preserving line strings plus
the supported normalization profile and separate export policy. Go strings can
hold non-UTF-8 bytes; terminal rendering must not assume valid UTF-8. During
replay the working state keeps those lines in chunks of at most 1,024, the way
Neovim's memline keeps blocks. An edit that changes the line count then moves
the chunks it touches instead of every later line: 1,000 insertions at the top
of a 100,000-line file replay in about 1 ms instead of 54 ms, and 250,000 in
one 1,000,000-line state in 41 ms instead of 101 s. An edit that stays inside
one chunk changes it in place and allocates nothing. A rope or piece table would
add balancing machinery that these numbers do not call for.

The diff uses Myers' linear-space line algorithm, documented in
[docs/diff.md](docs/diff.md). Its worst-case work grows with both the state
size and the edit distance, so the comparison counts its search work. Past a
fixed amount it aligns the rest at lines unique to each side, and past a
second amount it shows the rest as plain deletions and insertions. The result
is always a complete diff; only minimality is given up, and search time stays
under half a second. Choosing an internal implementation means owning that
algorithm's correctness; it is not free merely because it avoids a
dependency.

## Failure handling and resource bounds

Malformed input, unknown formats, missing files, mismatched base text, and
cancelled work are normal error paths. Return actionable errors; do not panic,
repair links heuristically, or substitute empty text. Broad panic recovery is
not a parser validation strategy. Input-derived indexes and allocations must
be checked before use, including during replay.

Use iterative graph walks so an unusually deep history cannot exhaust the
stack. Reject lengths and integer conversions that overflow the destination
type. File-size checks alone are insufficient: small inputs can claim enormous
counts or cause repeated reconstruction and diff work.

Ceilings, each far above what ordinary source files and histories need:

| Resource | Ceiling |
| --- | --- |
| Undo input | 256 MiB |
| Supplied base text | 64 MiB |
| Any reconstructed state | 64 MiB |
| History nodes | 1,000,000 |
| Text and extmark entries combined | 1,000,000 per file |
| Stored lines across entries / lines in one state | 4,000,000 each |
| Individual line, including the saved `U` line | 16 MiB |
| Undo directories per source-based search | 32 |
| Undo bytes read per search, rejected candidates included | 512 MiB |

These are project limits, not Neovim format limits. Decoded text cannot
outgrow the undo input, and a list of optional fields holds at most one
field, so neither needs a limit of its own. Output streams as it is rendered,
so it has none either.

The memory target is 1 GiB per command. Synthetic inputs at these ceilings
reached, as the highest peak resident memory and time of three runs on the
development machine:

| Input | Command | Peak memory | Time |
| --- | --- | --- | --- |
| 600,000 changes in a 240 MiB undo file | `inspect`, `show` | 325 MiB | 0.6 s |
| States built from 192 MiB of 16 MiB lines | `inspect`, `show`, `diff` | 396 MiB | 0.2 s |
| 1,000,000 entries on a 4,000,000-line state | `show` | 443 MiB | 0.4 s |
| A 4,000,000-line, 57 MiB state | `show` | 633 MiB | 0.6 s |
| Two such states holding the same lines in shuffled order | `diff` | 766 MiB | 1.5 s |

The last case is the slowest input found: every line must be looked up, and
none can be trimmed. The command sets a 768 MiB soft limit for the Go heap
unless `GOMEMLIMIT` is set, so the collector keeps the heap near its live size
on inputs this large.
These figures cover the Go heap and runtime overhead as the operating system
reports them; they are measurements of these inputs, not a guarantee for
every input.

Each limit is a safety ceiling sized so that no real file reaches it. Oversized
or corrupt input then gets a named error instead of a hang or an out-of-memory
kill. When the input limits already bound some work, that work gets no limit
of its own; make it cheap instead. Replay, for example, touches a few chunks
per edit, and the decoder's entry and stored-line limits bound a whole request,
so replay has no work limit. Check cancellation between bounded units of
work, such as a line, replay entry, or history header, so no unit runs for
more than a few milliseconds at these limits. The diff counts its search
work as effort and changes strategy instead of failing, as
[docs/diff.md](docs/diff.md) describes. TUI rendering stays viewport-bounded.

Keep limits in one validated configuration passed explicitly into the core.
Any later user override must retain finite ceilings and compatible arithmetic.
Do not add an unlimited switch. When a limit is hit, name the limit and explain
which operation could not complete.

Read inputs through read-only handles and close them deterministically. Work
from bounded in-memory copies rather than following live changes. Files can
still change while they are read, and an undo/base pair cannot be snapshotted
atomically this way. Detect observable changes, validate the pair, and report
inconsistency; copies taken while the editor is idle are the dependable input.

Treat recovered content, filenames, and error context as untrusted terminal
text. Escape control sequences before display, including in diagnostics.
Preserve the underlying bytes separately. Do not log recovered source text:
abandoned edits can contain secrets the user later removed.

## Open questions and format risks

The user-facing undo help documents behavior, not a stable binary interchange
specification. Neovim's writer, reader, replay implementation, and tests are
the authorities for supported formats. Start at `src/nvim/undo.c` and follow
its referenced definitions in the exact source revision under investigation.

The initial producer's answers are in [the format specification](docs/undo-format.md).
The following questions remain compatibility checks when extending that
profile or making broader reconstruction claims:

1. **Wire layout and format identity.** What are the magic bytes, version
   marker, byte order, integer widths, optional fields, record boundaries, and
   extension rules? Which differences are format changes versus release-only
   implementation changes? Keep those values isolated in the relevant decoder.
2. **Base state and checksum.** Exactly which buffer state must accompany the
   undo file? What bytes, line separators, counts, and encoding transformations
   participate in the check? How does the persisted current position relate
   to the text on disk, including explicitly written undo files?
3. **Replay semantics.** What does each record save, in which direction, and
   how are line insertions, deletions, replacements, and boundary lines applied?
   What changes when walking to an abandoned branch? Can unchanged text be
   absent from the undo file, making a missing base irrecoverable?
4. **Tree semantics.** Which links mean chronology, ancestry, or alternate
   branches? What identifies the current position, the oldest retained state,
   and write events? How does pruning change roots or references? Never assume
   the root represents an empty buffer.
5. **Text fidelity.** How are empty buffers, final newlines, CRLF, file
   encodings, embedded NULs, and invalid UTF-8 represented? Distinguish an
   exact reconstructed buffer from exact source-file bytes. Block unsupported
   raw export rather than silently normalize text.
6. **Timestamps and identity.** What do timestamps measure, and what are their
   units and range? Are sequence identifiers unique within all supported
   retained histories? Clock changes must not be mistaken for broken links.
7. **Unsupported variants.** Do target builds support encrypted or otherwise
   distinct undo variants? Which flags affect interpretation? Reject variants
   that have not been implemented and verified; do not infer support from a
   familiar prefix.
8. **Compatibility evidence.** Which exact producer builds pass fixtures?
   Support must be a tested matrix of format/features and producer versions.
   Unknown file-format versions fail explicitly; similar-looking bytes are not
   enough. Producer versions belong to fixture provenance unless the format
   actually records them; an arbitrary undo file may not identify its writer.
9. **Missing or moving files.** How should later discovery handle custom
   `'undodir'` settings, encoded filenames, renames, symlinks, and deleted
   sources? Explicit paths postpone discovery, not the need to match content.
10. **Git evidence.** A future result should say "not found in the examined
    commits" and name the search scope. It cannot prove code was never
    committed in an unavailable repository, deleted history, or unexamined
    object. Snippet equality and whole-state equality are different questions.

A later salvage mode would be a separate product decision. It would return
labeled fragments or explicitly incomplete evidence, never ordinary
`Snapshot` values with guessed contents.

## Verification strategy

Generate small synthetic histories with known actions: edit, undo, branch,
save, reopen, and visit selected states. Include the abandoned-experiment
example above. Record the producing Neovim version and source provenance, the
generation steps, the base text, and expected states and relationships.

The opt-in generator may run headless Neovim with isolated configuration and
temporary input/state directories. It must not load personal editor config or
read the user's real undo directory. Normal tests read checked-in fixtures and
require no Neovim installation. Verify that generated files actually exercise
the intended branch and persistence behavior.

Expected text must come from Neovim or explicit fixture contents, not from
xunhen's own decoder. Compare replay results across branches and repeated
navigation, not just one backward walk. Add corrupt fixtures for truncated
records, oversized counts, broken relationships, unsupported markers, and base
mismatches. Fuzz bounded decoding and validation so arbitrary bytes cannot
cause a panic, unbounded allocation, or non-terminating traversal.

For diffing, check that applying completed hunks to the left input produces
the right input. Cover empty text, repeated lines, identical states, and
states far enough apart to leave the exact search. For the TUI, verify node selection, stale-result rejection,
cancellation, terminal restoration, and safe rendering of control bytes.
Avoid tests tied to colors, spacing, or internal widget structure.

Implementation checks include `gofmt`, `go test ./...`, and `go vet ./...`.
Add race testing when the TUI worker exists. Exercise the built CLI on copied
fixtures, and verify both success output and error exit behavior. Test producer
compatibility before broadening claims in documentation.

## Build and release plan

Phases 0 through 11 are the required path to v1.0.0. Each has implementation
tasks, verification tasks, and a completion checkbox. Check a task only when
its behavior has been demonstrated; check the phase only when its tasks and
exit criteria are satisfied. Record progress here. Other files, command
output, and commit messages should describe the feature rather than repeat
phase numbers.

The architecture sections above explain the design. The checklists below own
the work, including resolution of the open questions. Planning a test, build,
or release does not count as running it.

### Phase 0 — Product contract and repository foundation

Outcome: a buildable Linux command and a repeatable development loop.

#### 0.1 Freeze the initial contract

- [x] Review the v1 feature contract, Linux/amd64 target, required archive/NixOS/AUR distribution channels, and post-v1 exclusions together.
- [x] Identify the AUR package maintainer and check the official Arch repositories and AUR for an existing package before planning a new submission.
- [x] Record the source provenance of the installed Neovim build; select the exact initial producer revision to study and test.
- [x] Select a supported Go minimum and current patch toolchain; use the same selected toolchain in local instructions and CI.
- [x] Define the supported text cases and initial resource limits, marking format-dependent limits for resolution during source study.

#### 0.2 Bootstrap the executable

- [x] Initialize module `github.com/nuggocto/xunhen` and `cmd/xunhen`, preserving `shrek` as the default branch and the existing Apache-2.0 license.
- [x] Add a minimal command dispatcher, top-level help, version reporting, and the documented exit-status mapping.
- [x] Add package directories only as their first behavior is implemented; keep file I/O at explicit boundaries.
- [x] Ignore local binaries, test artifacts, temporary fixtures, and private undo/source copies without ignoring the checked-in synthetic corpus.
- [x] Document one local sequence for formatting, building, testing, and vetting from a clean checkout.

#### 0.3 Establish CI

- [x] Add Linux CI for pull requests and changes to `shrek`: formatting checks, `go test ./...`, `go vet ./...`, and a command build.
- [x] Commit module checksums when dependencies appear; verify dependency integrity and fail on unexpected generated changes.
- [x] Pin workflow actions to reviewed immutable revisions, use read-only permissions for ordinary checks, and keep publishing credentials out of pull-request jobs.
- [x] Verify the module can build from a clean checkout without private files, a personal Go workspace, or Neovim installed.

- [x] **Phase 0 complete:** the empty application builds, help/version work, and baseline CI commands pass on an isolated checkout.

### Phase 1 — Format research and reference fixtures

Depends on phase 0. Outcome: a source-backed format description and an oracle
for recovery correctness, before parser assumptions become code.

#### 1.1 Resolve the binary and replay model

- [x] Trace the producer's undo writer, reader, and replay code; record pinned source links and the build provenance used.
- [x] Document field widths, byte order, record boundaries, version markers, optional features, and supported limits without inventing missing details.
- [x] Establish stored-link meanings, branch order, current/reference positions, pruning behavior, and the distinction between events and reconstructable states.
- [x] Establish base-checksum input and normalization, line operations in each replay direction, and final-newline/encoding behavior.
- [x] List unsupported variants explicitly and identify which facts cannot be learned from an undo file alone.
- [x] Resolve finite record-count, line-length, replay-work, diff-work, and output budgets in addition to the byte limits already proposed.

#### 1.2 Build an independent fixture corpus

- [x] Write an opt-in generator that invokes the pinned headless producer with isolated config, state, and temporary files.
- [x] Generate linear, branching, pruned, empty-buffer, repeated-line, and save/reopen histories with known base text and expected states.
- [x] Include an experiment undone before its source text was ever saved, then persisted as an abandoned branch after another edit is saved.
- [x] Exercise final-newline differences, CRLF, non-UTF-8 and embedded-control cases; classify supported reconstruction/export behavior separately.
- [x] Obtain expected node relationships and state text from Neovim or independently specified fixture contents, never xunhen's decoder.
- [x] Check in synthetic fixture bytes, generation instructions, producer provenance, and expected results; confirm ordinary tests need no editor executable.

#### 1.3 Confirm feasibility

- [x] Explain which base is required and demonstrate why a mismatching or missing base cannot be treated as empty text.
- [x] Review the normalized record and domain model against the source-backed examples, including alternate branches and any root special cases.
- [x] Revise the product contract before proceeding if the promised recovery path cannot be demonstrated.

- [x] **Phase 1 complete:** the reference corpus demonstrates the v1 recovery case, and the supported decoder/replay semantics are documented with evidence.

### Phase 2 — Bounded decoding and history inspection

Depends on phase 1. Outcome: `inspect` is useful on supported undo files even
when their source text is missing.

#### 2.1 Implement the decoder boundary

- [x] Open regular-file inputs read-only; implement a bounded reader with precise offset tracking and checked integer conversions.
- [x] Recognize the envelope, reject unsupported formats/features, and dispatch to isolated version-specific decoding code.
- [x] Validate lengths and counts before allocation, including cumulative payload and record budgets.
- [x] Return contextual typed errors for truncation, invalid records, unsupported interpretation, and exhausted limits.
- [x] Keep header-only diagnostic information separate from successfully decoded records.

#### 2.2 Construct the history model

- [x] Translate supported wire records into documented normalized meanings; keep wire types private.
- [x] Build private node storage, history-scoped IDs/references, and immutable lookup/navigation indexes.
- [x] Reject duplicate identities, dangling links, invalid ancestry, and unexplained disconnected nodes with iterative bounded validation.
- [x] Preserve recorded ordering and optional timestamps without assuming clock order defines ancestry.
- [x] Make unsupported zero values and foreign-history references return errors at public operation boundaries.

#### 2.3 Deliver the inspector

- [x] Implement `inspect --undo` with deterministic node selectors, readable relationships, format information, and metadata-availability labels.
- [x] Escape untrusted filenames, metadata, and error context before terminal output.
- [x] Verify known fixture graphs and errors for representative truncations, oversized fields, broken links, and unknown format markers.
- [x] Add bounded decoder/graph fuzz targets and retain any discovered regressions as small synthetic cases.
- [x] Check that inspection leaves input contents unchanged and works without source text or Neovim installed.

- [x] **Phase 2 complete:** the inspector reports the reference histories correctly and rejects malformed input without panics or presenting partial graphs as valid.

### Phase 3 — Base binding and exact state recovery

Depends on phase 2. Outcome: recover a retained abandoned implementation with
`show`, using a verified matching base.

#### 3.1 Bind the base safely

- [x] Load bounded base text through a read-only handle and preserve the metadata required by the verified text model.
- [x] Implement the documented producer-specific content check and reference-state association.
- [x] Construct a reconstructor only after successful history/base binding; distinguish missing base, mismatch, and unsupported text cases.
- [x] Reject observably inconsistent input reads rather than combine unrelated undo and source states.

#### 3.2 Implement replay

- [x] Implement checked normalized edit operations with explicit direction and line-range validation.
- [x] Traverse within a branch and across a shared ancestor using the verified semantics, including retained-root and current-position cases.
- [x] Bound replay work and resulting state size; support cancellation at meaningful work boundaries.
- [x] Return immutable snapshots with history/node identity and an explicit buffer-line representation; keep unavailable historical format metadata unknown.
- [x] Verify every selected fixture state against the independent oracle, including repeated back-and-forth and cross-branch navigation.
- [x] Test history/base mismatch, invalid replay ranges, cancellation, and missing information as errors rather than successful empty snapshots.

#### 3.3 Deliver recovery output

- [x] Implement terminal-safe `show` and redirected `show --raw` for verified export cases.
- [x] Verify exact serialized bytes under the selected UTF-8/LF and final-newline policy; keep unknown historical file options explicit and refuse unsupported conversions.
- [x] Reconstruct fully before writing raw output; handle short writes, broken pipes, and output errors without reporting success.
- [x] Demonstrate the abandoned-experiment recovery from the narrative using the built command, with byte-for-byte expected output.
- [x] Verify read-only behavior on undo/source inputs for both successful and failed recovery attempts.

- [x] **Phase 3 complete:** all supported reference states reconstruct correctly, raw export matches expected bytes, and missing or incompatible bases fail clearly.

### Phase 4 — Bounded diffing and complete CLI behavior

Depends on phase 3. Outcome: a scriptable tool that can inspect, recover, and
compare retained alternatives before the TUI exists.

#### 4.1 Implement line diffs

- [x] Implement the documented line-diff algorithm with checked workspace, comparison-work, and output budgets.
- [x] Define structured hunks, context-line rules, and final-newline reporting without conflating display escaping with text equality.
- [x] Handle identical states, empty inputs, repeated lines, large changes, and long lines deterministically.
- [x] Verify that applying completed hunks reconstructs the right-hand input; use independently known small examples for hunk semantics.
- [x] Exercise budget exhaustion and cancellation without emitting a completed-looking truncated diff.

#### 4.2 Finish the CLI contract

- [x] Implement `diff --from --to` and reject invalid or foreign-history selections.
- [x] Apply the same argument validation, help, diagnostics, and exit-status rules across `inspect`, `show`, and `diff`.
- [x] Verify redirected output, closed pipes, interrupted work, paths with spaces, and filenames that resemble options.
- [x] Make plain output deterministic enough for scripts; keep timestamps/time zones explicit and presentation independent of locale surprises.
- [x] Verify the executable works when `nvim` and `git` are absent from `PATH`.

- [x] **Phase 4 complete:** the CLI can reproduce a documented inspect/recover/compare session with correct output and exit behavior, including failures.

### Phase 5 — History discovery and input lifecycle

Depends on phase 4. Outcome: users can start from a source file and their chosen
undo directories while retaining the explicit-file workflow.

#### 5.1 Resolve candidates

- [x] Implement verified Linux undo-filename/path rules in `internal/discover` rather than assuming that every separator encoding is reversible.
- [x] Add the mutually exclusive `--source` convenience mode and repeatable `--undo-dir` inputs to relevant commands.
- [x] Search only supplied directories with limits on directory count, entries examined, candidate count, and total decode work; report incomplete searches as such.
- [x] Define symlink handling explicitly, require opened inputs to be regular files, and avoid blocking on FIFOs, devices, or recursive directory traversal.
- [x] Treat encoded paths as hints; verify candidate metadata and base association before reconstruction.
- [x] Report no matches, ambiguity, permission failures, missing sources, and renamed files without silently guessing or opening paths obtained solely from undo contents.

#### 5.2 Handle input changes

- [x] Read each selected input into bounded owned storage and close handles on every path.
- [x] Detect observable changes during loading and give a clear retry/copy-inputs message instead of merging inconsistent reads.
- [x] Define reload as a new history/base load with fresh identity and cache ownership.
- [x] Test custom directories, conflicting candidates, symlinks, deleted sources, permission failures, and files changed during load in temporary fixtures.
- [x] Verify discovery never writes to source/undo locations and that explicit `--undo` still works when discovery cannot resolve a file.

- [x] **Phase 5 complete:** source-based lookup succeeds for verified naming cases and explains ambiguity or inconsistency without weakening base validation.

### Phase 6 — Interactive terminal browser

Depends on phases 4 and 5. Outcome: explore branches and compare states without
repeatedly entering node selectors.

#### 6.1 Build the interaction model

- [x] Pin the selected Bubble Tea release and justify any extra UI dependencies before adding them.
- [x] Implement `browse` with a branch tree, selected-state preview, and comparison view using the existing core operations.
- [x] Add keyboard navigation, expand/collapse, scrolling, selecting a comparison pair, and jumping to a node ID.
- [x] Show the selected node, base/reference position, available timestamps, and loading/error state without inventing unavailable metadata.
- [x] Add in-app help and a documented route to export the selected state with the existing CLI.
- [x] Support small terminals, resize events, no-color display, and readable Unicode/control-byte handling without requiring patched fonts.

#### 6.2 Keep work bounded and results current

- [x] Add the single reconstruction/diff worker with at most one running and one latest pending request.
- [x] Cancel obsolete work and use load/selection generations to reject late results.
- [x] Implement a 128 MiB byte-accounted LRU snapshot cache, room for a compared pair of the largest 64 MiB states, with immutable entries and explicit ownership of retained backing storage.
- [x] Discard the cache on a new history/base load and ensure evicted entries cannot corrupt still-displayed snapshots.
- [x] Keep decoding, replay, and diff work out of rendering callbacks and preserve usable input handling while work runs.

#### 6.3 Verify terminal behavior

- [x] Exercise selection, comparison, rapid navigation, reload, cancellation, and stale-result rejection against synthetic histories.
- [x] Verify clean quit, Ctrl-C, relevant termination signals, and suspend/resume behavior; restore terminal settings and release the worker.
- [x] Reject non-interactive/dumb-terminal use with a useful CLI fallback rather than printing a broken full-screen interface.
- [x] Check at least one ordinary Linux terminal, an SSH session, and a tmux session using the built binary.
- [x] Add race testing for worker/cache interactions; keep assertions focused on behavior rather than colors or widget layout.

- [x] **Phase 6 complete:** a user can navigate and compare the recovery example interactively, including fast selection changes and clean exit under failure.

### Phase 7 — Correctness, resource, and compatibility hardening

Depends on phase 6. Outcome: release-blocking failure paths and resource
behavior are exercised across the complete application.

#### 7.1 Expand adversarial coverage

- [ ] Extend the corruption corpus across headers, record boundaries, references, counts, text lengths, replay ranges, and unsupported variants.
- [ ] Fuzz decoding, graph construction, and supported reconstruction using bounded inputs and explicit time/memory budgets.
- [ ] Run a short reproducible fuzz smoke in CI and a longer bounded campaign before the release candidate; preserve actionable failing inputs.
- [ ] Test terminal-control injection through source text, filenames, timestamps/metadata labels, and error messages.
- [ ] Test cancellation, read/output failures, limit boundaries, and failed reloads without damaging the previously valid UI state.
- [ ] Verify synthetic input content hashes before and after every command family; audit code for input-path writes and accidental private-content logging.

#### 7.2 Measure the intended workload

- [ ] Create reproducible small, ordinary, deep-branch, large-change, and near-limit synthetic workloads with recorded node/text sizes.
- [ ] Record load time, cold/warm state selection, diff time, allocations, peak memory, and cancellation latency on a named Linux reference machine.
- [ ] Set acceptance thresholds from the intended interaction and measured workloads; label them as project targets rather than universal performance guarantees.
- [ ] Account for retained string backing storage, graph/index overhead, cached states, diff workspace, and queued work in the memory assessment.
- [ ] Fix stalls, runaway work, or budget accounting gaps before adding more elaborate data structures; remeasure changed paths.

#### 7.3 Close compatibility claims

- [ ] Run the full oracle corpus for each producer/build and feature combination intended for v1 support.
- [ ] Publish explicit supported, rejected, and unverified text/format cases; avoid claiming support for every release sharing a version string.
- [ ] Check fixture provenance against the supported matrix and retain a regression for every discovered replay defect.
- [ ] Run formatting, tests, vet, race tests, module verification, and a pinned `govulncheck` invocation in the appropriate CI jobs.
- [ ] Review reachable dependency findings and dependency/license changes; update vulnerable build tools before packaging.

Race-check jobs may need a C toolchain and cgo. That is a test-runner
requirement, not permission to add a libc dependency to the release binary.

- [ ] **Phase 7 complete:** the advertised corpus passes, no release-blocking correctness or resource issue remains, and compatibility/performance claims have recorded evidence.

### Phase 8 — User documentation and Linux release machinery

Depends on phase 7. Outcome: a user can understand, install, and use a packaged
candidate; a maintainer can reproduce its build.

#### 8.1 Write the user and maintainer guides

- [ ] Expand `README.md` with the purpose, retained-history limitation, supported Linux target, archive/NixOS/AUR installation, and a complete inspect/show/diff/browse walkthrough.
- [ ] Document CLI flags, exit codes, TUI controls, raw export, discovery rules, resource-limit errors, and supported text formats.
- [ ] Add troubleshooting for disabled or unwritten persistent history, pruned branches, missing/mismatched bases, unsupported formats, permissions, and terminal limitations.
- [ ] Document how to build, run checks, regenerate isolated fixtures, add a decoder, and extend the compatibility matrix.
- [ ] Add a changelog and compatibility policy: v1 CLI/output contracts are stable within v1, while internal Go packages remain private implementation details.
- [ ] Document bug reporting with version/format/error context and minimal synthetic reproductions; explain why users should not upload real undo files by default.

#### 8.2 Automate reproducible packaging

- [ ] Add a small release build/package script using the selected Go toolchain and standard Linux archive/checksum tools; avoid a release framework unless it earns its dependency cost.
- [ ] Build Linux/amd64 with `CGO_ENABLED=0`, `GOAMD64=v1`, and trimmed build paths; record release version, source commit, toolchain, and module/build settings.
- [ ] Make release builds reject dirty source trees and uncontrolled local workspace overrides; avoid wall-clock timestamps in binaries and normalize archive metadata.
- [ ] Build twice in separate clean directories with the same pinned inputs and compare binary/archive hashes; investigate differences before claiming reproducibility.
- [ ] Inspect embedded build metadata and executable linkage; include the executable, README, license, and required dependency notices in the archive.
- [ ] Generate archive checksums and a provenance/dependency manifest tied to the source commit and CI run. Document that checksums alone are not publisher authentication.
- [ ] Validate archive paths and file permissions, and exclude fixture source content, local paths, credentials, and development-only artifacts.

#### 8.3 Package for NixOS

- [ ] Add `nix/package.nix` using `buildGoModule` for `cmd/xunhen`, with the application license, executable metadata, tests, and the supported `x86_64-linux` platform.
- [ ] Add `flake.nix` and commit `flake.lock`; expose named/default packages and apps without claiming unsupported CPU architectures or operating systems.
- [ ] Pin the nixpkgs input, select a compatible Go builder, and calculate the real Go dependency `vendorHash`; reject placeholder hashes and implicit toolchain downloads during the build.
- [ ] Keep source inputs limited to intended tracked files, preserve required synthetic fixtures for checks, and inject truthful release metadata without depending on a `.git` directory in the sandbox.
- [ ] Run `nix flake check` and the package build in sandboxed Linux CI; verify that the build/check steps do not depend on undeclared network access, personal configuration, or Neovim.
- [ ] Document tagged-flake build/run commands, user-profile installation, a NixOS `environment.systemPackages` example, and pin/update/rollback/remove procedures.

#### 8.4 Package for the AUR

- [ ] Prepare a maintained source-package recipe template under `packaging/aur/`, targeting `xunhen` on `x86_64`, with version, `pkgrel`, license, homepage, and appropriate build/runtime dependencies.
- [ ] Fetch a versioned source archive with a verified checksum, use the committed Go module versions/checksums, and keep the module cache within the package build environment.
- [ ] Implement `prepare`, `build`, `check`, and `package` behavior as needed: compile `cmd/xunhen`, run ordinary tests without Neovim, and install the binary plus required documentation/license material through `$pkgdir`.
- [ ] Follow the current Arch Go packaging guidance, document any pure-Go build-flag choices, and verify the package's actual linkage and declared runtime dependencies.
- [ ] Generate `.SRCINFO` with `makepkg --printsrcinfo` whenever the resolved recipe changes; lint the recipe/package and build with Arch devtools in a clean chroot.
- [ ] Document cloning/reviewing the AUR recipe and using `makepkg`/`pacman` without requiring an AUR helper; include upgrade, downgrade, and removal.
- [ ] Keep the AUR packaging Git repository and its required branch separate from the application repository; preserve `shrek` as the application default branch.
- [ ] Resolve source-archive checksums after the application tag exists, then publish the generated recipe; do not retag the application to embed a checksum of its own archive.

#### 8.5 Prepare the release workflow

- [ ] Separate read-only CI from the publish job; permit publication only for the intended version tag on a reviewed `shrek` commit after required checks pass.
- [ ] Pin release actions/tools and keep release-write permissions confined to the publish job; do not execute untrusted pull-request code with that authority.
- [ ] Stage built assets and metadata for verification before making the GitHub release public.
- [ ] Publish the already-verified artifact bytes rather than rebuilding between verification and upload.
- [ ] Write install, checksum verification, user-local `PATH`, upgrade, downgrade, and uninstall instructions with quoted paths and no root requirement for the normal path.
- [ ] Verify clean-checkout module installation and version reporting for both release archives and module-installed builds; test public tagged installs once candidate tags exist.
- [ ] Add independent Nix build/check and Arch clean-chroot packaging jobs, with package-specific logs and provenance alongside the archive checks.
- [ ] Define channel publication order: make the tested source tag/archive available, verify the tagged Nix outputs, resolve and validate the AUR recipe, then publish the AUR update.
- [ ] Keep any AUR publishing credentials confined to the maintainer's release path and out of ordinary build or pull-request jobs.

- [ ] **Phase 8 complete:** the archive, Nix package, and AUR recipe have repeatable build/check paths, and the documentation covers installation and maintenance for each channel.

### Phase 9 — Astro website and Cloudflare Pages

The scaffold can start after phase 0 and proceed alongside application work.
Final installation copy depends on phase 8. Outcome: a working public landing
page and changelog at `https://xunhen.org`, with a repeatable preview and
production deployment path.

#### 9.1 Build the static site in its own repository

- [ ] Scaffold Astro inside the existing `../xunhen-front` repository, preserving its README/license history and `shrek` default branch.
- [ ] Pin a supported Node/Astro setup and package-manager version, commit the lockfile, and document clean installation, local development, checks, and production builds.
- [ ] Configure static output and `site: 'https://xunhen.org'`; keep the site independent of the Go build, source checkout, and a running Neovim instance.
- [ ] Build the landing page with the purpose, recovery example, accurate limitations, synthetic TUI media, and installation sections for all v1 distribution channels.
- [ ] Add Home and Changelog navigation tabs using accessible links to static routes, with an active-page state and the same shared layout, typography, colors, header, and footer.
- [ ] Build `/changelog/` from checked-in Markdown entries that render the full user-facing notes locally, with validated version/date/category/link metadata, newest-first ordering, and stable release anchors.
- [ ] Group each release's notes into Added, Changed, Fixed, Removed, Deprecated, and Security sections as applicable; omit empty categories and include compatibility/migration notes when needed.
- [ ] Keep GitHub release links as secondary source/download actions, and provide a clear unreleased state before the first publication rather than empty or invented release entries.
- [ ] Use public repository/release links and locally owned assets; keep the site free of undo-file uploads, editor functionality, server-side code, and embedded credentials.
- [ ] Add readable mobile/desktop layouts, keyboard-accessible navigation, page metadata, canonical URLs, a favicon, and a useful static 404 page.

#### 9.2 Configure Pages and the domain

- [ ] Inspect the existing Cloudflare zone, nameservers, DNS records, and any Pages project before configuring deployment; domain ownership alone does not establish hosting readiness.
- [ ] Connect the `xunhen-front` repository to a Cloudflare Pages project with production branch `shrek`, repository-root builds, the pinned toolchain, `npm run build` for npm, and output directory `dist`.
- [ ] Enable separate preview deployments and add frontend CI for a locked dependency install, content/type checks, and the static build.
- [ ] Verify `xunhen.org` is an active Cloudflare zone in the Pages account and that its nameservers are correct; preserve unrelated records when adjusting DNS.
- [ ] Add `xunhen.org` through the Pages custom-domain configuration, wait for domain/certificate activation, and verify HTTPS and HTTP-to-HTTPS behavior.
- [ ] Define canonical handling for any enabled aliases without breaking preview URLs; keep preview deployments out of search indexes.
- [ ] Document Pages build settings, production/preview behavior, the domain setup, and how to restore a previous successful deployment.

#### 9.3 Verify the site and release handoff

- [ ] Exercise the built site locally and on a Pages preview at desktop and mobile sizes, including Home/Changelog navigation, keyboard access, release anchors, browser back/forward, the 404 route, and any copy controls.
- [ ] Verify that complete categorized release notes are readable on-site, the changelog uses the shared landing-page design, and every release anchor resolves to the expected version.
- [ ] Check that core content and installation instructions remain usable without client JavaScript, and that media has appropriate text alternatives.
- [ ] Verify browser console/network failures, missing assets, broken links, and accidental private fixture content before production publication.
- [ ] Deploy the accurate pre-release site to `https://xunhen.org` and verify domain/TLS behavior, canonical metadata, responsive layout, and production links.
- [ ] Define a release-content update checklist that uses published application versions and tested install instructions, with no invented release dates or premature stable-download claims.
- [ ] Verify a website rollback and document how to correct stale version/install links independently of the CLI release.

- [ ] **Phase 9 complete:** the separate static site is live on `xunhen.org`, its pages and deployment path pass browser checks, and the stable-release content handoff is documented.

### Phase 10 — Release candidate and packaged-artifact QA

Depends on phases 8 and 9. Outcome: evidence that the exact packaged application
works on the advertised Linux environments and that the site accurately
guides users to the available release channels.

#### 10.1 Produce a candidate

- [ ] Freeze the v1 behavior/compatibility matrix and prepare a versioned prerelease such as `v1.0.0-rc.1` from `shrek`.
- [ ] Run the full CI/release checks, build the candidate archive, and record its hashes and source commit.
- [ ] Download and extract the archive as a user would; perform the following checks on its executable, not a separate development build.

#### 10.2 Exercise installation and commands

- [ ] Install without root in clean pinned Debian, Ubuntu, and Alpine userlands with no Go or Neovim requirement; record results and exact environments.
- [ ] Verify the executable's CPU/kernel baseline on suitable native or VM hosts rather than inferring it from a successful container run.
- [ ] Check archive integrity, permissions, version/build metadata, help, source installation, upgrade/downgrade, and uninstall.
- [ ] Run the complete discovery/inspect/recover/diff walkthrough and compare exported fixture bytes against the independent expected results.
- [ ] Exercise wrong bases, missing files, denied permissions, truncated files, unsupported formats, ambiguous discovery, and resource-limit failures.
- [ ] Check redirects, raw-output TTY refusal, closed pipes, interruption, and non-interactive `browse` behavior with correct status and diagnostics.
- [ ] In a clean NixOS VM, build the candidate flake with its committed lock file, run it, install it through both a user profile and `environment.systemPackages`, and exercise rollback/removal.
- [ ] Build the candidate AUR recipe in a clean Arch chroot and install the resulting package in a disposable Arch system; verify package contents, dependencies, upgrades/downgrades, and removal.
- [ ] Run the recovery/diff corpus and TUI smoke on the Nix- and Arch-built executables, recording their own hashes, versions, and build inputs.
- [ ] Test prerelease packaging locally without replacing an existing stable AUR package with the candidate.

#### 10.3 Exercise the TUI and close defects

- [ ] Test branch switching, comparison selection, scrolling, resizing, rapid requests, cancellation, reload, and empty/near-limit histories.
- [ ] Verify terminal restoration after normal exit, Ctrl-C, load errors, and relevant signal paths in local, SSH, and tmux sessions.
- [ ] Confirm no unintended input-content changes, network access by the application, private-content logs, or dependence on personal configuration.
- [ ] Keep synthetic screenshots/transcripts and a concise QA report with reproducible failures, fixes, and any advertised limitations.
- [ ] Fix release-blocking defects, add focused regressions, issue a new candidate when the artifact changes, and rerun affected checks plus the installation/recovery smoke.
- [ ] Exercise the site's installation walkthrough against the candidate and stage the stable landing-page/changelog update in a frontend preview; keep production claims tied to currently published artifacts.

- [ ] **Phase 10 complete:** the archive and both package-manager builds pass Linux recovery/TUI checks, the site's instructions match the tested workflow, and no release blocker remains.

### Phase 11 — Publish v1.0.0 and establish maintenance

Depends on phase 10. Outcome: a public, installable Linux release, an up-to-date
website, and a clear path for maintaining both.

#### 11.1 Prepare the stable artifact

- [ ] Select the reviewed release commit on `shrek`, finalize the changelog and support matrix, and create the `v1.0.0` tag without moving any published tag.
- [ ] Build the stable-version assets through the release workflow and rerun artifact verification and installation/recovery smoke tests on those exact bytes.
- [ ] Confirm `--version`, archive naming, checksums, module-install behavior, license notices, and provenance all refer to the stable version and correct commit.
- [ ] Treat version stamping as an artifact change: passing QA on an `rc` executable alone does not verify the stable executable.
- [ ] Verify the stable tag includes the intended Nix derivation and lock file; build/check its published flake and resolve the stable AUR source URL, checksum, recipe, and `.SRCINFO`.

#### 11.2 Publish and verify delivery

- [ ] Publish the GitHub release with the Linux/amd64 archive, checksums, provenance/dependency information, installation links, and release notes.
- [ ] State supported producer formats/features, tested Linux environments, known limitations, and the read-only recovery contract in the release notes.
- [ ] Download the public assets, verify their hashes against the tested artifacts, and repeat the install/version/recovery smoke from the published release.
- [ ] Verify the public tagged `go install` path and user-local installation instructions.
- [ ] Verify NixOS installation from the public `v1.0.0` flake reference, including the documented declarative configuration and correct version output.
- [ ] Submit or update the maintained AUR package after the stable source archive is public, with the tested `PKGBUILD`, generated `.SRCINFO`, and packaging-source license.
- [ ] Fetch the public AUR recipe into a fresh Arch build environment, build/install it, and rerun the version/recovery smoke; include its package URL in the release documentation.
- [ ] Once the archive, tagged Nix package, and AUR recipe are verified publicly, deploy the matching v1 landing-page links and changelog entry from `xunhen-front` to Pages.
- [ ] Check `https://xunhen.org` and its Changelog tab after deployment: read the complete categorized notes on-site, test release anchors and installation links, and confirm the version, date, limitations, and HTTPS behavior.
- [ ] Keep prior version assets available for downgrade; never silently replace a published binary with different bytes.

#### 11.3 Hand off to maintenance

- [ ] Document patch-release criteria, supported Go/dependency update checks, and how a new Neovim producer enters the compatibility matrix.
- [ ] Define regression triage: reproduce safely, add a minimal fixture, fix the cause, and rerun relevant release gates before a patch release.
- [ ] Document the response to a broken release: describe the defect, recommend a known-good version when one exists, and publish a new patch version rather than retagging.
- [ ] Review the issue-report template and ensure version, platform, format context, and synthetic reproduction are enough to start diagnosis.
- [ ] Keep product roadmap checkboxes here; use release notes to explain shipped behavior rather than internal planning labels.
- [ ] Document coordinated updates for all distribution channels: application version, Nix inputs/dependency hash when needed, AUR `pkgver`/`pkgrel`, checksums, `.SRCINFO`, and installation smoke tests.
- [ ] Assign responsibility for AUR out-of-date reports and Nix build regressions; distinguish packaging-only fixes from application patch releases without moving published tags.
- [ ] Include frontend release-content updates, Astro/Node dependency maintenance, Pages build failures, domain renewal, and rollback in the maintenance instructions.

- [ ] **Phase 11 complete:** v1.0.0 is available and verified through all distribution channels, `xunhen.org` publishes the matching installation links/changelog, and maintenance procedures cover the application and site.

## References

- [Neovim undo help](https://neovim.io/doc/user/undo.html): documented persistence and branching behavior.
- [Neovim options](https://neovim.io/doc/user/options.html): `'undofile'`, `'undodir'`, and `'undolevels'`.
- [Neovim source](https://github.com/neovim/neovim): reader, writer, replay implementation, and tests; use pinned producer revisions for compatibility work.
- [Bubble Tea](https://github.com/charmbracelet/bubbletea): TUI framework; select a release and its matching API documentation at implementation time.
- [Go command](https://pkg.go.dev/cmd/go): build settings, module installs, embedded build information, and verification commands.
- [Nixpkgs Go packaging](https://nixos.org/manual/nixpkgs/stable/#sec-language-go): `buildGoModule`, dependency hashes, and Go package checks.
- [NixOS manual](https://nixos.org/manual/nixos/stable/): declarative package installation and system configuration.
- [AUR submission guidelines](https://wiki.archlinux.org/title/AUR_submission_guidelines): package naming, ownership, `.SRCINFO`, publication, and maintenance.
- [Arch Go packaging guidelines](https://dev.archlinux.org/package-guidelines/go/): module handling, build flags, checks, and package installation.
- [Astro configuration](https://docs.astro.build/en/reference/configuration-reference/): static output and canonical site settings.
- [Astro on Cloudflare Pages](https://developers.cloudflare.com/pages/framework-guides/deploy-an-astro-site/): Pages build/deployment settings; this project uses static output.
- [Pages custom domains](https://developers.cloudflare.com/pages/configuration/custom-domains/): apex-domain prerequisites, domain association, and HTTPS troubleshooting.

## After v1

Phases 12 through 15 are candidate follow-ups, not conditions for releasing
v1.0.0. Prioritize them using real recovery cases and measurements. A research
result may be that a feature is not feasible; record that conclusion rather
than weakening the read-only or correctness contracts to ship it.

### Phase 12 — Git cross-referencing

Outcome: identify retained states or snippets absent from a named set of git
history, with the limits of that evidence visible.

#### 12.1 Define and implement the comparison

- [ ] Define exact-state versus snippet comparison, examined refs/commits, path/rename behavior, and treatment of shallow or unavailable history.
- [ ] Choose the optional git integration boundary; prefer an explicitly invoked git executable over a large new dependency if it meets the requirements.
- [ ] Read repository objects without shell interpolation, external diff/text-conversion execution, hooks, or repository writes.
- [ ] Bound object traversal, text processing, cache storage, and cancellation work; record which scope was actually examined.
- [ ] Add CLI and TUI results labeled "not found in the examined commits," never an unqualified claim that code was never committed.
- [ ] Verify against synthetic repositories containing committed, uncommitted, renamed, and deliberately unexamined histories.

- [ ] **Phase 12 complete:** users can compare a recovered history with a declared git scope and inspect the evidence behind each result.

### Phase 13 — Search and larger archaeology workflows

Outcome: find relevant retained work across more than one history without
turning startup into an unbounded scan.

#### 13.1 Add search incrementally

- [ ] Start with useful in-history search and filters for node identity, recorded time, and reconstructed text.
- [ ] Add opt-in multi-history discovery with explicit directory/work budgets, cancellation, and per-file errors that do not hide successful results.
- [ ] Measure whether a persistent index is needed before adding one; define content retention, invalidation, deletion, and privacy behavior first.
- [ ] Consider machine-readable inspection output only with a versioned schema and clear incomplete/error semantics.
- [ ] Evaluate word-level diffs, syntax highlighting, and alternate diff layouts against actual navigation needs and dependency cost.
- [ ] Verify search results against a known corpus and keep cache/index staleness distinguishable from absent history.

- [ ] **Phase 13 complete:** the selected search features find known retained work within documented budgets and explain incomplete coverage.

### Phase 14 — Broader Neovim and Linux compatibility

Outcome: extend verified recovery coverage while keeping releases Linux-only.

#### 14.1 Expand supported producers and machines

- [ ] Add requested Neovim producer revisions and text variants through source review, independent fixtures, and explicit compatibility-matrix entries.
- [ ] Introduce another decoder only when verified format differences require it; retain every previously supported regression corpus.
- [ ] Evaluate Linux/arm64 demand and add native runtime/TUI QA, release packaging, and published checksums before advertising support.
- [ ] Consider additional Linux architectures, other distro packages, an AUR `xunhen-bin` variant, or upstream nixpkgs inclusion only with maintained builders, real execution tests, and upgrade/uninstall documentation.
- [ ] Revisit ropes, piece tables, replay checkpoints, or different diff algorithms only when measured supported workloads justify them.

- [ ] **Phase 14 complete:** each newly advertised producer or Linux target has executable compatibility evidence and tested release assets.

### Phase 15 — Explicitly incomplete salvage research

Outcome: determine whether damaged or orphaned files can yield useful evidence
without confusing fragments with complete reconstructed states.

#### 15.1 Establish what can be recovered

- [ ] Classify corruption and missing-base cases using synthetic examples; identify independently verifiable record boundaries and text fragments.
- [ ] Decide whether the evidence justifies a separate opt-in salvage command; a no-go conclusion is an acceptable research result.
- [ ] If proceeding, define separate fragment/provenance types that cannot enter ordinary `Snapshot` or complete-diff APIs.
- [ ] Keep strict decoding as the default and label every omission, uncertain relationship, and incomplete text result.
- [ ] Bound recovery scans and validate against intentionally damaged fixtures with known retained contents; never fabricate missing text or ancestry.

- [ ] **Phase 15 complete:** either a supported, clearly incomplete salvage workflow is verified, or its infeasibility and remaining questions are documented.
