---
title: Isolate guest devices by MAC address
---

# Isolate guest devices by MAC address

## Scenario

You have one LAN segment, but some devices should be treated as guests. They
should receive DHCP leases and use router DNS or NTP, but they must not reach
private networks such as management, lab, or home server ranges.

## Define fixed leases

Use `DHCPv4Reservation` when you want a stable address and a readable name for
the device.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: aiseg2
  spec:
    server: lan-v4
    macAddress: "18:ec:e7:33:12:6c"
    hostname: aiseg2
    ipAddress: 172.18.0.150
```

## Include mode

In include mode, only listed MAC addresses become guests. This is the safer
home-router default because new devices remain trusted until you classify them.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-devices
  spec:
    mode: include
    interfaces:
      - Interface/lan
    guestServices:
      - dns
      - dhcp
      - ntp
    classification:
      - macAddress: "18:ec:e7:33:12:6c"
        as: guest
        name: aiseg2
        ipv4Reservation: aiseg2
```

## Exclude mode

In exclude mode, all clients on the target interfaces become guests unless they
are listed as trusted. This is useful for BYOD or apartment-style networks.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: byod-default-guest
  spec:
    mode: exclude
    interfaces:
      - Interface/lan
    classification:
      - macAddress: "bc:24:11:e0:8e:3a"
        as: trusted
        name: admin-laptop
```

## Private network deny list

When `guestEgressDeny` is omitted, routerd denies these destinations for guest
clients:

- `10.0.0.0/8`
- `172.16.0.0/12`
- `192.168.0.0/16`
- `fc00::/7`

Set `guestEgressAllow` for explicit exceptions. Allow rules are generated
before deny rules.

## Platform support

`ClientPolicy` requires Linux nftables. FreeBSD pf does not provide the same
MAC-based routed filtering model, so routerd reports the resource as
unsupported on FreeBSD.

See [examples/guest-mode.yaml](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)
for a complete configuration.
