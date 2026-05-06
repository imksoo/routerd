---
title: Local DNS zones
slug: /how-to/dns-local-zone
---

# Local DNS zones

## Scenario

You want internal hosts to be reachable by name without hand-maintaining `/etc/hosts` files on every device. Specifically:

- A handful of static records (router, NAS, printer).
- Automatic A / AAAA / PTR records for every device that takes a DHCP lease.
- Forward and reverse lookups working for those names.

## How routerd solves it

`DNSZone` stores local authoritative records for one DNS domain. It can combine **manual records** (declared in YAML) with **DHCP-derived records** (built from the lease database). `DNSResolver` reads `DNSZone` resources as one of its sources, so internal queries get answered locally and external queries fall through to the configured upstreams.

DHCP-derived records are kept in sync via the event bus: dnsmasq invokes `routerd-dhcp-event-relay` whenever a lease changes, the relay publishes a routerd event, and `routerd-dns-resolver` updates its in-memory zone tables. The dnsmasq lease file is also re-read at startup so records survive a daemon restart.

## Example

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: lan
  spec:
    zone: lan.example.org
    ttl: 300
    dnssec:
      enabled: false
    records:
      - hostname: router
        ipv4: 192.0.2.1
        ipv6: 2001:db8:1::1
      - hostname: nas
        ipv4: 192.0.2.10
    dhcpDerived:
      sources:
        - DHCPv4Server/lan-dhcpv4
        - DHCPv6Server/lan-dhcpv6
      hostnameSuffix: lan.example.org
      ddns: true
      ttl: 60
      leaseFile: /run/routerd/dnsmasq.leases
    reverseZones:
      - name: 2.0.192.in-addr.arpa
```

After this resource is applied, queries for `nas.lan.example.org` and `<dhcp-client-name>.lan.example.org` resolve to local addresses, and PTR lookups for `192.0.2.x` return the same names.

## Notes

- Choose a domain you control or one that is reserved for internal use (`example.org`, `home.arpa`, etc.). Do not invent suffixes that collide with the public DNS namespace (such as `.lan`).
- If you keep DNSSEC enabled (`dnssec.enabled: true`), upstream DNSSEC validation continues to work for external queries; the local zone is unsigned by design.
- For multiple internal subnets, declare one `reverseZones` entry per subnet so PTR lookups work in both directions.

## See also

- [Private DNS upstreams](./dns-private-upstream.md)
- [DNS resolver concept](../concepts/dns-resolver.md)
