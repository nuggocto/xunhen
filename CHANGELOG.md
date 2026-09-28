# Changelog

User-visible changes to xunhen, newest first. Each release lists only the
categories it has entries for: Added, Changed, Fixed, Removed, Deprecated,
and Security. [docs/compatibility.md](docs/compatibility.md#stability-within-v1)
says which behavior stays fixed within a major version.

## [Unreleased]

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
