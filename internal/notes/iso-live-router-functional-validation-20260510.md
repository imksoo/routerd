# Live ISO router functional validation on PVE

Date: 2026-05-10

This note records the follow-up validation after the live ISO boot and wizard
screenshots were captured. The earlier validation proved that the ISO boots,
the wizard runs, `routerd apply` succeeds, and `routerctl status` reaches
`Healthy`. This run additionally proves that a LAN-side client can use the
configured router as a real IPv4 router.

## Environment

- PVE node: `pve07`
- Test VM: `999` (`routerd-live-router-test`)
- ISO: `/var/lib/vz/template/iso/routerd-live-v20260510.1811.iso`
- WAN bridge: `vmbr0`
- LAN bridge: `vmbr3`
- LAN client: temporary Linux network namespace on `pve07`
- Serial log: `internal/notes/iso-router-test-20260510-1842.log`
- Client-side functional log: `internal/notes/iso-live-router-functional-test-20260510.log`

`vmbr3` was used as the isolated LAN bridge because the requested `vmbr490`
bridge is not present on `pve07`.

## Wizard settings

- Router name: `routerd-live-router-test`
- WAN interface: `eth0`
- WAN IPv4 mode: `dhcp`
- DNS fallback: `1.1.1.1`
- LAN interface: `eth1`
- LAN address: `192.168.99.1/24`
- LAN client CIDR: `192.168.99.0/24`
- DHCPv4 server: enabled
- DHCPv4 pool: `192.168.99.100` - `192.168.99.200`
- DHCPv6: disabled
- RA: disabled
- DNS resolver: enabled
- NTP server: enabled
- Firewall: enabled
- NAT44: enabled
- Management placement: LAN
- USB persistence: disabled for this router-forwarding run

After `routerd apply`, `routerctl status` returned:

```text
phase=Healthy generation=1 resourceCount=14
```

## LAN client test

A temporary namespace was connected to `vmbr3` through a veth pair.

The client received:

```text
inet 192.168.99.186/24
default via 192.168.99.1 dev veth-rtest
```

DNS through routerd succeeded:

```text
dig @192.168.99.1 www.google.com A +short
142.251.156.119
...
```

External IPv4 connectivity through routerd NAT44 succeeded:

```text
curl -4 https://www.google.com/generate_204
http_code=204 remote_ip=142.251.156.119 time_total=0.024397
```

The Web Console and summary API were also reachable from the LAN client:

```text
http://192.168.99.1:8080/              -> HTTP 200
http://192.168.99.1:8080/api/v1/summary -> HTTP 200
```

## Cleanup

After validation:

- the temporary network namespace was deleted
- the veth pair was deleted
- VM `999` was stopped and destroyed with purge enabled

No persistent PVE VM was left behind.
