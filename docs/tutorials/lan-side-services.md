---
title: LAN-side services
sidebar_position: 3
---

# LAN-side services

After [First router](./first-router) the host has WAN connectivity and a
LAN address. This page adds the services LAN clients expect: DHCPv4
leases, DNS resolution, and IPv6 router advertisements. routerd renders
this through a managed `dnsmasq` instance described by an
`IPv4DHCPServer` + `IPv4DHCPScope` (and the IPv6 equivalents).

## What we add

- An `IPv4DHCPServer` resource that brings up a dnsmasq instance bound
  to the LAN, with DNS forwarding to the WAN's upstream resolver.
- An `IPv4DHCPScope` that gives LAN clients a lease range and gateway.
- Optional IPv6: `IPv6PrefixDelegation` to ask the upstream for a
  prefix, `IPv6DelegatedAddress` to put a `/64` on the LAN, and
  `IPv6DHCPServer` + `IPv6DHCPScope` for stateless DHCPv6 / RA.

## 1. Add the IPv4 DHCP server and scope

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4DHCPServer
      metadata:
        name: dhcp4
      spec:
        server: dnsmasq
        managed: true
        listenInterfaces:
          - lan
        dns:
          enabled: true
          upstreamSource: dhcp4
          upstreamInterface: wan
          cacheSize: 1000

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4DHCPScope
      metadata:
        name: lan-dhcp4
      spec:
        server: dhcp4
        interface: lan
        rangeStart: 192.168.10.100
        rangeEnd: 192.168.10.199
        leaseTime: 12h
        routerSource: interfaceAddress
        dnsSource: self
        authoritative: true
```

What this gives LAN clients:

- A DHCPv4 lease in `192.168.10.100–199` with `192.168.10.1` as the
  gateway (because `routerSource: interfaceAddress` advertises the
  router's LAN address).
- DNS resolution at `192.168.10.1`, forwarded toward the resolver
  learned through DHCPv4 on the WAN (`upstreamSource: dhcp4`).

## 2. Apply and verify

```bash
sudo routerd apply --once \
  --config /usr/local/etc/routerd/router.yaml
```

Verify dnsmasq is running and answering:

```bash
sudo systemctl status routerd-dnsmasq-dhcp4.service
ss -lntu | grep -E ':(53|67)\b'
```

From a LAN client:

```bash
# Should get a DHCP lease in 192.168.10.100-199
sudo dhclient -v <lan-iface>

# DNS query through the router
dig @192.168.10.1 example.com
```

## 3. Add IPv6 (optional, requires upstream PD)

If your upstream offers IPv6 prefix delegation (most fiber ISPs do),
extend the LAN with an IPv6 prefix:

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv6PrefixDelegation
      metadata:
        name: wan-pd
      spec:
        interface: wan
        client: networkd
        prefixLength: 60

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv6DelegatedAddress
      metadata:
        name: lan-ipv6
      spec:
        prefixDelegation: wan-pd
        interface: lan
        subnetID: "0"
        addressSuffix: "::1"
        announce: true
```

This requests a `/60` from the upstream, then assigns the first `/64`
sub-prefix to the LAN with the host suffix `::1`. The `announce: true`
flag tells routerd to advertise the prefix to LAN clients via the
DHCPv6/RA path (next step).

If you're on Japanese fiber (NTT FLET'S), use a profile to pull the
right defaults:

```yaml
        profile: ntt-hgw-lan-pd
```

See the [FLET'S IPv6 setup how-to](../how-to/flets-ipv6-setup) for
details and the lab pitfalls specific to NTT.

## 4. Add IPv6 DHCP server and RA

```yaml
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv6DHCPServer
      metadata:
        name: dhcp6
      spec:
        server: dnsmasq
        managed: true
        listenInterfaces:
          - lan
        ra:
          enabled: true

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv6DHCPScope
      metadata:
        name: lan-dhcp6
      spec:
        server: dhcp6
        interface: lan
        mode: stateless
```

This makes dnsmasq announce the prefix on the LAN with RA and answer
stateless DHCPv6 requests for clients that need DNS over DHCPv6.

## 5. Verify IPv6 on the LAN

After apply, the LAN side should have an IPv6 address derived from the
delegated prefix:

```bash
ip -6 addr show ens19
# Expect a global /64 derived from the delegated prefix
```

LAN clients should pick up an IPv6 address by SLAAC.

## What this does not do yet

LAN clients can resolve names and get addresses, but their traffic does
not yet go anywhere useful — there's no NAT for IPv4 and no firewall.
That's the [next tutorial](./basic-firewall).

## Next

- [Basic firewall](./basic-firewall) — IPv4 NAT and a default-deny preset.
- [API reference: IPv4DHCPServer / Scope](../reference/api-v1alpha1#ipv4dhcpserver-and-ipv4dhcpscope).
- [API reference: IPv6PrefixDelegation](../reference/api-v1alpha1#ipv6prefixdelegation).
