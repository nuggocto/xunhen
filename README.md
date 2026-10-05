# Xunhen ༼⁠ ⁠つ⁠ ⁠◕⁠‿⁠◕⁠ ⁠༽⁠つ

*Seek traces.*

> New branches grow beneath the brush; look back, and seek old traces.

You write a passage of code, undo it, and follow another path.
The first branch disappears from view. It may not be gone.

**xunhen** is a read-only Linux tool that reads Neovim's saved undo history,
including abandoned branches that never reached a file or git, and lets you
browse it in the terminal.

It runs on Linux 5.10 or later on x86-64 as one static executable. It never modifies your
undo files or sources, never starts Neovim, and needs no configuration.

## What can be recovered

Only history Neovim wrote to disk: with `'undofile'` on, an undo file is
written each time the buffer is saved. Edits made after the last write, and
states older than `'undolevels'` keeps, are gone. Rebuilding text also needs
the source as it was when the undo file was last written; the undo file
stores changes, not complete copies. [docs/troubleshooting.md](docs/troubleshooting.md)
explains what to do when something is missing.

## Install

There is no stable release yet. The release candidate `v1.0.0-rc.1` is
published for testing on the
[releases page](https://github.com/nuggocto/xunhen/releases), and installs
from:

- a [release archive](docs/install.md#release-archive), with checksums, into
  `~/.local/bin` without root;
- the [Nix flake](docs/install.md#nix-and-nixos), for a user profile or a
  NixOS system configuration;
- [Go source](docs/install.md#go-source), with `go install`.

The [AUR](docs/install.md#arch-user-repository) package `xunhen` arrives
with the first stable release.

## Usage

Tell xunhen once where Neovim keeps undo files. `:echo &undodir` in Neovim
prints the directory; the usual one is:

```sh
export XUNHEN_UNDO_DIR=$HOME/.local/state/nvim/undo   # in ~/.bashrc, ~/.zshrc, or fish's config
```

Then open the history of a file in the browser:

```sh
xunhen browse retry.go
```

The tree on the left holds every retained state; the selected one shows on the
right. Move with the arrows or `j` and `k`, compare two states with `space` to
pin one and `d` to diff it with the selection, and press `e` for the command
that saves the selected state to a file. `?` lists every key, and `q` quits.

The same work is available as plain commands, for scripts or terminals the
browser cannot use:

```sh
xunhen inspect retry.go
xunhen show    retry.go --node 2
xunhen diff    retry.go --from 2 --to 3
xunhen show    retry.go --node 2 --raw --final-newline=include > recovered.go
```

`--undo-dir DIR` names the undo directory for one command instead of the
variable. Or name the undo file and a matching copy of the source directly:

```sh
xunhen show --undo history.undo --base retry.go --node 2
```

- `inspect` lists the undo tree and its node numbers.
- `show` rebuilds one state. `--raw --final-newline=include` writes the
  exact text for redirecting to a file; the undo file does not record
  whether the original ended with a newline, so you choose.
- `diff` compares two states as a unified diff.
- `browse` does all three interactively. It needs a terminal; without one it
  exits with status 1 and points to the commands above.

[docs/usage.md](docs/usage.md) walks through a complete recovery with a
history from the test corpus and lists every flag and exit status.

## Documentation

- [Install](docs/install.md): every channel, checksums, upgrades, and removal
- [Usage](docs/usage.md): a recovery walkthrough, flags, exit codes, and output
- [Troubleshooting](docs/troubleshooting.md): what each error means and what to do
- [Browse](https://github.com/nuggocto/xunhen/blob/shrek/docs/browse.md): the terminal browser, its keys, and its memory use
- [Discovery](https://github.com/nuggocto/xunhen/blob/shrek/docs/discovery.md): how xunhen finds a source file's history
- [Compatibility](https://github.com/nuggocto/xunhen/blob/shrek/docs/compatibility.md): the supported producer, text cases, and what stays stable
- [Undo format](https://github.com/nuggocto/xunhen/blob/shrek/docs/undo-format.md): what is decoded and its limits
- [Diff](https://github.com/nuggocto/xunhen/blob/shrek/docs/diff.md): the comparison algorithm
- [Performance](https://github.com/nuggocto/xunhen/blob/shrek/docs/performance.md): workloads, measurements, and targets
- [Verification](https://github.com/nuggocto/xunhen/blob/shrek/docs/verification.md): what each test guards and how to run the checks
- [Releasing](https://github.com/nuggocto/xunhen/blob/shrek/docs/releasing.md): versions, reproducible archives, and packages
- [Changelog](CHANGELOG.md)

## Development

[CONTRIBUTING.md](https://github.com/nuggocto/xunhen/blob/shrek/CONTRIBUTING.md)
covers building, the checks, the fixture corpus, and adding a decoder.
Please report bugs with the issue form, and never attach a real undo file:
it keeps text you deleted, secrets included.

Licensed under [Apache-2.0](LICENSE). The executable includes third-party
modules whose licenses are in [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt).
