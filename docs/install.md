# Installing xunhen

xunhen runs on Linux 5.10 or later on any x86-64 (amd64) processor. It is one
static executable with no runtime dependencies: no Go, Neovim, git, or
shared libraries. Every channel
below installs the same program, built from the same tagged source.

> **No stable release yet.** The first release candidate, `v1.0.0-rc.1`, is
> published for testing on the
> [releases page](https://github.com/nuggocto/xunhen/releases). The commands
> below use `1.0.0`; replace it with `1.0.0-rc.1` to try the candidate
> through the archive, Nix, or Go. The AUR package arrives with the first
> stable release.

| Channel | Needs | Root? |
| --- | --- | --- |
| [Release archive](#release-archive) | `curl`, `sha256sum`, `tar` | No |
| [Nix and NixOS](#nix-and-nixos) | Nix with flakes | No, except for NixOS system configuration |
| [Arch User Repository](#arch-user-repository) | `base-devel`, `git` | For `pacman` |
| [Go source](#go-source) | Go 1.27.1 or later | No |

Check any installation with `xunhen version`. It prints the version, the
source commit, and the Go toolchain:

```console
$ xunhen version
xunhen v1.0.0
commit: 0123456789abcdef0123456789abcdef01234567
go: go1.27.1
```

## After installing

Tell xunhen where Neovim keeps undo files, once, in your shell's startup
file (`~/.bashrc`, `~/.zshrc`, or `~/.config/fish/config.fish`):

```sh
export XUNHEN_UNDO_DIR=$HOME/.local/state/nvim/undo
```

That is Neovim's default; `:echo &undodir` in Neovim prints yours, and a
list of several is separated by colons. Then `xunhen browse retry.go` opens
the history of `retry.go`. xunhen needs nothing else: no configuration file,
and it never reads Neovim's. [usage.md](usage.md) walks through a recovery.

## Release archive

Each GitHub release holds `xunhen_VERSION_linux_amd64.tar.gz`, the source
archive `xunhen_VERSION_source.tar.gz`, `SHA256SUMS.txt`, and
`provenance.json`, which records the commit, toolchain, and dependencies the
archives came from.

Download and check the archive, then install it for your user alone:

```sh
version=1.0.0
base="https://github.com/nuggocto/xunhen/releases/download/v$version"
curl -fLO "$base/xunhen_${version}_linux_amd64.tar.gz"
curl -fLO "$base/SHA256SUMS.txt"
grep " xunhen_${version}_linux_amd64.tar.gz\$" SHA256SUMS.txt | sha256sum -c
tar -xzf "xunhen_${version}_linux_amd64.tar.gz"
install -Dm755 "xunhen_${version}_linux_amd64/xunhen" "$HOME/.local/bin/xunhen"
```

`sha256sum` must print `OK` for the archive. The `grep` picks the archive's
line, so the check works with GNU coreutils and with BusyBox, as on Alpine,
whose `sha256sum` has no `--ignore-missing`. If `xunhen` is then not found,
add `$HOME/.local/bin` to `PATH` in your shell's startup file, for example
`export PATH="$HOME/.local/bin:$PATH"` in `~/.bashrc`.

The archive also holds the README, license, third-party notices,
changelog, and this guide with the usage and troubleshooting guides.

- **System-wide:** `sudo install -Dm755 "xunhen_${version}_linux_amd64/xunhen" /usr/local/bin/xunhen`.
- **Upgrade or downgrade:** repeat the steps with another version. Every
  published release stays available, and installing over the old file
  replaces it.
- **Uninstall:** `rm "$HOME/.local/bin/xunhen"`, or
  `sudo rm /usr/local/bin/xunhen`. xunhen keeps no configuration, cache, or
  data files.

### What the checksum proves

`sha256sum --check` proves the archive is the one `SHA256SUMS.txt` lists,
so the download was not damaged or swapped in transit. It does not prove who
published it: both files come from the same release page. The archives are
reproducible, so you can check the publisher independently: clone the
repository, check out the tag, and run `tools/reproduce.sh -tag v1.0.0`,
which builds the same archives byte for byte with Go 1.27.1.

## Nix and NixOS

The repository is a flake with a locked nixpkgs. It builds xunhen from
source in the Nix sandbox and runs the test suite as part of the build. It
provides `packages.x86_64-linux.xunhen` (also the default package) and a
matching app. Enable flakes first, for example with
`experimental-features = nix-command flakes` in `~/.config/nix/nix.conf`.

Run it once without installing:

```sh
nix run github:nuggocto/xunhen/v1.0.0 -- --version
```

Install it in your user profile:

```sh
nix profile add github:nuggocto/xunhen/v1.0.0
```

Older Nix releases call the command `nix profile install`.

- **Upgrade:** the reference names a fixed tag, so replace it:
  `nix profile remove xunhen && nix profile add github:nuggocto/xunhen/v1.0.1`.
- **Roll back** to the profile before the last change: `nix profile rollback`.
- **Remove:** `nix profile remove xunhen`.

### NixOS system configuration

In a flake-based NixOS configuration, add xunhen as an input and put it in
`environment.systemPackages`:

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    xunhen.url = "github:nuggocto/xunhen/v1.0.0";
  };

  outputs = { nixpkgs, xunhen, ... }: {
    nixosConfigurations.myhost = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        ./configuration.nix
        {
          environment.systemPackages = [ xunhen.packages.x86_64-linux.default ];
        }
      ];
    };
  };
}
```

Then run `sudo nixos-rebuild switch --flake .#myhost`. Leave out
`xunhen.inputs.nixpkgs.follows`: the package needs the Go 1.27 builder from
the nixpkgs revision its own lock file pins.

- **Update:** change the tag in `xunhen.url`, run `nix flake update xunhen`,
  and rebuild.
- **Roll back:** `sudo nixos-rebuild switch --rollback`, or pick an earlier
  generation from the boot menu.
- **Remove:** delete the entry from `environment.systemPackages`, then remove
  the input and rebuild.

The flake's checks boot a NixOS machine that adds xunhen this way, recovers
a file and runs the artifact checks with it, and removes it again. Another
machine runs the same checks on Linux 5.10 with a baseline x86-64 CPU, the
oldest the release supports.

## Arch User Repository

The AUR package `xunhen` builds from the release's source archive and checks
its SHA-256. The build runs the test suite and the artifact checks, then
installs `/usr/bin/xunhen` with its license and documentation. Build it with
`makepkg`, no AUR helper required:

```sh
git clone https://aur.archlinux.org/xunhen.git
cd xunhen
less PKGBUILD       # read the recipe before you build it
makepkg --syncdeps --install
```

- **Upgrade:** `git pull`, read what changed with `git log -p`, then run
  `makepkg --syncdeps --install` again.
- **Downgrade:** each build leaves its package in the clone. Install an
  earlier one with `sudo pacman -U xunhen-1.0.0-1-x86_64.pkg.tar.zst`, or
  check out an earlier commit of the recipe and build it.
- **Remove:** `sudo pacman -R xunhen`.

The recipe builds the same static executable as the release archive,
without cgo.
[packaging/aur/PKGBUILD.in](https://github.com/nuggocto/xunhen/blob/shrek/packaging/aur/PKGBUILD.in)
explains why it departs from Arch's PIE build flags.

## Go source

With Go 1.27.1 or later:

```sh
CGO_ENABLED=0 go install github.com/nuggocto/xunhen/cmd/xunhen@v1.0.0
```

This installs `$(go env GOPATH)/bin/xunhen`, usually `~/go/bin/xunhen`.
`CGO_ENABLED=0` makes it the same static executable as the other channels:
with a C compiler installed, Go otherwise links it against the C library.
`xunhen version` reports the module version, and `commit: unknown`, because
Go records no commit for a module download. Remove it with
`rm "$(go env GOPATH)/bin/xunhen"`.

To build from the release's source archive instead, with the same flags as
the release, take the commit from `provenance.json`:

```sh
tar -xzf xunhen_1.0.0_source.tar.gz
cd xunhen_1.0.0_source
CGO_ENABLED=0 go build -trimpath -buildvcs=false \
  -ldflags "-X main.version=v1.0.0 -X main.commit=COMMIT" -o xunhen ./cmd/xunhen
```
