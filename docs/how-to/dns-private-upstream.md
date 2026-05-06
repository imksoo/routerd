---
title: Private DNS upstreams
slug: /how-to/dns-private-upstream
---

# Private DNS upstreams

## Scenario

You want the resolver on the router to:

- Forward queries for the access network's internal zones (e.g. an ISP's AFTR FQDN, an enterprise intranet domain) to a specific DNS server learned dynamically.
- Use a private encrypted DNS provider (DoH / DoT / DoQ) as the default upstream.
- Keep a fast plain-DNS fallback if the encrypted upstream becomes unhealthy.
- Avoid exposing provider account IDs or private endpoints in shared examples.

## How routerd solves it

`DNSResolver` runs `routerd-dns-resolver`. The daemon listens on UDP/TCP and evaluates `spec.sources` in order for each listen profile. Sources can be `zone` (local), `forward` (forward queries that match a domain pattern), or `upstream` (regular forwarder for the rest).

| Scheme | Protocol | Default port |
| --- | --- | --- |
| `https://` | DNS over HTTPS | URL-dependent |
| `tls://` | DNS over TLS | 853 |
| `quic://` | DNS over QUIC | 853 |
| `udp://` | Plain DNS over UDP | 53 |

The order in `upstreams` is the priority order. routerd tries the highest-priority upstream that is healthy, and falls back through the list when one fails.

`upstreamFrom` (instead of `upstreams`) lets the upstream list come from another resource's status. This is the mechanism for using DNS servers learned through DHCPv6 information request.

## Conditional forwarding example

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: lan-resolver
  spec:
    listen:
      - name: lan
        addresses:
          - 192.0.2.1
        port: 53
        sources:
          - local-zone
          - access-network
          - provider-bootstrap
          - default

    sources:
      - name: local-zone
        kind: zone
        match:
          - lan.example.org
        zoneRef:
          - DNSZone/lan

      - name: access-network
        kind: forward
        match:
          - transix.jp
          - corp.example.com
        upstreamFrom:
          - resource: DHCPv6Information/wan-info
            field: dnsServers

      - name: provider-bootstrap
        kind: forward
        match:
          - dns.example-provider.net
        upstreams:
          - udp://1.1.1.1:53
          - udp://8.8.8.8:53

      - name: default
        kind: upstream
        match:
          - "."
        upstreams:
          - https://dns.example-provider.net/dns-query
          - tls://dns.example-provider.net
          - quic://dns.example-provider.net
          - udp://1.1.1.1:53
        healthcheck:
          interval: 15s
          timeout: 3s
          failThreshold: 3
          passThreshold: 2
        dnssecValidate: true
        viaInterface: ens18
        bootstrapResolver:
          - 1.1.1.1
          - 2606:4700:4700::1111

    cache:
      enabled: true
      maxEntries: 10000
      minTTL: 60s
      maxTTL: 24h
      negativeTTL: 30s
```

## Provider bootstrap

Some private DNS providers serve their resolver endpoint from a domain that the resolver itself is going to use. If a host tries to resolve that provider name through the provider, the query loops or fails before the resolver is healthy.

Add a conditional source for the provider domain that points to a public resolver or to access-network DNS. Keep account IDs (e.g. provider profile IDs) out of shared examples; put them only in your local secrets file or in a per-host YAML overlay.

The `bootstrapResolver` field on a source provides the same protection at a finer granularity: it specifies which resolvers to use for resolving the upstream URL itself, before the encrypted transport is established.

## Interface binding

`sources[].viaInterface` binds outgoing DNS queries to a specific Linux interface name. Use a literal OS interface name such as `ens18`. If the interface is created by another resource (e.g. a tunnel), declare the relationship with `ownerRefs` or resource ordering and keep the resolver pending until the interface exists.

FreeBSD does not currently provide the same `SO_BINDTODEVICE` enforcement, so platform-specific docs do not promise identical behaviour there.

## See also

- [Local DNS zones](./dns-local-zone.md)
- [DNS resolver concept](../concepts/dns-resolver.md)
