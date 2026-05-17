---
title: LAN DHCP とローカル DNS
sidebar_position: 20
---

# LAN DHCP とローカル DNS

1 つの LAN interface を、小さな家庭内 LAN や検証 LAN の service segment として使う例です。
routerd が LAN address、DHCPv4、local DNS zone、DHCP lease 由来の名前を管理します。

完全な YAML は `examples/example-lan-dns-dhcp.yaml` にあります。

## 構成図

```mermaid
flowchart LR
  router["[1] routerd host<br/>192.168.30.1"]
  lan["[2] LAN<br/>home.example"]
  dhcp["[3] DHCPv4 clients<br/>192.168.30.100-199"]
  nas["[4] NAS reservation<br/>192.168.30.10"]

  router --- lan
  lan --- dhcp
  lan --- nas
```

## 図の対応表

| 番号 | 意味 | 主な resource |
| --- | --- | --- |
| [1] | LAN の DNS 待受も兼ねる router address。 | `IPv4StaticAddress/lan-base`, `DNSResolver/lan-resolver` |
| [2] | DHCP search domain として配る local DNS zone。 | `DNSZone/home` |
| [3] | address と DNS 設定を受ける動的 client。 | `DHCPv4Server/lan-dhcpv4` |
| [4] | 固定 lease と名前を持つ基盤 host。 | `DHCPv4Reservation/nas`, `DNSZone/home` |

## この例で管理するもの

| 領域 | routerd resource |
| --- | --- |
| LAN address | `Interface/lan`, `IPv4StaticAddress/lan-base` |
| local name | `DNSZone/home` |
| resolver | `DNSResolver/lan-resolver` |
| DHCPv4 | `DHCPv4Server/lan-dhcpv4`, `DHCPv4Reservation/nas` |

## 要点

```yaml
# [2] router.home.example や nas.home.example の local zone。
- kind: DNSZone
  metadata:
    name: home
  spec:
    zone: home.example
    dhcpDerived:
      sources:
        - DHCPv4Server/lan-dhcpv4
      ddns: true

# [3] DHCP で router address を gateway / DNS として配る。
- kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    gatewayFrom:
      resource: IPv4StaticAddress/lan-base
      field: address
    dnsServerFrom:
      - resource: IPv4StaticAddress/lan-base
        field: address
    domainFrom:
      resource: DNSZone/home
      field: zone
```

## 確認

```bash
routerd validate --config examples/example-lan-dns-dhcp.yaml
routerd apply --config examples/example-lan-dns-dhcp.yaml --once --dry-run
routerctl describe DNSZone/home
routerctl describe DHCPv4Server/lan-dhcpv4
dig @192.168.30.1 router.home.example
```

## よく変えるところ

- `home.example` を自分の search domain に変える。
- NAS、printer、基盤機器は `DHCPv4Reservation` に足す。
- 一部 domain だけ private upstream に送りたい場合は `DNSResolver.spec.sources` を増やす。
