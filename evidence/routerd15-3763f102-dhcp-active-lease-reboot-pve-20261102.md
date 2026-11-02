# routerd 1.5 DHCPv4 active-lease PVE reboot qualification

Date: 2026-11-02

VM219 ran the Linux candidate from `3763f1025592b3b9b7ffa6aa361e0d87333e99c8` on PVE VLAN 3131. Independent VM224 acquired dynamic address `198.18.15.104`, gateway and DNS `198.18.15.1`, domain `home.test`, and a 120-second lease.

PVE then rebooted VM219 while that lease was active. After VM219 returned, `routerd` and `routerd-dnsmasq` were active and reported no current controller error. VM224 still held `198.18.15.104`, requested DHCP again, received the same address and options, and resolved `rebootclient.home.test` to `198.18.15.104` through the candidate resolver.

VM219, VM220, and VM224 were rolled back to their pre-test snapshots. VM219 and VM220 were restarted; VM224 was returned to its stopped VLAN-trunk configuration. This is scoped evidence only; WAN DHCP client behavior, interrupted apply, IPv6, FreeBSD, and whole-product parity remain unqualified.
