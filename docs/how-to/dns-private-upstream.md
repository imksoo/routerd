---
title: Private DNS upstreams
slug: /how-to/dns-private-upstream
---

# Private DNS upstreams

`DNSResolver` runs `routerd-dns-resolver`.
The daemon listens on UDP and TCP.
It evaluates `spec.sources` in order.
The first matching source answers the query.

dnsmasq no longer serves DNS.
It remains the DHCP server, DHCP relay, and RA helper.

## Upstream protocols

| Scheme | Protocol | Default port |
| --- | --- | --- |
| `https://` | DNS over HTTPS | URL dependent |
| `tls://` | DNS over TLS | 853 |
| `quic://` | DNS over QUIC | 853 |
| `udp://` | Plain DNS over UDP | 53 |

The order in `upstreams` is the priority order.
routerd first tries the highest healthy upstream.
If it fails, the resolver tries the next upstream.

## Example

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: lan-resolver
  spec:
    listen:
    - name: lan
      addresses:
      - 192.168.160.5
      - 127.0.0.1
      port: 53
      sources:
      - local
      - ngn-aftr
      - default

    sources:
    - name: local
      kind: zone
      match:
      - lab.example
      zoneRef:
      - DNSZone/lan

    - name: ngn-aftr
      kind: forward
      match:
      - transix.jp
      upstreams:
      - ${DHCPv6Information/wan-info.status.dnsServers}

    - name: default
      kind: upstream
      match:
      - "."
      upstreams:
      - https://cloudflare-dns.com/dns-query
      - tls://dns.google
      - quic://dns.google
      - udp://8.8.8.8:53
      healthcheck:
        interval: 15s
        timeout: 3s
        failThreshold: 3
        passThreshold: 2
      dnssecValidate: true
      viaInterface: ${Interface/wan.status.ifname}
      bootstrapResolver:
      - 2606:4700:4700::1111

    cache:
      enabled: true
      maxEntries: 10000
      minTTL: 60s
      maxTTL: 24h
      negativeTTL: 30s
```

Do not put provider-specific account identifiers in shared examples.
Use production provider URLs only in the host-local YAML file.
