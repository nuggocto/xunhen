# Xunhen ༼⁠ ⁠つ⁠ ⁠◕⁠‿⁠◕⁠ ⁠༽⁠つ

*Seek traces.*

> New branches grow beneath the brush; look back, and seek old traces.

You write a passage of code, undo it, and follow another path.
The first branch disappears from view. It may not be gone.

**xunhen** is a read-only Linux tool for inspecting Neovim's saved undo history.
It can reveal abandoned editing branches, including code that never reached a
source file or git. The current build has an `inspect` command; recovery,
diffing, and a terminal browser are planned.

Only edits that Neovim persisted and retained can be found. Reconstructing text
will also require a matching source file or copy.

## Inspect a history

```sh
go run ./cmd/xunhen inspect --undo testdata/undo/abandoned-branch/history.undo
```

`inspect` shows the validated undo tree and its reference state without reading
source text or starting Neovim. It does not reconstruct past text. The decoder
uses a Neovim v0.12.5 Linux/amd64 format profile; undo files do not identify
their producer or ABI, so other profiles are unverified. Run
`go run ./cmd/xunhen inspect --help` for options. The
[format notes](docs/undo-format.md) cover compatibility and limits.

## Development

Use Go 1.27.1 on Linux/amd64. Ordinary builds and tests need no Neovim
installation. With [mise](mise.toml):

```sh
mise run check
mise run fuzz
```

`check` runs tests, vet, and a build. `fuzz` runs short decoder and history
checks. Fixture generation is separate; see [testdata](testdata/README.md).

Licensed under [Apache-2.0](LICENSE).
