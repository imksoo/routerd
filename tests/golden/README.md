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

Lifecycle-specific behavior that must not collapse into generic
restart/reload commands is pinned separately under
`pkg/servicemgr/testdata/bespoke_lifecycle_commands.golden`. Run:

```sh
make check-bespoke-lifecycle
```

That target covers FRR live reload, keepalived no-op/reload behavior, dnsmasq
SIGHUP reloads, DHCP daemon IPC, IngressService nftables dataplane updates,
VRRP track script artifacts, DS-Lite dataplane hooks, DHCP event daemon
ordering, BFD daemon enablement, and FRR graceful-restart observation.

`tests/golden/coverage.txt` pins minimum line coverage for the Task #35
abstraction packages. `make check-render-golden` enforces the snapshot by
running `go test -cover` for `pkg/servicemgr`, `pkg/firewallbackend`, and
`pkg/netconfigbackend`; keep each package at or above the recorded threshold.
