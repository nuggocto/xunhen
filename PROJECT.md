---
tags: [rust, project, tui, git, diff, code-browser, lsp, search, ratatui, security]
created: 2026-08-12
updated: 2026-09-13
license: Apache-2.0
---

# Xunhen

> A diff-first terminal code browser for reviewing changes, searching the whole
> repository, and following symbols without opening an editor.

Git stays authoritative, Xunhen does not write to the repository, and semantic
answers appear only when the displayed bytes match the
current-worktree bytes most recently synchronized to the language server.

## Start here

The product sections describe the v1 target. The roadmap defines what to build
and qualify in each earlier release.

The first useful slice is a Linux Release binary that opens one unstaged
diff in a disposable repository, moves between files and hunks, shows exact old
and new coordinates, refuses unsafe output, and restores the terminal on exit.
Phase 1 delivers this journey for ordinary tracked text modifications. It
refuses unsupported cases explicitly. Phase 2 expands Git coverage and diff
presentation before repository search or language intelligence begins.

Add dependencies and modules only when their phase uses them. Search begins in
Phase 3; the LSP client begins in Phase 4. Release packaging begins in Phase 6.

## End goal

Xunhen v1 is a fast, read-only instrument for understanding a change in its
real codebase. It is not a general Git client with a source viewer attached.

V1 includes:

- one Rust codebase licensed under Apache-2.0;
- a canonical `xunhen` command and full-screen Ratatui interface;
- native Linux, macOS, and Windows support;
- working-tree, staged, commit, root-commit, merge-parent, and two-revision
  comparisons through the installed Git executable;
- changed-file navigation with unified and side-by-side diffs;
- syntax-aware source and diff presentation with exact old and new line
  coordinates;
- typo-tolerant fuzzy path search across changed and unchanged repository files,
  using an application-owned path catalogue and the `frizbee` matcher;
- fast plain, regular-expression, and fuzzy content search with bounded,
  cancellable results;
- a complete read-only source viewer for current worktree files and Git blobs;
- worktree-exact Language Server Protocol support for hover information, type
  definitions, definitions, references, implementations, and navigation
  history where the server advertises them;
- a keyboard-first symbol inspection action plus optional mouse hover mode;
- visible explanations when semantic navigation is unavailable or stale;
- deterministic behavior for unusual filenames, Unicode, binary files,
  renames, deleted files, submodules, linked worktrees, sparse worktrees, and
  both SHA-1 and SHA-256 repositories where the selected Git version supports
  them;
- explicit Git LFS and external-filter behavior that never downloads an object
  or launches a repository-selected filter, with indeterminate worktree status
  when classification depends on that filter;
- explicit limits for files, lines, diffs, search results, child output,
  queues, caches, language-server messages, timeouts, and retained memory;
- no repository mutation, staging, committing, checkout, reset, stash, rebase,
  merge, fetch, push, or hook execution by Xunhen in v1;
- no account, network service, telemetry, advertising, analytics, silent
  update, or automatic tool download;
- exact-artifact native QA, security review, dependency review, and
  reproducible performance evidence before `1.0.0`;
- `dist` (`cargo-dist`) archives and installers published as immutable GitHub
  Release assets for qualified Linux, macOS, and Windows targets;
- Homebrew, Scoop, and AUR package metadata published only after the referenced
  release artifacts pass native qualification.

Xunhen and the Git subprocesses it launches make no network connection or
repository write during ordinary use. Git lazy fetching is disabled. A missing
object in a partial or promisor repository produces an explicit local-object
unavailable result rather than a fetch. A language server is a separate
user-installed process and may use the network, write files, or run builds and
package managers under its own behavior. Xunhen states that boundary before
launching one and never presents a child process as part of its offline or
read-only guarantee.

The read-only guarantee also assumes the selected Git executable is trusted.
Xunhen constrains a qualified Git installation through arguments, environment,
timeouts, and output bounds, but it cannot contain a malicious replacement
binary running with the user's permissions. `xunhen doctor` reports the exact
executable path and version used by the current configuration.

## Name and mascot

Xunhen is the romanization of `寻痕` (*Xúnhén*), "seek the trace." A diff is
the visible trace of a change. Search finds the trail beyond the changed file,
and language intelligence follows it to the code that gives it meaning.

The command is `xunhen`. The project domain is `xunhen.org`.

The project owner confirms that `xunhen.org` was purchased through Cloudflare
Registrar on 2026-08-12, and a 2026-08-22 RDAP check confirms its registration.
The domain anchors the project name. The same preliminary check found no exact
public repository or crates.io, npm, or PyPI package. Xunhen will be hosted on
GitHub under Nuggocto's account, with GitHub Actions handling CI and releases.
Recheck material product, package, repository, social-name, and trademark
collisions before the first public release; domain or repository ownership is
not trademark clearance.

The mascot is **Dian**, a small Chinese mountain cat. Dian has a sandy coat,
short legs, dark ear tufts, and a thick ringed tail. The character follows one
thin ink trail across two sheets of paper, then rests a paw on the exact mark
where the trail changes. The expression is alert and patient, never frantic.

The Chinese mountain cat is endemic to China and is a vulnerable wild species.
The design should respect the real animal rather than turn it into a generic
house cat. Public material may include one concise conservation note and a link
to a reputable conservation source. It must not imply endorsement by a
conservation organization.

Dian helps explain the product:

- the ink trail is the diff;
- the two sheets are the compared snapshots;
- pawprints beyond the sheets are repository search results;
- a small knot in the trail is a symbol definition;
- a broken trail means the displayed snapshot cannot be matched safely to the
  current language-server workspace.

Changed, staged, binary, unavailable, stale, selected, and error states always
use text and accessible symbols. They never depend on Dian, animation, or color.
Artwork needs monochrome, no-motion, high-contrast, and tiny terminal-safe
fallbacks. The terminal application uses ordinary symbols and text, not image
protocols, for essential behavior.

## Product boundaries

Xunhen is not:

- a complete replacement for Fugitive, Magit, LazyGit, GitUI, Tig, or the Git
  command line;
- a staging, commit, branch, merge, conflict-resolution, history-rewriting, or
  remote-management interface;
- a text editor, terminal multiplexer, shell, build runner, debugger, or IDE;
- a structural diff engine in v1;
- a Git implementation or object database;
- a language server, compiler, indexer for semantic meaning, or package
  installer;
- a pull-request, issue, forge, review-comment, or collaboration client;
- an AI review tool, agent host, plugin runtime, or command palette for
  arbitrary repository scripts;
- a guarantee that every language server understands every valid project;
- a sandbox for untrusted language servers;
- a substitute for checking out and building the code under review.

V1 is deliberately read-only. Mutation may be reconsidered after v1 only if
users still need it once the browsing workflow is complete. A write feature
would require a new threat model, undo and recovery behavior, index-locking
rules, conflict handling, and exact native QA. It is not a small toggle.

## Product shape

### Invocation

Bare `xunhen` discovers the repository containing the current directory and
opens the complete current change against `HEAD`.

```text
xunhen
xunhen changes --scope all
xunhen changes --scope unstaged
xunhen changes --scope staged
xunhen commit <revision> [--parent <number>]
xunhen compare <base-revision> <target-revision>
xunhen file <repo-relative-path> [--line <number>] [--column <number>]
xunhen doctor
xunhen version
xunhen completions <shell>
```

`--repo <path>` selects a repository explicitly. Revision arguments are
resolved once through a bounded Git command with `--end-of-options` where the
qualified Git version supports it. Later commands receive resolved object IDs,
never the original user spelling.

`changes --scope all` and bare `xunhen` show every tracked staged and unstaged
change against `HEAD`, plus untracked regular files as clearly labelled
additions against an empty old side. On an unborn branch, the tracked base is
the empty tree. Intent-to-add entries keep an explicit `intent to add` state and
also use an empty old side. Ignored files remain excluded. Unmerged entries stay
labelled as conflicts and are never flattened into an ordinary two-sided diff.
Git status supplies these classifications; Xunhen does not infer them from file
contents. External-filter paths follow the indeterminate-status exception below;
they remain visible without being asserted to be clean or modified.

Bare help, `version`, and shell-completion generation never scan a repository,
build the file catalogue, or launch a language server.

### Main view

The ordinary wide layout has three regions:

```text
changes and files | diff or source | context, symbols, and help
```

The layout collapses predictably on smaller terminals. A narrow terminal shows
one focused region at a time. Resizing never allocates directly from unchecked
terminal dimensions.

The interface provides actions for:

- moving between files and hunks;
- switching unified and side-by-side presentation;
- opening either snapshot as source;
- jumping from a diff line to its full-file location;
- fuzzy file search across the current worktree;
- content search across the current worktree;
- search inside the current diff or source document;
- requesting hover, definition, type definition, implementation, or references;
- moving backward and forward through source-navigation history;
- returning to the exact diff row that began a navigation path;
- copying a repository path, line, or selected text through an explicit action;
- showing why a file, search result, or language operation is unavailable;
- cancelling any running search, Git request, or language-server request.

Exact default keys are decided after the interaction prototype. The public
action names and behavior stabilize before v1. Key bindings are configuration,
not domain policy.

### Accessibility scope

Keyboard-only operation, visible focus, no-color symbols, bounded motion, and
narrow-terminal layouts are product requirements. Screen-reader behavior also
depends on the terminal emulator and platform accessibility bridge. Phases 6
and 7 record what Narrator, VoiceOver, and Orca can discover in the qualified
terminals. Public support claims name the tested combinations and document an
unsupported combination instead of calling the TUI universally accessible.

### Search and source browsing

File search covers the present worktree, not only changed files. Content search
does the same. Ignored files, `.git`, nested repository internals, oversized
files, binary files, symlinks, and unavailable sparse paths follow explicit
documented policies.

Search does not silently switch snapshots. When reviewing a historical commit,
the search panel is labelled `current worktree`. Selecting a result opens that
worktree file beside the historical diff. Future snapshot-wide historical
search requires a separate bounded design and is not implied by v1.

### Language intelligence

Xunhen speaks the accepted Language Server Protocol 3.18 subset to
user-installed stdio language servers. It never implements language semantics
itself.

Semantic operations obey one rule:

```text
displayed bytes == current worktree bytes most recently sent to the server
```

Only then may Xunhen send a semantic request or present its result for that
document. The comparison uses a document identity containing the repository
path, snapshot kind, content digest, and language-server document version. The
request envelope also carries the server and request generations.

`textDocument/didOpen` and `textDocument/didChange` are LSP notifications. They
have no acknowledgement. Xunhen can prove which bytes it displayed and sent,
and that the semantic request followed that synchronization on the same ordered
stdio session. It cannot prove that a buggy or malicious server parsed or
analyzed those bytes correctly. The user trust decision and qualified
real-server tests cover that separate risk without turning it into a protocol
guarantee.

This means:

- the new side of an unstaged or complete worktree diff may receive semantic
  answers when its digest matches;
- a clean file opened from current worktree search may receive them;
- the staged snapshot receives them only when its blob is byte-identical to the
  current worktree file;
- deleted files, old sides, arbitrary commit blobs, and historical targets do
  not receive worktree language-server answers;
- non-file and outside-root navigation targets are unavailable in v1, including
  standard-library or dependency sources stored outside the selected root;
- a file changed after synchronization invalidates pending answers; Xunhen sends
  the accepted full or incremental update before issuing another semantic
  request with a fresh identity;
- navigation results that target a current worktree file require an identity
  captured before the request that produced the usable result, as described
  below; hashing an unknown target after a reply cannot validate its coordinates.

Cross-file locations need their own synchronization evidence. LSP `Location`
and `LocationLink` contain no target document version or content digest. For
each request, retain a bounded set of identities for documents currently open
and synchronized on that server session, including their versions and digests. A
target location is usable only if its document was in that set before the
request was written and still has the same identity when the result is used.

For a previously unsynchronized target, treat the first reply only as a bounded
candidate URI. Validate it beneath the repository root, read and synchronize
its current bytes, discard the old location, and issue a fresh request from the
still-valid source after the synchronization writes. Only locations from that
fresh reply may pass the identity gate. Synchronization after a reply never
retroactively validates it. Changed source or target bytes invalidate the
attempt. Closing a document invalidates its membership and pending locations;
a server restart invalidates the entire synchronized-document set. Reopening a
document requires fresh request evidence.

Target discovery has compiled limits for attempts, distinct targets, total
bytes, open documents, and elapsed time. Reference lists follow the same rule
per target document. Newly discovered or changed targets may exhaust the budget;
then return an explicit unavailable result or a visibly incomplete list of
validated locations. Do not chase targets indefinitely or label a partial list
complete. Do not merge location lists from different request generations.
Recheck identities before following a retained result or history
entry. These checks prove client synchronization and freshness, subject to the
same trusted-server limitation as source-document requests.

Xunhen says `semantic navigation unavailable for this snapshot` rather than
showing a plausible but wrong answer.

Keyboard inspection is always available through a named action. Optional mouse
hover is disabled by default so ordinary terminal text selection remains
predictable. When enabled, a short bounded dwell may request the same LSP hover
operation. Moving away, changing the document, or issuing a newer request
cancels or invalidates the result.

Language servers are never downloaded, installed, or selected from repository
content. A user-owned configuration outside the repository names an absolute
candidate executable and argument array. Starting it always requires an
explicit per-session trust action after Xunhen shows the resolved absolute path,
executable identity, arguments, working directory, and allowed environment
names. `ServerIdentity` contains the canonical absolute path, a content digest,
and available platform file identity. Xunhen rechecks it immediately before
each launch or restart, launches that resolved path without another `PATH`
search, and expires trust if the identity changes. This detects ordinary
replacement; it does not claim to contain a malicious local process that can
race the operating-system launch with the user's permissions. Xunhen sends no
edit, execute-command, code-action, rename, formatting, or
apply-workspace-edit request in v1 and refuses server requests that would
mutate files or execute a command.

## Rules that do not move

### Read-only describes Xunhen, not a language server

- Invoke Git with optional locks and lazy fetching disabled for every supported
  read-only operation.
- Detect partial and promisor repositories. Never fetch a missing object; report
  it as unavailable locally.
- Never stage, update the repository index, refresh it as a side effect, write
  refs, create worktrees, invoke hooks, or run maintenance.
- Never create cache, trust, configuration, or session files inside the
  repository.
- Keep Xunhen state in the platform user-data or cache directory.
- Detect and report repository, index, and worktree changes made by other
  processes. Never call them Xunhen changes.
- Xunhen-only and fake-server exact-artifact tests record relevant Git metadata
  before and after critical journeys and fail on an unexpected write.

A language server runs outside this guarantee. It may write generated files,
run project code, or trigger package tools under its own defaults. Starting one
requires explicit trust. Xunhen reports repository changes observed while a
server is running, but it does not claim that the server is read-only or that
Xunhen caused an external change. Real-server QA records any observed write and
attributes it to a process only when the evidence supports that conclusion.

Read-only does not mean race-free. Another process may change a file or Git
state while Xunhen reads it. Every result carries a generation and relevant
identity. Stale work is discarded rather than merged into newer state.

The promise covers repository content, the index, refs, repository
configuration, and Xunhen-owned state. Reading a file may update access-time
metadata on filesystems that still enable it. Xunhen does not claim to suppress
that operating-system behavior.

### Git is the authority

- Resolve the default `git` executable once and allow a user-owned absolute path
  override. Report the selected path and version before repository work.
- Use that executable through explicit argument arrays, never a shell command.
- Define and test a minimum supported Git version in Phase 0.
- Discover worktrees, resolve objects, classify changes, and produce patches
  through documented Git commands.
- Parse machine-oriented NUL-delimited output where Git provides it.
- Use resolved object IDs and explicit `--` path boundaries. For commands that
  accept pathspecs, pass exact repository paths with `--literal-pathspecs`;
  `--` alone does not disable globbing or pathspec magic.
- Preserve rename source and destination as separate lossless paths.
- Do not assume object IDs are always 40 hexadecimal characters.
- Do not recreate ignore, attribute, revision, merge, submodule, or worktree
  semantics from intuition.
- Do not use a search library or syntax highlighter as the authority for Git
  status.

Git invocations disable paging, optional locks, lazy fetching, external diff
programs, text-conversion programs, and configured filesystem-monitor commands.
For qualified Git versions, Xunhen sets `GIT_NO_LAZY_FETCH=1` and uses any
accepted command-level equivalent. Xunhen removes Git environment variables
that can redirect the repository, index, object store, pager, external diff, or
injected command configuration. It does not override Git's ownership safety
checks. These controls constrain qualified Git behavior; they do not sandbox a
compromised executable. Native no-mutation and network-isolation tests therefore
state the exact Git path and version under test.

Remove inherited `GIT_LITERAL_PATHSPECS`, `GIT_GLOB_PATHSPECS`,
`GIT_NOGLOB_PATHSPECS`, and `GIT_ICASE_PATHSPECS`, then select literal matching
explicitly for exact-path operations. User search patterns never become Git
pathspecs implicitly. Literal pathspec handling does not replace validation of
revision, object, or other non-pathspec arguments.

### Git LFS and external filters

Xunhen never launches `git-lfs`, a clean or smudge command, or a long-running
Git process filter. This applies to discovery, status, classification, and
indexing as well as patch acquisition. `git status` may run a clean filter;
disabling that filter can change its worktree status result.

Before any worktree-comparing operation, use qualified non-converting read-only
commands to enumerate bounded candidate paths from the captured index and
untracked paths, and inspect their effective filter attributes. Include tracked
filter paths even when a sanitized status command would omit them as clean.
Every command, including this preflight, runs under the no-external-execution
policy. Phase 1 qualifies suppression and coherent configuration and attributes
for its limited scope; Phase 2 extends that evidence to the full Git matrix. If
safe enumeration, suppression, or classification cannot be established, refuse
the operation visibly rather than run an unqualified status command.

In v1, a path naming an external filter has `IndeterminateExternalFilter`
worktree content status and an unavailable worktree patch. Neither clean nor
modified output obtained with the filter disabled establishes its ordinary
filtered status. Preserve independently established facts such as index changes,
untracked identity, deletion, and type changes. List indeterminate paths
separately from confirmed changes and mark the worktree summary incomplete;
never report `no changes` while this uncertainty remains. This is the explicit
exception to equivalence with ordinary Git worktree status. Commit-to-index and
commit-to-commit comparisons still use exact stored blobs and need no filter.

Commit, index, and historical views show the exact blobs stored by Git. A Git
LFS pointer therefore appears as a labelled pointer document. Current-worktree
source browsing and search use the exact bytes already present on disk. If the
worktree contains expanded LFS content, Xunhen may display and search it under
the ordinary file and memory limits, but it never obtains missing content.
Staged pointer blobs and expanded worktree files are not presented as if they
were the same snapshot.

### Git comparison bytes and worktree bytes

Disabling external filters and textconv does not disable Git's built-in
line-ending normalization, `ident` contraction, or `working-tree-encoding`
conversion. A worktree patch may therefore describe comparison bytes that differ
from the raw file. Keep `ComparisonBytes` and raw `DocumentBytes` distinct, with
separate digests and an explicit conversion classification. Do not assign a raw
worktree identity to transformed patch text.

Git remains the authority for the patch. Phase 2 qualifies a bounded mapping
from its comparison coordinates to raw worktree byte ranges for each supported
conversion. Mapping evidence binds the captured bytes and effective conversion
settings to that diff generation. CRLF handling must account for byte offsets;
`ident` may change columns within a line, and encoding conversion may change
every byte offset. Do not infer a mapping merely from matching line counts.

When a mapping is unavailable or stale, retain a labelled Git comparison view
but disable exact-position jumps into the raw worktree. Opening raw source
without a claimed target position remains an explicit separate action. V1
issues no semantic request directly from transformed comparison text. The user
may open raw source and request semantics there after its own byte-identity
gate passes. No conversion is silently treated as byte identity.

### Search is fast and bounded

- Catalogue source files only within the selected repository root.
- Never scan a filesystem root or the user's home directory implicitly.
- Do not follow symlinks.
- Exclude `.git` and honor the accepted ignore policy.
- Bound initial scan work, catalogue paths, retained metadata, cached content,
  query time, results, snippets, line length, file size, threads, and memory.
- Stream partial results with a visible `searching` state and observed result
  count. Never label a timed-out or limited search complete.
- Keep the latest query responsive while older work cancels.
- Validate every returned path again before opening it.

Xunhen owns the path catalogue, file reads, query lifecycle, result identity,
and resource accounting. Libraries perform matching on inputs Xunhen supplies.
There is one search implementation and no pluggable backend interface in v1.

### Search implementation

| Responsibility | Implementation |
| --- | --- |
| File enumeration and identity | Bounded traversal through the repository-root capability; lossless `RepoPath` values and generation-scoped `FileId` values |
| Ignore matching | `ignore` matchers built from bounded, safely read rules |
| Fuzzy path matching | `frizbee` over search text associated with `FileId`; bounded typo allowance and deterministic ties |
| Literal and regex content matching | `grep-searcher` and `grep-regex` over Xunhen-owned byte buffers |
| Fuzzy content matching | `frizbee` over bounded UTF-8 lines, with validated mappings back to raw byte ranges |
| File changes | Explicit refresh first; bounded `notify` events and rescans before Phase 3 completes |
| Git annotations | The repository session's qualified Git results |

Phase 3 pins released versions and reviews their APIs, licenses, dependencies,
unsafe code, allocation behavior, and native compatibility. No search library
may open a result path, create a second Git-status cache, or own repository
traversal implicitly. The implementation uses `grep-searcher::Searcher` with
`search_slice`; it does not use `search_path` or enable file-backed memory maps.
See the [searcher API](https://docs.rs/grep-searcher/latest/grep_searcher/struct.Searcher.html)
and [matcher API](https://docs.rs/frizbee/latest/frizbee/).

`FileEntry` retains its lossless path separately from its searchable text.
Non-UTF-8 names use a documented reversible escape projection. Search results
carry IDs, never paths reconstructed from display strings. IDs remain valid
only within their catalogue generation. Ties use a stable lossless path order
on the current platform. Persistent frecency and query-history databases are
outside v1; the first ranking uses match quality and path context.

Ignore policy includes repository `.gitignore` files, `.git/info/exclude`, and
global excludes selected by user-owned Git configuration. It does not load
`.ignore` or editor configuration. Tracked files remain candidates even when an ignore pattern
matches them; the qualified Git inventory establishes that fact. Exclude `.git`
at every depth and do not descend into submodules or nested repositories in v1.
Missing sparse files remain unavailable. Binary and oversized regular files may
appear as labelled path results, but content search and source display refuse
them. Symlinks and special files are excluded. Read ignore rules through the
same no-follow boundary; directory-walker flags alone do not prevent replacement
races. A refused rule file or enumeration error makes the catalogue incomplete.
The [`ignore` builder](https://docs.rs/ignore/latest/ignore/gitignore/struct.GitignoreBuilder.html)
accepts rules supplied by the caller.

Git metadata discovery identifies the permitted repository exclude file;
user-owned configuration identifies any global exclude file. These auxiliary
inputs may live outside the source root and use separately scoped, bounded
read-only access. Repository configuration cannot grant arbitrary outside-root
file access. Auxiliary paths never become searchable source candidates.

Content search uses the catalogue and reads candidate bytes on demand. It does
not retain a full-repository content index. Reuse the bounded document cache
only after identity validation. Check size and regular-file identity on the
opened handle, enforce the byte limit while reading, and reject a changed file
before publishing its matches. This applies before matching content or creating
snippets, as well as when following a retained result.

Plain, regex, and fuzzy modes are explicit. A zero-match plain search never
silently becomes fuzzy. Search is line-oriented in v1. Literal and regex modes
use raw bytes with transcoding and BOM stripping disabled. Fuzzy content mode
requires valid UTF-8. Each mode skips lines above its search-line limit and
reports an incomplete result with the reason; fuzzy mode does the same for
invalid UTF-8. The render limit alone never truncates bytes before matching.
Bound query length, regex compilation, fuzzy line size, matcher work, and worker
scratch space before invoking a library.

Every query captures a catalogue generation. Workers check cancellation and
deadlines during traversal, reads, and bounded matching batches, including
zero-match scans. A new query cancels older work; stale results cannot replace
it. Pagination belongs to that query generation and reuses retained matches.
At a result limit or deadline, report the observed count and an incomplete
state, not a guessed repository-wide total.

Watcher events invalidate affected documents immediately and schedule a bounded
catalogue rebuild. Coalesce events into one pending rescan. Retain at most one
published and one candidate generation, charging both to the shared budget;
cancel old queries before starting another rebuild. Publish complete or visibly
incomplete generations atomically. Queries inherit catalogue incompleteness and
never search a vector still being built. If both generations cannot fit, defer
refresh or invalidate and release the old one before rebuilding visibly.

An overflow, lost watch, watch-count limit, or failed rescan marks search stale
or unavailable until refresh succeeds. Watchers are hints; secure reads and
content identity checks remain required.

### Semantic truth requires byte identity

- Never map a historical position into the current worktree by line-number
  guesswork.
- Never request semantic data for a diff marker, hunk header, deleted line, or
  synthetic placeholder.
- Convert UTF-8, UTF-16, or UTF-32 LSP positions only according to the encoding
  negotiated with that server.
- Treat definitions, hover Markdown, symbols, diagnostics, progress, and server
  messages as untrusted input.
- Refuse edits, commands, dynamic executable configuration, and arbitrary
  server-driven file reads.
- Bound every frame, request, response, location list, Markdown block, log line,
  timeout, restart, and retained document.
- Stop and join every child process Xunhen owns.

### The terminal is an output boundary

- Remove or visibly escape control characters from paths, source, diffs,
  search results, Git messages, hover text, server logs, and errors before
  rendering.
- Preserve tabs, line endings, invalid UTF-8, and raw path bytes in domain data
  where the platform permits them. The safe display projection is separate.
- Never write repository-controlled bytes directly to the terminal.
- Never open a URL, execute a hyperlink, copy text, or launch another program
  without a focused user action.
- Restore terminal mode, mouse capture, cursor state, alternate screen, and
  bracketed-paste state on normal exit, handled failure, panic, and supported
  termination signals.
- Clamp dimensions, row counts, wrapping, and scrolling arithmetic before
  allocation or indexing.

Clipboard writes happen only through an explicit copy action and have a
compiled byte limit. The default uses a qualified platform clipboard API and
never shells out to `xclip`, `wl-copy`, PowerShell, or another command. OSC 52
is an explicit user option for terminals that support it. It is disabled by
default in remote sessions because it can cross the remote host and local
terminal boundary. Xunhen never reads the clipboard in v1.

### Performance never outranks correctness

- Correct file identity, line coordinates, search matches, Git state, and LSP
  positions are checked before timing is accepted.
- Measure release artifacts, not debug builds.
- Separate cold start, warm search, cached rendering, uncached rendering, and
  language-server startup.
- State cache conditions, input shape, sample count, independent runs, errors,
  timeouts, memory, CPU, and artifact size.
- Keep an optimization only when the same workload remains byte-correct and the
  gain exceeds measurement noise and the predeclared practical threshold.
- Never quote `fast`, `instant`, or `ultra fast` publicly without a readable
  report for the shipped version and workload.

### Configuration belongs to the user

V1 reads one user-owned configuration file from the platform configuration
directory. It does not load `.xunhen.*`, editor settings, arbitrary shell
fragments, or executable commands from a repository.

The configuration may define:

- theme and accessible display choices;
- action-to-key mappings;
- mouse-hover preference;
- limits within compiled maxima;
- an optional absolute path to the trusted Git executable;
- absolute language-server executable paths and argument arrays;
- language IDs, extensions, initialization options, and environment allowlists;
- user-facing behavior such as wrapping and side-by-side preference.

Unknown fields are rejected with a useful location. Configuration never raises
compiled safety maxima. Secret values do not belong in Xunhen configuration.

## Snapshot and navigation model

### Snapshots

```text
EmptyTree
Commit(ObjectId)
Index(IndexIdentity)
Worktree(WorktreeGeneration)
```

A comparison is an ordered pair:

```text
DiffSelection {
    old: Snapshot,
    new: Snapshot,
}
```

Examples:

```text
unstaged:       Index -> Worktree
staged:         HEAD -> Index
all tracked:    HEAD -> Worktree
untracked/ITA:  EmptyTree -> Worktree
commit:         Parent -> Commit
root commit:    EmptyTree -> Commit
two revisions:  BaseCommit -> TargetCommit
```

`IndexIdentity` is content-derived from the complete logical index, including
any split-index or shared-index state. File size, modification time, and a
before-and-after stat check are insufficient. One diff generation may combine
status, patch, and blob results only when every result is bound to the same
immutable index snapshot. Phase 1 qualifies ordinary-index capture on Linux;
Phase 2 adds split-index and shared-index cases, and Phase 7 qualifies native
macOS and Windows capture. A validated private snapshot outside the repository
is one possible procedure. Any private snapshot is bounded, owner-only,
session-temporary, and removed during normal cleanup and stale-cache recovery.
If the identity changes or cannot be
proved, Xunhen discards the generation after a bounded retry and reports it as
stale.

The `all` view is a composite, not a claim that one Git diff represents every
worktree state. Bounded Git status records classify tracked, untracked,
intent-to-add, ignored, and unmerged paths. Per-file Git patches represent
tracked comparisons. The accepted no-follow reader supplies untracked and
intent-to-add bytes only after Git classifies the path. Each row retains its
source classification.

Merge commits require an explicit parent number when the default first-parent
view is not intended. Combined merge diff presentation is deferred until it has
a clear interaction model. Xunhen never hides which parent was selected.

### Diff positions

Every rendered code row records:

- file-change identity;
- origin: context, addition, deletion, or metadata;
- optional old line number;
- optional new line number;
- byte range in the corresponding comparison representation where one exists;
- optional validated mapping to a raw worktree source range, with its identity
  and conversion classification;
- safe-display spans derived from, but not replacing, source bytes.

Headers and placeholders have no source coordinate. Navigation actions are
disabled on them. Comparison line numbers are not automatically raw worktree
byte coordinates; the conversion policy above governs worktree navigation.

### Document identity

```text
DocumentIdentity
├── RepositoryIdentity
├── RepoPath
├── Snapshot
├── ContentDigest
├── WorktreeGeneration
└── optional LspDocumentVersion
```

`ContentDigest` is an internal change detector, not a security signature. Git
object IDs retain their Git meaning. Xunhen does not describe either as proof of
authorship or trust.

### Navigation history

```text
DiffLocation -> SourceLocation -> DefinitionLocation -> ReferenceLocation
      ^                                                        |
      `-------------------- back / forward --------------------'
```

History entries own stable document identities and logical positions. They do
not retain widget references or raw pointers into caches. If the identity is no
longer available, the entry remains visible but cannot be opened silently as a
different document.

## Domain model

```text
RepositorySession
├── RepositoryIdentity
├── GitCapabilities
├── WorktreeState
├── DiffSession
│   ├── DiffSelection
│   ├── FileChange*
│   │   └── Hunk*
│   │       └── DiffRow*
│   └── DiffGeneration
├── FileCatalogue
│   ├── CatalogueGeneration
│   ├── FileEntry*
│   ├── SearchQuery
│   └── SearchResult*
├── DocumentStore
│   └── DocumentIdentity -> bounded immutable bytes
├── optional LanguageSession*
│   ├── ServerIdentity
│   ├── OpenDocument*
│   ├── RequestGeneration
│   └── SemanticResult*
└── NavigationHistory

App
├── ViewState
├── Focus
├── bounded CommandQueue
├── bounded EventQueue
├── owned ChildTask*
└── TerminalGuard
```

Git DTOs, matcher values, LSP DTOs, syntax spans, Ratatui widgets, configuration
records, and presenter messages do not become domain types.

### Principal types and decision points

| Type or type group | Responsibility | Decide in | Why then |
| --- | --- | --- | --- |
| `RepositoryIdentity`, `RepoRoot`, `RepoPath` | Represent one discovered non-bare repository and lossless relative paths | Phase 1, extended in Phase 2 | The first reader establishes identity; linked and sparse worktrees extend discovery |
| `GitVersion`, `GitCapabilities`, `ObjectFormat`, `ObjectId` | Record supported Git behavior without assuming SHA-1 | Phase 1 | The qualified Git matrix defines parsing and flags |
| `Snapshot`, `IndexIdentity`, `WorktreeGeneration`, `DiffSelection` | Name exactly which bytes are compared | Phase 1, extended in Phase 2 | Index-to-worktree identity comes first; commit and empty-tree comparisons follow |
| `FileChange`, `ChangeKind`, `WorktreeContentStatus`, `Hunk`, `DiffRow`, `SourceCoordinate` | Preserve confirmed change facts, indeterminate filter status, rename identity, hunk structure, and qualified line mapping | Phase 1, extended in Phase 2 | The first diff needs exact rows; broader Git states follow |
| `DocumentIdentity`, `ContentDigest`, `DocumentBytes`, `ComparisonBytes`, `ConversionKind`, `SourceMapping` | Distinguish raw source from Git comparison bytes and bind qualified mappings to one generation | Phase 1, extended in Phases 2 and 3 | Identity comes first; conversion mappings and unchanged files follow |
| `SafeText`, `DisplayPath`, `DisplayLine` | Project hostile bytes into terminal-safe text without corrupting domain data | Phase 1 and Phase 2 | Git and source fixtures provide the first control and encoding cases |
| `FileCatalogue`, `FileEntry`, `FileId`, `CatalogueGeneration` | Own lossless paths and immutable catalogue generations | Phase 3 | Repeated path queries need retained identities, not retained repository contents |
| `SearchQuery`, `SearchMode`, `SearchResult`, `SearchGeneration` | Own query limits, match coordinates, cancellation, and completeness | Phase 3 | The application defines search behavior independently of matcher DTOs |
| `LanguageId`, `ServerIdentity`, `ServerCapabilities`, `LspDocumentVersion` | Record one trusted child and its negotiated protocol behavior | Phase 4 | Real servers define capability and encoding variation |
| `SemanticRequest`, `SemanticResult`, `SynchronizedDocumentIdentity`, `SemanticUnavailableReason` | Bind source and target identities before a request, bound target discovery, and reject stale or unproven locations | Phase 4 | The first end-to-end semantic request proves both identity gates |
| `NavigationLocation`, `NavigationHistory` | Preserve exact back and forward movement across diff and source views | Phase 3, extended in Phase 4 | Source browsing needs history before semantic navigation adds locations |
| `RequestId`, `OperationGeneration`, `AppEvent` | Prevent stale async results from replacing current state | Phase 1, extended by later phases | Each asynchronous boundary earns the fields it needs |
| `Limits`, `MemoryBudget`, `BudgetReservation`, `TimeoutPolicy` | Enforce aggregate byte admission, local maxima, and user-selected lower values | Phase 0, with allocation ownership in Phase 1 | Resource measurements establish defensible starting values |
| `TerminalGuard`, `TerminalCapabilities` | Own setup, restoration, color, mouse, and width behavior | Phase 1 | The first usable diff must restore its terminal |

Do not create generic `Repository`, `SearchBackend`, `LanguageService`, or
`Renderer` traits in Phase 0. Start with concrete accepted implementations.
Extract one small consumer-owned trait only when a second implementation or a
focused deterministic seam has proved its value.

## Data structures and performance shape

The ordinary structures are enough when they match access patterns:

- `Vec<FileChange>` preserves Git order and supports cache-friendly movement;
- an optional `HashMap<RepoPath, Vec<FileIndex>>` handles direct lookup only if
  navigation needs it. One path may name several entries, such as a staged
  deletion and an untracked replacement at the same path;
- `Vec<Hunk>` and `Vec<DiffRow>` support indexed scrolling and binary search by
  source line;
- `VecDeque<NavigationLocation>` holds bounded back and forward history;
- bounded `mpsc` channels carry worker results without unbounded fan-in;
- `Arc<[u8]>` shares immutable document bytes between parsing and presentation
  only where measurement shows copying matters;
- a byte-budgeted least-recently-used cache retains documents, highlighted
  lines, and rendered diff windows;
- compact span vectors reference immutable buffers rather than allocating one
  styled string per terminal cell;
- `Vec<FileEntry>` owns one immutable path catalogue. A `FileId` combines its
  catalogue generation and entry index; replacing the catalogue invalidates
  those IDs. Matcher inputs and results refer back to these entries;
- no linked list, rope, arena, ECS, database, or custom allocator enters v1
  without a measured access pattern that needs it.

`Vec<FileChange>` is the source of truth for one immutable diff generation. A
path map is derived only if needed and is built after that vector is complete.
Rename and copy source paths remain fields on `FileChange`; they do not become
competing map keys. A generation change discards and rebuilds both collections.
Tests cover path collisions, including a staged deletion plus an untracked file
at the same path, and assert valid indexes and generation agreement so the two
collections cannot drift.

File enumeration builds one bounded candidate vector, sorts it by lossless
path, and publishes it as a catalogue generation. A repeated path query scans
that retained vector through the matcher. Content search reads files on demand.
Add a derived path lookup only when watcher or navigation measurements justify
it; rebuild it with the catalogue. There is no persistent content index or
matcher-owned copy of filesystem identity.

Xunhen is a browser, not an editor. Whole-document mutation is absent, so a rope
would add complexity without serving the principal workload. V1 reads worktree
files into bounded owned buffers; it does not memory-map mutable source files.

Visible-row rendering uses small bounded overscan. Scrolling does not format or
syntax-highlight the entire repository. Side-by-side alignment derives from
logical diff rows and measured terminal width. It never creates padding
proportional to hostile line length.

## Threat model

This is the design model for the full v1 scope, not a declaration that the
program is secure.

### Important assets

- confidentiality of source files inside and outside the selected repository;
- integrity of the repository, worktree, index, refs, configuration, and user
  files;
- the read-only product promise;
- terminal integrity and restoration;
- correctness of displayed diffs, paths, line numbers, and semantic answers;
- availability of the UI under large or malformed input;
- environment data inherited by child processes;
- user intent when launching a language server or copying content;
- release source, dependencies, CI credentials, and published artifacts.

### Main attackers and failures

- a malicious or merely strange repository, commit, path, blob, attribute,
  configuration, submodule, symlink, worktree, or object graph;
- a compromised or malicious `git` executable found on `PATH`;
- a compromised, malicious, buggy, or repository-influenced language server;
- malformed, oversized, delayed, reordered, duplicated, or unsolicited LSP
  traffic;
- terminal escape sequences and deceptive Unicode in names, code, diagnostics,
  hover Markdown, and errors;
- resource exhaustion from file count, diff size, line length, search matches,
  syntax work, child output, request fan-out, or rapid input;
- races with an editor, Git command, watcher, formatter, checkout, or build;
- path traversal, symlink following, path replacement, and time-of-check to
  time-of-use races;
- corrupt user configuration, cache, trust state, or interrupted cache writes;
- dependency, CI action, release credential, package, or artifact compromise;
- panic, signal, terminal disconnect, full disk, permission loss, and process
  death during cleanup.

### Required controls

| Boundary | Required control |
| --- | --- |
| Repository discovery | explicit root, non-bare v1 policy, Git ownership checks retained, environment redirection removed |
| Git process | no shell, explicit arguments, resolved object IDs, literal exact-path pathspecs, disabled pager, locks, lazy fetch, external diff, textconv, clean and smudge filters, process filters, fsmonitor, bounded output and time |
| Filtered worktree status | safe path enumeration before status, coherent attributes and filter suppression, explicit indeterminate status and incomplete summaries |
| Paths and files | lossless relative type, no traversal, no-follow access, regular-file policy, root capability, identity recheck |
| Git output | NUL-delimited machine formats, bounded parser, explicit encoding projection, fuzzing |
| Search catalogue and reads | lossless IDs, root-scoped no-follow traversal and reads, bounded ignore input, aggregate memory reservations, cancellation, generation checks |
| Source and diff rendering | separate comparison and raw bytes, proven coordinate mappings, terminal-safe projection, width-safe arithmetic, line and byte limits, explicit refusal |
| LSP launch | absolute resolved executable and identity, per-session trust, argument arrays, fixed working directory, restricted environment, no repository command config |
| LSP protocol | bounded JSON-RPC frames, capability checks, position encoding, request generations, timeout and restart budget |
| LSP authority | source and target identities captured before requests, bounded synchronization and fresh requests for unknown targets, edits and commands refused, historical semantics disabled |
| Terminal lifecycle | RAII guard, panic and signal restoration, no raw hostile output, native PTY tests |
| Clipboard and external tools | explicit focused action, no automatic URL or command execution |
| Local state | outside repository, owner permissions where supported, atomic bounded cache writes, corruption recovery |
| Release | locked graph, pinned immutable CI actions, dependency and license review, SBOM, provenance, checksum, exact-artifact QA |

Xunhen does not sandbox language servers. A trusted language server can read the
workspace, inherit allowed environment values, execute its own logic, and in
some cases cause builds or package-manager activity. The trust prompt and docs
say this plainly. Users should not launch language intelligence in an untrusted
repository. Xunhen strips nonessential inherited environment values where a
qualified server still functions and never sends repository-provided server
commands.

All parsers, collections, queues, workers, caches, retries, and child processes
have compiled ceilings. Reaching one produces a typed visible refusal. A
truncated patch, timed-out search, or partial location list is never labelled
complete.

## Required invariants

- Xunhen writes nothing inside the repository: no index update or refresh, ref
  write, hook, worktree creation, maintenance run, or state file.
- Every Git invocation is an explicit argument array, never a shell command,
  with the pager, optional locks, lazy fetch, external diff, textconv, clean and
  smudge filters, process filters, and fsmonitor disabled.
- No Git object is fetched. A missing object in a partial or promisor repository
  is an explicit locally unavailable result.
- One diff generation combines only results bound to the same
  `IndexIdentity` and `WorktreeGeneration`.
- `IndexIdentity` is derived from the complete logical index, including any
  split-index or shared-index state. Size, modification time, and a
  before-and-after stat check never stand in for it.
- `ComparisonBytes` and `DocumentBytes` stay distinct, with separate digests and
  an explicit conversion classification. A coordinate mapping between them is
  used only while it is qualified and bound to its generation.
- A path naming an external filter has `IndeterminateExternalFilter` status, is
  listed separately from confirmed changes, and marks the worktree summary
  incomplete. `no changes` is never reported while that uncertainty remains.
- Rename source and destination remain separate lossless paths. Neither becomes
  a competing key in a derived path map.
- A rendered row either carries a source coordinate or has navigation disabled.
  Headers and placeholders never acquire one.
- A semantic request is issued only when the displayed bytes equal the current
  worktree bytes most recently synchronized to that server, converted under the
  position encoding negotiated with it.
- A target location is usable only if its document was in that server's
  synchronized set before the request was written and still holds the same
  identity when the result is used. Synchronizing after a reply never validates
  it retroactively.
- A server restart invalidates the entire synchronized-document set. Closing a
  document invalidates its membership and every pending location for it.
- Location lists from different request generations are never merged.
- Xunhen sends no edit, execute-command, code-action, rename, formatting, or
  apply-workspace-edit request, and refuses server requests that would mutate
  files or execute a command.
- A language server launches only from an absolute resolved path whose
  `ServerIdentity` is rechecked immediately before launch or restart, after an
  explicit per-session trust action. A changed identity expires trust.
- Search covers only the selected repository root. Enumeration, ignore reads,
  content reads, and result opening enforce the no-follow boundary. Search text
  never replaces lossless path identity.
- All input-sized allocations reserve shared budget before growth. Queued
  values and retained generations keep their reservations until released.
- A truncated patch, timed-out search, partial location list, or limited result
  set is never labelled complete.
- Repository-controlled bytes never reach the terminal without a bounded safe
  projection. Domain data keeps the original bytes, including tabs, line
  endings, invalid UTF-8, and raw path bytes.
- Terminal mode, mouse capture, cursor state, alternate screen, and
  bracketed-paste state are restored on normal exit, handled failure, panic, and
  supported termination signals.
- Row, column, width, wrapping, and scrolling arithmetic is clamped before any
  allocation or indexing.
- Every asynchronous result carries a generation. A result that no longer
  matches current state is discarded, never merged into newer state.
- Every parser, collection, queue, cache, worker, retry, and child process has a
  compiled ceiling. Reaching one produces a typed visible refusal.
- Malformed input, unavailable files, child failure, timeouts, and unsupported
  capabilities produce typed errors. They never reach `panic!`, `unwrap()`, or
  `expect()`.
- The clipboard is written only through an explicit focused action under a
  compiled byte limit, and is never read.
- Xunhen state lives in the platform user-data or cache directory, outside the
  repository.

## Fault model

| Fault | Required result |
| --- | --- |
| `git` is missing, unresolvable, or below the minimum version | Refuse before repository work; `xunhen doctor` reports the exact path and version |
| Git subprocess exceeds its output or time budget | Cancel, kill after the deadline, return a typed refusal; no partial patch is labelled complete |
| Git emits malformed or truncated NUL-delimited output | The bounded parser rejects the generation; no partially parsed record enters domain state |
| The index changes while a diff generation is being built | Identity check fails; the generation is discarded after a bounded retry and reported stale |
| A worktree file changes between status and read | The generation is stale; rows are rebuilt rather than silently updated |
| Partial or promisor repository is missing an object | Explicit locally unavailable result; no fetch is attempted |
| A path names an external filter | `IndeterminateExternalFilter` status and an incomplete worktree summary |
| Line-ending, `ident`, or encoding conversion has no qualified mapping | The labelled comparison view is retained and exact-position jumps are disabled |
| A symlink or path is replaced between check and open | No-follow access and identity recheck refuse the open |
| File exceeds the size limit or is binary | Labelled path entry if enumeration succeeded; content search and display refuse it without treating partial bytes as a document |
| A line exceeds the compiled maximum | Bounded projection with an explicit truncation marker |
| Search matches exceed the result limit | Visible incomplete state with the observed match count; no claim to know the full total |
| Search exceeds its query deadline | The `searching` state ends as timed out, never as complete |
| A newer query arrives while a search runs | Older work cancels; the latest query stays responsive |
| The catalogue is incomplete, its watcher fails, or events overflow | Search is visibly incomplete, stale, or unavailable; a bounded refresh may recover it while diff and source views continue |
| Shared allocation budget is exhausted | Evict eligible caches, then defer or refuse new work; keep UI and shutdown capacity reserved |
| A language server fails to start or exits | Typed unavailable reason; no retry beyond the restart budget |
| Server identity changed since the trust decision | Trust expires and no launch occurs |
| Server sends an oversized or malformed JSON-RPC frame | The frame is rejected under the bounded reader; repeated failure ends the session |
| Server sends an unsolicited request or one that would mutate files | Refused; no edit, command, or file write is applied |
| Server does not answer within the request deadline | The request generation is cancelled and a late result is discarded on arrival |
| Target discovery exhausts attempts, targets, bytes, or time | Explicit unavailable result, or a visibly incomplete list of validated locations |
| Hover Markdown or diagnostics carry escapes or deceptive Unicode | Safe projection only; terminal state is unchanged |
| Terminal resizes to a degenerate size | Layout clamps; nothing allocates from raw terminal dimensions |
| A task panics | The terminal guard restores terminal state, then the process exits with a diagnostic |
| `SIGINT`, `SIGTERM`, or terminal disconnect arrives | Stop input, cancel requests, ask children to exit, kill after the deadline, join owned tasks, restore the terminal |
| A cache write is interrupted or the cache is corrupt | The atomic bounded write discards the partial; a corrupt cache is rebuilt rather than trusted |
| Disk is full or permission is lost while writing state | Typed error; browsing continues without the cache |
| Configuration is invalid or names an unknown field | Rejected with a useful location before any repository work; compiled maxima are never raised |

## Initial resource budget

These are starting engineering limits, not measured capacity claims. Phase 0
implements only limits used by its CLI and configuration. Phase 1 adds shared
memory admission; later phases add their own sublimits and measurements.
Configuration may lower a value; raising a compiled maximum requires a reviewed
code change and fresh qualification.

| Resource | Initial limit |
| --- | ---: |
| Accounted live allocations across Xunhen | 384 MiB |
| Reserved capacity within that budget for UI and shutdown | 16 MiB |
| Catalogue files | 1,000,000 |
| Retained catalogue metadata, including search text | 256 MiB |
| Traversal entries visited per scan | 2,000,000 |
| Traversal depth | 256 |
| Ignore input per file / total per scan | 1 MiB / 16 MiB |
| Changed files per diff generation | 20,000 |
| Hunks per file | 5,000 |
| Diff rows per file | 200,000 |
| Retained parsed diff data across generations | 64 MiB |
| File size for display and search | 16 MiB |
| Rendered line length | 8 KiB |
| Content-search line length | 1 MiB |
| Fuzzy content-search line length | 8 KiB |
| Query bytes / fuzzy query bytes | 4 KiB / 256 B |
| Compiled regex size | 8 MiB |
| Document cache | 128 MiB |
| Highlighted-line cache | 32 MiB |
| Rendered diff-window cache | 16 MiB |
| Overscan rows beyond the viewport | 64 |
| Retained search results | 10,000 |
| Snippet bytes per result | 512 |
| Search query deadline | 5 s |
| Catalogue scan or rescan deadline | 60 s |
| Active filesystem watches | 16,384 |
| Pending coalesced rescans | 1 |
| Concurrent search and scan workers combined | 4 |
| Search worker scratch space combined | 32 MiB |
| Concurrent Git subprocesses | 4 |
| Cumulative output per Git subprocess | 64 MiB |
| Retained Git output across subprocesses | 64 MiB |
| Git subprocess deadline | 30 s |
| Language servers per session | 4 |
| Open documents per server | 64 |
| LSP frame | 16 MiB |
| LSP request deadline | 10 s |
| Server restarts per session | 3 |
| Target discovery attempts per request | 8 |
| Distinct discovery targets per request | 16 |
| Target discovery bytes | 32 MiB |
| Target discovery elapsed time | 15 s |
| Locations per result list | 5,000 |
| Hover Markdown bytes | 64 KiB |
| Retained server stderr | 1 MiB |
| Navigation history entries | 256 |
| Command queue depth | 256 |
| Event queue depth | 1,024 |
| Queued payload bytes across both queues | 8 MiB |
| Clipboard write bytes | 1 MiB |
| Log line bytes | 4 KiB |
| Retained log on disk | 32 MiB |
| Xunhen peak resident memory during qualification | 512 MiB |
| Shutdown deadline | 10 s |

### Shared memory admission

The per-resource limits are ceilings, not independent reservations. Their sum
may exceed 384 MiB; the shared budget decides which work can coexist. Count
allocation capacity, matcher inputs and scratch, collection overhead, parsed
and encoded protocol data, and both old and replacement generations. Charge a
shared immutable buffer once and keep its reservation alive with its storage.
Queue depth alone never permits a large unaccounted payload.

Reserve before reading or allocating, extend the reservation before growth,
and release it on completion or cancellation. Bound library allocations through
their configuration and admitted input sizes. If a dependency's working memory
cannot be bounded, it cannot enter that operation. Evict unpinned render,
highlight, and document cache entries first, then defer background scans or
refuse new work. Do not evict bytes still required by a visible document or
pending identity check. Workers cannot consume the 16 MiB UI and shutdown
reserve.

The 512 MiB RSS ceiling is measured separately: allocator overhead, thread
stacks, and native library state prevent an allocation ledger from proving an
exact portable RSS bound. Git and language-server process memory is reported
separately from Xunhen's RSS; process-count, I/O, and lifetime limits still apply
to those children. Failure of either the allocation budget or measured RSS
ceiling holds qualification. A million-path limit is not a promise that every
million-path repository fits: long paths or other active work may exhaust the
byte budget first, producing an explicit incomplete catalogue.

A 16 MiB document is held once as immutable bytes. Diff rows reference compact
spans, and only the viewport plus overscan is formatted. Stream Git output into
bounded parsing; a 64 MiB cumulative output allowance does not require retaining
64 MiB for each child. LSP document admission also accounts for encoded frame
size: if synchronization cannot fit the frame and shared budgets, semantics are
unavailable for that document while source browsing remains usable.

## Rust shape

Start with one package:

```text
xunhen/
├── Cargo.toml
├── Cargo.lock
├── mise.toml
├── rust-toolchain.toml
├── README.md
└── src/
    ├── lib.rs
    └── main.rs
```

Keep the first vertical slice close. Add modules when code gives them ownership:

```text
src/
├── app.rs
├── config.rs
├── limits.rs
├── repository/
├── diff/
├── document/
├── search/
├── language/
├── terminal/
└── ui/
```

Do not begin with a Cargo workspace or one crate per noun. A single binary is
the product and one package is the clearest initial shape. Extract a library or
workspace member only when packaging, fuzzing, benchmarking, or a second real
consumer creates a boundary that modules cannot express cleanly.

### Ownership

- `App` owns mutable UI state and applies events on one logical UI owner.
- `TerminalGuard` owns terminal mode for exactly one process lifetime.
- one repository session owns Git capabilities, worktree generation, index
  snapshots, file catalogue, document cache, language children, and navigation
  history;
- each Git operation, search, and LSP request has an owner, cancellation path,
  deadline, output budget, and result generation;
- worker tasks return owned domain values. They never hold widget references;
- child stdout is protocol data, child stderr is bounded diagnostic data, and
  neither is written directly to the terminal;
- blocking filesystem and process work uses bounded workers when it cannot be
  made cancellation-safe;
- shutdown stops input, cancels requests, asks children to exit where the
  protocol permits it, kills after a deadline, joins owned tasks, then restores
  the terminal;
- the main package uses `#![deny(unsafe_code)]`. If a future native boundary
  requires project-owned unsafe code, move that adapter into a separate,
  narrowly scoped crate with its own lint policy, local `SAFETY` explanations,
  documented contracts, and native tests. That real boundary earns a workspace;
- libraries return typed errors where callers make decisions. The executable
  adds operation context and presenter text;
- expected malformed input, unavailable files, child failure, timeouts, and
  unsupported capabilities never use `panic!`, `unwrap()`, or `expect()`.

### Planned stack

| Need | Planned choice |
| --- | --- |
| Language | Rust 1.98.1 initially, edition 2024, Cargo resolver 3 |
| TUI | Ratatui 0.30.2 with Crossterm 0.29.0, first qualified on Linux in Phase 1 |
| Async and processes | Tokio with only required features |
| CLI | clap |
| Git | installed Git CLI through explicit process arguments |
| Path catalogue and ignores | Xunhen-owned traversal and lossless IDs, with `ignore` matchers |
| Fuzzy matching | `frizbee` for paths and bounded UTF-8 content lines |
| Literal and regex content search | `grep-searcher` and `grep-regex` over owned byte buffers |
| Filesystem events | `notify` under bounded event, watch, rescan, and shutdown policies |
| Syntax presentation | syntect or Tree-sitter candidate selected by measured coverage, size, safety, and viewport cost |
| LSP | Language Server Protocol 3.18 subset through a small stdio JSON-RPC client using maintained protocol DTOs; no server framework |
| Paths and text | standard `Path` types plus lossless byte-oriented helpers where required |
| Config and DTOs | serde, JSON for LSP, TOML for user config |
| Errors | thiserror for decision-bearing errors, anyhow at the executable boundary |
| Logging | tracing with bounded redacted file output outside the repository |
| Property tests | proptest where generated cases protect stated invariants |
| Fuzzing | cargo-fuzz for implemented parsers and terminal-safe projection |
| Benchmarks | Criterion or Divan for isolated hot paths, exact release binary harness for user actions |
| Dependency policy | cargo-deny and cargo-audit with reviewed advisories and licenses |
| Release | GitHub Actions invoking pinned `dist` tasks and publishing immutable GitHub Releases |

Rust 1.98.1 is the initial pin, reviewed on 2026-09-13. Review later stable and
patch releases intentionally. Do not promise an MSRV during early `0.x`
development. Commit `Cargo.lock`, specify only required dependency features,
and use `--locked` for graph-resolving CI and release commands.

### Toolchain and task contract

Xunhen uses both files, with one responsibility each:

- `rust-toolchain.toml` pins the Rust channel, profile, and required components
  such as `rustfmt` and Clippy;
- `mise.toml` reads that Rust pin, pins the remaining development tools, and
  defines the tasks used locally and in CI.

Documentation, local development, and handwritten CI invoke named `mise run`
tasks. They do not grow a second collection of direct Cargo, cargo-dist, shell,
or platform-specific commands. Tasks such as `fmt`, `lint`, `test`,
`build`, `audit`, `deny`, `check-all`, `dist-init`, `dist-plan`, and
`dist-build` remain small wrappers around pinned tools and explicit arguments.
Keep the Rust version only in `rust-toolchain.toml`; mise reads it through its
built-in support for that file.

`dist` configuration lives in the canonical location produced by the pinned
version. Initialization, planning, building, and validation run through `mise`
tasks. GitHub Actions uses the generated release workflow, and CI checks that it
matches the pinned configuration.

The listed Ratatui and Crossterm versions are planning inputs; Phase 1 still
qualifies their actual terminal behavior. Search dependencies enter in Phase 3
with exact released versions and explicit features recorded in `Cargo.lock`.
Library selection does not certify Xunhen's file access, cancellation, or memory
behavior. The integration must pass those checks on each supported platform.

## Roadmap

### How to use this roadmap

- The phase checkbox is the progress board.
- Check a task only when its behavior and focused evidence are complete.
- Check a phase only when every item under "Done when" is true.
- Make high-value tests fail once for the expected reason before trusting them.
- Add modules, dependencies, CI jobs, fixtures, documents, fuzz targets,
  benchmarks, and packaging in the phase that first uses them.
- Each phase measures its implemented boundaries and freezes budgets before
  public performance claims.
- Linux, macOS, and Windows are all required for v1. Support is earned natively,
  one platform at a time.

The first useful slice is the Phase 1 exact-binary diff journey described in
"Start here." Search and language intelligence do not begin until that slice
is usable and terminal-safe.

### Progress board

- [x] **Phase 0** - GitHub repository, one package, limits, CLI shell, and Linux CI
- [ ] **Phase 1** - First usable Linux diff, `0.1.0`
- [ ] **Phase 2** - Git coverage and diff presentation, `0.2.0`
- [ ] **Phase 3** - Whole-repository search and source browser, `0.3.0`
- [ ] **Phase 4** - Worktree-exact language intelligence, `0.4.0`
- [ ] **Phase 5** - Complete review workflow and feature alpha, `0.5.0`
- [ ] **Phase 6** - Linux native beta, `0.6.0`
- [ ] **Phase 7** - macOS and Windows native beta, `0.7.0`
- [ ] **Phase 8** - Security, resilience, and performance qualification, `0.8.0`
- [ ] **Phase 9** - Compatibility freeze, documentation, and release beta, `0.9.0`
- [ ] **Phase 10** - Release candidates and v1 publication, `1.0.0`

These phases are dependency ordered, not calendar promises. Later evidence may
move the release date, but calendar pressure does not remove promised v1
behavior or its release gates.

### Phase 0: GitHub repository, one package, limits, CLI shell, and Linux CI

#### Build now

- [x] Create the project repository on GitHub under Nuggocto's account and add
  Linux GitHub Actions CI using the same mise tasks as local development.
  Release workflows enter in Phase 6.
- [x] Create one edition 2024 binary package with `src/lib.rs`, `src/main.rs`,
  and no workspace.
- [x] Add Apache-2.0 licensing, `Cargo.lock`, explicit dependency features, a
  small lint policy, and `deny(unsafe_code)` for the main package.
- [x] Add `rust-toolchain.toml` with Rust 1.98.1, minimal profile, and required
  components.
- [x] Add `mise.toml` with pinned supporting tools and concise fmt, lint,
  test, build, audit, deny, and check-all tasks. Make these tasks the local and
  CI command contract and read the Rust pin from `rust-toolchain.toml`.
- [x] Add typed limits for the implemented CLI and configuration, with checks
  for defaults, lower values, maxima, and rejected higher values. Record the
  shared-memory design; implement reservations when Phase 1 allocates work.
- [x] Select the minimum Git version for the first Linux target. Record the
  resolved executable path, absolute override behavior, and the fact that Git is
  trusted outside Xunhen's containment boundary.
- [x] Implement `xunhen --help`, `xunhen version`, `xunhen doctor`,
  `xunhen completions <shell>`, and concise structured version output. Doctor
  reports the resolved Git path, version, and whether an absolute override is
  active without scanning a repository.
- [x] Add user-config discovery, strict parsing, and useful corrupt-config
  diagnostics without scanning a repository. Create cache or temporary storage
  only when an implemented operation needs it.
- [x] Add bounded redacted logs outside the repository. Prove that source text,
  hover text, environment values, and raw child messages are absent by default.
- [x] Keep the threat model and Phase 0 tracking in this file.

#### Done when

- [x] A clean checkout builds and tests with one documented mise task.
- [x] The active Rust compiler matches `rust-toolchain.toml`, and the mise task
  graph contains no conflicting toolchain pin or duplicate implementation.
- [x] Help and version work without a repository, file catalogue, language server,
  or network access.
- [x] Invalid limits and configuration fail before terminal raw mode begins.
- [x] The dependency graph contains only code used by this phase.
- [x] The Release binary starts, reports its version, and exits cleanly on the
  first supported Linux target.

### Phase 1: first usable Linux diff, `0.1.0`

Build the smallest complete review journey before expanding Git compatibility.
This release supports `xunhen changes --scope unstaged` for ordinary tracked
text modifications in a non-bare Linux repository with a regular, non-split
index. Unsupported commands, repository layouts, conversions, and change kinds
receive explicit unavailable results. Bare `xunhen` gains its complete-change
behavior in Phase 2. The README and help state the implemented scope.

#### Build now

- [ ] Implement bounded repository discovery, Git executable and capability
  checks, environment sanitization, and ownership checks.
- [ ] Capture an immutable ordinary index in bounded, owner-only temporary
  storage outside the repository. Bind status, patch, and old-blob reads to its
  identity; qualify cleanup and stale-snapshot recovery.
- [ ] Establish the no-follow root capability and bounded regular-file reader
  used by later source and search work. Preserve lossless paths internally.
- [ ] Preflight effective attributes and suppress pager, optional locks, lazy
  fetching, external diff, textconv, clean, smudge, process filters, and
  fsmonitor before worktree operations. Refuse safely if suppression or
  coherent configuration cannot be established.
- [ ] Parse NUL-delimited classifications and use literal exact-path arguments.
  Display unsupported entries explicitly. Filter paths are indeterminate and
  prevent a complete or no-changes summary.
- [ ] Acquire bounded per-file patches and parse context, addition, deletion,
  and metadata rows with exact old and new coordinates. Admit raw-source
  navigation only for proven byte-identical text; refuse transformed cases.
- [ ] Introduce shared memory reservations for buffers, parsed diffs, queues,
  workers, and retained generations before those allocations can grow.
- [ ] Add Ratatui and Crossterm, one UI-state owner, and one terminal guard.
  Render a plain-text unified diff, file list, line numbers, selected hunk,
  progress, errors, and unavailable states.
- [ ] Add file, hunk, page, and horizontal movement plus a discoverable exit
  action. Handle narrow terminals and resize with bounded layout arithmetic.
- [ ] Run Git and file work outside rendering. Cancel abandoned requests and
  reject results from older operation generations.
- [ ] Add terminal-safe projections for source, paths, errors, and metadata.
- [ ] Exercise real disposable repositories for ordinary changes, no changes,
  literal unusual paths, hostile filters, missing objects, unsupported states,
  concurrent changes, limits, and malformed or truncated output.
- [ ] Add focused coordinate and projection tests, small parser fuzz targets,
  and PTY journeys for startup, movement, resize, cancellation, failure, panic
  restoration, and exit. Record repository state and network attempts.
- [ ] Run the exact Release binary with a user reviewing a small real change.
  Record confusing actions and fix the review journey before Phase 2.

#### Done when

- [ ] A user opens the supported unstaged diff, moves between files and hunks,
  reads exact old and new coordinates, cancels loading, and exits cleanly.
- [ ] Unsupported cases cannot become ordinary diffs or a false no-changes
  result. The first release makes no broader Git or platform support claim.
- [ ] Unsafe text never reaches the terminal; literal paths remain literal.
- [ ] The tested operations cause no repository write, filter execution, or
  network attempt. Concurrent changes and budget exhaustion produce typed
  outcomes without panic.
- [ ] Terminal restoration and child cleanup pass the Linux PTY suite.
- [ ] The exact `0.1.0` artifact completes the first usable diff journey,
  with recorded latency, peak RSS, limitations, and user feedback.

### Phase 2: Git coverage and diff presentation, `0.2.0`

Extend the working TUI and its existing types. Retain Phase 1 regression
journeys while qualifying each additional Git behavior.

#### Build now

- [ ] Add bare `xunhen`, all and staged scopes, `commit`, `compare`, and
  explicit `file` opening. Resolve user revisions once and pass only resolved
  object IDs to later commands.
- [ ] Extend discovery and immutable index capture to linked worktrees, sparse
  worktrees, split/shared indexes, and both qualified object formats.
- [ ] Complete empty-tree, commit, index, and worktree comparisons. Cover
  root and unborn histories, explicit merge parents, renames, copies when Git
  reports them, mode and type changes, conflicts, submodules, binary files,
  tracked additions, untracked files, intent-to-add entries, and deletions.
- [ ] Build the composite all-changes view from Git classifications. Preserve
  distinct states at the same path, such as staged deletion plus an untracked
  replacement. Mark external-filter uncertainty separately.
- [ ] Extend filter suppression and effective-attribute coherence to the full
  command matrix. Show stored LFS pointers and locally expanded source under
  their actual snapshot identities; never download content.
- [ ] Qualify comparison-to-source mappings or exact-position refusal for
  CRLF, expanded `ident`, and `working-tree-encoding`. Matching line counts
  alone never establishes a mapping.
- [ ] Add source viewing for either diff side, exact diff-to-source movement,
  side-by-side presentation, optional wrapping, changed-line movement, and
  literal search within the current diff or source buffer. Repository-wide
  search and its matching libraries remain in Phase 3.
- [ ] Select and integrate the syntax highlighter using coverage, malformed
  input, viewport work, binary size, dependencies, and license evidence.
  Retain a bounded plain-text fallback.
- [ ] Complete empty, detached, invalid-revision, binary-only, unavailable,
  permission, stale-generation, long-line, large-patch, and timeout views.
- [ ] Extend deterministic fixtures to every added state, including partial
  clones with missing objects, split-index races, external filters, inherited
  pathspec settings, raw path bytes, Unicode, and missing final newlines.
- [ ] Compare classifications and coordinates with the qualified Git executable,
  including reviewed empty-tree projections for untracked and intent-to-add
  bytes. Expand property and fuzz tests only for new parsing boundaries.
- [ ] Extend presenter and PTY checks for side-by-side layout, source movement,
  no-color mode, narrow terminals, cancellation, and terminal restoration.

#### Done when

- [ ] All declared v1 comparison modes are usable in the Linux TUI. Root
  commits and merge parents have an explicit correct base.
- [ ] The all-changes view includes every declared state once, permits distinct
  states at the same path, excludes ignored untracked files, and never flattens
  conflicts or indeterminate filter status into ordinary changes.
- [ ] Unusual paths remain lossless and terminal-safe. Exact-path operations
  cannot expand wildcard or leading-colon filenames.
- [ ] Transformed comparison text either maps through qualified evidence or
  refuses exact-position navigation.
- [ ] Full Git fixtures pass the no-write, no-filter, no-fetch, stale-generation,
  resource, and terminal-cleanup checks.
- [ ] The exact `0.2.0` artifact completes diff and source review across the
  supported Git matrix before repository-wide search begins.

### Phase 3: whole-repository search and source browser, `0.3.0`

#### Build now

- [ ] Pin released `ignore`, `frizbee`, `grep-searcher`, `grep-regex`, and
  `notify` versions with explicit features. Review their licenses, unsafe and
  native dependencies, bounded input contracts, and shutdown behavior.
- [ ] Build the application-owned catalogue through the existing root
  capability. Enforce entry, depth, ignore-input, path-byte, deadline, worker,
  and shared-memory limits during enumeration.
- [ ] Retain lossless paths and generation-scoped IDs. Add reversible search
  text for non-UTF-8 names; never reconstruct an open path from that text.
- [ ] Add typo-tolerant path matching with `frizbee`, deterministic ties,
  visible indexing progress, and Git annotations from the repository session.
- [ ] Add explicit plain, regex, and fuzzy content modes with smart case,
  path constraints, raw-byte match coordinates, bounded snippets, pagination,
  cancellation, and incomplete-state reporting.
- [ ] Search owned buffers with `grep-searcher` and `grep-regex`; fuzzy-match
  only eligible UTF-8 lines. Enforce compilation, line, scratch, result, and
  time limits even when no match occurs.
- [ ] Reuse the source viewer for changed and unchanged worktree files and Git
  blobs. Add bounded diff-to-source-to-search history with an exact return row.
- [ ] Implement explicit refresh, then bounded watcher events and catalogue
  rebuilds. Invalidate stale content and results; expose lost watches and event
  overflow instead of claiming freshness.
- [ ] Validate results against their catalogue and document identities before
  navigation. Reopening uses the same no-follow reader as initial content
  acquisition.
- [ ] Exercise raw filename collisions, symlink and parent-directory replacement,
  tracked-but-ignored files, nested repositories, binary and oversized entries,
  invalid UTF-8, ignore failures, changed content, and sparse paths.
- [ ] Test zero-match cancellation, rapid query replacement, deterministic
  ranking, limited counts, stale pagination, watcher loss, and budget exhaustion
  while old and replacement catalogues coexist.
- [ ] Benchmark cold enumeration, retained-catalogue path queries, uncached and
  cached content queries, cancellation, source movement, peak RSS, CPU, and
  shutdown on versioned reference workloads. Do not infer speed from upstream
  matcher benchmarks.

#### Done when

- [ ] Search finds expected changed and unchanged files under the declared
  policy, preserves distinct raw paths, and never reads through a symlink or
  outside the selected root.
- [ ] File growth, invalid text, ignored rules that cannot be read, and scan or
  query limits produce explicit refusal or incomplete state.
- [ ] Plain searches never silently become fuzzy. Match coordinates refer to
  the captured raw bytes, not a sanitized snippet.
- [ ] Superseded work cannot replace current results. Cancellation and shutdown
  release reservations, owned workers, and watchers within their budgets.
- [ ] Search passes its correctness, latency, and memory budgets on declared
  workloads; failures require implementation changes before qualification.
- [ ] The exact `0.3.0` artifact completes diff to source to search to diff as
  one usable journey.

### Phase 4: worktree-exact language intelligence

Start with the fake-server protocol tests, then one pinned `rust-analyzer`
fixture for hover, cross-file definition, and return to the original diff.
Validate that journey with users before adding more servers or semantic actions.
Keep the worktree-only and outside-root restrictions visible during that trial.
The complete server and operation matrix remains a v1 requirement.

#### Build now

- [ ] Add explicit per-session trust for the selected repository and exact
  language-server executable identity before any server starts.
- [ ] Parse the user-owned absolute server executable path and argument array
  without a shell. Resolve and identify the executable once, then show its path,
  identity, arguments, working directory, and allowed environment names before
  trust is granted. Launch that path without another `PATH` search.
- [ ] Implement bounded stdio JSON-RPC framing, initialization, capability
  negotiation, request IDs, cancellation, timeouts, progress, shutdown, exit,
  kill deadline, and restart budget.
- [ ] Implement `didOpen`, bounded `didChange` or close-and-reopen behavior for
  current worktree documents, and `didClose` with exact document versions.
- [ ] Preserve ordered synchronization and request writes on each server's
  stdin. Tests prove a semantic request is never written before the matching
  `didOpen` or `didChange` notification.
- [ ] Negotiate and test UTF-8, UTF-16, and UTF-32 position encodings supported
  by the qualified server matrix.
- [ ] First add hover and definitions where advertised. Add type definitions,
  implementations, references, and document symbols after the initial journey
  passes its identity and usability checks.
- [ ] Add back and forward history across diff and worktree source locations.
- [ ] Gate every outgoing semantic request and incoming location through
  source and target identities captured before that request, server document
  versions, and current worktree digest checks. For unsynchronized targets,
  discard first-reply coordinates, synchronize bounded candidate documents, and
  reissue from the still-valid source after ordered synchronization writes.
- [ ] Enforce attempt, target-count, byte, open-document, and elapsed-time limits
  for target discovery. Return explicit unavailable or incomplete results when
  proof is unavailable, including mixed validated and unvalidated reference lists.
- [ ] Refuse `workspace/applyEdit`, rename, formatting, code action,
  execute-command, arbitrary dynamic executable registration, and server-driven
  file mutation.
- [ ] Sanitize and bound hover Markdown, messages, diagnostics, progress, paths,
  URIs, locations, and stderr before presentation or logging.
- [ ] Reject non-file and outside-root navigation targets in v1 with an explicit
  reason.
- [ ] Add a deterministic fake language server for unit and integration tests.
- [ ] Qualify one pinned `rust-analyzer` journey. Record whether target
  synchronization, unavailable historical semantics, and outside-root refusal
  still allow useful review; fix navigation and explanations before expanding.
- [ ] Extend the same contract to pinned `gopls`, `zls`, and `clangd` versions
  on representative small fixtures, without server-specific identity shortcuts.
- [ ] Test malicious frames, oversized content lengths, invalid JSON, duplicate
  IDs, late replies, unsolicited edits, outside-root locations, executable
  replacement before launch and restart, encoding edges, child crash, hang,
  restart loop, stderr flood, and shutdown refusal.
- [ ] Test a delayed A-to-B location after B changes, previously unopened B,
  target changes between synchronization and use, close/reopen invalidation,
  fresh replies naming another target, partial reference lists, and
  target-discovery budget exhaustion. Prove that opening B after a reply never
  validates the old coordinates.

#### Done when

- [ ] Hover and navigation work on a byte-identical current worktree document
  for every qualified server.
- [ ] Old, staged-but-not-worktree-identical, deleted, and historical documents
  receive a clear unavailable reason and no semantic result.
- [ ] A stale reply cannot appear after selection, bytes, server generation, or
  request generation changes.
- [ ] Cross-file navigation uses only locations produced after both source and
  target synchronization, with unchanged identities at use. Unknown targets
  require a fresh request, and exhausted discovery cannot produce false
  completeness.
- [ ] Transformed Git comparison text cannot issue an LSP request; opening raw
  source establishes its own document identity and semantic eligibility.
- [ ] No server request can make Xunhen edit a file or execute a command.
- [ ] Replacing the resolved server executable invalidates trust before launch
  or restart.
- [ ] All child processes end by graceful shutdown or bounded forced
  termination, then are joined.
- [ ] Tagged `0.4.0` has real-server evidence and states the unqualified server
  surface honestly.

### Phase 5: complete review workflow and feature alpha

#### Build now

- [ ] Join diff, current-worktree file search, content search, source viewing,
  hover, definitions, references, and navigation history into one coherent
  keyboard-first flow.
- [ ] Add optional mouse focus and bounded hover dwell without breaking the
  default terminal text-selection contract.
- [ ] Add a discoverable action palette, contextual help, and editable keymap
  validation without arbitrary command execution.
- [ ] Add accessible themes, no-color mode, non-color state symbols, focus
  visibility, configurable wrapping, and small-terminal fallback.
- [ ] Add explicit comparison, snapshot, worktree-search, LSP availability,
  indexing, stale, partial, and error labels to every relevant view.
- [ ] Add copy-path, copy-location, and copy-selected-text actions with terminal
  capability checks, byte limits, qualified platform writes, opt-in OSC 52,
  clear confirmation, no external command, and no clipboard read.
- [ ] Decide whether LSP inlay hints add useful review information without
  clutter. Add them only with a user toggle, viewport bounds, and real-server
  evidence; otherwise defer them.
- [ ] Add session-only visited-file and visited-hunk markers if user testing
  proves they help. Do not call them persisted review completion.
- [ ] Remove duplicate panels, speculative settings, unused abstractions,
  redundant key paths, and dependencies that no longer earn their cost.
- [ ] Run task-based sessions against the complete alpha on
  small changes, rename-heavy refactors, large diffs, unfamiliar repositories,
  and repositories without a language server.

#### Done when

- [ ] A new user can discover the principal workflow from contextual help.
- [ ] The complete product remains useful with no language server installed.
- [ ] Search and semantic navigation never blur current worktree and historical
  snapshot identity.
- [ ] Keyboard-only, no-color, narrow-terminal, and mouse-disabled journeys are
  complete.
- [ ] No feature in the alpha mutates Git or repository files.
- [ ] Tagged `0.5.0` is the first feature-complete alpha rather than a promise
  of platform support.

### Phase 6: Linux native beta

#### Build now

- [ ] Qualify current declared Linux distributions, glibc or musl choices,
  architectures, Git versions, filesystems, locales, terminals, tmux, and SSH
  sessions.
- [ ] Test XDG config, cache, logs, permissions, read-only worktrees, linked
  worktrees, symlinks, sparse worktrees, and terminal disconnect.
- [ ] Qualify `rust-analyzer`, `gopls`, `zls`, and `clangd` child lifecycle on
  the declared Linux matrix.
- [ ] Pin `dist` through mise, configure its targets and archive formats, and
  generate and review its GitHub Actions release workflow.
- [ ] Run the `dist` plan through mise and build immutable release artifacts
  with recorded linker, panic, symbols, stripping, LTO, features, dependency
  graph, checksums, and provenance.
- [ ] Run native install, first start, diff, search, source, LSP trust,
  navigation, cancellation, crash recovery, upgrade, rollback, and uninstall
  QA against the exact artifacts.
- [ ] Publish the AUR package metadata only after the referenced GitHub Release
  archive passes. Do not create an AUR packaging directory inside Xunhen.
- [ ] Measure the exact artifact against the frozen interaction, memory, CPU,
  startup, and size budgets.

#### Done when

- [ ] Linux satisfies the declared complete v1 behavior.
- [ ] No release-relevant intermittent Linux failure remains unexplained.
- [ ] Package metadata resolves to the exact qualified archive and checksum.
- [ ] Known filesystem, terminal, Git, LSP, and distribution limits are written
  plainly.
- [ ] The `0.6.0` benchmark report contains correctness, raw samples,
  independent runs, percentiles where valid, memory, CPU, size, errors, and
  environment.

### Phase 7: macOS and Windows native beta

#### Build now

- [ ] Qualify macOS Terminal, iTerm2, representative filesystems, Unicode path
  behavior, symlinks, app translocation considerations where relevant, config,
  cache, signals, child processes, signing, notarization, and archives.
- [ ] Qualify Windows Terminal, PowerShell and Command Prompt invocation,
  NTFS paths, reserved names, reparse points, drive boundaries, ConPTY,
  console modes, config, cache, process groups, and archives.
- [ ] Run the complete diff, search, source, and language-server matrix natively
  rather than treating cross-compilation as support.
- [ ] Verify lossless platform path handling and safe display for characters or
  byte sequences each system permits.
- [ ] Verify terminal restoration after Ctrl-C, child crash, Xunhen panic probe,
  window close, SSH loss where applicable, and ordinary exit.
- [ ] Build, sign, and notarize through the native GitHub Actions release path
  where the accepted platform path supports it.
- [ ] Protect Windows Authenticode and Apple Developer ID credentials in their
  native stores, restrict their use to release builds, submit macOS artifacts
  for notarization, staple the result where applicable, and exercise
  expiry, revocation, rotation, and recovery without exposing keys to CI jobs.
- [ ] Publish the Homebrew formula and Scoop manifest only after their exact
  `dist` artifacts pass. Continue the existing repository layouts and conventions.
- [ ] Measure exact macOS and Windows artifacts against the frozen budgets and
  compare only within equivalent platform baselines.

#### Done when

- [ ] Linux, macOS, and Windows all satisfy the complete declared behavior.
- [ ] Each system has native exact-artifact QA, not a build-only green mark.
- [ ] Direct GitHub Release archives and package-manager metadata resolve to
  recorded immutable artifacts and checksums.
- [ ] Platform-specific path, process, terminal, Git, and LSP limits are
  explicit.
- [ ] No release-relevant intermittent platform result remains unexplained.

### Phase 8: security, resilience, and performance qualification

#### Build now

- [ ] Freeze the candidate source, toolchain, dependencies, limits, benchmark
  inputs, supported platforms, Git matrix, terminal matrix, and language-server
  versions for this phase.
- [ ] Complete a threat-model-driven review of repository discovery, Git
  environment and commands, object and patch parsing, paths, no-follow reads,
  terminal output, config, cache, catalogue and search libraries, syntax
  highlighting, LSP trust and protocol, child processes, clipboard, CI, and
  release handling.
- [ ] Run bounded fuzz campaigns for implemented Git parsers, patch parser,
  config, search queries where owned, JSON-RPC framing, URI and position
  conversion, Markdown projection, terminal sanitizer, and relevant state
  machines.
- [ ] Convert valuable failures into small deterministic regression tests and
  minimize the retained corpus.
- [ ] Run dependency advisory, license, source, unsafe, secret, workflow,
  permission, SBOM, provenance, and artifact reviews.
- [ ] Exercise malicious repository config, external diff, textconv, fsmonitor,
  partial-clone missing objects, split-index replacement races, environment
  injection, path controls, symlink replacement, outside-root LSP location,
  language-server executable replacement, unsolicited edit, child flood, hang,
  crash, terminal escapes, deceptive Unicode, corrupt cache, full disk,
  permission loss, and rapid input in isolated local environments.
- [ ] Run unit, property, integration, exact-binary, PTY, real-Git, real-LSP,
  native platform, install, upgrade, rollback, and uninstall suites.
- [ ] Execute the full benchmark suite against exact Release artifacts on each
  platform. Measure cold and warm startup, first useful frame, file search,
  content search, query replacement, diff and source scroll, syntax work, LSP
  request latency excluding and including server time, CPU, I/O, steady and
  peak RSS, allocation signals where useful, artifact size, and shutdown.
- [ ] Compare baseline and candidate with the same workload, machine, toolchain,
  build flags, cache state, and randomized or interleaved run order. Report
  effect size, uncertainty or run-to-run range, independent runs, sample count,
  errors, timeouts, and the predeclared regression threshold.
- [ ] Profile only measured regressions or bottlenecks, then confirm any fix in
  a separate unprofiled run.

#### Done when

- [ ] No unresolved critical or high security issue remains in the reviewed
  scope.
- [ ] No release-blocking defect or unexplained intermittent result remains.
- [ ] Repository and index mutation checks and Git network-isolation checks
  remain clean for exact candidates.
- [ ] Terminal restoration and child cleanup pass on every supported system.
- [ ] Every exact candidate satisfies the frozen practical performance and
  resource budgets, or the release is held with a redesign rather than an
  edited claim.
- [ ] Reports state scope, evidence, exclusions, uncertainty, and residual risk
  without claiming the whole product is secure or universally fast.

### Phase 9: compatibility freeze, documentation, and release beta

#### Build now

- [ ] Freeze v1 CLI arguments, exit codes, config schema, action names, cache
  version, comparison behavior, snapshot labels, unavailable reasons, and
  platform support matrix.
- [ ] Freeze the supported Git minimum and tested versions, object formats,
  search behavior, syntax coverage, LSP protocol subset, position encodings,
  and qualified server versions.
- [ ] Define compatible config upgrades, cache invalidation, safe downgrade,
  unknown-field handling, and future extension rules.
- [ ] Complete README, installation, key reference, Git behavior including
  partial-clone missing objects and index generations, search scope, LSP trust
  and executable identity, semantic identity, configuration, security, privacy,
  performance, troubleshooting, platform support, and release documentation.
- [ ] Verify `xunhen.org` control, renewal, and recovery before public launch,
  keeping registrar secrets and personal contact data outside the repository.
- [ ] Complete commissioned Dian artwork, wordmark, icons, monochrome forms,
  terminal fallback, rights, licenses, editable sources, exports, and
  conservation note.
- [ ] Run documentation commands and journeys from clean machines and compare
  them with the exact beta artifacts.
- [ ] Remove dead features, stale decisions, rejected dependencies, duplicate
  docs, unused config, redundant tests, and benchmark cases that no longer
  answer a decision.
- [ ] Publish `0.9.0` beta artifacts without changing them after qualification.

#### Done when

- [ ] Every public v1 contract has a compatibility decision and exercised
  behavior.
- [ ] Documentation distinguishes Xunhen behavior, Git behavior, search scope,
  and external language-server risk.
- [ ] Every documented command and key action matches the exact beta.
- [ ] No placeholder or unlicensed public asset remains.
- [ ] Upgrade, downgrade, cache recovery, and uninstall were executed.

### Phase 10: release candidates and v1 publication

#### Build now

- [ ] Create immutable release candidates from reviewed source revisions,
  `Cargo.lock`, `rust-toolchain.toml`, the pinned `dist` version, and the release
  workflow.
- [ ] Run the qualified `dist` plan and native GitHub Actions artifact workflows
  through mise tasks.
- [ ] Record artifact contents, checksums, signatures where supported, SBOM,
  provenance, toolchain, and source commit.
- [ ] Smoke-test the exact GitHub Release archive and package-manager
  installation paths on clean supported systems.
- [ ] Run final current changes, staged changes, root commit, merge parent,
  comparison, full-repository file search, content search, source browsing,
  LSP trust, hover, definition, references, cancellation, no-LSP, narrow
  terminal, no-color, update, rollback, and uninstall journeys.
- [ ] Recheck the public repository, project site, downloads, checksums,
  documentation, package manifests, and security contact.
- [ ] Record the security scope, benchmark verdict, QA verdict, known issues,
  residual risks, and separate ship decision for each candidate.
- [ ] Publish `1.0.0` without rebuilding, replacing, or silently editing the
  qualified artifacts.

#### Done when

- [ ] Linux, macOS, and Windows meet the declared support contract.
- [ ] Every package and download resolves to the recorded immutable artifact
  and checksum.
- [ ] The public performance statement matches the exact release report.
- [ ] The project site and docs state the LSP trust boundary and snapshot limit
  beside the semantic feature.
- [ ] Upgrade, rollback, support, and security-response ownership are active.

## Test strategy

Testing follows behavior and risk, not module count.

### Layers

- **Unit tests.** Limits, revision validation, object IDs, snapshot identity,
  indeterminate filter status, comparison-to-source mappings, composite change
  states, same-path change collisions, derived path-index
  consistency, hunk coordinates, terminal-safe projection, LSP position
  conversion, request generations, cache eviction, and error mapping.
- **Property tests.** Parser bounds, line-coordinate monotonicity, path
  round-trips, display safety, snapshot identity, stale-result rejection,
  position encoding, navigation history, and bounded collection invariants.
- **Integration tests.** Real disposable Git repositories including split-index,
  partial-clone, Git LFS, status-triggered external filters, literal pathspecs,
  CRLF, `ident`, and encoding conversions, real filesystem
  boundaries, no-follow access, file catalogue, watchers, config, cache, child
  processes, fake LSP, PTY, and exact binary.
- **Differential tests.** Xunhen file and coordinate results against the
  qualified Git executable on generated repositories.
- **Real-server tests.** Pinned `rust-analyzer`, `gopls`, `zls`, and `clangd`
  fixtures after the generic fake-server contract passes, including cross-file
  target synchronization and fresh requests after target discovery.
- **End-to-end tests.** A small set of shipped binary journeys across diff,
  search, source, semantic navigation, cancellation, failure, and exit.
- **Fuzz tests.** Implemented Git output, patch, config, query, JSON-RPC, URI,
  Markdown, control-text, and coordinate boundaries.
- **Visual tests.** A small reviewed set for layout and style. Semantic presenter
  assertions carry most behavior so snapshots do not become paint noise.
- **Exploratory QA.** Unfamiliar repositories, large changes, rapid input,
  terminal differences, permissions, lifecycle, accessibility, upgrade,
  rollback, and recovery.

### Rules

- Test through public behavior or the narrowest owned boundary that exposes the
  risk.
- One test protects one named behavior and states the failure it would catch.
- Make high-value tests fail once for the expected reason before accepting them.
- Inject time, generations, process output, cancellation, and watcher events.
- Wait for readiness or state change, never a guessed sleep.
- Give every test its own directory, repository, config, cache, process, PTY,
  server, and cleanup.
- Assert negative space: no repository write by Xunhen or the fake server, no
  index refresh, no Git-triggered network attempt, no path escape, no raw
  control output, no stale result, no historical LSP answer, no edit or command
  request, no leaked child, no status-triggered filter, no literal-path expansion,
  no unproven source mapping, no retroactively validated LSP target, and no
  incomplete result marked complete.
- Use real Git and filesystem boundaries where a mock would hide semantics.
- Use a deterministic fake language server before real-server integration.
- Keep end-to-end journeys few, exact, and worth their maintenance cost.
- Preserve the first intermittent failure. Never retry a suite into green.
- Delete redundant or implementation-coupled tests as readily as adding useful
  ones.
- Bound every test and fuzz campaign by time, input, files, bytes, memory,
  processes, threads, and operations.

Fixture repositories are created deterministically with fixed identities,
timestamps, line endings, object format, and commit graph. They contain only
synthetic data. A fixture builder may call the qualified Git executable during
setup, but expected assertions are reviewed values rather than copies of the
same parser logic.

## Security review

Update the threat model whenever a phase changes a trust boundary. For each
meaningful risk record:

- asset and attacker;
- reachable path and preconditions;
- impact;
- mitigation;
- verification;
- remaining risk and owner.

Before the relevant feature ships, review:

1. Git executable discovery, version, environment, config, paging, locks, lazy
   fetching, external programs, fsmonitor, output, object resolution, and exit
   behavior, including status-time filters and literal pathspec handling;
2. repository ownership, paths, symlinks, special files, worktrees, submodules,
   sparse and split-index state, partial and promisor repositories, coherent
   index generations, replacement races, and outside-root reads;
3. file count, patch size, line length, parsing, search, syntax work, queues,
   caches, cancellation, content-conversion mappings, and resource exhaustion;
4. terminal controls, deceptive Unicode, width arithmetic, hyperlinks,
   clipboard, panic, signals, and restoration;
5. catalogue path identity, ignore matching, no-follow traversal and content
   reads, matcher dependencies, worker cancellation, watcher overflow,
   aggregate memory reservations, and shutdown;
6. LSP executable resolution and replacement, trust, environment, repository
   code execution, framing, JSON, Markdown, URIs, positions, stale results,
   outside-root locations, edits, commands, child floods, hangs, crash, and
   termination, including source and target synchronization, fresh-request
   ordering, and bounded target discovery;
7. config, cache, logs, permissions, corruption, privacy, and cleanup;
8. dependencies, CI, release credentials, actions, SBOM, provenance, package
   channels, update, downgrade, rollback, and artifact replacement.

Scanner output is a lead. It is not a finding until reachability and impact are
established. Reports separate technical severity, remediation priority, and
evidence confidence. They name the exact source and artifact, scope,
assumptions, exclusions, and residual uncertainty.

## QA and platform evidence

Each release-relevant run records:

- commit, dirty state, toolchain, dependency lock, build command, release
  profile, artifact digest, and package source;
- OS, architecture, kernel or OS version, filesystem, terminal, shell entry
  path, tmux or SSH state, locale, width, color mode, Git version and object
  format;
- search-library versions, explicit features, catalogue policy, and limits;
- language-server name, version, resolved executable path and identity,
  arguments, working directory, allowed environment names, and synthetic
  fixture;
- exact user steps, expected result, actual result, side-channel observations,
  logs, cleanup, and deviations.

Critical v1 journeys are:

1. start in a repository with no changes and exit cleanly;
2. review all, unstaged, and staged current changes;
3. review a root commit, selected merge parent, two revisions, and a locally
   complete partial clone; refuse a missing promisor object without network or
   repository writes;
4. navigate renames, deleted files, binaries, mode changes, submodules, Git LFS
   pointers and expanded worktree files, indeterminate external-filter status,
   literal unusual paths, mapped or unavailable transformed coordinates, long
   lines, and missing final newline;
5. fuzzy-find an unchanged file, content-search the worktree, open source, and
   return to the original diff row;
6. cancel indexing, content search, patch loading, and an LSP request while
   continuing to navigate;
7. trust and launch each qualified language server, inspect hover, follow a
   definition into a previously unopened file, visit references, reject stale
   target coordinates, then move back and forward;
8. attempt semantic navigation on old, historical, deleted, and stale bytes and
   receive the correct refusal;
9. run usefully with no language server, no color, no mouse, a narrow terminal,
   tmux, and a remote shell where supported; test bounded platform copy and
   opt-in OSC 52 without reading the clipboard;
10. survive Git failure, concurrent index replacement, language-server
    executable replacement and crash, output flood, permission loss, corrupt
    config, corrupt cache, terminal resize, Ctrl-C, and terminal close;
11. verify no repository or index mutation after each Xunhen-only and fake-server
    mutation-sensitive journey; record and attribute any change separately in
    real-server journeys;
12. install, launch, upgrade, safely downgrade or refuse, rollback, uninstall,
    and reinstall the exact package.

Use synthetic repositories in isolated local environments. The QA verdict is
`PASS`, `PASS WITH KNOWN ISSUES`, `FAIL`, `BLOCKED`, or `INCONCLUSIVE`. State
`ship`, `hold`, or `no recommendation` separately.

A passing Linux run does not qualify macOS or Windows. A cross-compiled binary
does not qualify the target. A passing internal test does not qualify a package.

## Benchmark policy

Performance measurements start with repository browsing in Phase 1 and
continue at boundaries that can change interaction latency. Public claims
wait until exact release artifacts exist.

The versioned workloads cover:

- empty and tiny repositories for startup overhead;
- a typical mixed-language repository;
- a wide repository with many files in shallow directories;
- a deep repository with long paths;
- a repository with many ignored files;
- a large current diff with short and long lines;
- rename-heavy and binary-heavy changes;
- rare and dense content-search matches;
- rapid superseding queries;
- cold and warm caches;
- one qualified real language server measured separately from Xunhen-owned
  request and render time.

Measure:

- process start to terminal ready;
- process start to first useful diff frame;
- input event to file-search result frame;
- input event to content-search result frame;
- file or hunk movement to rendered frame;
- source open and return-to-diff latency;
- Xunhen-owned LSP request, validation, and render overhead;
- language-server end-to-end response latency as a separate dependency metric;
- catalogue scan throughput and total time;
- CPU, system time, bytes read, steady RSS, peak RSS, retained catalogue and cache
  bytes, allocation signals where useful, and clean-shutdown time;
- uncompressed and compressed artifact size plus dependency weight.

Interactive latency reports p50, p95, p99, maximum, observation count, duration,
and independent process runs when the sample size supports those percentiles.
Microbenchmarks use harness-native estimates and uncertainty instead of invented
tail percentiles. Baseline comparisons report effect size and uncertainty or
run-to-run range.

Every benchmark first verifies expected file paths, match locations, source
coordinates, result completeness, content digests, and zero unexpected writes.
A faster incomplete search or incorrectly mapped diff fails before timing is
interpreted.

Run release-equivalent builds with recorded optimization, features, target,
linker, LTO, panic, stripping, symbols, and allocator. Keep setup outside the
measured region unless it is visible user startup. Separate cold and warm
caches. Preserve raw machine-readable samples and exact commands in approved
benchmark artifacts.

Normal shared CI may run small regression signals but cannot make release
performance decisions when its noise exceeds the practical threshold. Use a
controlled machine for qualification and record power, thermal, CPU, memory,
OS, background load, and virtualization conditions that matter.

## Release and packaging

Xunhen will be hosted on GitHub under Nuggocto's account. GitHub Actions uses
the same pinned `mise` tasks as local development for CI and releases.

Pinned `dist` configuration produces native archives, installers, and checksums.
The exact artifacts pass native tests, QA, security checks, and benchmarks before
they are attached to a GitHub Release or referenced by Homebrew, Scoop, or AUR
metadata. A failed release creates a new candidate; published assets are never
replaced.

Xunhen has no self-updater in v1. Direct users check releases intentionally.
Package-manager users update through the channel that installed it.

## Documentation policy

Do not create an empty mature `docs/` tree during Phase 0.

Start with:

- `README.md` for what exists now, exact tool setup, one build command, one
  test command, runnable behavior, and support status;
- `PROJECT.md` for the plan and progress checkboxes; phase numbers appear only
  in this file;
- `SECURITY.md` when the repository becomes public;
- `LICENSE` with Apache-2.0 text and third-party notice policy.

Add documents when their subject exists:

- supported Git commands, literal paths, coherent index, no-follow access,
  external-program suppression, and first key reference in Phase 1;
- expanded Git states, conversion mappings, side-by-side layout, and source
  navigation in Phase 2;
- search scope, ignores, limits, and performance in Phase 3;
- LSP trust, server configuration, supported operations, byte identity, and
  the pinned 3.18 subset, notification limitation, cross-file target proof,
  discovery limits, and qualified servers in Phase 4;
- accessibility and complete workflow in Phase 5;
- platform installation and support as native betas ship;
- benchmark methodology and results with measured versions;
- final configuration, compatibility, upgrade, rollback, and release notes in
  Phase 9.

Use a short decision record only for an expensive-to-reverse choice such as Git
process contract, safe worktree access, search ownership, syntax engine, LSP trust,
or cache format. Source, tests, manifests, locked dependencies, CI, and exact
release evidence remain authoritative for implementation details.

Documentation never says `all languages`, `instant`, `zero overhead`, `safe`,
or `no network` without the qualification that makes the statement true.

## Repository growth

The mature repository may contain:

```text
xunhen/
├── Cargo.toml
├── Cargo.lock
├── mise.toml
├── rust-toolchain.toml
├── README.md
├── SECURITY.md
├── LICENSE
├── src/
│   ├── lib.rs
│   ├── main.rs
│   ├── app.rs
│   ├── config.rs
│   ├── limits.rs
│   ├── repository/
│   ├── diff/
│   ├── document/
│   ├── search/
│   ├── language/
│   ├── terminal/
│   └── ui/
├── tests/
│   ├── integration/
│   └── end_to_end/
├── fixtures/
│   ├── git/
│   ├── lsp/
│   ├── config/
│   └── terminal/
├── fuzz/
├── benches/
├── benchmarks/
│   ├── generators/
│   └── reports/
├── assets/
├── docs/
└── .github/
    └── workflows/
        ├── ci.yml
        └── release.yml     # generated by pinned cargo-dist
```

This is a destination, not Phase 0 scaffolding. Directories appear only when
their contents protect or ship real behavior. Fuzz and benchmark harnesses may
be workspace members later if Cargo requires it; that does not justify splitting
the application into decorative crates. Homebrew, Scoop, and AUR files remain
in their existing shared repositories, not in this tree.

## Brand and art

Dian, the `寻痕` wordmark, project mark, icons, and public illustrations should
be made by a professional human artist before v1. Keep the detailed commission
brief separate from the engineering roadmap.

The brief should cover:

- respectful Chinese mountain cat anatomy and distinguishing coat, ears, legs,
  and ringed tail;
- front, side, seated, walking, listening, and paw-on-trace poses;
- one-sheet and two-sheet diff compositions;
- ink trail, pawprint, knot, broken-trail, file, hunk, search, and symbol motifs;
- logo, wordmark, icon, monochrome, small-size, terminal, high-contrast, and
  no-motion variants;
- light and dark palettes that do not encode added and deleted states by color
  alone;
- source files, layers, export formats, authorship, license, rights,
  attribution, trademark use, third-party materials, and permission before
  external AI processing;
- one concise conservation note based on a reputable source.

Disposable geometric placeholders are allowed during development. They do not
become the public identity through inertia.

## Planning references

These sources inform the roadmap. Exact versions and registrar controls still
require the checks in their owning phases.

- [Cloudflare Registrar documentation](https://developers.cloudflare.com/registrar/)
- [Rust release history](https://doc.rust-lang.org/stable/releases.html)
- [cargo-dist documentation](https://axodotdev.github.io/cargo-dist/)
- [Git partial clone documentation](https://git-scm.com/docs/partial-clone)
- [Ratatui](https://github.com/ratatui/ratatui)
- [Frizbee](https://github.com/saghen/frizbee)
- [Ignore matchers](https://docs.rs/ignore/latest/ignore/gitignore/struct.GitignoreBuilder.html)
- [Grep searcher](https://docs.rs/grep-searcher/latest/grep_searcher/struct.Searcher.html)
- [Grep regex](https://docs.rs/grep-regex/latest/grep_regex/)
- [Notify](https://docs.rs/notify/latest/notify/)
- [Language Server Protocol 3.18 specification](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/)
- [GitHub releases](https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases)
- [GitHub Actions security](https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions)
- [GitHub-hosted runners](https://docs.github.com/en/actions/using-github-hosted-runners/about-github-hosted-runners)
- [IUCN Cat Specialist Group: Chinese mountain cat](https://www.catsg.org/living-species-chinesemountaincat)

## Honest notes

Xunhen can replace the diff-browsing and code-following part of an editor-based
Git workflow. It does not replace Fugitive's write operations, LazyGit's Git
workflow, or an editor's complete language integration.

The sharpest product distinction is also its hardest invariant: Xunhen sends a
semantic request only for the worktree bytes it has synchronized and rejects a
result after that identity changes. This proves client-side ordering and
identity, not correct behavior inside the trusted language server. Historical
code navigation sounds simple until the server is asked about a tree it has
never loaded. V1 refuses that illusion. Temporary worktrees and one server per
snapshot may be explored after v1, with strict process, disk, privacy, and
resource bounds.

Repeated path queries reuse a bounded catalogue. Content queries read validated
files on demand and may reuse the existing document cache. This keeps file
identity and resource admission under Xunhen's control. It requires application
work for traversal, cancellation, and watcher recovery; choosing small matching
libraries does not prove those behaviors. Phase 3 qualifies the integration on
real reference workloads before making performance claims.

Git subprocesses are not a performance failure by definition. Git is the
semantic authority, and bounded long-lived search state covers the repeated hot
path. Replacing Git with a library is justified only by measured latency that
cannot be removed while preserving behavior. Partial-clone objects that are not
already local remain unavailable in v1; Xunhen does not trade its offline and
read-only promises for transparent lazy fetching.

The terminal can display a great deal, but it is still a grid with varied width,
color, mouse, signal, and clipboard behavior. Accessibility and restoration are
product requirements, not polish after the renderer is finished.

`xunhen.org` is registered through Cloudflare Registrar, and the project owner
confirms control. Registrar credentials, recovery material, billing details,
and personal contact data stay outside the repository.

V1 may arrive after more `0.x` releases than this board lists. Version numbers
do not certify quality. The evidence under "Done when" does.
