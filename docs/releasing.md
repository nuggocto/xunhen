# Releasing xunhen

Release from an annotated tag on `shrek`. Builds use the tagged source, not
the working tree, and produce reproducible binary and source archives,
`SHA256SUMS.txt`, and `provenance.json`.

## Prepare

1. Check for a newer patch release of the Go line in `go.mod`. Update
   `go.mod`, `mise.toml`, CI, and the Nix builder together when needed.
2. Set `VERSION` without the `v` prefix. Move the changelog's unreleased
   entries into a dated section for that version, leaving a new empty
   `Unreleased` section above it.
3. After module changes, regenerate `THIRD_PARTY_NOTICES.txt` with
   `tools/release.sh notices` and update the Nix `vendorHash` below.
4. Run the checks and inspect failures before tagging:

   ```sh
   mise run check
   mise run race
   mise run fuzz
   mise run vulncheck
   mise run notices
   nix flake check -L
   mise run fuzz-extended
   mise run measure
   ```

   The extended fuzz campaign runs each target for 15 minutes. Measurements
   use synthetic workloads; [performance.md](performance.md) records the
   baseline and raw samples. The stored Neovim corpus is independent of
   those workloads; [testdata/README.md](../testdata/README.md) explains how
   to regenerate it when adding a producer.
5. From a clean checkout, rehearse the archives and verify the binary:

   ```sh
   tools/reproduce.sh
   dir=.local/reproduce/$(git rev-parse HEAD)/first
   version=$(jq -r .version "$dir/provenance.json")
   go run ./tools/verify -archive "$dir/xunhen_${version}_linux_amd64.tar.gz" \
     -sums "$dir/SHA256SUMS.txt" -version "v$version" \
     -commit "$(git rev-parse HEAD)" -go go1.27.1
   ```

## Tag and publish

Once the release commit is on `shrek` and CI passes:

```sh
git tag -a vX.Y.Z -m "xunhen X.Y.Z"
git push origin vX.Y.Z
```

The **Release** workflow validates the tag and version, builds the archives
twice, and checks the binary, Nix and Arch packages, terminal behavior, and
Linux 5.10 baseline. It stages the verified bytes as a draft release.
Candidate tags such as `vX.Y.Z-rc.1` use the same checks and publish as prereleases.

Run **Publish release** with the tag as `release`. It compares the draft's
assets with the successful Release run's archive artifact before publishing;
it does not rebuild. Publish before that artifact expires (90 days by
default). Enable immutable releases in the repository settings. Never move
a published tag or replace its assets; fixes get another version.

To rehearse the workflows without a tag, run **Release** manually from
`shrek`. Its draft is named `rehearsal-COMMIT`. Run **Publish release** with
that name and `rehearsal` enabled to check and delete the draft.

## Public channels

After publishing the archives, check the tagged flake:

```sh
nix flake check -L github:nuggocto/xunhen/vX.Y.Z
nix run github:nuggocto/xunhen/vX.Y.Z -- --version
```

Then publish the AUR recipe for a stable release. It lives in its own AUR
repository; `packaging/aur/PKGBUILD.in` here is the maintained template.
On Arch with `devtools` and `namcap`:

```sh
version=X.Y.Z
curl -fLO "https://github.com/nuggocto/xunhen/releases/download/v$version/SHA256SUMS.txt"
commit=$(git rev-parse "v$version^{commit}")
sha256=$(awk -v f="xunhen_${version}_source.tar.gz" '$2 == f {print $1}' SHA256SUMS.txt)
packaging/aur/resolve.sh -version "$version" -commit "$commit" -sha256 "$sha256" \
  -maintainer 'Your Name <you at example dot org>' -out ../aur-xunhen
```

In the AUR clone, review the generated `PKGBUILD` and `.SRCINFO`, run
`namcap PKGBUILD`, `extra-x86_64-build`, and `namcap` on the package, then
commit and push the recipe. Expected warnings are no PIE, no full RELRO,
and an unstripped executable; the template explains them. Run the clean
chroot build directly on Arch, rather than using CI's privileged container
on a workstation. Keep the AUR SSH key on the maintainer's machine.

A packaging-only fix raises `-pkgrel` in `resolve.sh` and regenerates both
files without moving the application tag. Candidates do not go to AUR.

Run **Check a published release** (`delivery.yml`) with the tag once the
public channels are ready. It checks the download, Go module proxy, tagged
flake, NixOS installation, and AUR recipe. Update `xunhen-front` after those
checks pass; its README covers deployment and rollback.

## Updating Nix

- Run `nix flake update nixpkgs`, then `nix flake check -L`. A Go-line change
  also needs the corresponding builder in `nix/package.nix`. Check it with
  `nix eval --raw .#packages.x86_64-linux.xunhen.go.version`.
- After changing `go.sum`, set `vendorHash` to `lib.fakeHash`, run
  `nix build .#xunhen`, and copy the reported hash into `nix/package.nix`.

## Stability within v1

Every 1.x release preserves existing command names, flags, input forms, node
IDs for unchanged undo files, raw export bytes for a given newline policy,
and the `node ID:` lines and existing field names in `inspect`. Unified diffs
keep three lines of context and the headers `--- node FROM` / `+++ node TO`.
Unsupported or damaged input and mismatched bases are refused rather than guessed.

Exit statuses stay fixed: 0 for success (including differing states), 1 for
input/output/search failures, 2 for invalid arguments, and 130 for an interrupt.
The browser restores the terminal and exits 129 on SIGHUP or 143 on SIGTERM.
Discard partial stdout after a failure. Diagnostics may change; scripts should
use the exit status. Commands and fields may be added, and browser keys and
layout may change in minor releases. Breaking these contracts requires v2.
The internal Go packages are not a public API.
