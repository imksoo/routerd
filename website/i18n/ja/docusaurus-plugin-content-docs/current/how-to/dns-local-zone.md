---
title: ローカル DNS ゾーン
slug: /how-to/dns-local-zone
---

# ローカル DNS ゾーン

`DNSZone` はローカル権威レコードを保持します。
手動レコードと DHCP リース由来のレコードを併用できます。
`DNSResolver` は `zone` 応答元として `DNSZone` を参照します。

## 例

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

dnsmasq は DHCP スクリプトから `routerd-dhcp-event-relay` を呼びます。
routerd はリース変更をイベントバスへ発行します。
`routerd-dns-resolver` はそのイベントを受け、メモリー上のゾーン表を更新します。

起動時にはリースファイルも読みます。
これにより、デーモンを再起動しても A、AAAA、PTR レコードを復元できます。
