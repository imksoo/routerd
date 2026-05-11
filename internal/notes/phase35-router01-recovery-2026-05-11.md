# Phase 3.5 router01 recovery

Date: 2026-05-11
Host: router01 (FreeBSD, 192.168.123.120)

## Findings

- routerd binary was upgraded to v20260511.1428.
- The existing rc.d service still launched routerd without controller-chain flags.
- FreeBSD package installation failed while /etc/resolv.conf was empty, and that failure prevented rc.d script generation from continuing.
- DHCPv4Lease observed DNS servers but did not apply them to /etc/resolv.conf.

## Fixes

- DHCPv4Lease controller now writes /etc/resolv.conf when useDNS is enabled and DHCPv4 supplies DNS servers.
- FreeBSD apply now records package installation failures as warnings and continues with rc.d/network/pf rendering.

## Validation

- routerd rc.d script regenerated and routerd restarted manually after apply reported `service:routerd:restart-required`.
- routerd is running with controller-chain flags.
- routerd-dhcpv4-client child daemons are running for wan-dhcp4 and mgmt-dhcp4.
- WAN IPv4 address: 192.168.1.36/24 on vtnet0.
- IPv4 default route: 192.168.1.1 via vtnet0.
- /etc/resolv.conf is managed by routerd from DHCPv4Lease/wan-dhcp4 with 192.168.1.66 and 192.168.1.67.
- `ping -c 2 1.1.1.1`: 0% packet loss.
- `drill pkg.FreeBSD.org`: NOERROR.
- `pkg info -e dnsmasq`: installed.
- `service routerd_dnsmasq status`: running.
- `routerctl status --json`: phase Healthy, generation 3959, resourceCount 24.

## Remaining

- DHCPv6 PD is still not observed on router01. This was not part of P1-1 and remains a separate lab parity item if needed.
