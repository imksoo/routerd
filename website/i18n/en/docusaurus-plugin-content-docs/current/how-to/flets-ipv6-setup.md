---
title: DS-Lite over DHCPv6-PD (IPv6-only access network)
slug: /how-to/flets-ipv6-setup
---

# DS-Lite over DHCPv6-PD (IPv6-only access network)

## Scenario

The ISP delivers an IPv6-only access network and provides IPv4 connectivity through a DS-Lite tunnel to an Address Family Transition Router (AFTR). The router needs to:

- Take an IPv6 prefix via DHCPv6-PD and use it to address the LAN.
- Establish a DS-Lite (IPv4-in-IPv6 / `ip6tnl`) tunnel to the AFTR.
- Resolve the AFTR FQDN through the access-network DNS, since AFTR records are usually only authoritative inside the carrier network.
- Advertise IPv6 with RDNSS so SLAAC clients (Android included) get a working DNS server.

This pattern is common with several Japanese fibre carriers (NTT NGN with `gw.transix.jp` and similar AFTRs), but the configuration applies to any DS-Lite deployment.

## Prerequisites

- The WAN-facing interface is connected to a residential gateway or ONU that runs an IPv6-only access network.
- DHCPv6-PD is available on that interface.
- The AFTR DNS records may or may not be returned in DHCPv6 information-request; always plan for both cases.

## DHCPv6-PD

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: wan-pd
  spec:
    interface: wan
```

The lease is persisted at:

```text
/var/lib/routerd/dhcpv6-client/wan-pd/lease.json
```

The daemon's status is available over its Unix socket:

```bash
curl --unix-socket /run/routerd/dhcpv6-client/wan-pd.sock http://unix/v1/status
```

## LAN address derivation and RA

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: lan-from-pd
  spec:
    interface: lan
    prefixDelegation: wan-pd
    dependsOn:
      - resource: DHCPv6PrefixDelegation/wan-pd
        phase: Bound
    addressSuffix: "::1"

- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: lan-ra
  spec:
    interface: lan
    prefixFrom:
      resource: IPv6DelegatedAddress/lan-from-pd
      field: address
    oFlag: true
    rdnssFrom:
      - resource: IPv6DelegatedAddress/lan-from-pd
        field: address
```

The advertised RDNSS uses the LAN-side address derived from the delegated prefix. SLAAC clients pick up that resolver automatically.

## Conditional DNS forwarding for the AFTR

The AFTR FQDN is normally only resolvable through the carrier's own DNS servers. Use a conditional forwarder so queries for that domain go to the access-network resolver, while everything else uses your normal upstream.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: resolver
  spec:
    listen:
      - name: local
        addresses: [127.0.0.1]
        port: 53
    sources:
      - name: aftr
        kind: forward
        match: [transix.jp]
        upstreams:
          - udp://[2404:8e00::feed:101]:53
      - name: default
        kind: upstream
        match: ["."]
        upstreams:
          - udp://1.1.1.1:53
```

Replace `transix.jp` and the upstream IPv6 address with the values your ISP publishes.

## DS-Lite tunnel

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: ds-lite
  spec:
    interface: wan
    tunnelName: ds-routerd
    localAddressSource: interface
    aftrFQDN: gw.transix.jp
    dependsOn:
      - resource: DNSResolver/resolver
        phase: Applied
```

`localAddressSource: interface` uses the WAN-side IPv6 address that SLAAC/RA gave you (rather than a delegated-prefix-derived one) as the local endpoint of the tunnel. That address is usually available before LAN derivation finishes, so the tunnel comes up sooner.

If your ISP publishes a stable AFTR address and you prefer to skip DNS resolution, set `aftrIPv6` directly:

```yaml
spec:
  aftrIPv6: 2001:db8:cafe::1
```

When AFTR is not returned in DHCPv6 information-request (common with NTT NGN HGWs), the static `aftrFQDN` or `aftrIPv6` configuration is the correct fallback.

For the tunnel inner IPv4 address, the normal DS-Lite example uses the RFC 6333 B4-AFTR link range `192.0.0.0/29`.
If you need to carve an address from a LAN range instead, keep the address in an `IPv4StaticAddress` resource and reference it from `DSLiteTunnel.localAddressFrom` and `NAT44Rule.snatAddressFrom`.
See `examples/dslite-lan-range-snat.yaml` for the optional form.

## Verification

```bash
routerd apply --config router.yaml --once --dry-run
routerctl status

ip -6 tunnel show
ip route show default
nft list table ip routerd_nat

# IPv4 reachability through the tunnel
curl --interface ds-routerd https://1.1.1.1/
```

Run the dry-run first; only apply for real once the plan looks right and a rollback path is in place.

## See also

- [WAN-side services](../tutorials/wan-side-services.md)
- [Multi-WAN egress](./multi-wan.md)
- [Path MTU and MSS clamping](../concepts/path-mtu.md)
