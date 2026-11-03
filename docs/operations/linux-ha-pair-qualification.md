# Linux packaged two-router HA qualification

This is the next qualification design, not a passing result. Use clean dedicated
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
