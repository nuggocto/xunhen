# Finding a history from its source

The source argument is short for `--source FILE`. Its path names the undo
file and its text supplies the reconstruction base. For copied or renamed
histories, use `--undo PATH --base PATH` instead. User examples are in the
[README](../README.md#usage).

## Neovim's naming rules

The pinned [Neovim revision][revision] derives an undo name as follows:

1. [`FullName_save`][fullname] makes the name absolute. The directory resolves
   through `realpath(3)`; the final component stays as written. Relative paths
   use the physical working directory. Missing absolute directories remain
   as written.
2. [`resolve_symlink`][resolve] follows the final component through up to 99
   links. Relative targets resolve from the link's directory; dangling links
   name their missing targets. On failure, the earlier name stays.
3. [`u_get_undo_file_name`][name] replaces every `/` with `%`. Other bytes
   stay unchanged. An `'undodir'` entry of `.` instead names `.file.un~`
   beside the resolved source.

This encoding can collide: `/a%b/c.go` and `/a/b%c.go` both become
`%a%b%c.go`. The name only identifies a candidate; the reference hash and
line count must match the source. The recorded cases in
[`testdata/discovery/names.json`](../testdata/discovery/names.json) cover
symlinks, missing paths, unusual bytes, collisions, and sidecars.

## Search and selection

Each `--undo-dir` is one literal directory. Otherwise `XUNHEN_UNDO_DIR`
provides a colon-separated list; empty entries are skipped. Without either,
the command returns a usage error. Relative directories use the working
directory, not the source's directory. No Neovim configuration is read.

Search looks up only derived names, never lists or descends into directories,
and checks every supplied directory before choosing. Sidecars are considered
only when the source's own directory is supplied. Names over the filesystem's
255-byte entry limit cannot exist; only a sidecar can then match. Directory
aliases and hard links count once; separate copies remain separate candidates.

One verified history and a complete search succeeds. Multiple matches are
ambiguous. An unreadable directory or candidate makes the search incomplete;
invalid candidates and mismatched text are reported, not treated as absent.
`inspect` alone can show a single valid, unverified history when the source
is missing, unsupported, or different. The diagnostic explains the association.

## File access and limits

Explicit input paths follow symlinks and must end at regular files. Discovery
holds each directory open and uses handle-relative lookups with `O_NOFOLLOW`.
A candidate symlink leaves the search incomplete; use `--undo` to select it.
`O_PATH` lookups avoid opening devices or FIFOs for reading. All opens are
read-only and nonblocking; no path comes from undo-file contents.

Identity, size, and timestamps are checked around reads. Paths, source naming,
and candidate names are rechecked after search to catch replacements and
retargeting. This detects observable changes, not a filesystem transaction.
A failure asks the user to retry or copy inputs while the editor is idle.

Search accepts at most 32 directories and 64 candidate names, reads at most
512 MiB of undo data including rejected candidates, decodes one candidate at
a time, and holds at most two histories. Per-file [format limits](undo-format.md#bounds)
still apply. Base text is checked and hashed once.

To regenerate the naming corpus, follow [testdata/README.md](../testdata/README.md#undo-filenames).

[revision]: https://github.com/neovim/neovim/tree/5885a30e1e1225349079e7a1c4a3848aa8e43e42
[name]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/undo.c#L660-L755
[resolve]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/memline.c#L3229-L3292
[fullname]: https://github.com/neovim/neovim/blob/5885a30e1e1225349079e7a1c4a3848aa8e43e42/src/nvim/path.c#L515-L537
