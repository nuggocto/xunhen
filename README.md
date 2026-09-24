# Xunhen ༼⁠ ⁠つ⁠ ⁠◕⁠‿⁠◕⁠ ⁠༽⁠つ

*Seek traces.*

> New branches grow beneath the brush; look back, and seek old traces.

You write a passage of code, undo it, and follow another path.
The first branch disappears from view. It may not be gone.

**xunhen** is a read-only Linux tool that reads Neovim's saved undo history,
including abandoned branches that never reached a file or git. A terminal
browser is planned.

## Usage

Point it at your source file and your undo directory:

```sh
xunhen inspect --source retry.go --undo-dir ~/.local/state/nvim/undo
xunhen show    --source retry.go --undo-dir ~/.local/state/nvim/undo --node 2
xunhen diff    --source retry.go --undo-dir ~/.local/state/nvim/undo --from 2 --to 3
```

Or name the undo file and a matching copy of the source directly:

```sh
xunhen show --undo history.undo --base retry.go --node 2
```

- `inspect` lists the undo tree and its node numbers.
- `show` rebuilds one state. Add `--raw --final-newline=include` to write
  exact text you can redirect to a file.
- `diff` compares two states as a unified diff.

Only edits Neovim saved to disk can be found, and rebuilding text needs the
source as it was when the undo file was last written. Run
`xunhen help COMMAND` for a command's options.

## Documentation

- [Undo format](docs/undo-format.md): what is decoded and its limits
- [Discovery](docs/discovery.md): how `--source` finds a history
- [Diff](docs/diff.md): the comparison algorithm

## Development

Use Go 1.27.1 on Linux/amd64. Tests need no Neovim.

```sh
mise run check   # formatting, tests, vet, build
mise run fuzz    # short fuzz runs
```

Licensed under [Apache-2.0](LICENSE).
