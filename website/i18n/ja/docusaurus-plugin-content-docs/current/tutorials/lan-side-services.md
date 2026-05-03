---
title: LAN 側サービス
sidebar_position: 3
---

# LAN 側サービス

LAN 側の DHCP と IPv6 RA は管理対象 dnsmasq インスタンスが、DNS は `routerd-dns-resolver` がそれぞれ提供します。
routerd は複数のリソースから dnsmasq 設定 (DHCP/RA 用) と DNS リゾルバー設定 (`DNSResolver`) を生成します。

## DHCPv4

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    interface: lan
    addressPool:
      start: 192.0.2.100
      end: 192.0.2.200
      leaseTime: 12h
    gateway: 192.0.2.1
    dnsServers:
      - 192.0.2.1
    domain: home.example
```

固定割り当ては `DHCPv4Reservation` で書きます。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: printer
  spec:
    macAddress: 02:00:00:00:10:10
    hostname: printer
    ipAddress: 192.0.2.20
```

## IPv6 RA と DHCPv6

IPv6 LAN では、RA の RDNSS を必ず考えます。
Android は DHCPv6 の DNS だけでは名前解決できないためです。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: lan-ra
  spec:
    interface: lan
    mFlag: false
    oFlag: true
    rdnss:
      - 2001:db8:1::1
    dnssl:
      - home.example

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: lan-dhcpv6
  spec:
    interface: lan
    mode: stateless
    dnsServers:
      - 2001:db8:1::1
```

## ローカル DNS

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: lan-zone
  spec:
    zone: home.example
    records:
      - hostname: router
        ipv4: 192.0.2.1
        ipv6: 2001:db8:1::1
    dhcpDerived:
      sources:
        - DHCPv4Server/lan-dhcpv4
      hostnameSuffix: home.example
      ddns: true
```

最初は予行実行で dnsmasq 設定と `DNSResolver` の設定を確認します。
実際に LAN 端末へ配るのは、管理経路と戻し方を確認してからにしてください。
