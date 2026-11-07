---
title: Development checks
---

# Development checks

![Diagram showing development checks split between local pre-commit tests, CI pull request validation, and release workflow archive publishing](/img/diagrams/operations-development.png)

routerd uses two separate automation paths.

- The CI workflow checks normal pushes and pull requests.
- The release workflow builds signed release archives after a release tag is pushed.

The release workflow is intentionally separate because it builds multiple
operating system and architecture archives and publishes GitHub Release assets.


## routerd 1.5 native package gate

The 1.5 baseline is a clean OS followed by apt/pkg installation. Updates from
previous native packages must retain operator configuration and durable state.
Source installs, `make install`, manual copies and tar-installed systems are
outside this contract. Do not add migration or adoption tests for those states.
The release workflow builds native base packages; no public apt/pkg repository
or release is announced here. Linux native libnDPI uses a matching optional addon
package; see its
[package lifecycle gate](./ndpi-agent-libndpi.md#build-and-test).

Build Linux artifacts with `make dist-deb PACKAGE_VERSION=<native-version>`.
The shared distribution inventory supplies native `/usr` files and a vendor
systemd unit. The actual `/etc/routerd/router.yaml` and state database are not
package contents. Examples live under `/usr/share/doc/routerd/examples` and are
never applied automatically. Fresh installation leaves the service inactive;
prepare a 1.5 configuration before explicitly enabling/starting it. Removal and
purge retain operator configuration and database.

The following opt-in gate requires root in a fresh, disposable systemd VM,
working apt sources, and two local packages with increasing versions and different
daemon binaries. It installs test dependencies and changes guest services. Never
run it on an operator workstation or existing router.

```sh
sudo sh scripts/linux-deb-lifecycle-smoke.sh --isolated-vm /tmp/prior.deb /tmp/current.deb
```

PASS requires inactive initial installation, package-owned CLI/binaries,
independent UDP/TCP DNS replies, actual binary replacement and old main/helper
process exit, unchanged configuration, preservation of a value in the actual
SQLite database, and removal with retained configuration/state and removed
generated units. Local `go test ./tests/nativepackage` checks payload ownership,
wrong-architecture rejection, failed-stop refusal, restoration of previously
active services and failure of a native writer without publishing partial output.
Those service tests execute the real maintainer scripts with isolated fake commands.

The VM caller discards the guest and disks after the test. Failure prints bounded
apt/journal diagnostics; reproduce and fix locally before another guest gate.
Retain procedures, assertions and scoped results instead of permanent run logs.

FreeBSD uses native `gmake dist-pkg ROUTERD_OS=freebsd` with Python, `pkg`,
a C compiler, `pkgconf`, `readelf`, `dnsmasq`, `ca_root_nss` and `ndpi` installed.
The standard FreeBSD package includes the native libnDPI agent, records the actual
ELF SONAME in `shlibs_required`, and declares the native ndpi package dependency.
The gate checks real native TLS/SNI detection before and after package update.
For a fresh dependency-resolution check, keep routerd absent and remove the build
environment's ndpi SDK/runtime package after generating the artifacts; the actual
routerd pkg installation must pull it from the OS repository. Perform that cleanup
only in the disposable build/test guest, after native unit/build checks. In a disposable FreeBSD 14 guest, provide
`dig` and Python SQLite support, then run:

```sh
sudo sh scripts/freebsd-pkg-lifecycle-smoke.sh --isolated-vm /tmp/prior.pkg /tmp/current.pkg
```

The same configuration/database, independent DNS and real executable/process
criteria apply. The gate uses `pkg -o HANDLE_RC_SCRIPTS=true`: enabled services
are started by pkg after hook descendant cleanup, then native main rc.d startup
restores owned helpers. No global pkg policy is changed. The FreeBSD gate additionally injects a failed old-service stop
and requires native pkg to reject replacement while the prior daemon still
answers DNS. It then removes the fixture fault and tests normal update/removal.
Native hook unit tests run on FreeBSD with pkg's `POSIXLY_CORRECT` environment;
the rc.d boundary test covers ownership errors and normal process exit during
pidfile lock and signal operations. An owned foreign-prefixed transient service
must remain active through the Linux update and removal gate.

## Mandatory test and harness design rules

These rules apply to unit tests, offline fixtures, runtime smoke and reusable
helpers. Keep the product requirement, reproducible test procedure and direct
pass/fail assertions. A test should answer whether the required behavior works,
not whether its diagnostic output looks complete.

- Use completed, attributable observations for product PASS/FAIL. API/SSH
  instability, incomplete commands, missing input and watchdog timeouts are
  observation or infrastructure outcomes until product behavior is directly
  established.
- Keep deadlines that are part of the product contract. Use bounded watchdogs
  only to stop hung work; do not turn them into latency requirements.
- Successful routine tests delete temporary logs, captures and generated state.
  Retain the test procedure, expected behavior and concise result, not a bundle
  of successful run output.
- When a test fails, keep only temporary output needed to reproduce and diagnose
  that unresolved failure. Fix it through a local reproduction and product/test
  changes, rerun the relevant path, then delete the temporary output when the
  failure is resolved. Never let a one-off artifact become a permanent
  acceptance condition.
- Audit the actual entry and selected source path when that affects behavior.
  Candidate or mock checks establish only their stated scope; they do not prove
  real forwarding or packaged-service behavior.
- Do not preserve an old failure as a permanent log to prove that it happened.
  Keep a regression test for repaired behavior and a concise note only when a
  future operator needs the procedure or an unresolved limitation.

These rules do not weaken functional requirements, safety, ownership or cost
limits. Missing direct observations cannot establish PASS. For example, a
failed WireGuard read does not prove that no forbidden AllowedIP exists, and a
same-stack ping does not prove tunnel forwarding.

## CI workflow

`.github/workflows/ci.yaml` runs on branch pushes and pull requests.
It uses an Ubuntu runner and checks the development surface that should stay
green before review:

```sh
go test ./...
make check-schema
make validate-example
make website-build
```

When a change touches `webconsole/`, its checked-in static assets, the shared
quality workflow, or the Makefile, CI also runs the Web Console-specific gate:
`npm ci`, high-severity audits for all and production dependencies, TypeScript
type-checking, a production build, and a generated-asset drift check.

The CI workflow does not publish release artifacts.
Release archives are created only by the `Release` workflow on date-based tags.

## Native socket checks

Normal `go test ./...` is unprivileged. The actual ICMPv6 receiver test uses the
`routerd_native` build tag and must run separately with raw-socket privileges.
CI runs it in an isolated Linux network namespace; the FreeBSD native VM gate
also runs it explicitly. Enabling the tag retains permission failures as failures.

In a disposable Linux VM, compile as the normal user and execute only that test
with privilege:

```sh
go test -tags routerd_native -c -o /tmp/routeradvertisement-native.test ./pkg/routeradvertisement
sudo unshare --net sh -ec 'ip link set lo up; exec /tmp/routeradvertisement-native.test -test.run="^TestSolicitationWildcardReceiverOnActualSocket$" -test.v'
```

On a disposable FreeBSD VM, build with the same tag and execute the test binary
as root. This checks actual kernel reception and packet metadata; it does not
prove forwarding or peer interoperability. Remove the binary when finished.

## Native helper ownership and service capabilities

The Linux service needs `CAP_SYS_PTRACE` to read the exact procfs identity of
its dnsmasq child after dnsmasq drops UID, and `CAP_KILL` to signal that verified
child through a pidfd. The packaged unit and runtime renderer carry the same
capabilities. Do not weaken native identity checks or keep dnsmasq running as
root to hide a permission failure. The procfs access checks are described in
[ptrace(2)](https://man7.org/linux/man-pages/man2/ptrace.2.html).

Compile without privilege, then run the explicit native test in new PID/mount
namespaces. It drops the observer to the actual service bounding set, observes
a child under another UID and confirms its termination through the production
owner. Failure to observe or signal is a failed gate. CI runs the same test.

```sh
go test -tags routerd_native -c -o /tmp/chain-native.test ./pkg/controller/chain
sudo unshare --mount --pid --fork --mount-proc /tmp/chain-native.test -test.run="^TestDnsmasqOwnerWithServiceCapabilities$" -test.v
```

This proves the native privilege boundary. Actual DHCP acquisition, renewal,
DNS requests and service restart still require the isolated VM/client tests.
Remove the test binary after use.

## Linux system policy native gate

Run `make check-routerd15-system-native-linux` on Linux with user namespaces,
`unshare`, `mount`, `hostname` and `sysctl` available. Keep the Go toolchain and
system utilities in `PATH`; the check does not require host root privileges.

The target creates private UTS, network, user and mount namespaces, mounts a
private sysfs, and runs the production Hostname and Sysctl controllers. It checks
real hostname changes, a SQLite reopen, management release, a real forwarding
sysctl change, the private persistent fragment and a converged second reconcile.
The outer hostname and forwarding value must remain unchanged. Persistent files
are private test files, not the host's `/etc/hostname` or `/etc/sysctl.d`.

This is a Linux kernel/controller check. It does not qualify boot-time loading,
package installation, all SysctlProfile values or another operating system.

## Linux DHCPv6 server IA_NA check

Run `sudo ./tests/netns/dhcpv6-stateful.sh` on an isolated Linux host with
iproute2, dnsmasq, dhcpcd, Go and network-namespace support. The script renders
the current `DHCPv6Server` configuration, starts the rendered dnsmasq service
and uses dhcpcd as an independent client in private namespaces. Both
`stateful` and `both` modes must complete Solicit/Advertise/Request/Reply and
assign an address from the configured pool. Success prints those two results
and deletes temporary namespace state and logs. The test does not start an RA
service, so it does not use a ping or claim to qualify default-route or
forwarding behavior; those are covered by separate RA/client lifecycle gates.

## Linux DHCPv4 reservation and dynamic pool check

Run `sudo ./tests/netns/dhcpv4-reservation.sh` on an isolated Linux host with
iproute2, dnsmasq, BusyBox and Go. The test renders the current 1.5 DHCPv4
configuration, starts the actual dnsmasq process in a private router namespace,
and uses two independent BusyBox clients. The reserved client must receive its
configured address; the other must receive an address in the dynamic pool. Both
must install the configured router and DNS options and renew the same address
after dnsmasq restarts with its lease file. The renewal wait is a bounded
watchdog; it is not a latency target. Successful temporary state and logs are
deleted. A failure retains its current temporary directory for local diagnosis.
This check does not exercise package installation, service-manager supervision,
lease expiry, address-conflict handling, DNS answers, NAT, or FreeBSD behavior.

## Linux DHCPv4 relay packet check

Run `sudo ./tests/netns/dhcpv4-relay.sh` on an isolated Linux host with
iproute2, dnsmasq, BusyBox `udhcpc`, tcpdump, Go and network-namespace support.
The test renders the current DHCPv4 relay configuration, starts real dnsmasq
relay and upstream-server processes in separate namespaces, and uses independent
BusyBox clients on two separate LAN subnets served by one DHCPv4Relay resource.
PASS requires each client to receive an address from its own upstream pool with
its subnet-specific gateway and DNS server and the configured server identifier; no lease while the
upstream server is stopped; a lease after server recovery; no lease while the
relay is withdrawn; and a lease after the relay restarts. Successful runs print
one result and delete their temporary state and packet captures. A failed run
retains its bounded temporary diagnostics for local reproduction. The test
checks the renderer and dnsmasq packet path, not routerd service supervision,
firewall policy, package installation, reboot recovery or FreeBSD behavior.

For a test VM without Go, build the renderer on a Go-enabled build host and
provide that static binary to the VM alongside the test script:

```sh
CGO_ENABLED=0 go build -o /tmp/routerd-dhcpv4-relay-render ./tests/netns/dhcpv4-relay-render
sudo env ROUTERD_DHCPV4_RELAY_RENDERER=/tmp/routerd-dhcpv4-relay-render \
  ./tests/netns/dhcpv4-relay.sh
```

## Linux DHCPv4 relay daemon and systemd check

For the full product path, extract the Linux `make dist` package built from the
candidate revision, then run this on a disposable Ubuntu VM with systemd,
iproute2, dnsmasq, BusyBox `udhcpc`, and network-namespace support:

```sh
sudo scripts/linux-dhcpv4-relay-daemon-smoke.sh --disposable-vm \
  /path/to/extracted/linux-amd64-package
```

The script validates a fresh routerd 1.5 relay configuration with that package's
binary, then starts its actual daemon and systemd-managed dnsmasq against an
independent upstream server and client. PASS requires the configured lease and
options, no lease during upstream loss or relay withdrawal, recovery after the
upstream returns, successful relay reapplication, and a lease after restarting
the managed dnsmasq service. It then changes the externally owned LAN address
without adding a static-address declaration, requires the generated relay source
and owned dnsmasq PID to change, and requires another independent lease with the
upstream server's options. A changed file alone is not process readiness.
Cleanup waits for product-owned unit and PID withdrawal before removing fixture
resources. The entire run is bounded to 180 seconds. It refuses
existing routerd state or services and does not import or upgrade 1.0
configuration or state. Success removes temporary state and logs; failure keeps
only the bounded temporary diagnostics for reproduction. This checks the relay
controller and dnsmasq service lifecycle, not package installation, routerd
boot-service installation, firewall policy, reboot recovery, or FreeBSD.

## Linux DNS resolver runtime check

UDP response writes share one socket deadline. Concurrent replies wait within the
existing two-second write budget, including lock wait and actual send; brief
writer overlap does not immediately discard a reply. The existing 128-query
admission bound remains. Run `go test ./cmd/routerd-dns-resolver -run TestUDPReply`
to check real UDP delivery during overlap and expiry when the writer stays busy.

Run `sudo ./tests/netns/dns-resolver-runtime.sh` on an isolated Linux host with
iproute2, dnsmasq, `dig`, curl and Go. The test starts the actual
`routerd-dns-resolver` binary with a prepared 1.5 runtime configuration, then
queries it from an independent client. A local-zone answer and conditional
upstream answers must work over UDP and TCP. After changing the prepared zone
and sending SIGHUP, the new answer must appear; after stopping and restarting
the worker, local and TCP-forwarded answers must recover. Successful temporary
state and logs are deleted; a failed run retains its temporary directory for
diagnosis. This check covers the resolver process only, not the routerd
controller or service manager. DHCP-derived lease changes/tombstones, negative
replies, DoT/DoH, controller-driven configuration changes and FreeBSD runtime
remain outside this check.

## Linux PortForward DNAT and hairpin check

Run `sudo ./tests/netns/nat44-portforward.sh` on an isolated Linux host with
iproute2, nftables, curl, Python 3, Go and network-namespace support. The test
validates a prepared routerd 1.5 YAML config and runs the production NAT44
controller inside a private router namespace. Independent WAN, LAN server and
LAN client namespaces check WAN DNAT, LAN hairpin source translation and
direct-LAN source preservation. Withdrawing the rule must reset the established
WAN TCP flow, remove the routerd NAT table and prevent new WAN flows from
reaching the backend. Successful temporary state and logs are deleted; failures
retain the temporary directory for diagnosis. This is not a 1.0 configuration
or state import, packaged install, service-manager test, or qualification of
pinholes, session synchronization, multi-backend ingress or FreeBSD.

## Linux NAT44 source SNAT selector check

Run `sudo ./tests/netns/nat44-source-snat.sh` on an isolated Linux host with
iproute2, nftables, curl, Python 3, Go and network-namespace support. A prepared
routerd 1.5 configuration is validated before the production NAT44 controller
applies real nftables rules in a private router namespace. An independent WAN
HTTP peer observes the translated source. Only the selected source and
destination CIDRs may use the configured SNAT address; an excluded destination
and a client outside the source range must preserve their original addresses.
The translation-shaped `outboundInterface/sourceCIDRs/translation` rule must also
translate the address and constrain the TCP source port to its declared range.
The pool stage uses three noncontiguous addresses and verifies that all listed
addresses forward traffic, no unlisted address appears, and the port range holds.
Pool entries are individual addresses, not endpoints of an inclusive range.
Linux selects new flows round-robin from the deduplicated address list; conntrack
retains the translation of an established flow.
Set `ROUTERD_TEST_BINARY` to a formal distribution binary when running on a guest.
`ROUTERD_NAT44_TEST_DRIVER` may point to a prebuilt
`tests/netns/nat44-portforward-runtime` test executable so the guest needs no Go toolchain.
Updating `snatAddressFrom` must affect new connections, and withdrawing the rule
must remove the NAT table and restore the original source. Successful temporary
output is deleted; failures retain only the current diagnostic directory.

## Linux LocalServiceRedirect DNS interception check

Run `sudo ./tests/netns/local-service-redirect.sh` on an isolated Linux host
with iproute2, nftables, dnsmasq, BIND `dig`, Python 3, Go and IPv6 support.
The test validates a prepared routerd 1.5 config, then runs the production NAT
controller with real nftables in a private router namespace. Independent LAN
clients and a WAN-side DNS server check that the configured destination set,
port and ingress interface select local DNS for IPv4/IPv6 TCP/UDP queries; an
unselected address, port or interface must continue to the upstream. Changing
the selected LAN interface must move the redirect, and withdrawing it must
restore upstream answers and remove both NAT tables. This starts with a 1.5
config and fresh state; it does not import 1.0 config or state. Successful
temporary output is deleted; failures retain only the current diagnostic
directory for reproduction.

## Linux packaged DHCPv6 server lifecycle VM gate

Build the candidate with `make dist`, extract its Linux archive on a dedicated,
disposable systemd Linux VM, then run the packaged-daemon check:

```sh
sudo scripts/linux-dhcpv6-server-lifecycle-smoke.sh --disposable-vm \
  /path/to/extracted/package
```

The extracted package root must contain `bin/routerd` and `bin/routerctl`. The
VM must already have a healthy `routerd.service` with no active resources, an
installed `/usr/local/sbin/routerctl`, and `ip`, `jq`, `dhcpcd`, `tcpdump`,
`timeout`, `journalctl` and systemd tools. The check validates its temporary
routerd 1.5 configuration, then runs the candidate package's `routerd serve`,
its systemd-managed dnsmasq and an independent dhcpcd client over a private veth
and client namespace. The client wait is bounded at 150 seconds and renewal
observation at 90 seconds. PASS requires an IA_NA address from the configured
pool, completed client DAD, a DHCPv6 renew reply with the same address, an RA
Managed flag and an RA-installed default route. The existing router config and
state are left untouched; the baseline service is restarted after the candidate
exits. Successful runs delete their temporary output. On failure, the script
stops its candidate and peers, restarts the baseline service and retains only
the current temporary fixture path for local reproduction. This gate does not
establish DNS responses, IPv4 NAT, forwarding, reboot recovery, clean package
installation or complete router qualification.

## Pre-commit hook

The repository includes an optional pre-commit hook script:

```sh
ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x scripts/pre-commit.sh
```

After enabling it, `git commit` runs:

```sh
go test ./...
make check-schema
```

If either command fails, the commit is stopped.
This catches schema drift and test failures before they reach CI.

For an emergency local commit, set this environment variable:

```sh
ROUTERD_SKIP_PRE_COMMIT=1 git commit
```

Use that only when the follow-up fix is already clear.
CI still runs after the branch is pushed.

## Linux DHCPv4 receive checksum offload regression

The Linux packet socket must consume `PACKET_AUXDATA`: virtual links can deliver
an OFFER with `TP_STATUS_CSUMNOTREADY` before UDP checksum completion. Such a
kernel-marked packet is not a malformed wire reply. Ordinary frames still need
UDP checksum validation; IP integrity, bounds, fragmentation and UDP ports remain
checked in both paths. A capture showing a bad checksum alone cannot distinguish
this case from corruption; inspect the kernel metadata.

The native regression sends an independent UDP OFFER across a veth, requires the
kernel's pending-checksum marker, and checks delivery through the production
transport. Ordinary unit tests skip this case. From the repository root:

```sh
offload_dir=$(mktemp -d /tmp/routerd-dhcp-offload.XXXXXX)
go test -c -o "$offload_dir/dhcp.test" ./cmd/routerd-dhcpv4-client
sudo unshare --net --mount bash -c '
  set -eu
  mount --make-rprivate /
  mkdir -p /run/netns
  mount -t tmpfs tmpfs /run/netns
  ROUTERD_DHCPV4_OFFLOAD_TEST=1 "$1" -test.run=^TestLinuxReceivesChecksumOffloadedOffer$ -test.v -test.timeout=30s
' sh "$offload_dir/dhcp.test"
rm -r -- "$offload_dir"
```

Require the pending-checksum observation and PASS, not SKIP. The explicit opt-in
also refuses the initial host network namespace. This is a real-kernel transport
regression, not a complete DHCP exchange, a VM reboot verdict, or FreeBSD proof.

## Linux WAN DHCPv4 lifecycle VM gate

`scripts/linux-wan-dhcpv4-lifecycle-smoke.sh` tests the installed `make dist`
package in a dedicated disposable Linux VM with an empty router configuration
and independent management access. It replaces that guest's configuration and
changes its test interfaces and IPv4 forwarding. Prepare iproute2, Python 3,
curl, jq, dig, tcpdump, dnsmasq and Kea DHCPv4 with its libraries. The guest must
use systemd-resolved's stub resolv.conf; another Kea instance or its `/run/kea`
directory is a collision. The fixture uses Kea 2.4.1 for Ubuntu 24.04.

```sh
python3 scripts/verify-dhcpv4-rebind_test.py
sudo bash scripts/linux-wan-dhcpv4-lifecycle-smoke.sh --disposable-vm \
  "$PWD/tests/fixtures/linux-wan-dhcpv4-lifecycle.json" /opt/routerd-wan-peer
```

The package root contains `usr/sbin/kea-dhcp4` and
`usr/lib/x86_64-linux-gnu`. The upstream and LAN clients are separate namespaces
inside the VM, not separate physical machines. Kea supplies a single IPv4 lease
with T1/T2/expiry at 20/40/90 seconds. Pass requires the actual leased address,
metric-qualified default route, per-link resolved DNS/domain, LAN DNS over
UDP/TCP and an independent HTTP peer observing the leased NAT source.

After a successful renewal, the fixture stops Kea until the next T1 attempt and
restarts it before T2. The temporary capture must contain a unicast Renew,
broadcast Rebind with nonzero ciaddr, and ACK correlated by transaction ID and
client MAC. The lease epoch must advance. This distinguishes actual
[RFC 2131 rebind](https://www.rfc-editor.org/rfc/rfc2131#section-4.4.5) from a
fresh acquisition. A second outage must remove address, route, DNS/domain and
forwarding at real lease expiry; returning Kea must restore them without a
product restart. Empty apply must then become Healthy with no stale errors.
The capture is stopped only after the saved file contains the required exchange;
daemon acknowledgement alone does not prove libpcap has flushed its kernel buffer.

Successful raw captures and outputs are deleted. A failed stage stops the
product and peers and retains only the current fixture for local reproduction;
repair and verify locally before another native run. Successful completion
removes the namespaces and restores the original forwarding sysctl. This gate
does not qualify physical WAN devices, other DHCP servers, a reboot, or FreeBSD.

## Linux home-router clean-install lifecycle gate

Run `scripts/linux-home-router-lifecycle-smoke.sh --disposable-vm` only on a
dedicated Linux VM with independent management access. The command takes a
prepared routerd 1.5 JSON config, an extracted Linux `make dist` package root,
and an extracted Kea/ndisc6 package root. The VM must have `ip`, `ss`, `nft`,
systemd and active `systemd-resolved` with `/etc/resolv.conf` pointing to its
stub resolver, `resolvectl`, `dnsmasq`, BusyBox, `dhcpcd`, `curl`, `dig`, `jq`,
Python 3 and `timeout` installed, with `/proc/sys/net/netfilter/nf_conntrack_count`
available.

When the management NIC also has a DHCP default route, set its route metric in
the guest network manager above the fixture WAN's 4242 (for example, 65000).
Keep the management subnet reachable. The gate checks that the test destination
selects the isolated WAN after DHCP acquisition, before starting client traffic;
a management default winning instead is a fixture setup error.

The final argument is the VM's independent management interface. The fixture has
a `management` interface and trust zone; the script substitutes this interface
name before validating the effective routerd 1.5 config. This keeps the VM's
management connection allowed by the input firewall while WAN/LAN behavior is
tested. The gate refuses to start if routerd binaries, canonical config,
`/var/lib/routerd`, the routerd service, or its test interfaces/namespaces already
exist. It validates the prepared config with that package's own binary, installs
the package, starts the real systemd service, and tests that same config. It imports
no old config or database and has no routerd 1.0 upgrade step. After a successful
run it uninstalls the package and removes its state and temporary output. On
failure, it first applies an empty config to withdraw routerd-managed state, then
removes the fixture and retains only bounded diagnostics and the config for local reproduction.

```sh
sudo scripts/linux-home-router-lifecycle-smoke.sh --disposable-vm \
  tests/fixtures/linux-home-router-lifecycle.json \
  /path/to/extracted/routerd-package /opt/routerd15-fixtures/kea eth0
```

The Kea package root must contain `usr/sbin/kea-dhcp4`, `usr/sbin/kea-dhcp6`,
`usr/bin/rdisc6` and `usr/lib/x86_64-linux-gnu`; keep `rdisc6` root-owned.
Ubuntu's installed Kea binaries can have AppArmor profiles that reject the
fixture's temporary configuration paths. An existing file reported as
`Unable to open file` needs a kernel audit check for an AppArmor denial.
Use matching packages extracted as root into the dedicated fixture directory,
and verify their configuration loading in the intended peer namespace during
environment preparation, before installing or testing routerd. The
single config exercises WAN DHCPv4 acquisition, natural T1 renewal and DNS/route
withdrawal, LAN DHCPv4 acquisition and natural renewal, DHCPv6 IA_NA assignment
from the active
delegated LAN prefix, DNS A/AAAA over UDP/TCP from IPv4 and IPv6 clients,
firewall-denied unsolicited WAN-to-LAN traffic, IPv4 NAT with the translated
source observed by an independent peer, DHCPv6-PD renewal and renumbering,
RA/SLAAC, routed IPv6 with the delegated client source, service restart, WAN
loss/return, PD expiry/reacquisition and managed withdrawal. This is the
integrated Linux home-router gate; it does not establish physical ISP
interoperability, WAN discovery/PPPoE/DS-Lite, reboot recovery, FreeBSD parity or
the complete routerd 1.0 function inventory.

The upstream IPv6 default route is supplied by the disposable WAN fixture, as
it would be learned from an upstream router. The fixture gives that default
metric 50 so a management NIC's metric-100 RA default does not divert the test
traffic. Before product installation it requires the test destination to select
the isolated WAN, and cleanup removes that exact fixture route. This gate exercises IPv6 forwarding
through that route; it does not claim to qualify `IPv6StaticRoute` rendering or
WAN router-advertisement discovery.

## Linux delegated-source DS-Lite package gate

`scripts/linux-dslite-pd-smoke.sh` extends independent PD/DS-Lite checks to the
four-source deployment path: one real Kea /60, four managed WAN /128 addresses
shared by four DS-Lite tunnels, delegated LAN addressing, source-hash primary
routing and lower-weight fallback. It uses the apt-installed service and native
paths under `/usr`, `/etc/routerd` and `/var/lib/routerd`. No old-install adoption
or configuration migration is involved.

Use a clean dedicated Ubuntu VM with independent management access. Install the
formal `.deb` with apt but do not start or configure it. Complete OS preparation
first and install iproute2, nftables, jq, curl, Python 3, conntrack, tcpdump and
`kea-common`. Extract a matching `kea-dhcp6-server` package into a root-owned
fixture directory. Its private executable avoids the installed server profile's
restrictions on temporary fixture configuration; it is an independent peer, not
an alternate product binary. Verify `ldd` has no missing libraries and `kea-dhcp6
-v` succeeds. The local peer preparation uses real DHCP acquisition before the
VM campaign; the VM gate itself checks the peer configuration/listener, IPv6
underlay, DAD and independent HTTP listeners before starting routerd.

```sh
sudo bash scripts/linux-dslite-pd-smoke.sh --disposable-vm \
  tests/fixtures/linux-dslite-pd.json /opt/routerd15-fixtures/kea eth0
```

Fresh install may already have scheduled a main-unit restart. The gate observes
native timer/service completion and rechecks readiness before entering the
steady-state renumber test; it does not sleep an arbitrary fixed settling delay
or treat an initial handoff as a DHCP-triggered restart.

Pass requires actual delegated /128 sources with finite native lifetimes,
matching kernel tunnel endpoints and 64-source stable forwarding observed by
independent AFTRs. Natural Renew must advance the lease transaction. Changing
only Kea's pool and independent AFTR return endpoints must replace the four
sources and remove the revoked prefix without restarting routerd or Kea.
Restart must recover the new delegated paths. Stopping only Kea must naturally
expire the lease and remove all four sources/tunnels, the LAN address and client
forwarding. Restarting only that peer must recover forwarding without product
repair. Empty apply must withdraw sources/tunnels and converge to Healthy with
zero resources and no current controller errors.

A removed tunnel declaration must not be recreated as a Disabled display row
by native withdrawal. Public status is deleted after actual native absence and
before releasing the owner receipt; a failed deletion keeps durable retry
input. A declaration retained but conditionally inactive still keeps its status.
The zero resource count may be omitted by the control API's existing JSON shape;
the gate treats that omitted numeric zero consistently with the schema.

The link observer similarly retires only Interface observation rows absent from
the full declaration. It retains conditionally inactive declarations and other
APIs/resource kinds, propagates enumeration/deletion errors for retry, and never
changes native interfaces while removing stale display state.

The two peer prefixes are documentation addresses. AFTR return endpoints are
prepared for the replacement prefix before Kea advertises it; this changes the
independent network, never product state. Successful raw output and namespace
fixtures are removed. Failures retain bounded capture/status output for local
reproduction; stop the run before debugging and remove the disposable VM when
finished. This gate does not prove ISP AFTR/DNS discovery, BGP/HA or lease/session
synchronization. The maintained procedure is not a pass until its executed
product/harness revisions and outcome are recorded in the qualification ledger.

## Linux multi-path DS-Lite package gate

Deferred main-service handoffs use a timer with
[`StopPropagatedFrom=routerd.service`](https://www.freedesktop.org/software/systemd/man/249/systemd.unit.html#PropagatesStopTo=).
An operator stop must cancel that handoff, while a normal self-restart must not
restart its own timer. Do not order the timer after the notify service: queuing
must finish while that service is still activating. This requires systemd 249
or later, declared by the native Debian package.

The opt-in test below uses uniquely named dummy services and temporary runtime
unit files in the ordinary user's manager. It reproduces actual resurrection,
then checks persistent stop, a single successful restart and scheduling during
notify activation. It never targets system services or host networking:

```sh
ROUTERD_RUN_USER_SYSTEMD=1 go test ./pkg/controller/chain \
  -run '^TestSelfRestartUserSystemdLifecycle$' -count=1 -v
```

The main service's Package controller needs `CAP_FOWNER` for apt's downloader-owned
cache directory modes and `CAP_DAC_OVERRIDE` to move downloads out of private
cache directories. UID 0 under a restricted capability bounding set is
insufficient. The runtime renderer and packaged vendor unit must agree.
`TestRouterdPackageCachePermissionsNative` reproduces the actual chmod/rename
operations in temporary directories under the rendered capability set. It is an
explicit root filesystem-only check, not a package installation or network test:

```sh
go test -c -o /tmp/routerd-package-caps.test ./pkg/render
sudo env ROUTERD_RUN_PACKAGE_CAPS=1 /tmp/routerd-package-caps.test \
  -test.run '^TestRouterdPackageCachePermissionsNative$' -test.v
```

The existing-flow regression uses the real production nft renderers and one
persistent UDP socket across state changes. It requires Linux, root, `unshare`,
`nsenter`, iproute2, nftables, conntrack and Python 3. It refuses an unverified
outer namespace and never registers namespaces under the host's `/run/netns`.
All networking changes disappear with the isolated namespaces:

```sh
work=$(mktemp -d)
CGO_ENABLED=0 go test -c -o "$work/render.test" ./pkg/render
sudo env ROUTERD_RUN_NETNS=1 ROUTERD_HOST_NETNS="$(readlink /proc/self/ns/net)" \
  unshare -n "$work/render.test" -test.run '^TestEgressExistingFlowNative$' \
  -test.v -test.timeout=45s
rm "$work/render.test"
rmdir "$work"
```

Pass requires the same UDP tuple to leave a failed member's conntrack mark,
retain its new healthy mark, move to fallback when all primary members fail,
and remain on healthy fallback when primary members return. Normal unit tests
also check that unhealthy or absent interfaces cannot enter the restore set and
that filtering does not mutate desired configuration. This is a real-kernel mark
regression, not proof of remote TCP-session continuity across ISP NAT changes.

`scripts/linux-multi-dslite-smoke.sh` tests a clean apt-installed product on a
dedicated disposable Ubuntu VM. It creates four independent kernel AFTR
namespaces with HTTP receivers and a separate LAN client. Three primary paths
use source-address hashing; a lower-weight fourth path provides fallback.
The fixture uses documentation/benchmark addresses, not deployed configuration. After updating to
the candidate package, the gate also sends 64 fixed UDP tuples through real
DS-Lite/NAT peers. Healthy flows must keep their native conntrack IDs and peer
paths. Linux masquerade removes a flow and drops its first packet when its
output interface changes; affected tuples must recover with at most one client
retransmission. Receivers must observe surviving primary paths, fallback, then
continued healthy fallback respectively. UDP peers are positively checked during
environment certification. These are actual forwarded UDP results, not a promise
that remote TCP sessions survive ISP NAT.

The kernel behavior is directly reproducible, without routerd or product-state
repairs, using the same UDP socket across two masqueraded output interfaces:

```sh
sudo env ROUTERD_RUN_NETNS=1 ROUTERD_HOST_NETNS="$(readlink /proc/self/ns/net)" \
  unshare -n python3 tests/netns/masquerade-oif.py
```

The reference requires an observed deletion before retransmission and successful
peer response with the replacement NAT source. This matches Linux 6.8's
[nf_nat_oif_changed](https://github.com/torvalds/linux/blob/v6.8/include/net/netfilter/nf_nat.h#L51-L63)
and [drop/removal path](https://github.com/torvalds/linux/blob/v6.8/net/netfilter/nf_nat_core.c#L802-L809).
Requiring every masqueraded flow to retain its original ID through interface
changes is an invalid test assumption. ID preservation remains required for
unaffected paths and the healthy fallback after primary recovery.


Before qualification, finish OS initialization/reboot and install `iproute2`,
`nftables`, `jq`, `curl`, `python3`, `conntrack`, `tcpdump` and `dnsmasq-base`.
Use a management NIC independent of the test interfaces. The guest must have no
routerd installation/configuration/database or colliding fixture interfaces.
Build two genuine Linux candidate `.deb` revisions through `make dist-deb`, with
increasing package versions. Do not copy standalone development binaries into
the guest. Validate the fixture locally and check the shell syntax first:

```sh
go run ./cmd/routerd validate --config tests/fixtures/linux-multi-dslite.json
bash -n scripts/linux-multi-dslite-smoke.sh
# Transfer the script, fixture and both packages to the disposable guest.
sudo bash scripts/linux-multi-dslite-smoke.sh --disposable-vm \
  tests/fixtures/linux-multi-dslite.json /tmp/prior.deb /tmp/current.deb eth0
```

The script certifies the IPv6 links and independent HTTP listeners before
installing routerd. Health probes must bind to their actual routing target.
Before testing a changed path set, it observes the native nft marking rules;
health publication alone does not acknowledge route reconciliation. The
90-second observation bounds are test timeouts, not a product failover SLA.

Pass requires all 64 source addresses to retain their selected AFTR across new
connections, all three primary AFTRs to receive traffic, and each independent
receiver to observe the translated inner source. Failure of one primary must
remove it from new-flow selection. Failure of all primaries must select the
fourth AFTR; restoration must restore the primary set. Actual systemd restart
and apt update must retain the configuration, replace old processes, preserve
package integrity and restore traffic without product-state repair. Empty
configuration must remove the tunnels and stop that forwarded traffic.
After withdrawal, a local-only Telemetry intent changes the actual main unit.
The gate then observes a pending restart timer, checks its stop propagation,
stops main and requires it to remain inactive past the ten-second handoff delay.
This intent is a restart trigger, not a collector delivery qualification.

The caller discards the VM and its disks afterwards. Successful captures/logs
are removed; a failure retains bounded diagnostics and two one-megabyte outer
packet captures for local reproduction before another qualification run. This
gate does not qualify delegated/RA source expiry, real ISP AFTRs, host reboot,
DHCP/DNS, BGP, HA/session synchronization or complete deployed-router replacement.

## Linux PD/RA lifecycle VM gate

`scripts/linux-pd-ra-lifecycle-smoke.sh` exercises an installed `make dist`
package through its actual systemd service. Use a dedicated, disposable Linux VM
with an empty router configuration, independent management access, and no
production traffic. The explicit `--disposable-vm` argument is required. It
replaces that guest's configuration and changes its test interfaces and IPv6
forwarding; it is not part of ordinary unit tests.

Certify the guest before running: systemd must be healthy; install iproute2,
Python 3, curl, jq, dig and dnsmasq. Prepare Kea DHCPv6 and ndisc6 from packages
matching the guest, with their shared libraries. Either use an installed package
root or extract the packages as root into a private directory. In particular,
`rdisc6` must remain root-owned: extracting its setuid executable as an ordinary
user changes its privilege behavior. Check Kea's shared libraries with `ldd`.
The tested peer was Kea 2.4.1 on Ubuntu 24.04.4. Its transient unit owns `/run/kea`;
an existing Kea service/runtime directory is a collision, not permission to stop it.

```sh
sudo scripts/linux-pd-ra-lifecycle-smoke.sh --disposable-vm \
  "$PWD/tests/fixtures/linux-pd-ra-lifecycle.json" /opt/routerd-pd-peer
```

The package root above contains `usr/sbin/kea-dhcp6`, `usr/bin/rdisc6` and
`usr/lib/x86_64-linux-gnu`; use `/` only when the prerequisites are already
installed and no Kea instance owns its runtime directory. See the
[Kea DHCPv6 configuration reference](https://kea.readthedocs.io/en/kea-2.4.1/arm/dhcp6-srv.html)
for the independent PD pool and lease timer semantics.

The test uses separate upstream and client namespaces. Kea delegates a /60 with
20/40-second T1/T2 and 60/90-second preferred/valid lifetimes. Linux processes
real RA packets to create its SLAAC address and default route; `rdisc6` separately
checks the PIO. Pass requires local and forwarded AAAA answers over UDP/TCP,
an HTTP peer observing the client's delegated source, actual Renew/Reply with
an advanced lease timestamp, and an independent server pool change from
`2001:db8:1500::/60` to `2001:db8:1501::/60`. Kea reloads on SIGHUP; the product
configuration remains unchanged. Require the same router and Kea PIDs, a new
client source and AAAA answer, the new PIO, removal of the revoked router
address, and failure of DNS queries to the old endpoint. Any remaining old
SLAAC client address must be deprecated; its short original valid lifetime can
remain under RFC 4862's two-hour rule.

Then require service restart and upstream-stop expiry removing the router
address and client default route. Traffic must fail while the product service
remains active. Restart the independent server and require automatic PD,
SLAAC/default-route, DNS and forwarding recovery. Stop the upstream again and
wait for expiry before applying the empty configuration. Without restarting
routerd, require Healthy with zero resources, no resource phase issues and no
current controller errors. Removed PD/DNS rows must disappear after the owner
acknowledges shutdown; failed native withdrawal receipts must remain visible.
Peer shutdown, namespace removal and restoration of the original forwarding
setting are also required.

DAD is asynchronous: a newly installed delegated address holds RA in Pending
without failing configuration apply. Positive advertisement resumes only after
address readiness; failed DAD and unknown ownership remain errors. DNSForwarder
rules are evaluated in configuration order, so the fixture's local zone precedes
the default upstream rule.

Successful raw output is deleted automatically; retain the revision and verdict,
this procedure, its fixture and the regression tests. Failures stop the product
and peers and retain the exact fixture path for diagnosis. Reproduce a product
failure locally before another native run, and remove retained output after the
failure is resolved. WAN transit and its return route are fixture scaffolding:
this gate does not qualify WAN RA discovery, DHCPv6 IA_NA, overlapping multi-prefix renumbering,
reboot/crash recovery, firewall isolation, FreeBSD or all routerd 1.0 functions.

## Linux WAN RA/SLAAC lifecycle gate

Use `scripts/linux-wan-ra-lifecycle-smoke.sh --disposable-vm` only in a dedicated,
empty Linux VM with an installed `make dist` package and independent management
access. The canonical config must contain an empty Router resource list in JSON.
Certify dnsmasq, Python 3, curl, jq, iproute2 and systemd-networkd first; the
independent `rdisc6` decoder must exist at
`/opt/routerd15-fixtures/kea/usr/bin/rdisc6`. No package installation or repair
occurs during this gate. An existing `rd15-ra` interface, `rd15-ra-upstream`
namespace or matching networkd/sysctl artifact is a collision and aborts setup.

The fixture gives networkd a matching WAN `.network` file with RA disabled.
A separate namespace advertises a /64 using dnsmasq RA-only mode, a 15-second
router lifetime and 120-second preferred/valid prefix lifetimes. The HTTP peer
lives on a different subnet, requiring the learned default route. Before the
product apply, decode the RA and verify the independent HTTP service locally.

Pass requires all of the following:

- Fresh `IPv6RAAddress.managed: false` leaves acceptance disabled and creates no
  networkd adoption; no default route is acquired.
- Switching to managed mode enables reception without manually assigning a
  WAN global address/default route. Require kernel SLAAC, a `proto ra` default,
  and an HTTP peer observing the SLAAC source.
- Restart the service, then separately SIGKILL its main process. Require a new
  Healthy daemon with the same two declared resources, unchanged canonical
  configuration and peer processes, and successful HTTP after each recovery.
  The SIGKILL path must recover through exactly one systemd automatic restart;
  do not manually start the service or reapply its configuration.
- Freeze only the RA sender using SIGSTOP so it cannot send a shutdown RA.
  After the router lifetime, route lookup and HTTP must fail. Linux can retain
  an expired row until garbage collection: an `expires -Nsec` row alone is not
  forwarding authority. The kernel's
  [IPv6 route selection](https://github.com/torvalds/linux/blob/v6.8/net/ipv6/route.c)
  rejects expired entries. Require natural SLAAC address removal after its
  valid lifetime as well.
- Resume only the peer. Require address/default-route acquisition and HTTP
  recovery with the same routerd PID and no controller errors.

The script finally reapplies empty intent and removes its own networkd/sysctl
fixtures and interfaces. That explicit fixture teardown is not evidence of
automatic rollback when managed intent is removed. Failed gates stop routerd
and peers and preserve diagnostic files; investigate and reproduce locally
before retrying. Successful raw output is deleted. Retain the tested revisions,
procedure and criteria. This gate excludes RDNSS/DNSSL consumption, DHCPv6 IA_NA,
LAN forwarding, host reboot, loss of kernel state, storage failure and other
platforms. Service restart/SIGKILL checks retain the kernel network state.

## Linux DHCPv6 address and prefix client gate

Use a dedicated disposable Linux VM with iproute2, util-linux, curl, jq,
Python 3 and an extracted Kea 2.4.1 package. Run the helper from a checksum-pinned
`make dist` archive; the script creates isolated client and peer network namespaces.
It does not replace the installed routerd service or its configuration.

```sh
sudo bash scripts/linux-dhcpv6-association-smoke.sh --disposable-vm \
  /var/tmp/routerd-CANDIDATE-linux-amd64.tar.gz ARCHIVE_SHA256 /opt/kea-package
```

First certify link-local reachability, Kea configuration and its UDP/547 socket.
Then test both helper modes, `--request-mode address` and `--request-mode pd-address`:

- Match acquired IA_NA addresses and IA_PD prefixes against Kea's lease database.
- Restart the helper and require restoration without extending acquisition time.
- Observe a natural T1 Renew. Freeze the peer and require Renew, T2 Rebind and
  actual 45-second lease expiry in both public state and the persisted snapshot.
- Resume only the peer and require autonomous acquisition. Release must withdraw
  local lease authority; helper termination and all fixture cleanup must succeed.

Keep the archive/source/script hashes and scoped verdict; successful raw output is
deleted. A failed gate stops its processes and retains diagnostics for local
reproduction. This is a DHCPv6 helper protocol gate. It does not establish kernel
address installation, DAD/Decline, WAN routing, forwarding, full routerd supervision,
FreeBSD support, or complete `DHCPv6Address` resource qualification.

The 1.5 supervisor resolves `DHCPv6Address` and `DHCPv6PrefixDelegation`
declarations by kernel interface. One of each on the same interface uses one
`pd-address` worker, including when the declarations use different interface
aliases. Duplicate address or duplicate prefix declarations on that interface
are rejected. Leave `DHCPv6Address.spec.client` empty or set it to
`routerd-dhcpv6-client`; an external client is not an additional socket owner.
An address-only worker uses the internal identity `DHCPv6Address/NAME`, while
its resource status and address events retain the declared name. Protocol
`Bound` is an observation and must not promote native address status to `Applied`.
Local declaration/supervisor tests do not replace the native ownership gate above.

## Linux native Health to PBR gate

Build a checksum-pinned archive with `make dist`. On a Linux test host with
iproute2, nftables, Python 3, curl, jq and namespace/mount privileges, run:

```sh
sudo make check-routerd15-health-pbr-native-linux \
  HEALTH_PBR_ARCHIVE=/absolute/path/to/routerd-archive.tar.gz \
  HEALTH_PBR_ARCHIVE_SHA256=EXPECTED_SHA256 \
  HEALTH_PBR_WORK_PARENT=/absolute/path/to/existing/private/work-directory
```

The gate creates private network and mount namespaces, two independent HTTP
upstreams and a LAN client. It certifies both paths before product startup, then
removes its certification routes. Existing `/run/routerd` and `/var/lib/routerd`
directories are required and mounted privately. No host route, firewall rule or
installed service is changed.

Pass requires actual packaged Health workers, IPC, routerd policy selection and
client HTTP forwarding through the preferred upstream; upstream loss must switch
traffic to the alternate, and upstream-only recovery must restore the preferred
path. Terminating a Health worker must expire its old success and select the
alternate within the fixture's 155-second bound (the current freshness contract
has a two-minute minimum plus at most 30 seconds of normal polling). Worker recovery and router restart must restore
forwarding. Configuration withdrawal must remove owned rules, routes and the
default marking table, and client forwarding must stop. When both upstreams fail,
selection must become Pending/NoReadyCandidates and withdraw the marking table;
peer-only recovery must restore traffic. Unrelated rule/route tuples must survive.

The fixture starts Health helpers explicitly; it does not qualify systemd
activation, package upgrade, NAT, every policy/source mode, VRRP or FreeBSD.
Retain source/archive/script identities and the scoped verdict. Successful raw
output is removed; failure stops owned processes and retains bounded diagnostics
under the selected work directory for reproduction.

## Linux IPsec package and provider gate

The [IPsec package and service procedure](linux-ipsec-provider-test.md) exercises
actual dependency installation, systemd/VICI, scoped ownership and crash recovery
on an assigned disposable Ubuntu VM. It preserves the installed router configuration
and state and requires restoration; it is not a complete IPsec qualification.

## Linux native DHCPv6 address gate

Use the same disposable VM prerequisites and a checksum-pinned `make dist`
archive. `scripts/linux-dhcpv6-native-smoke.sh --disposable-vm ARCHIVE SHA256
KEA_PACKAGE_ROOT` runs routerd itself with its supervisor and native address
controller. Fixed runtime/state paths are mounted privately inside the fixture.

Pass requires an independent Kea lease, an exact finite `/128`, completed DAD,
bidirectional peer traffic, service stop/restart with the same SQLite acquisition,
natural Renew, actual kernel expiry while Kea is stopped, automatic reacquisition,
and configuration removal that completes native withdrawal before deleting the
receipt. An independent host claiming the leased address must cause DAD failure,
a server-observed Decline and withdrawal of the failed local address.

The helper owns protocol transactions; the address controller owns native effects.
Unknown helper state does not extend or immediately revoke a known valid lease.
Only DAD-ready, unexpired addresses appear as applied addresses. A lost native
operation acknowledgment retains pending ownership instead of adopting a present
foreign tuple. Service stop leaves finite addresses to their existing deadlines;
removing the declaration requests explicit withdrawal. Retain the procedure and
scoped verdict, and delete successful raw output. This gate does not qualify WAN
routing, transit forwarding, FreeBSD, or all routerd 1.0 functions.

## FreeBSD native DHCPv6 address gate

`scripts/freebsd-dhcpv6-native-smoke.sh` prepares the same ownership checks for
FreeBSD 14.3. Its addition and cross-compilation do **not** establish a native
pass. Use a disposable VM with independent management access, root, VNET/epair,
nullfs, devfs/fdescfs, Kea under `/usr`, sqlite3, jq, Python 3 and curl already
installed. Finish package/environment preparation before qualification.

Build with `make dist ROUTERD_OS=freebsd GOARCH=amd64 VERSION=... GIT_COMMIT=...`.
Transfer the checksum-pinned archive and script. From the repository directory
in the guest, with an existing private work parent such as `/root/routerd-tests`:

```sh
sudo timeout -k 30 600 sh scripts/freebsd-dhcpv6-native-smoke.sh \
  --disposable-vm /absolute/routerd-freebsd-amd64.tar.gz ARCHIVE_SHA256 \
  /usr/local/sbin/kea-dhcp6 /root/routerd-tests
```

The work parent must be outside `/usr` (which is mounted read-only into each
jail) and temporary storage. Separate jail roots keep supervisor sockets and
SQLite/helper state away from the installed service. Certification first checks
the scoped IPv6 return path, finite `/128` setup, an independent duplicate and
Kea configuration/listener. A certification failure must not become a product
failure or trigger repairs during qualification.

Pass requires Kea lease agreement, finite `/128` and completed DAD, all three
packets each way, stop/restart without extending acquisition, timer-driven Renew
and native lifetime refresh, actual expiry, peer-only recovery, configuration
withdrawal, Kea's `DHCP6_DECLINE_LEASE` event for the conflicting address plus its
native withdrawal, and shared PD/NA acquisition. Cleanup must stop owned children,
remove owned jails/epair and unmount private filesystems. Success removes raw
output; failures retain their bounded diagnostics under the work parent. Record
the stage and inspect those files before constructing a local reproduction.
Neither this gate nor cross-build covers all source modes, WAN forwarding or
the complete routerd 1.0 feature set.

## Linux DHCP and firewall withdrawal gate

Run the packaged service in an empty disposable VM with independent management
access. Certify systemd and install `conntrack`, dnsmasq, nftables, iproute2,
BusyBox (with udhcpc), Python 3, curl, jq and dig first. The test replaces the
router configuration; the final argument is the guest's management interface.

```sh
sudo scripts/linux-dhcp-firewall-lifecycle-smoke.sh --disposable-vm \
  "$PWD/tests/fixtures/linux-dhcp-firewall-lifecycle.json" eth0
```

Independent LAN, WAN and management-zone peers first establish positive HTTP
controls. Pass requires DHCP acquisition with the expected gateway/DNS options,
a timer-driven renewal of its two-minute lease, local/upstream DNS over UDP/TCP,
NAT source observation and preservation across service restart. Applying zones
must block WAN-to-LAN, LAN-to-management and WAN-to-router DNS. The HTTP negative
checks require both client failure and absence from the destination log, with
matching nft chain counters. A management-to-LAN positive control must continue.
An explicit LAN-to-management exception must work and stop working after removal.
Removing all firewall resources must remove `inet routerd_filter` and restore
the original positive controls. Empty apply must withdraw managed addresses.
Successful output is deleted; failed output is retained only until the failure
is reproduced locally and resolved. A VM reboot, DHCP WAN client, rebind/expiry
and production WAN behavior are separate gates.

The focused native withdrawal regression runs locally in a separate network
namespace and is skipped by ordinary `go test`:

```sh
go test -c -o /tmp/firewall.test ./pkg/controller/firewall
sudo unshare --net env ROUTERD_RUN_NETNS=1 /tmp/firewall.test \
  -test.run '^TestFirewallNativeWithdrawal$' -test.v
rm /tmp/firewall.test
```

nft ownership comments must be set by the first table creation; a later table
block does not update the kernel comment. Withdrawal checks that native marker
and deletes the observed handle, retaining retry input on read/delete failure.
An unmarked table from an older build is not automatically adopted for deletion.
Resolve such a table explicitly during isolated-environment preparation; a manual
cleanup is not a passing product withdrawal result.

## Two-VM reboot and interrupted-apply gate

`scripts/linux-router-reboot-fixture.sh` uses two dedicated disposable Linux VMs
on an isolated Ethernet segment that passes tagged VLANs 3131–3133. The router
runs the formal package; the independent peer runs BusyBox udhcpc, dnsmasq,
Python HTTP servers, curl and dig. Both need iproute2, jq, systemd and independent
management access. The fixture changes only guest networking and creates a
boot unit for the router's external test ports. It does not create hypervisor
networks. Do not use a segment whose VLAN IDs collide with other work.

If management and the isolated trunk use separate NICs, pass the trunk NIC to
`setup-router` and `setup-peer`. Configure that trunk to come up at boot using
the guest's network manager, without adding a test-subnet address to its parent.
Before the first apply, set `Interface/management.spec.ifname` in the generated
`candidate.json` to the actual management NIC and validate the candidate again.
The generated admin zone already references that resource. This keeps management
access in the intended firewall zone while test VLANs remain on the trunk.

The dual-stack mode uses `tests/fixtures/linux-dualstack-reboot.json`, which
replaces the static WAN IPv4 setup with DHCPv4 and combines managed WAN RA reception with PD, delegated LAN
addressing, DHCPv6/RA and dual-stack DNS. Its configuration passes the real CLI
validator. Router setup supplies a disabled networkd RA baseline on the fixture
WAN; desired product resources enable reception. A third `setup-router` argument
(after the candidate path) can name the separate management NIC.
The independent radvd peer was checked locally with real SLAAC, a learned
default route and peer-observed HTTP source. Dual-stack peer mode requires
radvd and a root-extracted, matching Kea DHCPv6 package root, including its
shared libraries; installed confined Kea executables may reject fixture paths.
After router setup, read its non-tentative `rd15-wan` link-local address and pass
it to peer setup. It is the independent upstream's return next hop for the PD
pool, and must remain unchanged across router reboot.

```sh
ssh router-fixture 'sudo bash /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm setup-router ens19 /home/ubuntu/linux-dualstack-reboot.json eth0'
# Read rd15-wan's link-local address on the router, then substitute it below.
ssh peer-fixture 'sudo bash /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm setup-peer ens19 <router-wan-link-local> /opt/routerd15-fixtures/kea'
```

Peer setup waits for WAN link-local DAD before starting Kea, then checks
RA/PD/DNS service state, DHCP sockets and independent IPv6 HTTP/DNS before the
router apply. An active Kea process without a DHCPv6 socket is not ready. The existing `probe` and `isolation`
commands then require dual-stack DNS A/AAAA over UDP/TCP, delegated client
SLAAC/default route, peer-observed IPv6 forwarding, and IPv6 WAN-to-LAN/DNS
isolation in addition to the IPv4 checks. The LAN IPv6 client is kernel SLAAC;
this probe does not yet establish DHCPv6 IA_NA renewal/reboot behavior.
The combined two-VM reboot campaign passed at product `6977c66e` with fixture
`96e00032`; see the scoped result below. The earlier IPv4 result alone does not
qualify this path.

The reboot fixture's DNS source has the default three-second per-upstream query
deadline. Its independent-client DNS probes use a five-second `dig` deadline so
the test client does not abandon a query that the resolver is still allowed to
complete. This is not a DNS latency target: a timeout, SERVFAIL, empty answer or
wrong address still fails the positive probe, and any DNS response still fails
the WAN-isolation probe.

Extract the current Linux `make dist` package on the router VM and install it
from that package directory with `sudo ./install.sh --no-install-deps --enable-service`.
Do this on a clean VM with no existing routerd binary,
configuration, or database; use a configuration already expressed in routerd
1.5 format. Copy the shell script and
`tests/fixtures/linux-dhcp-firewall-lifecycle.json` to
`/home/ubuntu/` on both VMs, and `scripts/linux-interrupted-apply-smoke.py` to
the router. Below, `router-fixture` and `peer-fixture` are operator-configured
SSH aliases with verified keys; `eth0` is the isolated management/trunk port.

```sh
ssh router-fixture 'sudo bash /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm setup-router eth0 /home/ubuntu/linux-dhcp-firewall-lifecycle.json'
ssh peer-fixture 'sudo bash /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm setup-peer eth0'
ssh router-fixture 'sudo install -m 0600 /var/tmp/rd15-reboot/candidate.json /usr/local/etc/routerd/router.yaml && sudo systemctl start routerd'
ssh router-fixture 'sudo /usr/local/sbin/routerctl apply -f /var/tmp/rd15-reboot/candidate.json --replace'
ssh router-fixture 'sudo /usr/local/sbin/routerctl get status -o json | jq -e ".status.phase==\"Healthy\" and ([.status.controllers[] | select(.currentError==true)] | length==0)"'
ssh peer-fixture 'sudo bash /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm probe'
ssh peer-fixture 'sudo bash /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm isolation'
```

Before each interruption, record both `/proc/sys/kernel/random/boot_id` values,
the router's `systemctl show -p MainPID --value routerd`, and the peer's
`systemctl show -p MainPID --value rd15-dhcp-client`. Preserve the first `bound`
address and subsequent `renew` lines in the peer's `/var/tmp/rd15-reboot/leases`.
Do not restart the peer or its DHCP process between checks.

1. Run `sudo systemctl reboot` on the router. Within 90 seconds require a
   different router boot ID, active routerd, systemd `running`, and no
   `currentError` controllers in `routerctl get status -o json`.
2. Repeat `probe` and `isolation` from the peer. Its boot ID and DHCP PID must
   match the recorded values. Require the same lease address and another actual
   timer renewal. Check the router's `wan_to_lan`, `lan_to_admin` and `wan_to_self`
   nft chain counters after the denied probes.
3. Run `sudo systemctl kill --kill-whom=main --signal=SIGKILL routerd` on the
   router. Within 60 seconds require an active replacement PID, an increased
   `NRestarts`, no controller errors, and repeat the peer checks. The router's
   boot ID must remain unchanged for this process-crash case.
4. Run the following only after the previous configuration is healthy:

```sh
ssh router-fixture 'sudo python3 /home/ubuntu/linux-interrupted-apply-smoke.py --disposable-vm'
```

The helper adds `recovery.home.arpa`, observes canonical file replacement and
kills the exact original product process through pidfd while the apply client
is still awaiting a response. It requires a failed client response; success
already acknowledged by the client does not prove this scenario. It does not
assert whether directory fsync had returned at the interruption. The failed
apply response must retain an unconfirmed outcome, not claim rollback or success.
Within 60 seconds require automatic service recovery. From the peer's LAN
namespace require `dig +short @198.18.15.1 recovery.home.arpa A` to return
`198.18.15.1`, then repeat `probe`, `isolation`, unchanged peer identity and DHCP
renewal checks. The helper alone does not establish recovery.

The probe requires local/upstream DNS over UDP/TCP, a WAN HTTP peer observing
the NAT source, and the management-to-LAN positive control. Isolation requires
client failure and absence from destination HTTP logs, plus WAN DNS rejection.
The probe and isolation modes wait up to 45 seconds for the initial DHCP lease.
The independent peer keeps its processes and lease throughout all interruptions.
No manual repair of product state is permitted to turn a failed gate into PASS.
A fixture setup failure is environment preparation, not a product result.

After a successful campaign, run `cleanup-router` on the router and
`cleanup-peer` on the peer with the same `--disposable-vm` flag. Router cleanup
requires empty apply, native firewall absence and address withdrawal before
removing the fixture ports and unit override. Peer cleanup stops the owned units
and removes its namespaces. Router setup records the initial
`net.ipv4.ip_forward` value; cleanup restores that value after withdrawing the
fixture. These actions delete the temporary raw output.
Retain the procedure, revisions and verdict instead. This gate does not prove
power-loss durability or every possible interruption point. IPv6 recovery
requires dual-stack peer mode and the IPv6 checks; the IPv4 mode alone cannot
qualify it.

Latest scoped run: Linux package `603126ea073f6d8e4ce3729975a2e97411d36850`
passed fresh installation, independent-client DNS A over UDP/TCP, NAT and
management/isolation checks, then router reboot, main-process SIGKILL and an
apply interrupted after canonical replacement before the client response.
All three recovery phases retained the peer boot ID and DHCP PID and observed
an actual renewal of the same lease. The interrupted apply returned failure;
automatic recovery served its new DNS record. A reboot capture paired the
client's first upstream query with the upstream reply and client response.
This scoped result does not establish the cause of the earlier one-second
probe failure or qualify IPv6 reboot or full 1.0 preservation.

Two-VM dual-stack scoped result (2026-11-03): formal Linux package
`6977c66e9b3eb31074aaac502d3618f8ff159a57`, archive SHA-256
`c10fa0115dc98ba028e722d357c836b711bd40f7727d7d855ef3649f33887575`,
with fixture `96e0003240341d4638ef5837cdfc430f645660f9`, passed fresh
installation and all three recovery stages above. The Ubuntu 24.04 router
booted Linux 6.8.0-147 after reboot; its independent peer stayed running.
WAN DHCPv4, managed WAN RA/default-route discovery, DHCPv6-PD, delegated
SLAAC/default route, local/upstream DNS A/AAAA over IPv4/IPv6 UDP/TCP,
peer-observed IPv4 NAT and delegated IPv6 forwarding, management positive
control and IPv4/IPv6 isolation passed before and after each interruption.
The peer's boot ID, DHCPv4 PID and upstream RA/PD/DNS PIDs stayed unchanged;
the same DHCPv4 address renewed after each interruption. SIGKILL and interrupted
apply each caused one automatic systemd restart. The interrupted client reported
an unconfirmed outcome; the new canonical DNS record resolved over both families
and transports after recovery. WAN link-local identity stayed unchanged, and
native deny counters were nonzero.

The first attempt exposed a product regression: the Linux packet socket lost
kernel checksum-offload metadata and discarded an independent dnsmasq OFFER.
The real-kernel regression failed before `6977c66e` and passed after it; the
package was then uninstalled and freshly installed before this successful run.
This result covers the stated two-VM path, not LAN DHCPv6 IA_NA renewal/reboot,
power-loss durability, other platforms or all routerd 1.0 functions.
Successful teardown withdrew managed state, uninstalled the package and removed
the two disposable VMs, their disks and temporary fixture media.

## IPv6 static route default metric

On Linux, an omitted or zero `IPv6StaticRoute.metric` uses kernel priority 1024.
Verify the effective metric in the native ownership receipt and kernel output;
do not compare the kernel value with the unexpanded configuration zero. The
[route readback procedure](../design/routerd15-ipv6-route-readback.md) covers
application, client forwarding and withdrawal. The local regression command is:

```sh
go test ./pkg/controller/chain -run '^TestIPv4RouteControllerAppliesAndWithdrawsIPv6StaticRoute$' -count=1
```


## Packaged IngressService failover and drain

Use a dedicated Linux VM with the matching formal packages, an empty canonical
configuration, management access independent of test links, and no existing test
namespaces. Never run this against a production router. The lower-level
`tests/netns/ingress-conntrack-survive.sh` uses handwritten nft rules and does not
qualify the IngressService controller or daemon.

Create three isolated peer namespaces: a client and two independent TCP echo
backends, each on a separate veth subnet with a return route through the router.
Declare the three router links as external Interface resources, an explicit
runtime forwarding Sysctl, and one IngressService: TCP listener on the client
link, two named backend IP/port pairs, failover selection, hairpin off, TCP checks
with 500ms timeout and healthy/unhealthy thresholds one. Certify direct peer
responses before applying the configuration through the normal daemon API.

1. Require `routerctl get IngressService/service -o json` to select backend1 and
   a new independent client connection to receive backend1's response.
2. Keep one TCP connection open, sending and checking backend1 echo replies
   every 200ms. The client must never reconnect.
3. Run `routerctl ingress drain ingress/service backend=backend1`. Within a
   bounded 45-second convergence window, require status and new connections to
   select backend2 while the original connection continues on backend1.
4. Restart the main daemon. Require the drain and backend2 selection to persist,
   and the original TCP connection to continue making progress.
5. Run `routerctl ingress undrain ingress/service backend=backend1`; require new
   connections on backend1 and then finish the original client normally.
6. Stop backend1's process. Require automatic selection and actual new traffic
   to backend2; restart backend1 and require automatic return to backend1.
7. Apply empty configuration. Require Healthy and no owned NAT table or helper;
   stop peers, join their clients, remove namespaces/veths and temporary files,
   and restore the fixture's original forwarding setting before stopping the VM.

Status alone is insufficient: compare backend identity on actual TCP responses.
On failure, capture the current resource status, owned nft table and client result
before ending the run; do not manually change product state. This procedure
covers failover/drain with surviving connections, not all-backend outage policy,
sourceHash/random distribution, every address source or guest reboot.


## Linux dynamic NAT flow pinhole check

Run `sudo ./tests/netns/nat44-flow-dnat-pinhole.sh` in an isolated Linux test VM
with nftables, iproute2, Python and Go. It creates separate router/client/WAN
namespaces, invokes the production NAT controller, and exchanges UDP with an
independent peer that replies from a different source port. A matching formal
validator binary and prebuilt controller test driver can replace guest Go:
set `ROUTERD_TEST_BINARY` and `ROUTERD_NAT44_TEST_DRIVER` as for the source-SNAT
check. Bound the run with `timeout 180`; never substitute a handwritten nft
ruleset or run the namespace mutations on a production router.

Pass requires a permitted response; denial for unmatched source, outbound
address/port, inbound source port and incoming interface; denial after the
five-second learned-map expiry and after policy removal; no restored stale
mapping after replacement; and actual forwarding for the newly authorized
source while the previous source is denied. The peer must observe each outbound
request, so missing outbound connectivity cannot satisfy a negative assertion.
The fixture removes namespaces and temporary files on completion. On failure,
inspect the peer receipt, response identity and owned map before changing code.
This covers the controller and real kernel, not normal-daemon restart or every
collision/translation combination.

NAT and policy renders replace statically declared address-set membership in the
same nft transaction as their rules; removed addresses must not remain authorized.
DNS-populated sets keep their existing address-controller ownership. The opt-in
`TestNativeNATAddressSetReplacement` checks IPv4/IPv6 replacement and repeated
apply with real nftables, plus preservation of DNS-owned members. Run it in an
isolated user/network namespace with `ROUTERD_RUN_NETNS=1` and
`ROUTERD_HOST_NETNS` set to the parent namespace, as in the NAT pool syntax check.


## Packaged address-set redirect lifecycle

Use a dedicated Ubuntu guest with the formal base/addon packages, an empty
canonical configuration and the normal routerd service. The independent peers
need iproute2, dnsmasq and dig. Create separate client and WAN namespaces joined
by separate veth pairs to the guest. Give both links IPv4 and IPv6 addresses,
provide client default routes and WAN return routes, and record original
forwarding sysctls for cleanup. These are fixture prerequisites, not manual
repairs of product state. Do not run this on a production host.

Run an independent DNS server on the WAN with two addresses per family, replying
`192.0.2.22` for `probe.test`, and another on the guest LAN at port 15353 replying
`192.0.2.11`. Configure external Interface resources, forwarding Sysctl resources,
an IPAddressSet containing the first WAN address in each family, and a
LocalServiceRedirect selecting that set on the LAN for TCP/UDP port 53 to 15353.
The shape is in `tests/netns/local-service-redirect-runtime/config.yaml`. Validate
each input with the installed routerd, then use
`routerctl apply --timeout 90s -f CONFIG --replace` from the fixture directory.

After each apply, allow at most 45 seconds for Healthy status. From the independent
client run `dig +short +time=2 +tries=1 @ADDRESS probe.test A`, both without and
with `+tcp`, over each IPv4 and IPv6 WAN address. The selected addresses must
return the local answer and unselected addresses the WAN answer. Change the set
to the second address in each family, apply, and require both directions of the
switch while the main PID is unchanged. Restart the service and require the same
selection. Remove the set and redirect together and require all queries to
return the WAN answer. Each query assertion has a bounded 15-second convergence
window; missing replies never count as successful exclusion.

Finally apply the empty configuration and require Healthy, zero resources, no
owned NAT tables or managed helpers. Stop the service and independent DNS peers,
remove fixture namespaces/veths/files, restore saved forwarding values and stop
the guest. On failure retain only the current status, nft sets/rules and client
results needed to distinguish apply, membership and forwarding failures. This
qualifies normal-daemon static selection and restart, not DNS-populated refresh,
policy-routing traffic, guest reboot or every NAT flow-pinhole combination.


## Packaged firewall response-pinhole check

Use the normal service from a formal package in a dedicated Ubuntu guest with
an empty canonical configuration. Create separate routed LAN-client and WAN-peer
namespaces, save forwarding settings, and explicitly include the management
interface in an external Interface and `mgmt` FirewallZone so new SSH connections
remain possible. Use `trust` for LAN and `untrust` for WAN. This is a firewall
forwarding test without NAT. Validate inputs with the installed routerd and apply
with `routerctl apply --timeout 90s -f CONFIG --replace`; require Healthy within
45 seconds before sending traffic.

An independent UDP peer listens on port 32100 at its first WAN address, records
each request ID, and responds from port 32099 on either that address or a second
address in the allowed destination range. The client binds a fresh source port
for each request and waits two seconds beyond the requested peer response delay.
Different request/response ports ensure ordinary established-flow acceptance
cannot substitute for the pinhole. Configure FirewallFlowPinhole from LAN to
WAN with the client's source range, the WAN destination range, outbound port
32100, inbound port 32099 and a five-second timeout.

With `correlation.key: remoteAddress`, require a same-address response and reject
a response from the second address. Apply `localEndpoint` and require the second
address response, while still rejecting source port 32098. Delay a response six
seconds and require timeout after expiry. Allow 120 seconds to observe the
service restart (including its configured stop deadline), then wait for
Healthy and verify a fresh permitted exchange. Remove just the pinhole, retaining
the zones, and require rejection of a new response. Every negative assertion
also requires an independent peer receipt for that request; never accept missing
outbound connectivity as success. Do not reuse client ports across assertions.

End with empty configuration, Healthy and no helpers or owned filter table. Stop
the service and peer, remove namespaces/veths/files, restore forwarding and stop
the guest. On failure collect the relevant nft set/counters and request/response
identity before local reproduction. This checks the two IPv4 firewall correlation
modes, configured response port, expiry, restart and withdrawal; it does not
qualify NAT response maps, every selector combination, guest reboot or FreeBSD.


### DNS-populated address-set variation

Extend the packaged redirect lifecycle with `names: [selected.test]` and
`refreshInterval: 1s` instead of static addresses. Give the independent WAN
dnsmasq an additional hosts file containing the selected IPv4/IPv6 pair, with
a one-second local TTL. Supply its address as the normal daemon's system DNS
using a temporary runtime systemd drop-in with
`BindReadOnlyPaths=FIXTURE_RESOLV_CONF:/etc/resolv.conf`; reload systemd before
starting the service. This scopes test DNS to the service mount namespace and
leaves the host resolver unchanged.

After initial traffic succeeds, replace the peer hosts entries with the second
address pair and send HUP to that peer. Do not reapply routerd configuration or
edit its status. Require IPAddressSet status to contain exactly the new pair
within 60 seconds, allowing the existing 30-second controller scheduling period.
Repeat the eight old/new address, IPv4/IPv6, TCP/UDP response assertions and
require unchanged main PID. Restart, verify the new selection, then withdraw
as above. After stopping the daemon remove the runtime drop-in and reload
systemd before removing fixture files. This qualifies successful DNS answer
replacement, not resolver outage, NXDOMAIN/empty-answer policy or TTL-expiry
withdrawal; those are distinct from a configured refresh interval.


## Packaged WebConsole browser check

Use a dedicated Ubuntu guest with formal packages and an empty canonical config.
Apply a WebConsole bound only to loopback, with a distinctive title and unused
port, through the normal routerctl/service path. Forward that port with SSH to
a loopback-only local port; use an existing Chrome and Playwright core without
API interception or seeded SQLite observations. Keep the tunnel lifetime bounded
and terminate it even if a browser assertion fails.

Open `/#resources` and require the configured WebConsole resource in
`#resources-table` within 30 seconds. The document title includes both the
selected page and configured title (`Resources - TITLE`). From that page, POST
to `/api/v1/config` and require 405/read-only; WebConsole is not a configuration
mutation API. Add an unreferenced static IPAddressSet through routerctl and,
without reloading the browser, require its row within 45 seconds and its name
in GET `/api/v1/config`. Remove it through routerctl and require its row to
disappear or explicitly say it is not declared in current config; retained
withdrawal observations must not be misrepresented as current declarations.

Restart the normal service with a 120-second observation bound, reload the page
and require the remaining resource to render without page JavaScript errors.
Finally apply empty intent, require Healthy and no WebConsole listener/helpers,
stop the daemon, close browser/tunnel, remove inputs and stop the guest. This
qualifies actual packaged display/current-config/read-only behavior, not every
UI workflow, admission policy or unavailable-observation display; retain the
separate controlled browser tests for stale response ordering and unknown data.


### Packaged DHCP lease to DNS lifecycle

Use a dedicated Ubuntu guest with checksum-verified formal Debian packages,
empty router intent, independent management, busybox and dig. Prepare an isolated
veth/client namespace: router `198.19.87.1/24`, client with no static IPv4 address.
Declare the external Interface, managed dnsmasq DHCPv4Server pool
`198.19.87.100`–`198.19.87.110` with one-minute leases, a DNSZone `lease.test`
whose `dhcpDerived.sources` selects that named server, and DNSResolver/Forwarder
on the router address selecting that zone for forward and reverse queries.
Validate, start the normal packaged service and apply with a 90-second deadline.

Run independent `busybox udhcpc -f -n -i eth0 -t 10 -T 2
-x hostname:lease-client -s <fixture-hook>` in the client namespace under a
bounded service. Its bound/renew hook installs the offered address and records
it; deconfig clears only the client's address. Within 60 seconds require `dig`
UDP and TCP A queries for `lease-client.lease.test` to return that exact lease,
and PTR queries to return the exact hostname. Each dig uses `+time=2 +tries=1`.
Restart routerd using systemd (allow 120 seconds); require the named A record
again without replaying a lease or editing SQLite. Send SIGUSR2 to the independent
udhcpc process to request DHCP RELEASE. Within 60 seconds require actual
NXDOMAIN responses with ANSWER 0 for A and PTR over both transports; silence is
not a negative DNS answer. This exercises the product lease callback/journal,
not a synthetic event injection.

Apply empty intent and require Healthy, no current errors and no product helper
processes. Stop the client, remove its namespace/veth and private staging, then
stop the guest. On failure retain bounded DHCP service/callback and DNS status
output, inspect actual lease/journal state, and reproduce locally before repair.
This gate covers acquisition, main restart and release, not natural lease expiry,
HA failover, DHCPv6 names or all transport/policy combinations.


### Packaged IngressService distribution and complete outage

Reuse the isolated three-peer setup above, with `onNoHealthyBackends: reject`.
Certify both echo servers directly before product apply. Add 17 distinct source
addresses to the independent client, then select `sourceHash` through normal
configuration apply. Within 45 seconds require both active backends and the
requested effective selection. From each address open four new TCP connections,
send a newline-terminated payload and verify the exact backend-tagged response.
Require the same backend for each source's four connections and both backends
across the sample. This checks affinity within an unchanged ruleset, not stable
mapping across configuration replacement. Repeat with `random`, requiring every
response from an allowed backend and both backends represented; this finite sample
is not a statistical uniformity guarantee. Bound each connect/read to three seconds.

Stop both independent backend processes; within 45 seconds require zero healthy
backends. A new client connection must fail with connection refused, not silently
time out or establish. Restart backend2, requiring actual backend2 payloads;
restart backend1 and repeat the sourceHash response checks with both active.
Finally use the empty-intent/peer/link/forwarding cleanup above. Preserve no raw
output beyond current failure investigation. Dynamic address sources, alternate
outage policies, UDP and physical failure domains remain separate checks.


### IPv6 preferred-source loss recovery

Linux can retain a policy default route while clearing its preferred `src` when
that address disappears. The policy owner must withdraw the selector and the
acknowledged route, then restore both when a usable source returns. Recovery also
covers disappearance/return between reconciliations. Only an acknowledged receipt
in the same kernel scope and interface identity can authorize repair of the
otherwise identical source-less route. Unowned or pending acquisitions remain
fenced; new acquisition still requires exact source readback.

`make check-routerd15-system-native-linux` includes
`TestPolicyIPv6NativeKernelSourceLossRecovery`, using private network/UTS/mount
namespaces, a real veth and SQLite with the production controller. It checks actual
kernel clearing, route/rule withdrawal, source return and final removal. Ordinary
unit tests skip this explicitly enabled kernel case. Package/service and independent
payload qualification are separate from this local controller regression.

After successful policy-effect withdrawal, removed EgressRoutePolicy status is
retired so a former PreferredSourceUnavailable/Pending result cannot keep an
empty router unhealthy. Declared but disabled policies, dry-run observations and
failed withdrawals retain their status.


For the packaged IPv6 host-policy gate, use a dedicated Ubuntu VM with empty
intent and independent management. Certify a veth-linked independent HTTP peer:
router source `2001:db8:88::1/64`, peer link-local gateway `fe80::88` and an
HTTP destination on peer loopback `2001:db8:89::99/128`. Direct peer-link HTTP must
work before product apply; the loopback destination must be unreachable. Declare
an external WAN Interface and IPv6 priority EgressRoutePolicy with hostTraffic,
preferredSource interface, static link-local gateway, table 128, priority 10128
and metric 50. Apply through the normal formal-package daemon with a 90-second
limit. Within 60 seconds require Applied, the exact kernel route with preferred
source and `iif lo` rule, and the loopback HTTP payload.

Remove only the fixture's external source address. Require Pending with
PreferredSourceUnavailable, both policy route and rule absent, and HTTP failure.
Restore that source and require automatic rule/route/payload recovery without
changing the main PID. Restart the main service (120-second bound), then require
the same payload. Apply empty intent and require Healthy, no stale policy status,
no policy route/rule and failed HTTP. Stop the peer and remove its namespace;
allow up to five seconds for the paired veth to disappear, then remove staging
and stop the guest. This is host-originated IPv6 traffic, not transit/hash policy
or all dynamic WAN modes. Use explicit `if unexpected-command; then exit 1; fi`
for negative shell assertions: a bare `! command` is not enforced by `set -e`.


### Enforced negative assertions in namespace gates

`tests/netns/vxlan-l2-mss-clamp.sh` and
`tests/netns/vrrp-vmac-lan-address-lifecycle.sh` explicitly exit on unexpected
success of forbidden operations or unexpected presence of withdrawn objects.
Bare `! command` under `set -e` is insufficient for an assertion followed by
another command. Conditions used by `if`/loops or a checked function return are
not the same problem; inspect the surrounding control flow before changing them.

The VXLAN gate accepts `ROUTERD_PATH_MTU_DRIVER` as an absolute executable path
to the test driver built from the current source. This permits building as the
normal user and running only the namespace test with explicit sudo. It remains
a controller/real-kernel test, not a packaged normal-daemon qualification.
Use a private outer mount/network namespace as well as the test's peer namespaces;
its driver must be able to inspect `/proc/1/ns/net` to prove isolation. Run the
VRRP gate with `ROUTERD_RUN_NETNS=1` and `ROUTERD_VRRP_VMAC_BIN` pointing to the
formal package helper; it creates its own private mount/network namespaces.
Bound each gate with `timeout`, require exit 0 and successful cleanup, and retain
only unresolved failure diagnostics.

## Linux overlapping VRF isolation

`make check-routerd15-native-parent-linux` includes
`TestVRFNativeLinuxOverlappingPeerIsolation`. Python 3 and kernel VRF support
are required. The explicit opt-in target isolates network and sysfs views; never
set `ROUTERD_RUN_NETNS=1` in the host network namespace.

The production VRF/address controllers create two routing domains with the same
local and peer IPv4 addresses. Each domain also has an independent client
namespace with an overlapping client subnet. Enable IPv4 forwarding only inside
the isolated router namespace and give each server a return route to its client.
Independent peer namespaces return distinct UDP payloads. Run each assertion both
from a VRF-bound router socket and from its client through the router. Require
the correct payload from each VRF, successful access to each
peer's unique address, and timeout when accessing the other VRF's unique address.
Repeat after SQLite reopen. Withdraw one VRF and its address and require the
other peer to remain reachable. Final withdrawal must remove both VRFs, owned
addresses and receipts. This is a native controller test, not packaged-daemon
restart or delegated IPv6/RA qualification.

## Linux VRF delegated address and RA

`make check-routerd15-native-parent-linux` also runs
`TestVRFNativeLinuxDelegatedRA`. It requires Linux tools
(`ip`, `sysctl`, `nsenter`, `ping`) and explicit namespace isolation as above.
A finite PD lease is supplied through SQLite; the production VRF, delegated
address and RA controllers perform the native effects. A separate client
namespace must acquire the advertised prefix through kernel SLAAC and a
`proto ra` default route, then reach the router's delegated IPv6 address and an
independent upstream namespace through the VRF. Forwarding, upstream addresses
and return routes are isolated fixture setup. Replace the stored PD prefix,
require a new preferred SLAAC source for upstream traffic and deprecation of
the old client address. Withdrawal must remove both owned router prefixes, the
client's RA default and the RA journal. This does not test the DHCPv6 exchange
or packaged-daemon restart/reboot. The extended gate exposed an initial RA `sendmsg: invalid argument`: the
link-local source was still tentative although the delegated address had finished
DAD. Linux now checks native source flags before opening the sender, publishes
`LinkLocalDADPending` while DAD runs, and resumes on controller reconciliation.
The gate also includes `TestListenRejectsTentativeLinkLocal`, which forces that
kernel state and verifies readiness before and after DAD. The existing controller
has status-event reconciliation and a 30-second periodic check; this test invokes
reconciliation directly and does not qualify packaged startup latency.

## Packaged VRF lifecycle prerequisites

Use an isolated Linux router VM and an independent peer VM. On Ubuntu generic
kernels, certify VRF availability before applying the candidate:

```sh
modinfo vrf
# If the minimal OS image omitted the module package:
sudo apt-get install kmod "linux-modules-extra-$(uname -r)"
sudo modprobe vrf
```

The module package must match the running kernel. Install it during environment
preparation, then re-certify; do not repair the kernel during qualification.
The observed Ubuntu 24.04 minimal guest had `CONFIG_NET_VRF=m` but no `vrf.ko`.
Keep package version/hash, independent Kea readiness and both isolated L2 paths
in preflight. Restore fixture interfaces before routerd on guest boot.
Wait for the peer interface link-local address to leave tentative/DAD-failed
state before starting Kea, and require an actual UDP 547 listener; an active
process alone does not prove DHCP readiness. Persist fixture setup before a
power-loss test. After normal withdrawal and fixture cleanup, flush guest
filesystem writes before final poweroff so deleted fixture units cannot return.

Require an empty Healthy starting state, then apply VRF with LAN/WAN members,
IPv4 addresses, real DHCPv6 PD, delegated LAN/WAN addresses and RA. Verify peer
IPv4/IPv6 transit, daemon restart and cold router boot while preserving the peer
VM and DHCP server process. Finish with empty configuration and verify Healthy,
no owned VRF/addresses/helpers, and no removed VRF health rows. A withdrawn
`Absent/OwnedGenerationWithdrawn` display row must be retired once neither the
configuration nor the ownership journal retains that VRF. This cleanup must also
recover already-withdrawn rows after restart.
After OS reboot, retire receipts from the previous Linux boot before acquiring
a fresh VRF generation. A namespace change within the same boot remains an
error; a same-named foreign interface must never be adopted or removed. Dry-run
planning preserves the old receipt. `TestVRFRebootRecovery` covers these paths.

## Tailscale advertisement removal and recovery

The upstream CLI regression uses a private Unix LocalAPI fixture, with no host
daemon or tailnet access:

```sh
ROUTERD_TEST_TAILSCALE=/absolute/path/to/tailscale make check-routerd15-tailscale-cli
```

For the packaged-daemon check, certify two disposable Linux guests, independent
Headscale/TLS, authenticated test-only node identities, and an HTTP endpoint
behind the router before qualification. Keep the control path outside the data
path under test. Use the declared Sysctl and IPv4Route resources for forwarding
and the remote destination.

1. Apply a TailscaleNode with subnet and exit-node advertisements through the
   normal daemon. Approve those routes on the test control server. Require an
   independent client to retrieve the exact HTTP payload through the subnet
   route, then a destination outside that subnet through the selected exit node.
2. Deselect the exit node on the client. Remove advertised routes and set
   `advertiseExitNode: false` in YAML, then apply. Require Running/Healthy, empty
   daemon advertisements, empty control-server available routes, absent client
   subnet route, and failure to retrieve the old subnet payload. Headscale may
   omit an empty `available_routes` field; approved routes alone are not proof
   that a node still advertises them.
3. Reapply the advertisements. Require both payload paths to recover. Restart
   routerd and require the exit-node payload again without repairing product
   state or reauthenticating.
4. Apply an empty configuration. Require Healthy, no managed unit/route and
   `WantRunning: false`. Stop independent peers/control services, restore fixture
   forwarding settings, remove fixture namespaces/files and stop the guests.

IPv6 exit traffic, expired credentials and control-server outage recovery need
separate checks; IPv4 forwarding does not qualify them.

### Tailscale authentication expiry

Using the certified isolated setup above, first require the normal daemon's
Running/Healthy status and an independent subnet HTTP payload. Expire only the
router's test node with Headscale `nodes expire --identifier <test-node-id>`.
Require the actual backend to enter NeedsLogin, then observe
Pending/AuthenticationRequired and non-Healthy overall status within the
existing service reconciliation interval. Do not apply or restart to force this
observation; check that the main PID is unchanged and the old payload fails.

Generate a fresh single-use lab credential without logging it. Write a private
0600 environment file and declare `authKeyEnv`/`authKeyFile` through the normal
configuration API. Require Running/Healthy, cleared old reason/runtimeError and
restored independent payload. Complete empty withdrawal and remove the one-use
credential with the fixture. This verifies expired node credentials and explicit
credential renewal, not automatic secret generation or control-server outage.

### Dual-stack exit traffic and control-server outage

Extend the isolated endpoint with IPv6 and declare an IPv6StaticRoute to a
destination outside the control LAN. Before applying, require that the independent
client cannot already reach it. After selecting the router as exit node, require
the expected IPv4 and IPv6 HTTP payloads and an IPv6 route through `tailscale0`.

Keep the control server's reply path independent of the selected exit path. If
Headscale and the test client share a guest, use the client's
`--exit-node-allow-lan-access=true` and verify the server-to-router connected-LAN
route and a fresh HTTPS health request after exit selection. Without this, a
SYN can arrive on the LAN while its SYN-ACK leaves through the exit node. A
cached connection is not sufficient certification. For a transient systemd-run
server, recreate the same command after stopping it; the stopped transient unit
may have been collected.

Stop the control server, require failed health requests from both nodes, and
make repeated fresh TCP HTTP requests over both address families. Restart the
control server without reauthenticating either node, require both nodes online
in its current observations, and repeat both payload paths. Restart routerd and
check IPv6 traffic again. Empty withdrawal must remove both owned destination
routes, disconnect the node and return Healthy. Reset the independent client's
exit selection/LAN exception and remove the fixture as above. This is a bounded
outage check, not a promise of forwarding through arbitrary credential expiry.
