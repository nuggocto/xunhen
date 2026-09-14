# Xunhen · 寻痕

A read-only terminal code browser for following changes through a codebase.
Review unstaged tracked text modifications on Linux in a unified diff.

Install [mise](https://mise.jdx.dev), rustup, a C compiler, and Git 2.55.0 or newer.
Then build and run:

```sh
mise trust
mise install
mise run build
target/release/xunhen
target/release/xunhen doctor
```

During development, `cargo run` opens the same unstaged diff viewer.

Use `version --json` for version information and `completions bash` for shell
completions. Configuration is optional and lives at
`$XDG_CONFIG_HOME/xunhen/config.toml` or `$HOME/.config/xunhen/config.toml`.

Run `mise run test` for tests or `mise run check-all` for all CI checks.

Use `--repo <path>` to select a repository. The footer names the active pane.
`Tab` switches panes, `j`/`k` move, `[`/`]` jump between hunks, Page Up/Down
scroll, and left/right scroll horizontally. `Esc` cancels loading, `r`
refreshes, and `q` exits.

Colors use your terminal's palette and default background.

Supported repositories have an ordinary SHA-1 index and an existing commit.
Other change kinds, symlink entries, linked or sparse worktrees, split indexes,
Git configuration includes, system attributes, and conversion attributes
are unavailable. External-filter paths remain indeterminate. Untracked files
are outside this view.

Loading is bounded to 20,000 tracked entries, 16 MiB per file, and 256 MiB of
private temporary snapshots outside the repository. Memory and queue limits
can refuse a comparison sooner. Snapshots are removed on exit and recovered
after interrupted sessions. No filters are run and missing objects are not fetched.

Parser fuzz targets live in `fuzz/`. With cargo-fuzz and a nightly toolchain,
run `cargo +nightly fuzz run patch -- -max_total_time=30` or replace `patch`
with `records`.

[Project plan](PROJECT.md) · [Security](SECURITY.md) · [Apache-2.0](LICENSE)
