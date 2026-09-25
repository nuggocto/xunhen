## Skills

- Use the `go` skill and then the `tiger-style` skill for project work.
- Use the `unslop` skill for all writing, including comments and user-facing
  text.
- Use the `test-quality` skill when writing or reviewing tests.
- Use the `security` skill when work touches a trust boundary, such as puzzle
  packs, archives, paths, SQLite, terminal output, CI, installers, releases, or
  the frontend deployment.
- Use the `qa` skill when verifying user-visible behavior, packaged artifacts,
  supported platforms, the frontend, or a release candidate.
- Use the `show-me` skill when ownership, data flow, control flow, state
  transitions, or dependencies are easier to understand as a graph.

## Project rules

- Never mention phase numbers anywhere outside `PROJECT.md`, including source
  code, comments, tests, command output, filenames, and other documentation.
  Track progress only by marking completed checkboxes in `PROJECT.md`.
- Treat `shrek` as the permanent default branch. Do not rename or replace it.
- Write each commit with a simple, well-written subject line and an explanatory
  body. The body must explain why the change was needed instead of restating the
  diff.
- Preserve user work and avoid unrelated edits.
- Write Go tests in table-driven form, with named cases run through `t.Run`.
- Only add a test if its failure would tell you something is actually broken.
  Assertions on styling values, colors, or internal structure fail on harmless
  changes and pass on real bugs, so leave them out.
- Keep `xunhen-front` a separate static Astro site on Cloudflare Pages. It has
  a project landing page and a release changelog, it should never become the working TUI.
- Do not edit `../xunhen-front` unless the current work or user request includes
  it.
- Do not spawn sub-agents unless the current work or user request requires it.
