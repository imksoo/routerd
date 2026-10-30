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


## Mandatory test and harness design rules

These rules apply to unit tests, offline fixtures, runtime smoke, release qualification and reusable old helpers, templates and copy sources, including code without a current caller. Every test change must identify its product requirement, required direct evidence and observation limits.

- Preserve observations and diagnostic logs; separate them from product acceptance. Journal text, progress counters, latency statistics and whole-host diagnostics must not become acceptance conditions without a concrete product requirement.
- Judge PASS or product FAIL using completed, attributable direct evidence of that requirement. API/SSH instability, acquisition failure, missing or malformed input, incomplete guest commands and timeouts must remain observation or infrastructure outcomes. Never convert them into zero, no violation, successful rejection or measured product failure.
- Reduce arbitrary speed assertions, repeated probes and duplicate gates. Keep contractual deadlines and a bounded watchdog to stop hung work; distinguish watchdogs from product performance measurements.
- Before deleting temporary data, retain raw failed attempts, source identity, exits, timestamps and cleanup outcomes. Continue cleanup only for resources this invocation owns. Keep cleanup failure visible and preserve product evidence.
- Do not repeat a failed real trial without investigating its cause. Stop at the existing retry, time or cost budget; report failure, uncertainty, remaining budget and proposed fix before further trials. Reacquire only justified failed or missing observations, with bounds and retained attempts.
- Audit the actual entry, selected source paths and hashes, dependencies and final result propagation. Candidate and mock validation proves that scope; it cannot prove that a running entry selects the candidate or that real forwarding works.
- Distinguish implementation, prepared candidate, deployed code, saved-original replay, deliberately modified fixture and unverified behavior. Preserve original negative evidence; never rewrite a historical failure as PASS.
- Apply these rules before reusing old or unconnected code. Keep sealed historical evidence unchanged; repair reusable sources. An absent current caller does not excuse a defect.

These rules do not weaken real functional requirements, safety or ownership checks, or spending limits. Missing necessary evidence cannot establish PASS. Keep proof of normal communication and direct violations; remove only unjustified coupling to diagnostics.

Good: curl exit 0 and HTTP 200 with required body/path evidence can PASS when optional latency is missing; record the omission. Bad: a failed WireGuard read proves no forbidden AllowedIP; a forced invalid-apply timeout counts as successful rejection; a same-stack ping proves tunnel forwarding.

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

Copy the shell script and `tests/fixtures/linux-dhcp-firewall-lifecycle.json`
to `/home/ubuntu/` on both VMs, and `scripts/linux-interrupted-apply-smoke.py`
to the router. Below, `router-fixture` and `peer-fixture` are operator-configured
SSH aliases with verified keys; `eth0` is the isolated management/trunk port.

```sh
ssh router-fixture 'sudo /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm setup-router eth0 /home/ubuntu/linux-dhcp-firewall-lifecycle.json'
ssh peer-fixture 'sudo /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm setup-peer eth0'
ssh router-fixture 'sudo /usr/local/sbin/routerctl apply -f /var/tmp/rd15-reboot/candidate.json --replace'
ssh peer-fixture 'sudo /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm probe'
ssh peer-fixture 'sudo /home/ubuntu/linux-router-reboot-fixture.sh --disposable-vm isolation'
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
ssh router-fixture 'sudo /home/ubuntu/linux-interrupted-apply-smoke.py --disposable-vm'
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
The independent peer keeps its processes and lease throughout all interruptions.
No manual repair of product state is permitted to turn a failed gate into PASS.
A fixture setup failure is environment preparation, not a product result.

After a successful campaign, run `cleanup-router` on the router and
`cleanup-peer` on the peer with the same `--disposable-vm` flag. Router cleanup
requires empty apply, native firewall absence and address withdrawal before
removing the fixture ports and unit override. Peer cleanup stops the owned units
and removes its namespaces. These actions delete the temporary raw output.
Retain the procedure, revisions and verdict instead. This gate does not prove
power-loss durability, every possible interruption point, IPv6 reboot, or a
cross-version cutback; those remain separate checks.

## Cross-version cold cutover and cutback

Use the same two-VM fixture for this separate gate. Build both the pinned 1.0
baseline and the candidate with `make dist`; verify the archive checksums before
extracting them on the router. Keep the peer VM and its DHCP process running.
This is a planned outage with lease reacquisition, not a hitless upgrade. Do not
reuse a 1.0 state database in 1.5 or assume a binary-only downgrade is sufficient.

1. On 1.0, validate the fixture configuration, require Healthy status without
   controller errors, and run the peer's `probe` and `isolation` roles. Record the
   version, boot ID, peer DHCP PID, address and a timer renewal.
2. Stop `routerd`, then `routerd-dnsmasq` and `routerd-dns-resolver@lan`. Save the
   stopped `/var/lib/routerd` directory, `/usr/local/etc/routerd`, generated
   `/etc/systemd/system/routerd.service` and its adjacent
   `routerd.service.routerd-activation.json` receipt as one 1.0 recovery set.
   Keep the external fixture boot unit and management override in place.
3. Select a fresh `/var/lib/routerd` directory and the validated 1.5 configuration.
   From the extracted candidate archive, run
   `sudo ./install.sh --no-install-deps --no-restart --no-config-update`.
   Require successful unit regeneration before boot, both `CAP_SYS_PTRACE` and
   `CAP_KILL` in its capability set, and an inactive service. Reboot the router.
4. Require the expected version and a changed router boot ID, systemd `running`,
   Healthy status and no controller errors. Repeat both peer roles and wait for
   an actual DHCP timer renewal after any reacquisition. The peer boot ID and
   DHCP PID must be unchanged. Inspect the current boot for helper ownership or
   permission errors. A responsive data path alone does not pass this gate.
5. For cutback, stop the same services, save the stopped 1.5 recovery set, and
   restore the saved 1.0 configuration and state directories. Install the exact
   baseline archive with the same no-restart options. Restore its saved unit and
   activation receipt, run `sudo systemctl daemon-reload`, then reboot. Repeat
   all step 4 checks against the 1.0 version and verify the restored canonical
   configuration equals the saved file.
6. Restore the stopped 1.5 configuration/state with its matching candidate
   package and repeat the cold boot and peer checks. Only after all gates pass,
   run the fixture cleanup roles and remove this campaign's temporary archives,
   snapshots and raw output. Keep source revisions, the procedure and verdict.

Stop on the first failed gate and retain only the working recovery set and
failure data needed to reproduce it locally. Repair and qualify a new package
before restarting the failed gate; do not repair generated unit permissions by
hand to turn the candidate into PASS. This fixture qualifies IPv4 DHCP/DNS,
NAT and zone isolation; IPv6, WAN protocol modes and other 1.0 functions need
their own cutover coverage.
