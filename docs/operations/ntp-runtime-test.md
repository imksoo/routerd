# Native NTP runtime checks

Managed NTP resources own their generated configuration fragments. Their private
receipts join the resource, provider, absolute path, random marker and exact file
digest. Pending writes, provider reloads and withdrawals remain recoverable after
a failed operation or SQLite reopen. An existing unacquired fragment or a foreign
replacement is preserved. Removing an owned fragment reloads the provider; it does
not claim exclusive ownership of the OS service.

A declared managed NTPServer owns a shared provider's configuration and service.
It includes currently enabled NTPClient upstreams. On Linux a chrony server also
owns a client requesting systemd-timesyncd, preserving the existing chrony
priority without repeatedly enabling and disabling timesyncd. The client publishes
Pending until the server's exact confirmed fragment contains its upstreams, then
reports the effective provider and requested provider separately. A false server
condition retains active client upstreams and removes downstream access. Removing
both declarations retires their public results after confirmed withdrawal.

Standalone previews report `Rendered`: they do not write provider files, run
service commands or acquire private ownership.

## Linux

Prerequisites: Go from go.mod, chronyd/chronyc, iproute2, unshare, mount, enabled
user/network/mount namespaces and a writable private temporary directory.

```sh
make check-routerd15-ntp-native-linux
```

The script builds the actual chain tests, records source hashes and invokes them
in new user, network and mount namespaces. Private mounts shadow the provider's
configuration and working directories. Foreground chronyd uses `-x`, so the trial
does not discipline the shared clock. A private independent source and UDP caller
check source selection, allowed replies, denied replies, condition withdrawal and
return, process restart, SQLite reopen and removal. The paired-client case also
checks preserved upstream selection while serving is disabled.

## FreeBSD 14.3

Use an isolated disposable VM as root. Required tools are the base ntpd/ntpq,
VNET jail/epair facilities, timeout, sha256 and a Go test binary built from the
exact source being qualified. Cross-compilation alone does not run its body.

```sh
CGO_ENABLED=0 go test -c -o /absolute/private/path/chain.test ./pkg/controller/chain
make check-routerd15-ntp-native-freebsd NTP_TEST_BINARY=/absolute/private/path/chain.test
```

The fixture creates two private VNET jails and starts owned foreground ntpd
processes. Both jails must reject an invalid-timespec clock_settime call with
EPERM before any ntpd starts. A second guard requests an unsupported, effect-free
NTP adjustment mode and requires EPERM as well. The kernel rejects clock-setting
and NTP adjustment privileges inside these jails. `disable kernel` surrounds the
unmodified generated fragment; the protocol loop stays enabled because disabling
NTP feedback also leaves ntpd replies in leap-alarm state. Shared rc.conf and rc.d
services are not changed: the provider command
adapter maps only this trial's operations to its private processes. The separate
reference's LOCAL association uses `prefer` to avoid its documented orphan
startup wait. Numeric ntpq output must identify the exact selected source, and
an independent UDP probe must confirm a usable reference reply before product
assertions begin.

The intended checks are usable NTP replies for an allowed source, silence for a
denied source, retained client upstream selection while serving is disabled,
return to serving and SQLite/process recovery. Process Wait, jail removal, epair
absence and module restoration must succeed before the final PASS marker.

## Evidence and limits

Each script prints its private evidence directory and retains source/binary
identity, operation output and failure results. Use the revision-bound
[executed audit](../design/routerd15-audit.md) for observed passes and remaining
failures. A configured provider, an Applied display or a compiled test binary
does not establish communication. These isolated checks do not qualify actual
clock discipline, packaged service-manager behavior, physical failure domains or
target-storage crash durability.

## Pending: actual FreeBSD rc.d and sysrc lifecycle

This next gate is a design, not an executed qualification. Existing packet checks
use a private provider adapter and do not establish packaged rc.d withdrawal.

The proposed gate uses private filesystem VNET jails, with base binaries/libraries
mounted read-only and private `/etc`, `/var` and `/usr/local`. It copies the VM's
unchanged `rc.subr`, `rc.d/ntpd` and default rc configuration into that private
root; actual `service ntpd` and `sysrc` run inside it. The prior `path=/` packet
jails are unsuitable because their rc.conf would be shared. No host rc.conf,
NTP service or clock is changed. Both existing effect-free jail clock guards
must return EPERM before any daemon starts. Private `ntpd_user=root` avoids a
MAC-policy module load. All mounts/jails must be recorded and removed by exact
identity, followed by independent absence checks; unknown cleanup cannot pass.

For standalone Client, standalone Server and shared Client+Server, the gate will
check initial rc values, acquisition, actual PID/socket, stop/reopen recovery,
full declaration withdrawal, prior rc-value restoration and final process/socket
absence. A receiver/kernel limitation must be distinguished from a product
withdrawal failure. Ntpd behavior after removal of its selected configuration is
not assumed; preserve command output and native observations even on failure.

Primary sources: [FreeBSD 14.3 ntpd rc.d](https://github.com/freebsd/freebsd-src/blob/releng/14.3/libexec/rc/rc.d/ntpd),
[rc.subr](https://github.com/freebsd/freebsd-src/blob/releng/14.3/libexec/rc/rc.subr),
[sysrc](https://github.com/freebsd/freebsd-src/blob/releng/14.3/usr.sbin/sysrc/sysrc).
Actual VM execution remains a separate, explicitly scoped operation.
