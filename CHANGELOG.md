# Changelog

User-visible changes to xunhen, newest first. Each release lists only the
categories it has entries for: Added, Changed, Fixed, Removed, Deprecated,
and Security.
[Versioning](https://github.com/nuggocto/xunhen/blob/shrek/docs/releasing.md#stability-within-v1)
says which behavior stays fixed within a major version.

## [Unreleased]

## [1.0.1] - 2026-10-05

### Changed

- The release archive and the AUR package carry the README, changelog,
  license, and third-party notices. The separate install, usage, and
  troubleshooting guides are gone: the README covers installation and use,
  and `xunhen help COMMAND` lists every flag.

## [1.0.0] - 2026-10-05

The first stable release. It recovers states from the undo history Neovim
saved to disk, including branches you undid and never saved. See the
[installation instructions](README.md#install), and report problems in
[issues](https://github.com/nuggocto/xunhen/issues/new) using synthetic files
rather than real undo files, which may contain deleted secrets.

### Added

- `xunhen inspect` describes a Neovim undo history without its source text:
  the format, the retained states and their branches, the reference state,
  and recorded times.
- `xunhen show` rebuilds any retained state, including abandoned branches
  that were never saved, from a copy of the text the undo file was written
  against. `--raw` with `--final-newline=include|omit` writes the exact text
  for redirecting to a file.
- `xunhen diff` compares two retained states as a unified diff.
- `xunhen browse` explores a history in the terminal: a branch tree, a
  preview of each state, comparisons, and the command that exports a state.
- Every command takes the source file as its argument, as in
  `xunhen browse retry.go`, and finds its history by the file's path in the
  undo directories you name with `--undo-dir` or once in `XUNHEN_UNDO_DIR`.
  Missing or ambiguous matches are reported instead of guessed.
- Release archives for Linux on x86-64 with checksums and a provenance
  record, a Nix flake for NixOS, the AUR package `xunhen`, and `go install`.

### Compatibility

- Supports undo format 3 as written by Neovim 0.12.5 on Linux x86-64, the
  producer the test corpus comes from. Other producers are unverified.
- Base text must be UTF-8 with LF line endings, and must be the source as it
  was when the undo file was last written. Other text is refused with a
  named reason.
- Runs on Linux 5.10 or later on any x86-64 processor, as one static
  executable. It only reads: undo files and sources are never changed.

### Known limitations

- Only history Neovim wrote to disk survives: edits after the last write,
  and states older than `'undolevels'` keeps, are gone.
- The undo file does not record the original encoding or whether the file
  ended with a newline, so export takes an explicit `--final-newline`.
- `browse` needs an interactive terminal; `inspect`, `show`, and `diff` work
  anywhere.

## [1.0.0-rc.1] - 2026-10-05

The first release candidate, published for testing. It is a prerelease:
the AUR package follows the first stable release.

### Added

- `xunhen inspect` describes a Neovim undo history without its source text:
  the format, the retained states and their branches, the reference state,
  and recorded times.
- `xunhen show` rebuilds any retained state, including abandoned branches
  that were never saved, from a copy of the text the undo file was written
  against. `--raw` with `--final-newline=include|omit` writes the exact text
  for redirecting to a file.
- `xunhen diff` compares two retained states as a unified diff.
- `xunhen browse` explores a history in the terminal: a branch tree, a
  preview of each state, comparisons, and the command that exports a state.
- Every command takes the source file as its argument, as in
  `xunhen browse retry.go`, and finds its history by the file's path in the
  undo directories you name with `--undo-dir` or once in `XUNHEN_UNDO_DIR`.
  Missing or ambiguous matches are reported instead of guessed.
- Release archives for Linux on x86-64 with checksums and a provenance
  record, a Nix flake for NixOS, and an AUR recipe.

### Compatibility

- Supports undo format 3 as written by Neovim 0.12.5 on Linux x86-64, the
  producer the test corpus comes from. Other producers are unverified.
- Base text must be UTF-8 with LF line endings. CRLF, Latin-1, and other
  encodings are refused with a named reason.
- Runs on Linux 5.10 or later on any x86-64 processor. The archive is
  checked on Debian 13, Ubuntu 26.04 LTS, and Alpine 3.24, the Nix package
  on NixOS, the Arch package in a clean chroot, and the executable on Linux
  5.10 with a baseline x86-64 CPU.
