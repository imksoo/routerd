# System policy and dependency checks

The Linux gates exercise the existing controller worker and command boundaries
without changing the shared machine's hostname, kernel settings or packages.

## Entry points

Use the Go toolchain selected by go.mod:

```sh
make check-routerd15-system-policy
make check-routerd15-system-native-linux
```

The first target runs the focused controller, package-query and registration
regressions. The second target runs the explicit Linux kernel tests from
`system_policy_native_linux_test.go` in private user, UTS, network and mount
namespaces, with a fresh sysfs mount. It records the external hostname and IPv4
forwarding value before and after execution. Namespace setup failure is a failed
gate, not permission to fall back to shared settings.

The native target is maintained with the other development gates in
[development.md](/operations/development.md). It does not install packages, start services or
load kernel modules.

## Behaviors under test

- `Hostname` with `managed: true` reaches the registered controller. Linux
  persistence uses the hostname file and runtime changes use the actual
  `hostname` command. A fresh runtime observation is required before reporting
  `Applied`. Failure remains visible and the next reconciliation can converge.
- Removing hostname management stops enforcement. It does not invent a previous
  hostname. The kernel gate checks an external name remains after management
  removal, and reopens the same SQLite database before reconciling.
- `Sysctl` supports runtime-only and persistent-only intent.
  `runtime: false` with `persistent: true` creates the configured Linux
  fragment without calling a kernel setter. Runtime matching retains the
  existing whitespace normalization and `atLeast` comparison used by
  `SysctlProfile`.
- Required sysctl observation failure stops mutation. Optional failure can be
  skipped, but required status-save errors are returned.
- Debian package queries use a private dpkg database and the actual
  `dpkg-query`. Selection such as `hold` is distinct from installation
  state. A removed package with configuration files is missing even when the
  query exits successfully. Unknown query output or a failed observation is
  reported before the installation boundary.
- Package installation decisions are checked at the existing command boundary.
  No apt, dnf or pkg installation is performed by these regressions.
- The actual standby worker and sandbox options keep these controllers
  observational. `Hostname` changes are guarded by `DryRunHostname` alongside
  the existing package and sysctl options.

The current `Hostname`, `Sysctl`, `SysctlProfile` and `Package` APIs do not
expose a `when` field. These tests use the actual managed, runtime, persistent
and read-only contracts.

## Evidence and scope

Retain the tested commit, source hashes before and after execution, complete
output and exit status, binary hash, and outer namespace/settings observations.
A compile result, an installation decision and a real kernel change are separate
observations.

These Linux gates do not qualify a real package installation or an OS boot.
Removing a declaration does not imply rollback to an unknown previous kernel
value or hostname. The existing Linux per-resource sysctl fragment behavior is
unchanged by the FreeBSD persistence work.

The new FreeBSD exact-block persistence owner, SQL compare-and-save and startup
GC tests remain on the separate system-policy work branch. Its unit results do
not establish a FreeBSD boot or a native persistence test. Further FreeBSD
qualification is deferred while Linux is prioritized.
