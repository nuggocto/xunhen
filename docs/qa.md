# Release candidate QA

A release candidate is a tagged prerelease, such as `v1.0.0-rc.1`, whose
archive, Nix package, and Arch package are checked as users receive them
before a stable release reuses the same source. This page says what a
candidate must show, where each check runs, and how its evidence is kept.
[Releasing](releasing.md) covers the release workflow itself, and each
candidate's results are in `docs/qa/`.

Every check runs a built executable: the one in the release archive, the
one Nix builds from the tagged flake, or the one the Arch recipe builds.
None of them compiles a stand-in. The tools that do the checking,
`tools/verify` and `tools/qa.sh`, are built separately and never replace
the executable under test.

## What a candidate must show

The behavior under test is the v1 contract and nothing more:

| Area | The candidate is held to |
| --- | --- |
| Platform | Linux on x86-64, built with `GOAMD64=v1` and `CGO_ENABLED=0`, one static executable with no loader or shared library |
| Producer | Undo format 3 as Neovim 0.12.5 writes it on Linux x86-64, as [Compatibility](compatibility.md) states |
| Commands | `inspect`, `show`, `diff`, and `browse`, with explicit `--undo` and `--base` inputs and with a source file found through undo directories |
| Text | UTF-8 with LF line endings; raw export with an explicit final-newline policy; other text refused by name |
| Inputs | Opened read-only and never changed; no file of xunhen's own written anywhere |
| Limits | The ceilings in [Usage](usage.md#limits), each failing with a named limit |
| Exit status | 0, 1, 2, and the signal statuses in [Usage](usage.md#exit-status) and [Browse](browse.md#terminal-requirements-and-exits) |
| Isolation | No other program started, no network use, and no log of recovered text, with or without `TEA_TRACE` and `TEA_DEBUG` |

## Support matrix

[`tools/qa-environments.json`](../tools/qa-environments.json) pins every
environment by image digest or locked revision, and the release workflow
and `tools/qa.sh` read it. A label such as `latest` never stands in for a
result.

| Environment | Pinned as | What it shows |
| --- | --- | --- |
| Debian 13 (trixie) | `debian:13` by digest | The archive installed and run with glibc's usual userland |
| Ubuntu 26.04 LTS | `ubuntu:26.04` by digest | A second mainstream userland |
| Alpine 3.24 | `alpine:3.24` by digest | No dependence on glibc; BusyBox's tools follow the install steps too |
| NixOS | the nixpkgs revision in `flake.lock` | The flake builds, tests, and installs through a user profile and `environment.systemPackages` |
| Arch Linux | the `archlinux` image by digest, with devtools | The recipe builds in a clean chroot, and the package installs, upgrades, downgrades, and is removed |
| Linux 5.10 on a qemu64 CPU | `linuxPackages_5_10` from the locked nixpkgs, under QEMU with KVM | The oldest kernel and CPU the release claims |
| The reference machine | [Performance](performance.md#reference-machine) | Resource targets |

The kernel baseline is Linux 5.10, the oldest kernel line the locked
nixpkgs still ships; 5.4 is gone from it. QEMU's `qemu64` model has SSE2 but
no SSE4.1, SSE4.2, or POPCNT, so it sits below x86-64-v2, and
`nix/baseline-test.nix` checks both facts inside the machine before it
runs the verifier. The release therefore claims Linux 5.10 or later on any
x86-64 processor. Older kernels may work, since Go itself supports Linux
3.2, but nothing has shown it.

Containers share their host's kernel and processor, so the three userland
rows show userland compatibility only. The baseline row is a virtual
machine for that reason.

## Where each check runs

| Check | Tool | Runs in |
| --- | --- | --- |
| Archives reproduce byte for byte from two clean checkouts | `tools/reproduce.sh` | Release workflow, Release archives |
| Archive layout, checksums, metadata, linkage, version, recovery corpus, discovery, diff, 19 failure cases, permission denials, unusual paths, interrupt while blocked, 9 browser sessions, unchanged inputs, no program started | `tools/verify` | Release workflow, Release archives; again inside every environment below |
| Install, verify, replace, and uninstall as an ordinary user in Debian, Ubuntu, and Alpine, with no file left in `HOME` | `tools/qa.sh userland` | Release workflow, Candidate QA; delivery workflow on the public download |
| No other program, no socket, no input opened for writing, no trace log | `tools/qa.sh isolation` | Release workflow, Candidate QA |
| Quit, resize, ctrl+c, SIGTERM, SIGHUP, failed load, and suspend and resume in tmux; browsing and a dropped connection over SSH | `tools/qa.sh terminal` | Release workflow, Candidate QA; locally for the Nix and Arch executables |
| Linux 5.10 on a qemu64 CPU, archive executable and Nix executable | `nix/baseline-test.nix` | Release workflow, Kernel and CPU baseline and Nix package |
| Flake build with the test suite, `tools/verify` on the package, NixOS add and remove through `environment.systemPackages`, user profile install, rollback, and removal | `nix flake check`, `nix profile` | Release workflow, Nix package; delivery workflow from the public tag |
| Clean-chroot build with the suite and `tools/verify` in `check()`, payload documents, installed document links, install, verify, upgrade, downgrade, removal | devtools, pacman | Release workflow, Arch package; delivery workflow from the public source URL |
| Public assets equal the verified build | `gh`, `cmp` | Delivery workflow |
| `go install` of the tag from the module proxy, with fresh caches | Go | Delivery workflow |
| Resource and cancellation targets on the candidate executable | `tools/measure.sh -binary` | The reference machine |

The release workflow stages a draft only after every row it runs has
passed. The delivery workflow, `.github/workflows/delivery.yml`, runs by
hand once `publish.yml` has made a release public, since only then do the
public download, the module proxy, and the tagged flake exist.

## Running the checks locally

Each subcommand takes a directory holding a release's four assets, as
`gh release download` leaves them, and reads the version, commit, and
toolchain from its `provenance.json` after checking the assets against it:

```sh
tools/qa.sh artifacts DIR              # checksums, provenance, tools/verify on the archive
tools/qa.sh userland DIR [EARLIER]     # Debian, Ubuntu, Alpine; EARLIER replaces in both directions
tools/qa.sh isolation DIR              # strace in a container
tools/qa.sh terminal EXECUTABLE LABEL  # tmux and SSH, for any channel's executable
tools/measure.sh -binary EXECUTABLE    # resource measurements on the reference machine
```

The userland, isolation, and SSH checks run unprivileged Docker containers
as uid 1000. Only strace and sshd are installed from the network; the
checks themselves run with no network. Permission checks need an ordinary
user, so `tools/verify` fails rather than skip them when it can read a file
with no read permission.

## Evidence

`tools/qa.sh` writes to `.local/qa/VERSION/`, or `QA_OUT`, one directory per
check. Every log starts with the same identity, so a result cannot be
credited to other bytes:

- the candidate version, source commit, toolchain, and archive SHA-256
- the executable's SHA-256 and `--version` output, for the terminal check
- the image digest or kernel and CPU the check ran on
- the commit of the QA tools, and whether they had uncommitted changes
- the date

The release workflow keeps the same evidence as run artifacts:
`qa-evidence`, `baseline-logs`, `nix-logs`, `arch-results`, and
`archive-logs`. A candidate's report in `docs/qa/` names the runs, the
hashes, and every result, and ends with a ship or hold recommendation for
that candidate.

## When a check fails

Record the candidate, the artifact hash, the environment, a minimal
reproduction, and the expected and actual behavior. Keep the first failure:
a later pass does not explain it. A fix that changes the executable is a new
candidate, `-rc.2` after `-rc.1`; a published tag never moves and its assets
are never replaced. The new candidate reruns every check its change could
affect, and always the installation and recovery smoke.
