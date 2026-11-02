# DHCPv4 active lease: interrupted apply recovery on PVE

## Scope

- Executed revision: `59a55f8bf93031e5464b5bd7de3fdfaf362708ac`
- Candidate VM: PVE VM219
- Independent DHCP client VM: PVE VM224 on VLAN 3131
- Configuration transition: add the static record `interrupted.home.test -> 198.18.15.1` while preserving the existing DHCPv4, DNS, firewall, NAT44, and VLAN resources.

## Method

1. Installed the revision's Linux binaries in VM219 and applied the baseline VLAN configuration.
2. VM224 acquired `198.18.15.104` from the candidate DHCPv4 service. The observed lease was 120 seconds and the provided DNS server was `198.18.15.1`.
3. Started `routerctl apply -f /var/tmp/routerd-dhcp-interrupted.json --replace` in VM219.
4. After 50 milliseconds, injected `SIGKILL` into the main `routerd` process. This delay only creates the fault window; it is not an acceptance-time target.
5. Checked the client result, service restart, committed DNS state, independent client lease, DNS, and DHCP renewal.

## Observed result

- Before the injection, `routerd` was active with `MainPID=3088`, `NRestarts=0`. VM224 resolved `router.home.test` through `198.18.15.1`.
- The apply client wrote exit status `1` and reported `outcome is unconfirmed` with an HTTP EOF. It did not report a successful apply.
- Systemd restarted `routerd` once: `MainPID=3462`, `NRestarts=1`, and both `routerd` and `routerd-dnsmasq` became active.
- After recovery, `routerctl get DNSZone/home` contained only the original `router` record. Independent queries for `interrupted.home.test` returned `NXDOMAIN`, while `router.home.test` still returned `198.18.15.1`. The interrupted configuration was therefore not presented as committed.
- VM224 retained `198.18.15.104`. A forced DHCP renewal completed with the same address, gateway, DNS server, domain, and 120-second lease. The independent DNS query also succeeded after the renewal.
- The candidate continued listening on UDP port 67 at `rd15-lan`.

## Verdict and boundary

This is a passing scoped Linux/PVE recovery observation: an in-flight apply interruption was reported as unconfirmed, the previous committed configuration recovered, and an active independent DHCP lease renewed successfully. It does not qualify WAN DHCP client behavior, server identity changes, multiple WANs, or the complete DHCPv4 feature.
