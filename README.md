# Xunhen · 寻痕

A read-only terminal code browser for following changes through a codebase.
The current build provides CLI diagnostics on Linux. Repository browsing is planned.

Install [mise](https://mise.jdx.dev), rustup, a C compiler, and Git 2.55.0 or newer.
Then build and run:

```sh
mise trust
mise install
mise run build
target/release/xunhen --help
target/release/xunhen doctor
```

Use `version --json` for version information and `completions bash` for shell
completions. Configuration is optional and lives at
`$XDG_CONFIG_HOME/xunhen/config.toml` or `$HOME/.config/xunhen/config.toml`.

Run `mise run test` for tests or `mise run check-all` for all CI checks.

[Project plan](PROJECT.md) · [Security](SECURITY.md) · [Apache-2.0](LICENSE)
