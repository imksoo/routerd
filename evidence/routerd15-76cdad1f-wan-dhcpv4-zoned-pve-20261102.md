# Linux WAN DHCPv4 acquisition through zoned PVE VLANs

- Revision: `76cdad1fe671e58ed524b49a31ca41b70845ec13`.
- Environment: VM219 candidate and VM220 independent dnsmasq server on PVE VLAN 3132. VM219 used a new 1.5 configuration with LAN trust, WAN untrust, and the default firewall policy.
- Result: VM219 DHCPv4Client/wan bound `198.18.46.100/24`, gateway `198.18.46.1`, DNS `198.18.46.1`, and a 120-second lease from VM220.
- Security observation: the generated `wan_to_self` rule allowed only UDP destination port 68 on `rd15-wan`; its counter was 2 packets. The `wan_to_lan` deny counter remained 0.
- Boundary: this confirms acquisition and the DHCP firewall hole on an independent PVE peer. Renewal, rebind, expiry, server identity change, multi-WAN, forwarding, and recovery remain unqualified.
