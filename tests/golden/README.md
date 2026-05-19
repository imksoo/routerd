# Render Golden Tests

These fixtures pin routerd render output across the supported host backends:
Linux, Alpine/OpenRC, FreeBSD/rc.d, and NixOS.

Run:

```sh
make check-render-golden
```

Refresh after intentionally changing renderer behavior:

```sh
make update-render-golden
git diff -- tests/golden/render
```

The test uses committed `HEAD` content for tracked example files that are dirty
in a developer worktree, so local operator edits do not leak into snapshots.
