# Xunhen ༼⁠ ⁠つ⁠ ⁠◕⁠‿⁠◕⁠ ⁠༽⁠つ

*Seek traces.*

> New branches grow beneath the brush; look back, and seek old traces.

You write some code, undo it, and take another path. xunhen lets you browse
the abandoned branch in Neovim's saved undo history.

It's a read-only terminal tool for Linux x86-64. It never changes your
source or undo files.

## Install

### Arch Linux (AUR)

```sh
yay -S xunhen
# or: paru -S xunhen
```

### Nix / NixOS

With flakes enabled:

```sh
nix profile add github:nuggocto/xunhen/v1.0.1
```

For NixOS, add `inputs.xunhen.url = "github:nuggocto/xunhen/v1.0.1"` to
your flake. Pass `xunhen` into your configuration and add
`xunhen.packages.x86_64-linux.default` to `environment.systemPackages`.

Prebuilt binaries are on [GitHub Releases](https://github.com/nuggocto/xunhen/releases).

## Usage

Run `:echo &undodir` in Neovim to find your undo directory, then:

```sh
export XUNHEN_UNDO_DIR="$HOME/.local/state/nvim/undo"
xunhen browse path/to/file.go
```

Replace the directory with yours. You can also pass `--undo-dir DIR`.

Use the arrows or `j` / `k` to move, `space` to pin a state, and `d` to
compare it with the selection. Press `e` for the export command, `?` for
help, and `q` to quit.

For plain command output:

```sh
xunhen inspect path/to/file.go
xunhen show path/to/file.go --node 2
xunhen diff path/to/file.go --from 2 --to 3
```

`xunhen help COMMAND` lists the flags.

Recovery needs saved undo history and the source text it was written against.
Edits Neovim never persisted cannot be recovered. Tested with Arch's Neovim
0.12.5 on Linux x86-64; source text must be UTF-8 with LF line endings.

[Changelog](CHANGELOG.md) · [Report a bug](https://github.com/nuggocto/xunhen/issues/new)

Use synthetic files in bug reports. Real undo files can contain deleted secrets.

Licensed under [Apache-2.0](LICENSE). See [third-party notices](THIRD_PARTY_NOTICES.txt).
