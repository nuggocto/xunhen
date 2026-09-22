# Undo fixtures

Neovim produces these histories. Each directory in `undo/` contains:

| File | Contents |
| --- | --- |
| `initial.bin` | Source text before editing |
| `base.bin` | Text matching the saved history's reference state |
| `history.undo` | Neovim's original binary output |
| `oracle.json` | Editing steps, tree metadata, and states read back by Neovim |

`corpus.json` records the producer and each file's SHA-256. The generator accepts
only the pinned Arch `neovim 0.12.5-1` executable on x86_64.

## Run the tests

```sh
go test ./...
```

Tests read the stored files; they do not start Neovim. They check file hashes,
expected text and ancestry, the undo envelope's reference hash, and every
recorded replay state against the independent oracle. Command tests also load
the supported base files from disk.

## Regenerate

From the repository root, with the pinned Neovim installed:

```sh
go run ./tools/fixtures -nvim /usr/bin/nvim -out /tmp/xunhen-fixtures-new
```

The destination must not exist. Review the new corpus before replacing the
stored one. A failed run may leave partial output; `corpus.json` is written
only after every case passes.

Each case uses two editor processes: one creates the history, the next loads
it and visits the requested states. The reader receives no editing steps.
Results are checked against the expected text and parents in `cases.go`.

Both processes use private HOME/XDG directories and an empty PATH. Config,
plugins, ShaDa, swap, modelines, and exrc are disabled. Each process has a
20-second timeout; a three-minute context or an interrupt cancels generation.
Temporary editor files are removed on return.

Neovim records real timestamps, so regeneration can change binary hashes.
Preserve those bytes rather than normalizing them.

## Reading the results

`lines_hex` holds `nvim_buf_get_lines` bytes as hex, preserving NUL and invalid
UTF-8. `created.observations` describes editing; `loaded.states` describes
replay. Use the latter as the reconstruction oracle.

Cases worth starting with:

- **abandoned-branch:** state 2 holds the experiment; the saved base is state 3.
- **linear:** an unchanged line is absent from the undo file and needs the base.
- **pruned:** state 0 is the retained root, not the original source text.
- **joined-edits:** an insertion makes inverse-entry order matter.
- **eol-option-change:** undo restores text but leaves the current newline option.
- **unsaved-wundo:** the undo file needs text that had not been saved to disk.
- **intermediate-branch:** the newest marker remains on another branch; the
  next-redo header's parent identifies the reference state.

The corpus also covers empty buffers, repeated lines, save/reopen, line moves,
CRLF, Latin-1, invalid UTF-8, NUL, and terminal controls. A fixture documents a
case; it does not imply that xunhen already supports it. See the
[format specification](../docs/undo-format.md) for decoding and export rules.
