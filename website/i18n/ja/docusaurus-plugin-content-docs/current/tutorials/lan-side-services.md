---
title: LAN 側サービス
sidebar_position: 3
---

# LAN 側サービス

routerd は LAN 側サービスを 2 つの境界に分けます。

- dnsmasq は DHCPv4、DHCPv6、DHCP 中継、IPv6 RA を提供します。
- `routerd-dns-resolver` は DNS ゾーン、転送、キャッシュ、ログを提供します。

DHCP リース処理は dnsmasq に寄せます。
DNS ポリシーは型付き routerd リソースで扱います。

## DHCPv4 スコープ

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    interface: lan
    addressPool:
      start: 172.18.1.64
      end: 172.18.1.191
      leaseTime: 12h
    gateway: 172.18.0.1
    dnsServers:
      - 172.18.0.1
    ntpServers:
      - 172.18.0.1
    domain: home.internal
```

自動割り当ての端末と固定割り当ての端末で範囲を分けると、運用で見分けやすくなります。

## DHCPv4 固定割り当て

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: panasonic-aiseg2
  spec:
    server: lan-dhcpv4
    macAddress: "18:ec:e7:33:12:6c"
    hostname: aiseg2
    ipAddress: 172.18.0.150
```

`DHCPv4Reservation` は dnsmasq の固定割り当て状態を生成します。
Web Console とイベントでも、端末に安定したリソース名を付けられます。

## IPv6 RA と DHCPv6

IPv6 LAN では、RA に RDNSS を載せることを基本にします。
Android は DHCPv6 で DNS を設定しません。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: lan-ra
  spec:
    interface: lan
    prefixFrom:
      resource: IPv6DelegatedAddress/lan-base
      field: address
    mFlag: false
    oFlag: true
    rdnss:
      - 172.18.0.1
    rdnssFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    dnssl:
      - home.internal
    mtu: 1454

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: lan-dhcpv6
  spec:
    interface: lan
    mode: stateless
    dnsServers:
      - 172.18.0.1
    domainSearch:
      - home.internal
```

DHCPv6 でアドレスも配る場合は、`mode: stateful` または `mode: both` を使います。

## ローカル DNS ゾーン

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: home
  spec:
    zone: home.internal
    ttl: 300
    records:
      - hostname: router
        ipv4: 172.18.0.1
        ipv6From:
          resource: IPv6DelegatedAddress/lan-base
          field: address
    dhcpDerived:
      sources:
        - DHCPv4Server/lan-dhcpv4
        - DHCPv6Server/lan-dhcpv6
      hostnameSuffix: home.internal
      ddns: true
      ttl: 60
```

`ipv4` と `ipv6` は固定値です。
ほかのリソース状態に追従させる場合は、`ipv4From` または `ipv6From` を使います。

## DNS リゾルバー待ち受け

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
        addressFrom:
          - resource: IPv6DelegatedAddress/lan-base
            field: address
        port: 53
        sources: [local-zone, default]
    sources:
      - name: local-zone
        kind: zone
        match:
          - home.internal
        zoneRef:
          - DNSZone/home
      - name: default
        kind: upstream
        match:
          - "."
        upstreams:
          - udp://1.1.1.1:53
          - udp://8.8.8.8:53
    cache:
      enabled: true
      maxEntries: 10000
```

最初は `routerd plan` と `--dry-run` で確認します。
実際の LAN 待ち受けは、管理経路と戻し方を確認してから有効にしてください。
