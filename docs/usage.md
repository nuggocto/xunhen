# Using xunhen

This guide walks through a recovery with a history from the repository's
test corpus, then lists every command, flag, exit status, and output
contract. [install.md](install.md) covers installation, and
[troubleshooting.md](troubleshooting.md) explains the errors.

## A recovery, step by step

The example is `testdata/undo/abandoned-branch` from the source repository
or the source archive. Neovim wrote it while this happened to a three-line
file, `retry.go`:

1. The last line was replaced by `// common` (node 1).
2. That line became an experiment, `func experiment() int { return 42 }`
   (node 2).
3. The experiment was undone before it was ever saved.
4. A different fix, `func chosen() int { return 1 }`, was written and saved
   (node 3).

Git could only ever have seen node 3. The experiment exists nowhere but the
undo file. `base.bin` is `retry.go` as it was saved, the text the undo file
was written against. Run these from the repository root:

```sh
cd testdata/undo/abandoned-branch
```

### Inspect the history

`inspect` needs only the undo file. It validates every record and
relationship, then lists the retained states:

```console
$ xunhen inspect --undo history.undo
History: "history.undo"
Format: Neovim undo 3
...
Retained states: 4 (including node 0, the retained root)
Reference node: 3
...
Nodes in recorded branch order (zero child/sibling means absent):
node 0: retained root; preferred-child=1; time=unavailable; save=unavailable
node 1: parent=0 preferred-child=3 next-sibling=0 previous-sibling=0 time=1789769088 save=none text-entries=1 extmarks=1 flags=0x01
node 3: parent=1 preferred-child=0 next-sibling=2 previous-sibling=0 time=1789769088 save=1 text-entries=1 extmarks=1 flags=0x01
node 2: parent=1 preferred-child=0 next-sibling=0 previous-sibling=3 time=1789769088 save=none text-entries=1 extmarks=1 flags=0x01
```

The reference node, 3, is the buffer when the undo file was written, and it
was saved (`save=1`). Node 2 shares node 1 as its parent: it is the other
branch, never saved (`save=none`). That is the abandoned experiment.

### Preview the experiment

`show` rebuilds a state from the base text. The base must match the
history's reference exactly, or nothing is reconstructed:

```console
$ xunhen show --undo history.undo --base base.bin --node 2
package sample

func experiment() int { return 42 }
```

Plain `show` output is for reading: terminal controls and invalid bytes in
recovered text appear as escapes such as `\x1b`.

### Compare it with the saved branch

```console
$ xunhen diff --undo history.undo --base base.bin --from 2 --to 3
--- node 2
+++ node 3
@@ -1,3 +1,3 @@
 package sample
 
-func experiment() int { return 42 }
+func chosen() int { return 1 }
```

### Export it

`--raw` writes the exact text, and `--final-newline` says how the file ends.
The undo file does not record whether the original ended with a newline,
so you choose:

```console
$ xunhen show --undo history.undo --base base.bin --node 2 --raw --final-newline=include > experiment.go
$ cat experiment.go
package sample

func experiment() int { return 42 }
```

xunhen never writes files itself. Redirect raw output, as above; `--raw`
refuses to write to a terminal. Never redirect onto the source or base: the
shell empties the target before xunhen starts. `(set -C; xunhen show ... >
FILE)` makes bash, zsh, or sh refuse any file that already exists.

### The same, from your own source file

With your own files, xunhen finds the history by the source file's path.
First tell it where Neovim keeps undo files. `:echo &undodir` in Neovim
prints the directory; set the variable in your shell's startup file so it
lasts:

```sh
export XUNHEN_UNDO_DIR=$HOME/.local/state/nvim/undo
```

Then give the source file as the argument:

```sh
xunhen inspect retry.go
xunhen show retry.go --node 2
xunhen diff retry.go --from 2 --to 3
xunhen browse retry.go
```

The source file must still hold the text the undo file was written against,
which is normally true until you edit it again outside Neovim.

## Commands

```text
xunhen inspect FILE
xunhen show    FILE --node ID [--raw --final-newline=include|omit]
xunhen diff    FILE --from ID --to ID
xunhen browse  FILE
xunhen help [COMMAND]
xunhen version
```

FILE is the source file. Its path finds the history in the undo
directories, and its text is the base the states are rebuilt from. Instead
of FILE, every command also accepts the undo file directly, with a matching
copy of the text: `--undo PATH --base PATH`, where `inspect` needs no
`--base`. `xunhen help COMMAND` and `xunhen COMMAND --help` print each
command's full help.

| Argument | Commands | Meaning |
| --- | --- | --- |
| `FILE` | all four | Find the history by this file's path; the file is also the base. Flags may come before or after it; put `--` before a name that starts with a dash. |
| `--undo-dir DIR` | with `FILE` | An undo directory to search. Repeat it for more, up to 32. Commas are part of the path, unlike in `'undodir'`. It replaces `XUNHEN_UNDO_DIR` for that command. |
| `--source PATH` | all four | Same as `FILE`. |
| `--undo PATH` | all four | The undo file, instead of `FILE`. Symbolic links are followed; it must be a regular file. |
| `--base PATH` | show, diff, browse | A copy of the text the `--undo` file was written against. |
| `--node ID` | show | The state to rebuild. IDs are the numbers `inspect` prints; 0 is the retained root. |
| `--from ID`, `--to ID` | diff | The left and right states of the comparison. |
| `--raw` | show | Write the exact UTF-8 text instead of escaped display text. Refused when stdout is a terminal. |
| `--final-newline include\|omit` | show, with `--raw` | End the export with a newline after the last line, or not. Required with `--raw`. |
| `-h`, `--help` | all | Show help. It must be the only argument. |
| `--version` | top level | Same as `xunhen version`. |

`FILE` cannot be combined with `--undo` or `--base`. Each flag may be given
once, except `--undo-dir`. Node IDs are plain decimal numbers.

### Where xunhen looks for the history

With `FILE`, xunhen searches the undo directories you give with
`--undo-dir`. Without `--undo-dir`, it searches the ones `XUNHEN_UNDO_DIR`
lists, separated by colons as in `PATH`, such as
`XUNHEN_UNDO_DIR=$HOME/.local/state/nvim/undo:/backup/undo`. With neither, it
stops with a usage error that shows how to set the variable. It never reads
Neovim's configuration or guesses a directory.

Neovim names an undo file after the source's full path, with every `/`
replaced by `%`, as in `%home%me%src%retry.go`. xunhen resolves the source
path the way Neovim does, looks only at that name (and at `.retry.go.un~`
when the source's own directory is one of the searched directories), and
never lists or descends into directories. A candidate counts only after it
decodes, validates, and matches the source text. No match, several matches,
and an unreadable directory are all errors that list every candidate;
nothing is chosen by order or date. [discovery.md](discovery.md) has the
details.

### The terminal browser

`browse` shows the history as a tree beside the selected state or a
comparison. It needs a terminal on stdin and stdout and a `TERM` other than
`dumb`.

| Key | Action |
| --- | --- |
| up/down, `j`/`k` | Select the previous or next row, or scroll the text |
| left/right, `h`/`l` | Fold or unfold a branch, or scroll sideways |
| page up/down, home/end | Move by a screen, or to the first or last row |
| tab | Move between the tree and the text |
| `p` | Preview the selected state |
| `d` | Compare the pinned state with the selected one |
| space | Pin the selected state as the left side of comparisons |
| `g` | Go to a node by its ID |
| `e` | Show the `show --raw` command that saves the selected state |
| `r` | Read and validate the inputs again |
| `?` | Help |
| `q`, ctrl+c, ctrl+z | Quit, interrupt, suspend |

[browse.md](browse.md) describes the browser in full.

## Exit status

| Status | Meaning |
| --- | --- |
| 0 | Success. `diff` exits 0 whether or not the states differ. |
| 1 | An input, output, or search failure. Diagnostics are on stderr. |
| 2 | Invalid arguments. |
| 130 | Interrupted (SIGINT or ctrl+c). |
| 129, 143 | `browse` ended by SIGHUP or SIGTERM, after restoring the terminal. |

When the status is not 0, discard anything on stdout: a write can fail
partway. Everything that can fail for another reason, such as reading,
replay, and comparison, finishes before the first byte is written.

## Output

Results go to stdout and diagnostics to stderr, each diagnostic line
starting with `xunhen: `. No command prints color or terminal controls
unless it is `browse`. Terminal controls and invalid bytes from undo files,
paths, and error text appear as escapes such as `\x1b`, so output is safe to
print. Times are signed Unix seconds in command output; the browser shows
local time.

`show --raw` writes the state's lines joined by LF, with a final LF under
`include` and none under `omit`. An empty buffer is one empty line:
`include` writes a single LF and `omit` writes nothing. Raw export refuses a
state holding invalid UTF-8 or NUL, which retained edits can contain; plain
`show` still displays it with escapes.

`diff` writes a unified diff with three lines of context and one-based line
numbers, headed `--- node FROM` and `+++ node TO`. Identical states print
nothing. The undo file records neither state's final newline, so the diff
never reports one.

The [compatibility guide](compatibility.md#stability-within-v1) says which
of these contracts stay fixed within v1.

## Text the base may hold

A base or source file must be UTF-8 with LF line endings and no byte-order
mark or NUL. A lone CR is part of a line. CRLF, Latin-1, and other text is
refused with the reason; `inspect` still works on those histories. The
[compatibility guide](compatibility.md#text-and-history-cases) lists every
case.

## Limits

xunhen refuses input past these sizes, naming the limit and the input,
rather than running out of memory:

| Input | Limit |
| --- | --- |
| Undo file | 256 MiB |
| Base or source text | 64 MiB |
| Any reconstructed state | 64 MiB, 4,000,000 lines |
| One line | 16 MiB |
| Retained states | 1,000,000 |
| Text and extmark entries | 1,000,000 per file |
| Undo directories per search | 32 |
| Undo bytes read per search | 512 MiB |

Ordinary source files and histories stay far below all of them.
