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

Each script prints its temporary output directory and source/binary identity.
Successful checks remove their temporary binaries/logs after required cleanup;
failed runs retain bounded diagnostics for the unresolved failure. Use the revision-bound
[executed audit](../design/routerd15-audit.md) for observed passes and remaining
failures. A configured provider, an Applied display or a compiled test binary
does not establish communication. These isolated checks do not qualify actual
clock discipline, packaged service-manager behavior, physical failure domains or
target-storage crash durability.

## Packaged Linux service and clock discipline

Use two dedicated disposable Ubuntu VMs with an isolated link, independent
management access, chrony installed and its normal systemd service available.
The router uses `198.18.97.1/24`; the reference VM uses `198.18.97.2/24` plus
client-source aliases `198.18.97.10` and `198.18.97.20`. These addresses belong
only to this isolated link. Do not run the clock-adjusting steps on a shared
host, in a container, network namespace or FreeBSD jail.

Prepare the OS before starting the product: stop the router's clock services,
keep chrony's stock drift and `makestep` settings and `confdir /etc/chrony/conf.d`,
and remove default external pools from this fixture so the selected reference
is unambiguous. Start a separate reference chronyd with `local stratum 8`, the
isolated listen address and an allowlist for this subnet. The reference may use
`-x`; the product-managed systemd chrony service must run normally without it.
Prove the reference returns usable NTP replies before changing the router clock.

Build with `make dist`, verify the archive checksum, and install using its
installer. On the dedicated router only, introduce a measured ten-second clock
offset before starting routerd, then start it with the prepared
`tests/fixtures/linux-ntp-packaged.json`. Create its external `ntp-gate` dummy
link up beforehand. The fixture uses that link's observed phase as the server
condition; the clock source and management access use the separate real link.

Within two minutes require an acknowledged NTPServer configuration, NTPClient
Applied through the shared chrony provider, selected reference `198.18.97.2`,
chronyc tracking with normal leap status, and actual correction of the introduced
clock offset. This bound measures this prepared fixture; it is not a precision
or convergence SLO for arbitrary external time servers. The client requests
timesyncd and must report its effective shared provider as chrony, without
starting a conflicting service.

From the reference VM run the independent UDP caller:

```sh
python3 scripts/ntp-peer-query.py --server 198.18.97.1 --source 198.18.97.10
python3 scripts/ntp-peer-query.py --server 198.18.97.1 --source 198.18.97.20 --expect silence
```

Repeat the allowed probe after the denied probe so a stopped provider cannot
explain the denial. Require exact originate-timestamp correlation, a synchronized
mode-4 reply and valid stratum; the probe never adjusts a clock. Lower the dummy
gate link and require NTPServer WhenFalse and denied downstream replies while
the client retains its selected upstream. Raise it and require serving recovery.
Use the actual service manager and repeat after routerd restart. Replace the
pair with a chrony-only NTPClient, requiring removal of the server fragment and
downstream access while synchronization remains usable. Finally apply empty
intent, require both owned fragments removed, uninstall and clean up the two
VMs, disks, media and temporary package/log data.

A failure needs observed service/configuration/packet state and local
reproduction before another product run. Keep only unresolved failure data.
Success retains the procedure and scoped PR/ledger result, not a permanent
raw-output bundle. These checks do not qualify timesyncd-only service operation,
FreeBSD rc.d, target-storage crashes or physical clock precision.
