---
title: DHCPv6-PD and AFTR on NTT NGN-style access networks
---

# DHCPv6-PD and AFTR on NTT NGN-style access networks

![Diagram showing DHCPv6-PD and AFTR acquisition on NTT NGN-style access from prefix delegation and information request through carrier DNS AFTR resolution to DS-Lite tunnel, IPv4 route, NAT44, and LAN connectivity checks](/img/diagrams/knowledge-base-ntt-ngn-pd-acquisition.png)

Field notes for routers placed behind a residential gateway that exposes an NTT NGN-style (Japan IPv6 fibre) IPv6 access network. The same patterns apply to other carriers that combine DHCPv6-PD with a DS-Lite path to an in-network AFTR.

## DHCPv6-PD

`routerd-dhcpv6-client` obtains DHCPv6-PD reliably behind these residential gateways. There is no need for aggressive retries or unusual acquisition tricks; standard solicit / advertise / request / renew is enough.

What we have observed in steady state:

- Multiple routers behind the same RGW receive distinct prefixes (no collision).
- T1 / T2 renewals succeed indefinitely.
- The lease survives daemon restarts via `lease.json`.

## AFTR may not be returned in DHCPv6 information-request

For some RGW / ONU combinations, DHCPv6 information-request returns DNS, SNTP, and domain-search options but **not** the AFTR option. An empty AFTR field is normal in those cases.

For DS-Lite, supply the AFTR explicitly with one of:

- `DSLiteTunnel.spec.aftrIPv6` — pin the AFTR's IPv6 address directly.
- `DSLiteTunnel.spec.aftrFQDN` — let routerd resolve the FQDN through a known resolver.

## AFTR FQDN often needs conditional DNS forwarding

Carrier-managed AFTR FQDNs (for example `gw.transix.jp`) typically resolve only through the carrier's own DNS servers. Public resolvers may answer with NXDOMAIN.

In routerd, express that with a `forward` source on `DNSResolver`:

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
```

The DS-Lite controller resolves the AFTR FQDN through `routerd-dns-resolver`, not through the system stub resolver.

## DS-Lite end-to-end checklist

When a DS-Lite tunnel is fully working you should see:

- The conditional forwarder resolves the AFTR FQDN.
- An `ip6tnl` tunnel device exists.
- The IPv4 default route points into the tunnel.
- nftables NAT44 is in place for outbound IPv4 from the LAN.
- Outbound IPv4 (HTTP, ICMP) succeeds from a LAN client.

## Scope of these notes

These observations come from routerd evaluation labs that exercise the same RGW model the carrier ships. They are intended as guidance for similar deployments, not as a guarantee for every Japanese ISP plan or every RGW firmware revision. Treat them as a starting point for your own validation.
