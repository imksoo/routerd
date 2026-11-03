# Linux packaged two-router HA qualification

The basic lifecycle has a scoped result in the
[audit](../design/routerd15-audit.md); this procedure alone does not qualify the
shared-PD/four-AFTR combination below. Use clean dedicated
Ubuntu guests with the same formal routerd `.deb`; never use production routers.
The synthetic [fixture](../../tests/fixtures/linux-ha-pair.json) represents the
deployed combinations: non-preemptive VRRP, one election owning WAN/LAN VMACs,
gated DHCP/DNS/NAT, sticky leases, SSH lease synchronization, conntrackd and BGP.
It does not yet include the previously qualified shared PD/four-AFTR topology,
so passing this procedure alone is not full deployed-router equivalence.

The deployed pair retains unconditional SNAT rules on the standby. The fixture
models that condition with `always-peer-snat`: LAN traffic to `198.18.31.10`
uses the node's own WAN address, while traffic to the BGP-learned application
prefix uses the role-gated shared address. Make the application peer listen on
both addresses and check the independent unconditional path as well. Removing
this rule creates a separate zero-NAT-rules standby regression case; do not
confuse its result with qualification of the deployed condition. Initial results
from the older fixture did not include this unconditional rule.
For UDP, use a socket explicitly bound to each destination address. A wildcard
UDP listener can reply using the peer's connected-subnet address instead of the
request's destination; that invalidates the reverse NAT tuple. Certify both
application paths and require successful TCP and UDP sequences before injection.

## Environment certification

Before starting routerd, finish OS initialization, install package dependencies
and the independent peers (FRR, rsync/OpenSSH, busybox DHCP client). Verify
`dpkg -V routerd`, an inactive service, absent product config/state, management
access and recovery through the VM console. Keep management outside the fixture
LAN/WAN. Connect the guests through two isolated Ethernet segments; do not add
routes or change bridges on a production host. Test both segments and peer SSH
before qualification. Stop distribution FRR/conntrackd/keepalived services before
routerd takes ownership; do not repair the environment during qualification.
Make guest test links persistent and cold-boot both guests during certification,
so a later router power-cycle does not require repairing the test network.

The checked-in fixture describes node A. For B, change the LAN and WAN host
addresses from `.2` to `.3`, VRRP priority from 150 to 100 and its peer to `.2`,
the lease-sync target to `.2`, conntrackd local/peer addresses, and BGP router ID.
Bind `management` to each guest's actual management interface. Generate test-only
SSH keys locally in the guests, install peer public keys and use the fixture's
key path. Keep private keys and infrastructure addresses out of this repository.
Validate both complete configurations before starting either daemon.

Place independent FRR and application peers at `198.18.31.10` in a separate
network namespace or third VM. FRR ASN 64512 peers with both ASN 64500 routers,
advertises `10.250.0.0/24` and accepts `198.18.30.0/24`. Its kernel must provide
an application endpoint in the advertised prefix, so a received BGP route is
also checked with real traffic. Place a DHCP client on the LAN outside node A's
failure domain. Use a persistent TCP connection and a stable UDP tuple to the
independent peer. Record sequence numbers and source addresses at both ends;
successful new connections do not prove existing connection survival.

## Qualification and acceptance

1. Start A and wait for the real election and graceful VIP publication, then
   start B. Exactly one node must own the gateway and failover VMACs. B must
   remain backup with no competing DHCP server or shared NAT address.
2. Acquire a real DHCP lease. Verify the offered gateway/DNS, local DNS answer,
   sticky DB contents and peer DB contents after a reported successful sync.
   Read the DB through SQLite, not by checking file presence or rsync exit alone.
   Verify this again after another lease event while synchronization is running.
3. Observe both BGP sessions at the independent FRR peer, the permitted LAN
   advertisement, the learned prefix in both routers' kernel FIBs, and actual
   client traffic through that learned route. Verify an unpermitted prefix is
   absent. Confirm TCP/UDP reach the application with the shared NAT source.
4. Confirm the standby conntrackd external cache has the active TCP/UDP tuples,
   then power off only A using the disposable VM controller. Do not manually
   move VIPs, commit conntrack entries or restart a helper to make the test pass.
5. B must promote, publish the gateway and VMACs, activate DHCP/DNS/NAT and
   retain the client's lease. The same TCP socket and UDP tuple must resume
   delivery after the handover with the shared source. Check application
   sequence continuity, not a fixed arbitrary interruption threshold. BGP
   traffic must remain usable through B's established peer session.
6. Start A again and verify non-preemptive standby behavior, no competing shared
   addresses, reverse lease/session synchronization and new client service.
   Capture the WAN segment before A boots and inspect the whole startup interval
   for shared-address ARP announcements from A's physical MAC. A final backup
   status alone cannot exclude transient duplicate ownership during startup.
   Restart the active main daemon without manually changing native state and
   verify service/ownership recovery. Withdraw the configuration on both nodes:
   zero declared resources, no current controller errors and no owned helpers,
   VIPs or VMACs. Explicit service stop must persist.

A product failure ends this run: collect bounded diagnostics, reproduce the
specific failure locally and repair it before designing another native run.
Retain the reproducible procedure, source/package identity and scoped verdict;
remove successful-run raw logs, temporary snapshots, disposable guests and their
disks. Never change the ledger to “pass” until these paths actually execute.

Complete withdrawal stops conntrackd and removes its managed unit/configuration.
After stopping the final keepalived instance, routerd removes its declared VMACs
using the saved generated configuration and native type/parent/mode/MAC checks.
It retains that configuration if removal fails, permitting a later retry.
Backup role changes continue to retain their VMACs DOWN; declaration withdrawal
and a standby role are separate cases. A link name alone never authorizes deletion.

## Local unicast receive regression

Before repeating a failed native election, reproduce the address-order failure
with the actual renderer, kernel and installed independent keepalived. From the
repository root, on Linux with `ip` and `keepalived` installed:

```sh
go test -c -o /var/tmp/routerd-vrrp-render.test ./pkg/render
sudo unshare -n -m bash -euc '
  mount --make-rprivate /
  mount -t tmpfs tmpfs /run
  mkdir /run/netns
  exec env ROUTERD_RUN_NETNS=1 /var/tmp/routerd-vrrp-render.test \
    -test.run=TestKeepalivedUnicastNativeAddressOrder -test.v -test.timeout=35s
'
rm -- /var/tmp/routerd-vrrp-render.test
```

The opt-in test refuses the host network namespace. It creates two isolated
namespaces and runs a real independent active peer. A shared VIP precedes the
standby's own address; the fixture then withdraws that old VIP. After the native
master-down interval, the standby must still be BACKUP, have a receive socket
bound to its own address, and have no shared VIP. The component fixture omits
notify commands and owns that withdrawal stimulus; it does not claim to test the
VMAC/conntrackd hooks. The packaged pair procedure above covers those hooks.
An ordinary `go test` skips this explicit native test.

## Shared PD and four AFTRs: integrated qualification

The next input is [linux-ha-dslite-pd.json](../../tests/fixtures/linux-ha-dslite-pd.json).
It adds the deployed legacy LAN subnet, shared PD identity, four delegated
outer source addresses, tunnel-owned inner IPv4 endpoints and explicit SNAT, stateless DHCPv6,
RA, IPv6 upstream DNS and three routing policies. Keep the unconditional SNAT
rules and the 60-second Internet policy hysteresis. `datapathState: Ready` may still
be using the legacy fallback: observe the selected balanced candidate before
starting connections intended to prove DS-Lite continuity.

The 1.5 tunnel owns its standard `192.0.0.2` inner endpoint. Configure each
tunnel's SNAT address directly and omit redundant `IPv4StaticAddress` resources
on that same tunnel. Copying the former separate inner-address declarations
creates competing owners, and changing their prefix to `/32` duplicates the
endpoint exactly. The integrated input therefore has 66 resources; preparing
the full deployed configuration the same way reduces 109 declarations to 105
without removing any of its four tunnels or their SNAT forwarding functions.
The deployed former `.2`/`.3`/`.4`/`.5` inner source choices need no configuration
compatibility path: the prepared 1.5 configuration uses the standard endpoint
on each independent tunnel. LAN and AFTR addresses are unaffected.

Use a third disposable Linux VM outside both router failure domains. Connect
its `rd15-lan`/`rd15-wan` to the two isolated segments. Before product startup,
assign `198.18.29.1/24` and `198.18.30.10/24` to LAN, `198.18.31.10/24`,
`2001:db8:25::2/64` and `fe80::15:30ff:fe00:402/64` to WAN. Install FRR, radvd,
dnsmasq-base, busybox, isc-dhcp-client, Python, nftables and iproute2. Provide
an independently packaged Kea tree. From the repository root run, inside that
VM only:

```sh
sudo env ROUTERD_DISPOSABLE_PEER=yes bash scripts/linux-ha-dslite-peer.sh /opt/independent-kea
sudo systemd-run --unit=rd15-integrated-app --property=Type=exec \
  ip netns exec rd15-inet-server python3 "$PWD/scripts/linux-ha-traffic.py" \
  server /var/tmp/rd15-integrated-peer
```

The setup refuses a reused work directory and an active routerd service.
Certify the independent services and application replies before starting the
product. Four AFTR namespaces decapsulate into a separate Internet namespace
and apply their own IPv4 NAT; the server reports the public tuple it actually
receives. This exercises routed traffic through the AFTR, not an application
placed on its tunnel endpoint. The fixture MTUs (1430 underlay/1390 DS-Lite)
allow a guest overlay without changing a virtualization host's networking.
For an external/unmanaged physical WAN, certify the OS IPv6 RA client before
qualification; routerd's host IPv6 policy needs that interface's usable source.
If the test segments themselves use an IPv6 overlay, keep its transport outside
that policy with OS-declared routes/rules for the exact overlay peers. With
networkd, declare these rules in the fixture interface's `.network` file so
its lifecycle also preserves them; an ad hoc rule alone is insufficient. Otherwise
the policy can route the overlay's own packets back into the overlay: a local
capture sees a DHCPv6 solicit that never reaches Kea. Certify multicast from a
temporary macvlan through the complete overlay with the host policy present,
then remove that temporary stimulus before product startup. Do not change the
virtualization host or alter product behavior to repair test transport.
Make a failed certification terminate its caller before product startup. In a
shell, use `if command; then exit 1; fi` for a forbidden presence: `set -e` does
not make a standalone `! command` abort when that negation fails. A missing
precondition or failed client reset invalidates the run even if the product
later reaches a healthy status.
The deployed graceful-activation gate observes the policy's `datapathState`,
not `phase`: the latter is `Applied` in 1.5. Keep the observed field and its
value together when preparing a configuration. A candidate permitted in both
MASTER and BACKUP is a standby-capable fallback, not proof of convergence of a
master-only path. Keep the gateway withheld while only that fallback is ready:
otherwise the initial packets can overwrite imported DS-Lite conntrack marks
with a route incompatible with their preserved NAT mapping.

An interrupted native promotion must remain retryable. A runtime election
marker of `activating` records unfinished cache/VMAC/RA preparation; it does not
authorize master-gated resources or publication of the deferred gateway. The
controller reports an unknown role and retries the preparation while keepalived
is active. Only successful preparation publishes `master`. An authoritative
BACKUP notification supersedes pending activation, including queued repairs.
Exercise this locally with `go test ./cmd/routerd-vrrp-vmac ./pkg/controller/vrrp`
before a native campaign; it is not a native qualification result.

On the LAN client acquire a real DHCPv4 lease and SLAAC address/default route;
check the offered DNS through an independent stateless DHCPv6 client. On Ubuntu,
use the stock AppArmor-supported hook path `/etc/dhcp/dhclient-script`, lease
path `/var/lib/dhcp/dhclient6-rd15.leases` and PID path `/run/dhclient6-rd15.pid`;
a hook under a private temporary directory is denied. This information-request
probe may finish after its successful reply; it need not stay alive during a
router fault. Keep OS protection enabled. Each product DNS listener must include
both `local` and `upstream-test` in `sources` to authorize both query paths. Check
local DNS and forwarded A/AAAA answers over IPv4 and IPv6. Add test source
aliases `198.18.30.120/24` through `.183/24` to its `eth0`, then run:

```sh
sudo systemd-run --unit=rd15-integrated-traffic --property=Type=exec \
  ip netns exec rd15-ha-client python3 "$PWD/scripts/linux-ha-traffic.py" \
  client /var/tmp/rd15-integrated-peer
```

The probe owns eight persistent TCP sockets with the same source port across
different LAN addresses, 64 fixed UDP tuples sharing a source port, a BGP-path
TCP socket and a native IPv6 TCP socket. TCP exchanges 16 KiB in each direction;
there is no reconnect path. `traffic.json` records each flow's actual translated
tuple, last successful exchange, maximum interruption and any tuple change.
`accepts.jsonl` records application connections independently. Require all 74
flows to progress, no tuple changes, and the expected single accept per TCP
identity before and after faults. For initial DS-Lite flows, verify the public
sources belong to the three selected primary AFTRs rather than the legacy
fallback or fourth AFTR. Both peers must have usable synchronized leases and
conntrack entries before power loss.

Power off A only; require automatic B promotion and all original flows to
resume. Renew the LAN lease, return A as standby, inspect reverse v4/v6/PD
synchronization and absence of duplicate shared-address announcements. Restart
active B's main service while retaining helpers/flows, then power off B only
and require the same observations after automatic promotion of A. The third
VM's persistent application and DHCPv4 client processes must remain unchanged
throughout both fault tests.
Use the same withdrawal criteria as the basic lifecycle on both routers.
After cold return, an RA receipt from the previous boot must be retired before
opening a socket on a standby VMAC without a link-local source. It must not send
prefix withdrawal using the active peer's shared identity. Wait for both healthy
empty status and actual native effect removal within the bounded cleanup wait;
main-daemon stop alone is not confirmation of successful withdrawal.

Bound each observation wait (allow over 60 seconds for the configured routing
hysteresis) and the complete run to 30 minutes. Stop at a product failure;
retain only diagnostics needed for local reproduction. A passing setup or an
unexecuted phase is not an integrated passing result. Destroy this run's three
disposable VMs and disks after the campaign; retain the commands, input/source
identities and scoped outcome rather than successful-run raw logs.

## Native IPv6 RA route lifecycle regression

Query the unfiltered default-route listing: iproute2 omits the `dev` field when
that same field is a selector. Set the WAN VMAC's `ra_defrtr_metric=50` before
accepting RAs, so the kernel owns preference, refresh, withdrawal and expiry of
one RA route. Do not clone it into another userspace-programmed route: that
copy can expire while fresh RAs maintain a different metric and leave IPv6
forwarding stuck on the management default. The per-interface setting is
specified in the [kernel documentation](https://www.kernel.org/doc/html/v6.1/networking/ip-sysctl.html).

From the repository root, with iproute2, util-linux and independent radvd installed:

```sh
go test -c -o /var/tmp/routerd-vrrp-native.test ./cmd/routerd-vrrp-vmac
sudo unshare -n -m env ROUTERD_RUN_NETNS=1 /var/tmp/routerd-vrrp-native.test \
  -test.run=TestVMACNativeRARenewalWithdrawalAndExpiry -test.v -test.timeout=45s
rm -- /var/tmp/routerd-vrrp-native.test
```

The test refuses the initial network namespace, starts radvd in an independent
router namespace and checks kernel FIB selection beyond the first RA lifetime.
Graceful peer stop must withdraw the preferred default; abrupt peer loss must
let it expire. Both preserve the independent management default. A static route
fixture alone cannot establish these RA lifecycle semantics. This is a native
FIB component check; the integrated gate must still demonstrate the original
IPv6 application socket through failover.

Before router power loss, observe an actual same-prefix PD renewal while all
application sockets are already open. Keep the delegated address's confirmed
Applied observation during a still-valid lifetime refresh; publish the extended
lifetime only after the address update succeeds. A DS-Lite borrower may keep
using that shorter confirmed acquisition, with current native scope/index and
address readback, while the renewed lease still covers it. Do not confuse a
Pending refresh intent with loss of the existing address. Shortened authority,
withdrawal, foreign identity and expired acquisitions still require fencing.
Check the original flow tuples and tunnel identity after renewal, then inject
power loss; waiting for a new connection after renewal is insufficient.
