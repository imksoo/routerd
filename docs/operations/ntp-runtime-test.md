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

Runtime dependencies follow the managed provider: standalone Linux timesyncd
clients request `systemd-timesyncd`, chrony owners request `chrony`, and a managed
shared chrony server suppresses the conflicting timesyncd package. Unmanaged
NTP declarations do not install a provider; FreeBSD's base ntpd needs no package.
A timesyncd-only qualification must verify that applying the configuration leaves
the timesyncd package installed instead of silently replacing it with chrony.

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

Build with `make dist`, verify the archive checksum, extract it, change into
the payload directory containing `bin/`, and run `sudo ./install.sh --prefix /usr/local`.
On the dedicated router only, introduce a measured ten-second clock
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


Executed scope (2026-11-03): formal product package `1d012587`, fixture/probe
`5f7275ec`, Ubuntu 24.04 / Linux, chrony 4.5 and two dedicated PVE VMs
passed the packaged path above. The normal systemd chronyd process corrected an
introduced clock difference by -9.997554 seconds, selected the independent
stratum-8 reference and reported normal leap status. Allowed/denied/allowed
queries, WhenFalse upstream retention and downstream silence, WhenTrue recovery,
routerd restart/SQLite recovery, transition to a chrony-only client, empty-intent
fragment/source withdrawal and uninstall passed. Both VMs, disks and snippets
were deleted. The reference used `-x`; the product service did not.

NTPClient/NTPServer were Applied with no NTP controller errors. Overall status
remained Unavailable for the autogenerated ConntrackObserver because this
NTP-only guest had not loaded nf_conntrack; that unrelated observation was not
repaired or relabeled as whole-router health. This is actual relative clock
discipline and the stated native NTP path, not an external UTC-precision claim,
a timesyncd-only/package-provider replacement or FreeBSD rc.d execution.
The FreeBSD wrapper's successful-output removal change has syntax/lint checks;
its native execution was not repeated in this campaign.


## Packaged Linux timesyncd-only lifecycle

Use a dedicated Ubuntu VM with independent management access. Before qualification,
save the original time-provider package version and enabled/active state, download
its exact distro package for restoration, and certify systemd-timesyncd installation.
APT may remove a conflicting chrony package: handle that during fixture preparation.
Install checksum-verified base/addon Debian packages from `make dist-deb
 dist-ndpi-agent-libndpi-deb`; do not copy a manually built binary.

Prepare an isolated link with router address `198.19.86.1` and an independent
chronyd reference listening on `198.19.86.2` and `198.19.86.3`. Run the reference
with `-x`, local stratum 8 and an allowlist for the router. Before starting routerd,
use `scripts/ntp-peer-query.py` to require three usable, correlated replies from
each address. The reference can run in a network namespace; do not introduce a
clock offset because it shares the guest clock. This checks actual synchronization
exchanges, not external UTC accuracy or correction of an injected offset.

1. Validate and apply a Router containing only a managed NTPClient with
   `provider: systemd-timesyncd`, `source: static`, and `servers: [198.19.86.2]`.
   Use the normal packaged routerd service and `routerctl apply --timeout 90s
   -f <config> --replace`.
2. Within 120 seconds require Applied, the exact effective provider/server,
   active systemd-timesyncd, and `dpkg-query -W -f='${Status}' systemd-timesyncd`
   equal to `install ok installed`. Require `timedatectl show-timesync -p
   ServerAddress --value` to match the selected address, `NTPSynchronized=yes`,
   and `LC_ALL=C timedatectl timesync-status` to show normal leap and a positive
   packet count. Applied or a pre-existing synchronization flag alone is insufficient.
3. Change the server to `198.19.86.3`, apply, repeat those assertions, and require
   the routerd MainPID unchanged. Restart routerd through systemd and repeat the
   same synchronization assertions without another apply.
4. Apply empty intent. Require Healthy, no active resource/helper, and absence of
   the owned fragment recorded in NTPClient status. Stop the fixture services,
   remove its namespace/link/staging, and restore the original provider package
   and enabled/active state. Stop the disposable VM when finished.

On failure inspect dependency install commands, dpkg state, provider unit status,
owned fragment and reference replies before constructing a local regression.
Do not repair packages during qualification and count the repaired run as passing.

Executed on 2026-11-06 with formal product `59373f17`, package
`1.5.0~20261106212625+g59373f17`: both addresses, live change, service restart and
withdrawal passed; each synchronization observation had normal leap, stratum 8
and packet count 1. Original chrony was restored and the VM stopped. Dynamic
DHCP sources, per-interface modes and native fallback transitions remain separate
checks; the local source-loss/return regression uses a fake service executor.
