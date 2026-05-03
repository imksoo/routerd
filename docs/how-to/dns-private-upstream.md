---
title: Private DNS upstreams
slug: /how-to/dns-private-upstream
---

# Private DNS upstreams

routerd can run a local DNS proxy for encrypted and fallback upstreams.
The managed dnsmasq instance forwards ordinary LAN DNS traffic to the local
proxy address. The proxy then selects an upstream by priority.

The daemon name is still `routerd-doh-proxy`.
The native backend now supports four URL schemes:

| Scheme | Protocol | Default port |
| --- | --- | --- |
| `https://` | DNS over HTTPS | URL dependent |
| `tls://` | DNS over TLS | 853 |
| `quic://` | DNS over QUIC | 853 |
| `udp://` | Plain DNS over UDP | 53 |

## Example

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DoHProxy
  metadata:
    name: public-dns
  spec:
    backend: native
    listenAddress: 127.0.0.1
    listenPort: 5053
    upstreams:
    - https://1.1.1.1/dns-query
    - tls://dns.google
    - quic://dns.google
    - udp://8.8.8.8:53
    healthcheck:
      interval: 15s
      timeout: 3s
      failThreshold: 3
      passThreshold: 2

- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolverUpstream
  metadata:
    name: default-resolver
  spec:
    zones:
    - zone: .
      servers:
      - type: doh
        proxyRef: public-dns
        listenAddress: 127.0.0.1
        listenPort: 5053
```

The order of `spec.upstreams` is the priority order.
routerd first tries the highest healthy upstream.
If that upstream fails, the proxy tries the next one.
Periodic probes mark an upstream down after repeated failures.
When probes succeed again, the upstream returns to the priority list.

## Notes

Cloudflared 2026.2.0 removed the `proxy-dns` command.
For new configurations, use `backend: native`.
The external `cloudflared` and `dnscrypt` backends remain in the API for older
experiments, but the native backend is the path used by the router05 lab.

Do not put provider-specific account identifiers in shared examples.
Use production provider URLs only in the host-local YAML file.
