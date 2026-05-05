---
title: LAN services
sidebar_position: 3
---

# LAN services

routerd splits LAN service into two clear boundaries:

- dnsmasq provides DHCPv4, DHCPv6, DHCP relay, and IPv6 Router Advertisement.
- `routerd-dns-resolver` provides DNS zones, forwarding, cache, and logs.

This keeps DHCP lease handling close to dnsmasq while keeping DNS policy in
typed routerd resources.

## DHCPv4 scope

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    interface: lan
    addressPool:
      start: 172.18.1.64
      end: 172.18.1.191
      leaseTime: 12h
    gateway: 172.18.0.1
    dnsServers:
      - 172.18.0.1
    ntpServers:
      - 172.18.0.1
    domain: home.internal
```

Use a separate pool for automatic clients and keep a smaller range for
reserved devices if that makes operations clearer.

## Static DHCPv4 reservation

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: panasonic-aiseg2
  spec:
    server: lan-dhcpv4
    macAddress: "18:ec:e7:33:12:6c"
    hostname: aiseg2
    ipAddress: 172.18.0.150
```

`DHCPv4Reservation` renders to dnsmasq host reservation state. It also gives
the Web Console and events a stable resource name for the device.

## IPv6 RA and DHCPv6

IPv6 LANs should publish RDNSS in Router Advertisement. Android does not use
DHCPv6 for DNS configuration.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: lan-ra
  spec:
    interface: lan
    prefixFrom:
      resource: IPv6DelegatedAddress/lan-base
      field: address
    mFlag: false
    oFlag: true
    rdnss:
      - 172.18.0.1
    rdnssFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    dnssl:
      - home.internal
    mtu: 1454

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: lan-dhcpv6
  spec:
    interface: lan
    mode: stateless
    dnsServers:
      - 172.18.0.1
    domainSearch:
      - home.internal
```

Use `mode: stateful` or `mode: both` when DHCPv6 address assignment is required.

## Local DNS zone

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: home
  spec:
    zone: home.internal
    ttl: 300
    records:
      - hostname: router
        ipv4: 172.18.0.1
        ipv6From:
          resource: IPv6DelegatedAddress/lan-base
          field: address
    dhcpDerived:
      sources:
        - DHCPv4Server/lan-dhcpv4
        - DHCPv6Server/lan-dhcpv6
      hostnameSuffix: home.internal
      ddns: true
      ttl: 60
```

The `ipv4` and `ipv6` fields are literals. Use `ipv4From` or `ipv6From` when a
record should follow another resource status.

## DNS resolver listener

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: lan-resolver
  spec:
    listen:
      - name: lan
        addresses:
          - 172.18.0.1
        addressFrom:
          - resource: IPv6DelegatedAddress/lan-base
            field: address
        port: 53
        sources: [local-zone, default]
    sources:
      - name: local-zone
        kind: zone
        match:
          - home.internal
        zoneRef:
          - DNSZone/home
      - name: default
        kind: upstream
        match:
          - "."
        upstreams:
          - udp://1.1.1.1:53
          - udp://8.8.8.8:53
    cache:
      enabled: true
      maxEntries: 10000
```

Start with `routerd plan` and `--dry-run`. Turn on the real LAN listener only
after the management path and rollback route are known.
