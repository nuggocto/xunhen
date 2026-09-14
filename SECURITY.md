# Security

Xunhen is in early development. It provides a Linux terminal viewer for
unstaged tracked text modifications and CLI diagnostics. No supported stable
release exists.

Do not include vulnerability details, source code, credentials, or private
paths in a public issue. Request a private security contact from the maintainer,
[Nuggocto](https://github.com/nuggocto), without describing the vulnerability.
Once a private channel is established, include the revision, platform,
reproduction using synthetic data, and expected and actual behavior.

Xunhen trusts the selected Git executable; its process controls do not sandbox
a malicious replacement. The viewer reads tracked worktree files and Git data.
It captures private, bounded index and worktree snapshots outside the repository,
removes them on normal exit, and recovers abandoned snapshots on later starts.
User configuration and logs also stay outside repositories.

Repository reads reject symlinks and special files. Git runs with optional locks,
lazy fetching, external diff, text conversion, filters, and fsmonitor execution
disabled for the supported comparisons. Missing objects and unsupported states
produce visible refusals. Repository-local `core.attributesFile` overrides are
refused; outside-root attribute files must come from user or system configuration.
Configured Git time and output limits apply to both diagnostics and review work.

Help, version, completions, and doctor do not scan repository content. Xunhen
does not launch language servers in this build. The tested read-only and
no-network behavior covers Xunhen and the qualified Git executable, not a
malicious replacement or unrelated processes.
