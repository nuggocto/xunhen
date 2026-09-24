# Finding a history from its source

`--source PATH --undo-dir DIR` finds the undo file Neovim wrote for a source
file, checks that it really belongs to that file, and hands it to `inspect`,
`show`, or `diff`. This page describes the names xunhen looks for, where it
looks, what counts as a match, and what happens when there is no single
match. The explicit form, `--undo PATH` with `--base PATH`, stays available for
copied, renamed, and orphaned histories.

```sh
xunhen inspect --source ./retry.go --undo-dir ~/.local/state/nvim/undo
xunhen show --source ./retry.go --undo-dir ~/.local/state/nvim/undo --node 42
xunhen diff --source ./retry.go \
  --undo-dir ~/.local/state/nvim/undo \
  --undo-dir /backup/undo \
  --from 17 --to 42
```

In this mode the source file does two jobs: its path names the history, and
its text is the base that reconstruction starts from.

## How Neovim names an undo file

These rules come from the pinned Neovim revision
[`5885a30`][revision], and
[`testdata/discovery/names.json`](../testdata/discovery/names.json) records
Neovim's own answers for every case below.

1. **Make the name absolute.** [`FullName_save`][fullname] calls
   [`path_to_absolute`][to-absolute], which resolves the directory part with
   `realpath(3)` through [`path_full_dir_name`][full-dir] and keeps the last
   component as written. Symlinked directories and `..` components therefore
   resolve to the physical directory. A relative name joins the physical
   working directory that `getcwd(2)` reports, not a logical path through a
   symlink. When an absolute directory does not exist, the name is used as
   written.
2. **Follow the last component.** [`resolve_symlink`][resolve] follows a
   symlinked file through up to 99 links. A relative link resolves against the
   link's own directory, and the result is made absolute again. A dangling link
   is named after its missing target. If anything fails, the name from step 1
   stays.
3. **Encode the path.** [`u_get_undo_file_name`][name] replaces every `/` with
   `%`. `/home/me/src/retry.go` becomes `%home%me%src%retry.go` inside an undo
   directory. Spaces, UTF-8, bytes that are not UTF-8, leading dots, and
   literal `%` all pass through unchanged.
4. **Or use a sidecar.** An `'undodir'` entry of `.` puts the history beside
   the resolved source instead, as `.retry.go.un~`.

The encoding cannot be reversed. `/a%b/c.go` and `/a/b%c.go` both encode as
`%a%b%c.go`, so a matching name only makes a file a candidate. It never proves
which source the history belongs to.

| Case in the corpus | What Neovim does |
| --- | --- |
| Absolute and relative paths, `..` | Names the physical absolute path |
| Spaces, Unicode, a byte that is not UTF-8, a dotfile | Keeps every byte |
| `%` in a directory or file name | Produces the collision above |
| File symlink, relative link chain, dangling link | Names the final target |
| Symlinked directory, symlinked working directory | Names the physical directory |
| Trailing slashes on the undo directory | No change to the name |
| `'undodir'` of `.` | `.name.un~` beside the target, not the link |
| Missing file, missing directory | Still names a file; an absent absolute directory stays as written |

## Where xunhen looks

- **Only the directories you pass.** Each `--undo-dir` is one literal path:
  commas are filename characters, not separators as in `'undodir'`. Repeat the
  flag for more directories, up to 32. A relative path is relative to where
  the command runs, so `--undo-dir .` means the current directory.
- **Only the derived names.** In each directory xunhen looks up the encoded
  name and nothing else. It never lists a directory and never descends into
  subdirectories, so a directory with thousands of unrelated histories costs
  the same as an empty one.
- **The sidecar only beside the source.** xunhen looks for `.name.un~` only in
  a supplied directory that is the resolved source's own directory.
- **Names that cannot exist.** A directory entry holds at most 255 bytes, and
  the encoded name spells out the whole source path. For a longer path,
  Neovim cannot write an undo-directory history at all, so xunhen treats that
  name as absent and only the sidecar can match.
- **Every directory before any choice.** Neovim reads the first existing file
  in `'undodir'` order. xunhen examines every supplied directory first and
  refuses to choose between two matches, so neither argument order nor
  directory order nor modification time decides the result.
- **Aliases once, copies apart.** A directory given twice, or through a
  symlink, is searched once. A hard link to a file already seen counts once.
  Two copies of a history are two candidates, even with identical bytes.

## What counts as a match

A candidate becomes the source's history only when all three facts hold:

1. A regular file sits at a derived name.
2. It decodes, and its records form a valid history.
3. Its reference hash and line count match the source text.

The third fact is what reconstruction depends on. Each candidate is reported
with one outcome:

| Outcome | Meaning | Effect on the search |
| --- | --- | --- |
| verified | All three facts hold | A match |
| not verified | A valid history, but the source is missing, unsupported, or different | Inspection may use it |
| rejected | Malformed, an unsupported format, or not a regular file | Not a match |
| not examined | A symlink, a permission failure, a limit, or a read error | The search is incomplete |
| duplicate | The same file as an earlier candidate | Counted once |

Then the search result is decided:

| Situation | Behavior |
| --- | --- |
| One verified history, complete search | Used by `inspect`, `show`, and `diff` |
| Several verified histories | Ambiguity: every candidate is listed and nothing is chosen |
| Any directory or candidate not examined | Incomplete search: neither a match nor absence is claimed |
| Only histories for other text | Base mismatch; empty text is never substituted |
| Only rejected files | Reported as invalid, not as absent |
| Nothing at any derived name | Not found: the diagnostic lists the searched scope |
| Source unreadable or unsupported | `show` and `diff` stop; `inspect` continues |

`inspect` reads metadata only, so it accepts one exception. When no candidate
verifies but exactly one valid history exists, it shows that history and
marks the association unverified, with the reason. This covers a deleted
source, whose old path still names its history, and a source edited since the
history was written. Two or more such histories are still ambiguous.

A failed search's diagnostic names the resolved source path, the encoded
name, each directory and candidate with its outcome, and the explicit commands
to continue with. Paths and messages are escaped for the terminal. Nothing is
written to stdout when a search fails.

## Files, symlinks, and handles

| Path | Policy |
| --- | --- |
| `--undo`, `--base`, `--source` | Followed through symlinks; must end at a regular file |
| `--undo-dir` | Followed through symlinks, then held open while it is searched |
| A symlink at a derived name | Refused, leaving the search incomplete; pass it with `--undo` to use it |

Candidates are looked up and opened relative to the held directory handle,
with `O_NOFOLLOW`, so a symlink planted between the lookup and the open is
still refused. The lookup uses an `O_PATH` descriptor, which cannot read, so
a FIFO or device at a derived name is never opened for reading. Every open is
read-only and nonblocking, and every handle is closed before the command
writes anything. xunhen never opens a path it derived from an undo file's
contents.

## Changes during a load

Several reads of an ordinary filesystem are not a transaction, so this is
best-effort detection. On any observable change the load fails with a
message to retry, or to copy the inputs while the editor is idle.

| Change | How it is caught |
| --- | --- |
| A file grows, shrinks, or is rewritten while read | Device, inode, size, and modification and change times compared before and after the read |
| A path is atomically replaced | The path's identity compared with the open file's after the read |
| A symlink is retargeted | Same as replacement: the path must still name the loaded file |
| The source changes during the search | The source rechecked, and its name derived again, after the search |
| A candidate changes between its lookup and its read | Refused before any byte is read: the open file must match the lookup's size and times |
| A candidate appears, disappears, or changes | Every derived name looked up again after the search |
| A directory path is retargeted | Every supplied path, including one skipped as a repeat, must still name the directory searched |

A reload is simply another load. It builds a new decoded file, history, and
base binding, so a node reference from the earlier load fails against the new
history, even when its number still exists.

## Limits

| Resource | Ceiling |
| --- | --- |
| Undo directories per search | 32 |
| Undo bytes read per search, rejected candidates included | 512 MiB |
| Candidate names per directory | 2, so at most 64 per search |
| Candidates decoded at once | 1 |
| Decoded histories held | 2: the first verified one and the candidate being read |

The byte limit bounds what is actually read, not only what is counted. Each
candidate is charged its size when it is looked up, a file that changed since
then is refused, and no read continues past the size a file had when it was
opened, even if it grows. The per-file limits from
[the format notes](undo-format.md) apply to each candidate as well. The base
text is checked and hashed once, so verifying it against many candidates costs
almost nothing extra.

Measured on the development machine, with `go test ./internal/discover -bench
BenchmarkSearch` and the built binary:

| Search | Time | Peak memory |
| --- | --- | --- |
| One directory holding 10,000 unrelated files | 14 µs | |
| 32 directories, the match in the last | 85 µs | |
| 31 rejected 64 KiB candidates before the match | 370 µs | |
| `show --source` of a 4,000,000-line, 57 MiB source | 0.5 s | 732 MiB |
| The same, beside a 201 MiB candidate for other text | 0.6 s | 769 MiB |
| `diff --source` of that pair of candidates | 1.5 s | 782 MiB |

## Using a history directly

When the search cannot decide, name the files yourself:

```sh
xunhen inspect --undo /path/to/history.undo
xunhen show --undo /path/to/history.undo --base /path/to/matching-copy --node 42
```

The base must be the text the history was written against. For a history
whose source has since changed, that is usually an older copy from backup or
version control.

## Regenerating the naming corpus

With the pinned Neovim installed, from the repository root:

```sh
go run ./tools/fixtures -nvim /usr/bin/nvim -out /tmp/xunhen-names-new -names
```

The generator builds each case in a private directory, asks `undofile()`, and
writes the source with `'undofile'` set. It fails unless Neovim's answer, the
file it wrote, and the authored expectation in `tools/fixtures/names.go` all
agree. Ordinary tests replay the recorded layouts without Neovim.

[revision]: https://github.com/neovim/neovim/tree/5885a30e1e1225349079e7a1c4a3848aa8e43e42
[name]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo.c#L660-L755
[resolve]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/memline.c#L3229-L3292
[fullname]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/path.c#L515-L537
[to-absolute]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/path.c#L2360-L2417
[full-dir]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/path.c#L2294-L2328
