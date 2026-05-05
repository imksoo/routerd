---
title: Private DNS upstreams
slug: /how-to/dns-private-upstream
---

# Private DNS upstreams

`DNSResolver` starts `routerd-dns-resolver`.
The daemon listens on UDP and TCP.
It evaluates `spec.sources` in order for each listen profile.

dnsmasq no longer serves DNS. It remains the DHCPv4, DHCPv6, DHCP relay, and RA
helper.

## Upstream protocols

| Scheme | Protocol | Default port |
| --- | --- | --- |
| `https://` | DNS over HTTPS | URL dependent |
| `tls://` | DNS over TLS | 853 |
| `quic://` | DNS over QUIC | 853 |
| `udp://` | Plain DNS over UDP | 53 |

The order in `upstreams` is the priority order. routerd tries the highest
healthy upstream first. If it fails, the resolver tries the next upstream.

Use `upstreamFrom` when an upstream list comes from another resource status.
This is important for access-network DNS servers learned through DHCPv6
information request.

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
          - 172.18.0.1
        port: 53
        sources:
          - local-zone
          - ngn-aftr
          - private-provider-bootstrap
          - default

    sources:
      - name: local-zone
        kind: zone
        match:
          - home.internal
        zoneRef:
          - DNSZone/home

      - name: ngn-aftr
        kind: forward
        match:
          - transix.jp
        upstreamFrom:
          - resource: DHCPv6Information/wan-info
            field: dnsServers

      - name: private-provider-bootstrap
        kind: forward
        match:
          - example-dns-provider.test
        upstreams:
          - udp://1.1.1.1:53
          - udp://8.8.8.8:53

      - name: default
        kind: upstream
        match:
          - "."
        upstreams:
          - https://dns.example.net/dns-query
          - tls://dns.example.net
          - quic://dns.example.net
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

Some private DNS providers serve the resolver endpoint from the same domain
that the resolver is going to use. If a browser or host tries to resolve that
provider name through the provider itself, the query can fail or loop through
an unwanted path.

Add a conditional source for the provider domain and send it to an access
network DNS server or a public resolver. Keep provider account IDs only in the
host-local YAML file. Do not commit them to shared examples.

## Interface binding

`sources[].viaInterface` binds outgoing queries to a Linux interface name.
Use a literal OS interface name such as `ens18`. If the interface itself is
created by another resource, declare that relationship with `ownerRefs` or
resource ordering and keep the resolver pending until the interface exists.

FreeBSD does not currently provide the same `SO_BINDTODEVICE` behavior, so
platform-specific docs should not promise identical enforcement there.
