---
title: nDPI agent native package
---

# nDPI agent native package

The routerd 1.5 proposal distributes the optional native nDPI agent as the Debian
package `routerd-ndpi-agent-libndpi`. The base Linux package remains statically
linked with `CGO_ENABLED=0`. This optional agent uses `CGO_ENABLED=1 -tags libndpi`
and the target OS's shared libnDPI runtime. The release build currently targets
Ubuntu 24.04 amd64. No public 1.5 package release or apt repository is announced.

## Install and update

On a clean OS, obtain and checksum-verify matching base and addon packages:

```sh
sudo apt install ./routerd_<version>_amd64.deb \
  ./routerd-ndpi-agent-libndpi_<version>_amd64.deb
```

Use the actual filenames. The addon depends on the exact matching base version;
update them together with the same command. Native shared-library dependencies
are calculated by `dpkg-shlibdeps` from the actual ELF file and installed library
metadata. Apt resolves them through the OS repositories. Do not overlay an
archive or manually copy a binary onto a package-managed installation.

The agent still runs at `/usr/sbin/routerd-ndpi-agent`. The addon registers a
package-owned `dpkg-divert`: base-package updates write their static agent to
`/usr/sbin/routerd-ndpi-agent.routerd-static`, while the addon owns the active
path. A diversion belonging to another package or the administrator is refused,
not adopted or removed. This is normal package ownership, not migration from
source/manual installations.

The addon uses the base package's owned-service stop and restore cycle around
replacement, including the main daemon and helpers. Main otherwise reconciles a
stopped helper back into service while files are being replaced. Expect a traffic
interruption and protocol reconvergence even for addon-only changes.
First installation without a configured running router does not start services.

Verify the actual backend:

```sh
/usr/sbin/routerd-ndpi-agent selftest
sudo curl --fail --silent --unix-socket /run/routerd/ndpi-agent/default.sock \
  http://unix/v1/status
```

The native package's self-test requires `libndpiLoaded: true`. Its TLS self-test
uses the real library and reports `reason: libndpi_protocol_detection` with the
expected SNI. A running socket exists only when router configuration requests the
agent; an installed file alone does not prove runtime classification. For forwarded
client traffic, inspect `routerctl get traffic-flows -o json`: native TLS detection
has `engine: ndpi-agent`, `metadata.reason: libndpi_protocol_detection` and the
expected `tlsSNI`. Firewall event logs depend on the configured event sampling and
are not a substitute for the flow query. DNS names are reported as `dnsQuery`,
HTTP hostnames as `httpHost`, and TLS/QUIC names as `tlsSNI`; an arbitrary native
protocol hostname must not be labeled as TLS SNI.

## Remove the addon

Removing or disabling the last `FirewallEventLog` stops its managed collector.
When no enabled `TrafficFlowLog` requests application classification or TLS SNI
and no enabled `FirewallEventLog` remains, reconciliation also stops and removes
the generated classifier and agent units. Stop acknowledgement precedes unit
removal; a failed stop retains the unit for retry. Re-enabling the resources
recreates the required units using the installed package.

```sh
sudo apt remove routerd-ndpi-agent-libndpi
```

Dpkg removes the addon-owned binary, removes that package's diversion, and restores
the current base package's static agent. Active owned agent/classifier services
are restarted against that file. The router configuration and database remain.
The static agent does not load libnDPI; removing this addon therefore removes
native application classification. Do not use addon removal when native
classification is still required.

## Build and test

Build on the target Linux ABI with Go, a C compiler, `libndpi-dev`, `pkg-config`,
`dpkg-dev`, `dpkg-deb`, `file` and `readelf` available:

```sh
make dist-ndpi-agent-libndpi-deb ROUTERD_OS=linux GOARCH=amd64 \
  PACKAGE_VERSION=<native-version>
go test ./tests/nativepackage
```

The builder shares the existing nDPI payload and verifies a loaded native backend.
The release workflow checks the resulting `.deb`, real self-test and baseline
x86-64 ISA. Its definition is not proof of an executed release.

The opt-in OS gate is destructive and requires a fresh disposable systemd guest,
root and working apt repositories. Supply two base/addon pairs with increasing
versions and different daemon executables:

```sh
sudo sh scripts/linux-ndpi-deb-lifecycle-smoke.sh --isolated-vm \
  /tmp/base-prior.deb /tmp/base-current.deb \
  /tmp/addon-prior.deb /tmp/addon-current.deb
```

It checks the generated services requested by `TrafficFlowLog`, independent
UDP/TCP DNS, actual loaded native backend and TLS detection, addon-only main/DNS
process exit and recovery, matched base/addon update, package file verification,
configuration/SQLite value retention, restoration of the updated static agent
and final removal. A separate unrelated prefixed service must keep running.
Discard the guest/disks and successful run logs after recording a scoped result.
This package gate does not by itself prove packet capture/classification of
forwarded client traffic or all routerd 1.0 functionality.
