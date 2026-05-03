---
title: Local DNS zones
slug: /how-to/dns-local-zone
---

# Local DNS zones

`DNSZone` stores local authoritative records.
It can combine manual records with records derived from DHCP leases.
The resolver reads these zones through `DNSResolver` sources.

## Example

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: lan
  spec:
    zone: lab.example
    ttl: 300
    dnssec:
      enabled: false
    records:
    - hostname: router
      ipv4: 192.168.160.5
      ipv6: 2001:db8:160::1
    dhcpDerived:
      sources:
      - DHCPv4Server/lan-dhcpv4
      - DHCPv6Server/lan-dhcpv6
      hostnameSuffix: lab.example
      ddns: true
      ttl: 60
      leaseFile: /run/routerd/dnsmasq.leases
    reverseZones:
    - name: 160.168.192.in-addr.arpa
```

dnsmasq calls `routerd-dhcp-event-relay` from its DHCP script hook.
routerd publishes the lease change on the event bus.
`routerd-dns-resolver` receives the event and updates the in-memory zone table.

The lease file is also read at startup.
This keeps local A, AAAA, and PTR records available after a daemon restart.
