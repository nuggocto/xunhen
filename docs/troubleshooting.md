# Troubleshooting

Each section starts with what you see, then explains it and says what to do.
Every diagnostic starts with `xunhen: `, and paths in it are escaped, so a
strange file name shows as `\x1b` or `‮` rather than acting on your
terminal.

## There is no undo file, or it holds less than you hoped

xunhen reads only what Neovim wrote to disk. Neovim writes an undo file when
`'undofile'` is on and the buffer is written, or when you run `:wundo`. So:

- With `'undofile'` off, which is the default, no history survives the
  editor closing. Check with `:set undofile?`, and add `set undofile` to
  your configuration for the future. xunhen cannot recover history that was
  never written.
- Edits after the last write exist only in the editor that made them. Write
  the buffer, or run `:wundo FILE`, to save them.
- `'undolevels'` limits how many changes Neovim keeps. Older ones are
  dropped, so the oldest retained state, node 0, may not be the file you
  started with. `inspect` calls it the retained root.
- Undo history groups changes into blocks. Each node is the buffer after
  one block, not after every keystroke.

## `no undo history for this source path in the supplied directories`

```text
xunhen: no undo history for this source path in the supplied directories
xunhen:   source resolves to /home/me/src/retry.go
xunhen:   undo file name %home%me%src%retry.go
xunhen:   searched /home/me/.local/state/nvim/undo
```

No file exists under the name Neovim would use. Check, in order:

1. The directory. Run `:echo &undodir` in Neovim and pass each directory it
   lists with its own `--undo-dir`. A `.` entry means the source's own
   directory, where the name is `.retry.go.un~`.
2. The path. Neovim names the history after the path the file had when it
   was written, through symbolic links. A moved or renamed source keeps its
   history under the old name: pass the old path to `--source`, or find the
   file with `ls ~/.local/state/nvim/undo | grep retry.go` and use `--undo`.
3. Whether it was written at all; see the section above.

## `more than one undo history matches; choose one with --undo`

Two directories, or the undo directory and a `.retry.go.un~` beside the
source, both hold a valid history that matches the source. xunhen does not
guess. The diagnostic lists each candidate: pass the one you want with
`--undo PATH --base retry.go`.

## `the search could not examine every undo directory and candidate`

```text
xunhen:   could not search: open undo directory ../locked: permission denied
```

A directory or candidate could not be read, so xunhen cannot say that the
match it found is the only one. Fix the permission, drop that directory from
the command, or use `--undo` directly. A candidate that is a symbolic link
is also left unexamined: discovery does not follow links, but `--undo` does.

## `base mismatch`

```text
xunhen: retry.go: base mismatch: base text: line count 4 does not match undo reference
xunhen: retry.go: base mismatch: base text: content hash does not match undo reference
```

Reconstruction starts from the exact text the undo file was written
against, and the undo file stores its line count and hash. The file you gave
is different, usually because it was edited or checked out again after
Neovim last wrote the history. xunhen will not treat a different text, or no
text, as the starting point: the result would be wrong without any sign of
it.

- Inspection still works, since it needs no text: `xunhen inspect`.
- Find the matching text. It is the file as it was when the undo file was
  last written: often the last saved version, or a commit from around then.
  `git show COMMIT:path/retry.go > copy.go`, then
  `xunhen show --undo HISTORY --base copy.go --node ID`.
- A history written by `:wundo` of an unsaved buffer needs that unsaved text,
  which may exist nowhere else.

With `--source`, the same problem reads `an undo history exists for this
source path, but its reference text differs from the source`.

## `unsupported input` or `invalid input`

```text
xunhen: v2.undo: byte 9: unsupported input: version: format 2; supported format is 3
xunhen: notes.txt: byte 0: invalid input: magic: not a Neovim undo file
xunhen: base.txt: unsupported input: base text: CRLF base text
```

- A format other than 3, or a Vim or encrypted undo file, is not supported.
  [compatibility.md](compatibility.md) lists the one verified producer.
- `invalid input` or `truncated input` with a byte offset means the file is
  not a complete undo file: copied while Neovim was writing it, cut short, or
  something else entirely. Copy it again while the editor is idle.
- A base or source that is not UTF-8 with LF line endings is refused, with
  the reason: CRLF, a byte-order mark, invalid UTF-8 such as Latin-1, or
  NUL. Neovim does not record the file's encoding or line endings in the
  undo file, so xunhen cannot convert them back. `inspect` still works.
- `raw export unsupported` means the chosen state holds invalid UTF-8 or
  NUL. Plain `show`, without `--raw`, displays it with escapes.

## A limit is exceeded

```text
xunhen: big.undo: undo input has 314572800 bytes, more than its 268435456-byte limit
xunhen: history.undo: byte 1234: limit exceeded: history nodes: count exceeds limit 1000000
```

xunhen refuses input past fixed sizes rather than run out of memory; the
diagnostic names the limit. They are far above ordinary files, so hitting
one usually means the file is not what it seems. The limits are listed in
[usage.md](usage.md#limits) and cannot be raised from the command line.

## The browser will not start

```text
xunhen: browse needs an interactive terminal: stdin is not a terminal
```

`browse` needs a terminal on both stdin and stdout and a `TERM` other than
`dumb`, so it refuses under a pipe, a redirect, or a plain editor
terminal. The diagnostic lists the `inspect`, `show`, and `diff` commands
that print the same information. If the screen looks wrong in a terminal
that does work, try another `TERM` value; `NO_COLOR=1` turns colors off.
The browser keeps working in terminals as small as 20 by 4 cells.

If the browser was killed with SIGKILL, it had no chance to restore the
terminal; run `reset`.

## Output fails, or raw export refuses the terminal

```text
xunhen: raw output requires redirected stdout
xunhen: cannot write output
```

`--raw` writes exact bytes, which may include terminal controls from the
recovered text, so it refuses to write to a terminal. Redirect it:
`... --raw --final-newline=include > recovered.go`.

`cannot write output` means a write failed: a full disk, a closed pipe such
as `| head`, or a removed output file. The exit status is 1, and whatever
was written is incomplete; delete it and run the command again.

## An input changed while it was read

```text
xunhen: history.undo: undo input changed while it was read; retry, or copy the inputs while the editor is idle
```

The undo file or source changed between reads, often because Neovim wrote
it at that moment. Run the command again, or copy both files while the
editor is idle and use the copies.

## Reporting a problem

Before opening an issue, collect:

- `xunhen version` output, and how you installed xunhen.
- Your distribution and, for the browser, the terminal and `TERM`.
- The exact command and the complete diagnostic.

**Do not attach a real undo file or source file.** Undo files keep text
you deleted, including secrets you removed on purpose. Reproduce the problem
with a small synthetic file instead: run `mkdir -p /tmp/undo`, write a few
lines in a fresh file with `nvim -u NONE -c 'set undofile undodir=/tmp/undo'`,
make the edits that
trigger the problem, and share that file only after reading it. The issue
form asks for the same things.
