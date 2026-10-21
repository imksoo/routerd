---
title: DHCP lease sync for HA routers
slug: /how-to/dhcp-lease-sync
---

# DHCP lease sync for HA routers

![Diagram showing active DHCP lease sync using platform-derived lease files, VirtualAddress role gating, rsync over hardened SSH, and standby warm leases](/img/diagrams/how-to-dhcp-lease-sync.png)

Use `DHCPv4ServerLeaseSync`, `DHCPv6ServerLeaseSync`, or
`DHCPv6PrefixDelegationLeaseSync` when two routerd nodes share a DHCP role and
the active node must keep the standby node's lease state warm. These resources
are intended for active-to-standby sync; they should normally be gated by a
`VirtualAddress` role so the backup does not copy stale leases back to the
active node.

The complete example is in `examples/dhcp-lease-sync-ha.yaml`.

## Use persistent defaults

routerd stores dnsmasq server leases under its platform state directory by
default. Keep the DHCP resources free of runtime-only lease paths:

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    interface: lan
    addressPool:
      start: 192.168.30.100
      end: 192.168.30.199

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: lan-dhcpv6
  spec:
    interface: lan
    mode: stateful
    addressPool:
      start: fd00:30::100
      end: fd00:30::1ff
```

The sync resource derives the actual file path from the source kind, so the
configuration does not need to repeat implementation paths.

## Sync only from the active node

Gate the lease sync on the local `VirtualAddress` status. The sync runs only
while the VIP role is `master`:

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4ServerLeaseSync
  metadata:
    name: lan-v4-leases
  spec:
    source:
      resource: DHCPv4Server/lan-dhcpv4
    interval: 30s
    targets:
      - name: standby
        host: routerd-standby.lan.example
        user: routerd
    when:
      state:
        VirtualAddress/lan-vip.role:
          equals: master
```

For a stateful DHCPv6 server, use `DHCPv6ServerLeaseSync` with
`source.resource: DHCPv6Server/<name>`. For WAN-side prefix delegation, use
`DHCPv6PrefixDelegationLeaseSync` with
`source.resource: DHCPv6PrefixDelegation/<name>`.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegationLeaseSync
  metadata:
    name: wan-pd-lease
  spec:
    source:
      resource: DHCPv6PrefixDelegation/wan-pd
    targets:
      - name: standby
        host: routerd-standby.lan.example
        user: routerd
    when:
      state:
        VirtualAddress/lan-vip.role:
          equals: master
```

On promotion, the standby starts from its last synced lease state instead of an
empty database.

## SSH requirements

Lease sync uses `rsync` over SSH. Prepare non-interactive SSH before enabling
the resource:

- Create or install an SSH key for the routerd process user on the active node.
- Install the public key in `authorized_keys` for `target.user` on the standby
  node.
- Preload or manage `known_hosts` for `target.host`; `BatchMode=yes` prevents
  interactive host-key prompts.
- Ensure the target user can create the derived destination directory and write
  the lease file.

routerd adds these SSH defaults when `target.sshOptions` does not override the
same key:

```text
-o BatchMode=yes -o ConnectTimeout=10
```

It also adds `rsync --timeout=60` and runs each sync command with a controller
context deadline. Operators can override the SSH options with
`target.sshOptions` and the rsync timeout with `target.options`:

```yaml
targets:
  - host: routerd-standby.lan.example
    user: routerd
    sshOptions:
      - -o
      - ConnectTimeout=5
    options:
      - --timeout=30
```

## Check it

```bash
routerctl validate -f examples/dhcp-lease-sync-ha.yaml --replace
routerctl plan -f examples/dhcp-lease-sync-ha.yaml --replace
routerctl describe VirtualAddress/lan-vip
routerctl describe DHCPv4ServerLeaseSync/lan-v4-leases
```

When the node is not master, `routerd serve` filters the resource through
`spec.when`. When the node is master and the lease file exists, status should
move to `Synced`; if the lease file is missing, it stays `Pending` until dnsmasq
creates it.

## Callback recovery across routerd restarts

The managed dnsmasq hook is a symlink to `routerd-dhcp-event-relay` with no
additional arguments. It commits each callback to
`<platform StateDir>/dhcp-events/pending.db` before exiting; it no longer needs
a running control socket. Linux uses `/var/lib/routerd`, FreeBSD uses
`/var/db/routerd`, and a serve sandbox uses its own platform state directory.
Configuration paths, runtime socket paths, and `--state-file` do not redefine
this shared directory. `--state-file` still selects only the main state DB.
No additional service or dnsmasq configuration change is required.

A manual relay invocation with explicit `--socket` keeps HTTP transport and
uses the receiving daemon's queue. It does not write a second local queue or
retry through a different transport. An unavailable socket therefore remains
an explicit failure. Use this API mode to target a sandbox or a daemon with a
custom socket, rather than inferring persistence paths from that socket.

Bounds and ownership:

- The producer has a 3-second operation deadline and a 4-second process ceiling.
  SQLite lock waits are 100 milliseconds. Storage failures and queue exhaustion
  return nonzero; a successful callback means a committed queue record.
- The queue holds at most 4,096 pending callbacks. Its database ceiling is 8 MiB;
  DELETE journaling also bounds the rollback journal by the database size.
  MAC, IP, and interface fields allow 64 bytes each, hostname allows 253 bytes,
  and the encoded payload allows 2,048 bytes.
- The queue directory is mode 0700 and files are 0600, owned by the executing
  user (root for production routerd/dnsmasq). Symlinks, hardlinked files,
  nonprivate modes, or a different owner are rejected. Existing directories
  are not silently chmodded. No queue is created by an idle serve startup.
- One consumer in the existing serve process drains FIFO order. A file lock
  excludes a second consumer without holding a SQLite transaction during event
  processing. Failure retains the head and does not stop routing controllers.
  Repair a full/unwritable/corrupt store before expecting the queue to drain.
  Do not delete or replace the queue while routerd is running.
- Pending callbacks are local to a node and are not replicated by lease sync.
  Preserve the directory in backups; do not replay both nodes' queues on one
  node. This is restart recovery, not delivery after destruction of that disk.

Sticky metadata and the durable journal are processed before removal from the
queue. A crash between journal commit and queue deletion may repeat a history
record. Original observation times are preserved in `attributes.observedAt`;
the journal's `createdAt` remains ingestion time. Equal-time retries do not
extend sticky retention. Newer lease observations, including copied HA state
and a different MAC on the same IP, prevent older metadata from taking over.
`stickyHoldDays: 0` still records ordering metadata with `sticky_until=0` and
never creates a held reservation.

Callbacks captured before the consumer starts are historical. For IPv4, the
consumer uses the same explicit/default `leaseFile` selection as dnsmasq. It
forwards additions/renewals to live DNS only when MAC, IP, hostname, and lease
validity still match; it forwards removal only when that IP has no active
lease. Missing, unreadable, malformed, or oversized lease files retain the
callback for a later attempt. A confirmed mismatch is journaled as
`history-only-lease-state-changed`. With no configured DHCP source it is
`history-only-no-dhcp-source`. Historical IPv6 identity mapping is not inferred
from IPv4-style fields: such callbacks are journaled as
`history-only-unverified-dhcpv6-identity`, without changing current metadata or
DNS. Fresh IPv4/IPv6 callbacks retain normal delivery.

Historical notifications carry `replayed=true` and never renew or withdraw
mobility ownership. Ownership waits for a fresh observation or existing expiry
processing; this change does not add a lease-file rescan to mobility discovery.
Fresh DHCP notifications preserve their original observation time there too.
These safeguards intentionally restore audit without applying uncertain old
state to live consumers. Local subscriber delivery is still best effort; the
queue does not provide acknowledgements from every DNS or mobility consumer.
Callbacks lost before this version were never queued and cannot be recovered
from this mechanism.

## Investigate a recovered synchronization failure

The existing routerd service log records `DHCP lease synchronization failed`
for all three lease-sync kinds. It includes the resource, reason, source file,
target, command name, error, and subprocess output. When available, `exitCode`
contains the process exit status (`-1` means no normal exit code); `timedOut`
and `canceled` distinguish the command context cause from an exit error.
Invalid specifications and source-stat failures are also logged.

Read the service journal after the resource has recovered:

```sh
journalctl -u routerd.service --since '1 hour ago' --no-pager | grep 'DHCP lease synchronization'
```

Each textual diagnostic field retains at most its last 4,096 bytes, preserving
valid UTF-8; a `<field>Truncated=true` attribute marks truncation. Command
arguments, SSH options and lease contents are not explicitly logged. Subprocess
diagnostics may contain paths and host names, so apply normal log access controls.
Identical consecutive failures are logged once; changed diagnostics and a failure
after recovery are logged again. Successful recovery emits
`DHCP lease synchronization recovered`. Logging happens before status persistence,
so a failed status write does not hide the original transfer error.

These records use the existing service logger and its retention settings; they
do not add a log file, background collector, or status-change event. Current
resource status still changes to `Synced` on recovery. Sync cadence, retries,
timeouts and transfer behavior are unchanged. A sync failure concerns copying
state to the peer and does not by itself prove a DHCP response failure.

DHCPv4 sticky lease synchronization copies the SQLite database and WAL, but excludes the transient, machine-local `-shm` index. SQLite reconstructs that index on the receiver. Database and WAL transfer failures remain errors; this does not provide an atomic snapshot of concurrent database writes.
