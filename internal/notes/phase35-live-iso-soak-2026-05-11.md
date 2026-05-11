# Phase 3.5 Live ISO soak

Date: 2026-05-11

Scope: repeat the Live ISO router test on PVE and add a short continuous LAN-client soak. This is a compressed soak, not a 24-hour wall-clock run.

## Environment

- PVE node: `pve07`
- Test VM: `999` (`routerd-live-soak`)
- ISO: `/var/lib/vz/template/iso/routerd-live-v20260511.1240.iso`
- WAN bridge: `vmbr0`
- LAN bridge: `vmbr3`
- Serial log: `internal/notes/iso-soak-20260511.log`
- Client soak log: `internal/notes/iso-soak-client-20260511.log`

`vmbr490` is not present on `pve07`, so `vmbr3` was reused as the isolated LAN bridge.

## Wizard settings

- Router name: `routerd-live-soak`
- WAN interface: `eth0`
- WAN IPv4 mode: `dhcp`
- DNS fallback: `1.1.1.1`
- LAN interface: `eth1`
- LAN address: `192.168.99.1/24`
- LAN client CIDR: `192.168.99.0/24`
- DHCPv4 server: enabled
- DHCPv6: disabled
- RA: disabled
- DNS resolver: enabled
- NTP server: enabled
- Firewall: enabled
- NAT44: enabled
- Management placement: LAN
- USB persistence: disabled

After wizard apply:

```text
routerctl status: phase=Healthy generation=1 resourceCount=14
```

Runtime state inside the Live ISO VM:

```text
/run tmpfs: 293.6M total, 72.0K used
root tmpfs: 734.0M total, 176.4M used
routerd, routerd-dhcpv4-client, routerd-dns-resolver, dnsmasq running
```

## LAN client validation

A temporary Linux network namespace on `pve07` was attached to `vmbr3` through a veth pair.

The client received:

```text
inet 192.168.99.126/24
default via 192.168.99.1 dev vsoak-c
```

Initial checks:

```text
getent hosts www.google.com: resolved through 192.168.99.1
curl -4 https://www.google.com/generate_204: 204
```

Compressed soak:

```text
start=2026-05-11T16:51:12+09:00 end=2026-05-11T16:53:15+09:00 ok=60 fail=0
```

Each iteration performed an IPv4 HTTPS request through the Live ISO router and NAT44 path.

## Cleanup

- VM `999` was stopped and destroyed with purge enabled.
- The temporary client namespace was deleted.
- The veth pair was deleted.

No persistent PVE VM was left behind.
