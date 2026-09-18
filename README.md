# Xunhen ༼⁠ ⁠つ⁠ ⁠◕⁠‿⁠◕⁠ ⁠༽⁠つ

*Seek traces.*

> 落笔生新枝，回首寻旧痕。
>
> New branches grow beneath the brush; look back, and seek old traces.

You write a passage of code, undo it, and follow another path.
The first branch disappears from view. It may not be gone.

**xunhen** will be a read-only Linux tool for exploring Neovim's saved undo
history. That history can preserve abandoned editing branches, including code
that was never saved as source text and never committed to git.

## Follow the branches

The planned application will let you:

- Browse an undo tree in a terminal interface.
- Read recoverable past states, including abandoned branches.
- Compare two states and export recovered text.
- Inspect, recover, and diff history through CLI commands.

It will read undo files and matching base text without modifying them, and
work independently of a running Neovim instance.

Recovery has limits. Only history that Neovim actually persisted and retained
can survive between sessions. Complete reconstruction may require matching
source text or a preserved copy; an undo file is not a record of every
keystroke forever.

## Still taking root

The project is in planning. The implementation will use Go, with Bubble Tea
for the terminal interface. The v1 release will target Linux, with a downloadable
binary, a Nix/NixOS package, and an AUR package.

The architecture, format questions, and build-to-release checklists live in
[PROJECT.md](PROJECT.md).

Licensed under [Apache-2.0](LICENSE).
