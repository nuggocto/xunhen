# Contributing to xunhen

xunhen is a Go program for Linux on x86-64. The default branch is `shrek`.

## Build and check

You need Go 1.27.1, the version `go.mod` names, plus `bash` and `git`.
[mise](https://mise.jdx.dev) installs the pinned Go from `mise.toml`; any
other way of installing Go 1.27.1 works too. Neovim is not needed.

```sh
git clone https://github.com/nuggocto/xunhen.git
cd xunhen
go build -o bin/xunhen ./cmd/xunhen
```

The checks CI runs:

```sh
mise run check      # gofmt, go test, go vet, and a build
mise run race       # the tests under the race detector (needs cgo and a C compiler)
mise run fuzz       # every fuzz target for about 10 seconds
mise run vulncheck  # reachable known vulnerabilities, with the pinned govulncheck
mise run notices    # fails if THIRD_PARTY_NOTICES.txt no longer matches the linked modules
mise run reproduce  # builds the release archives twice from a clean clone and compares them
```

Without mise, each task in `mise.toml` is one command you can run
directly. [docs/verification.md](docs/verification.md) lists what each test
guards and the longer fuzz campaign and measurements to run before a
release.

Some tests need things a minimal system lacks, and they fail rather than
skip without them: the terminal tests need a pseudo-terminal, the quoting
tests need `bash`, the release-tool tests need `git`, and the artifact
verifier's tests need `/bin/sh`. Tests that build the command inherit
`GOFLAGS`, so they work in vendored builds such as Nix's.

## Checking a built executable

`tools/verify` runs the checks every release channel passes against an
executable you already have. It never compiles anything:

```sh
version=$(go version -m bin/xunhen | awk '$1 == "mod" { print $3 }')
go run ./tools/verify -binary bin/xunhen -version "$version" -commit "$(git rev-parse HEAD)" -go go1.27.1
```

A build from a clone reports the pseudo-version Go derives from the commit,
such as `v0.0.0-20260927154418-65a2d2281456`, which the first line reads
back from the executable.

It checks the embedded build settings and static linkage, exports every
recorded state of every fixture and compares it with Neovim's record, walks
through discovery and a diff, feeds damaged inputs, drives the browser on a
pseudo-terminal, and confirms nothing wrote to the inputs or started another
program. A build from a working tree with uncommitted changes fails the
metadata check, as it should.

## Tests

Tests are table-driven, with named cases run through `t.Run`. Add a test
only if its failure would mean something is broken. Tests that pin colors,
spacing, or internal structure break on harmless changes and miss real bugs,
so leave them out. Expected recovered text comes from Neovim's recorded
states in the fixture corpus, never from xunhen's own output.

## The fixture corpus

`testdata/undo/` holds undo files Neovim wrote, with the states Neovim read
back from each. [testdata/README.md](testdata/README.md) explains the files
and how to regenerate them with the pinned Neovim build:

```sh
go run ./tools/fixtures -nvim /usr/bin/nvim -out /tmp/xunhen-fixtures-new
```

The generator refuses any other executable. It runs each case in a private
editor with no configuration and checks Neovim's own reading of every
history. Regenerated files carry new timestamps, so their bytes change;
review the oracle states before replacing the stored corpus.

## Adding a decoder or a producer

[docs/undo-format.md](docs/undo-format.md) specifies the one format xunhen
reads, with pinned Neovim source links. To support something new:

1. Read the new producer's undo writer, reader, and replay code at a pinned
   revision, and write down what changed in `docs/undo-format.md`.
2. If the wire format changed, add a decoder file beside
   `internal/undofile/v3.go`. Dispatch on the file's verified version and
   flags, never on the Neovim release, and translate into the same
   normalized records. `internal/history` and everything above it must not
   learn about format versions.
3. Generate a corpus with that producer's executable: add its cases to
   `tools/fixtures`, record its provenance, and check in its fixtures.
4. Run the whole matrix against it and add the producer to
   [docs/compatibility.md](docs/compatibility.md). Until then it stays
   unverified, even if its files happen to decode.

A release that shares a version string with a supported one is still a
different producer until its own corpus passes.

## Releases

[docs/releasing.md](docs/releasing.md) covers versions, the release
workflow, reproducible archives, and the Nix and Arch packages.

## Commits

Write a short subject line and a body that explains why the change was
needed; the diff already shows what changed. Keep unrelated changes apart.

## Reporting bugs

Use the bug report form. Include `xunhen version`, the command, and the
complete diagnostic. **Never attach a real undo file.** Undo files keep
deleted text, including secrets you removed. Reproduce the problem with a
small synthetic file instead; [docs/troubleshooting.md](docs/troubleshooting.md#reporting-a-problem)
shows how.
