# AUR recipe

`PKGBUILD.in` is the template for the AUR package
[`xunhen`](https://aur.archlinux.org/packages/xunhen), which builds from the
release's source archive. `resolve.sh` fills in a released version, its
commit, the archive's SHA-256, and the maintainer, then generates
`.SRCINFO`:

```sh
packaging/aur/resolve.sh -version 1.0.0 -commit COMMIT -sha256 SUM \
  -maintainer 'Your Name <you at example dot org>' -out ../aur-xunhen
```

The resolved `PKGBUILD` and `.SRCINFO` go to the separate AUR repository,
never to this one. [docs/releasing.md](../../docs/releasing.md#the-aur-package)
covers the clean-chroot build and publication, and the template's header
explains its build flags.
