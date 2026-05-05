---
title: DNS resolver
slug: /concepts/dns-resolver
---

# DNS resolver

Phase 2.0 splits routerd DNS into two resource kinds.

`DNSZone` owns local authoritative data.
It stores manual records and records derived from DHCP leases.

`DNSResolver` owns the daemon instance.
It defines listen addresses, source ordering, upstream selection, and cache policy.
One `DNSResolver` resource starts one `routerd-dns-resolver` process.

## Source ordering

`DNSResolver.spec.sources` is evaluated in order.
A `zone` source answers from `DNSZone`.
A `forward` source sends matching zones to a selected upstream.
An `upstream` source handles the default recursive path.

The resolver supports DoH, DoT, DoQ, and plain UDP DNS.
It tries upstreams by priority and falls back when a higher source fails.

## Multiple listen profiles

`spec.listen` is a list.
Each entry can select a subset of source names.
This allows LAN and VPN listeners to behave differently while sharing one resolver resource.

Use `listen[].addressFrom` when a listen address comes from another
resource status. This keeps the dependency explicit and lets the controller
reconfigure the daemon when the source resource changes.

```yaml
listen:
  - name: lan
    addresses:
      - 172.18.0.1
    addressFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    port: 53
```

If a required address source is not available yet, the resolver stays
`Pending(AddressUnresolved)` instead of starting with a stale address.

## Dynamic zone records

`DNSZone.spec.records[].ipv4` and `ipv6` are literal addresses.
Use `ipv4From` or `ipv6From` when a record address comes from another
resource status.

```yaml
records:
  - hostname: router
    ipv4From:
      resource: IPv4StaticAddress/lan-base
      field: address
    ipv6From:
      resource: IPv6DelegatedAddress/lan-base
      field: address
```

If a required record source is not available yet, the record field is marked in
`DNSZone.status.pendingRecords`. The resolver is regenerated when the source
resource changes, and the record is published after the field resolves.

## Network-constrained upstreams

`sources[].viaInterface` binds outgoing queries to a specific interface on Linux.
Use a literal OS interface name, for example `ens18` or `wg0`.
When a tunnel or VRF resource creates that interface, make the dependency
explicit with resource ownership or ordering and keep the resolver pending
until the interface exists.

`sources[].bootstrapResolver` supplies DNS server addresses for resolving DoH and DoT endpoint names.
This is useful when the endpoint name is only resolvable inside an access network.

Use `upstreamFrom` when the upstream server list comes from another resource
status.

```yaml
sources:
  - name: ngn-aftr
    kind: forward
    match:
      - transix.jp
    upstreamFrom:
      - resource: DHCPv6Information/wan-info
        field: dnsServers
```

## dnsmasq boundary

dnsmasq is now limited to DHCPv4, DHCPv6, DHCP relay, and RA.
It does not generate `server=`, `local=`, or `host-record=` lines.
All DNS answering and forwarding goes through `routerd-dns-resolver`.
