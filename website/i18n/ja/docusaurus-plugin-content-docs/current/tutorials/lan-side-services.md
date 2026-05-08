---
title: LAN 側サービス
sidebar_position: 5
---

# LAN 側サービス

このページでは、ルーターの LAN 側を扱う routerd リソースを紹介します。
内側インターフェースのアドレス、DHCPv4 / DHCPv6 配布、IPv6 Router Advertisement、ローカル DNS resolver といった役割です。

WAN 側 (上流からのアドレス取得) は [WAN 側サービス](./wan-side-services.md) を参照してください。

## サービス分担

routerd は LAN 側サービスを 2 つのデーモンに明確に分割します。

- **dnsmasq** が DHCPv4、DHCPv6、DHCP relay、IPv6 Router Advertisement を担当。
- **`routerd-dns-resolver`** が DNS ゾーン、条件付き転送、キャッシュ、クエリログを担当。

DHCP は実績のある dnsmasq を再利用し、DNS のポリシーを型付き routerd リソース (`DNSResolver`、`DNSZone`) で表現する分担です。

## サマリ

| 役割 | リソース | 担当デーモン |
| --- | --- | --- |
| LAN インターフェースのアドレス | `IPv4StaticAddress`、`IPv6DelegatedAddress` | (kernel) |
| DHCPv4 スコープ | `DHCPv4Server` | dnsmasq |
| DHCPv4 固定割り当て | `DHCPv4Reservation` | dnsmasq |
| DHCPv6 (stateless / stateful) | `DHCPv6Server` | dnsmasq |
| IPv6 Router Advertisement | `IPv6RouterAdvertisement` | dnsmasq (RA mode) |
| LAN 側の時刻サーバー広告 | `DHCPv4Server`、`DHCPv6Server` | dnsmasq |
| DNS ゾーン (ローカル権威) | `DNSZone` | `routerd-dns-resolver` |
| DNS resolver listener | `DNSResolver` | `routerd-dns-resolver` |
| DHCP リースイベント中継 | (組み込み) | `routerd-dhcp-event-relay` |

## DHCPv4 スコープ

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    interface: lan
    addressPool:
      start: 192.0.2.64
      end: 192.0.2.191
      leaseTime: 12h
    gatewayFrom:
      resource: IPv4StaticAddress/lan-base
      field: address
    dnsServerFrom:
      - resource: IPv4StaticAddress/lan-base
        field: address
    ntpServerFrom:
      - resource: IPv4StaticAddress/lan-base
        field: address
    domain: lan.example.org
```

自動割り当てクライアント用と、固定アドレス用で範囲を分けると運用が読みやすくなります。

## 静的 DHCPv4 予約

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: smart-meter
  spec:
    server: lan-dhcpv4
    macAddress: "02:00:00:00:00:01"
    hostname: smart-meter
    ipAddress: 192.0.2.10
```

`DHCPv4Reservation` は dnsmasq の host reservation エントリに展開されます。
Web Console と event log には、デバイスの現在の IP に依存しない安定したリソース名で現れます。

FreeBSD では、dnsmasq のリースファイルを `/var/db/routerd/dnsmasq` 配下に置きます。
`/var/run` だけに置くと、再起動でリースが失われるためです。
rc.d スクリプトは、起動前にランタイムディレクトリとリースディレクトリを作成します。
`routerd apply` は、dnsmasq を再起動する前に `dnsmasq --test` を実行します。
また DHCP、DHCPv6、RA、DNS に必要な pf の穴も自動生成します。

## IPv6 RA と DHCPv6

IPv6 LAN では Router Advertisement に RDNSS を含めて配布してください。
Android は DHCPv6 で DNS を取得しないため、RDNSS が必要です。
Windows クライアントには加えて DHCPv6 stateless サーバーも用意します。

Router Advertisement には標準の NTP サーバー広告はありません。
ルーター自身を LAN の時刻参照先として配る場合は、DHCPv4 option 42 と DHCPv6 option 31 (SNTP) を使います。

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
    rdnssFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    dnssl:
      - lan.example.org
    mtu: 1454

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: lan-dhcpv6
  spec:
    interface: lan
    mode: stateless
    dnsServerFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    sntpServerFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    domainSearch:
      - lan.example.org
```

DHCPv6 でアドレス自体も配布したい場合は `mode: stateful` または `mode: both` を使います。

## ローカル DNS ゾーン

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: lan
  spec:
    zone: lan.example.org
    ttl: 300
    records:
      - hostname: router
        ipv4From:
          resource: IPv4StaticAddress/lan-base
          field: address
        ipv6From:
          resource: IPv6DelegatedAddress/lan-base
          field: address
    dhcpDerived:
      sources:
        - DHCPv4Server/lan-dhcpv4
        - DHCPv6Server/lan-dhcpv6
      hostnameSuffix: lan.example.org
      ddns: true
      ttl: 60
```

固定レコードは `records:`、DHCP リース由来は `dhcpDerived.sources` から。
両者は問い合わせ時に統合されます。

## DNS resolver の listener

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: lan-resolver
  spec:
    listen:
      - name: lan
        addressFrom:
          - resource: IPv4StaticAddress/lan-base
            field: address
          - resource: IPv6DelegatedAddress/lan-base
            field: address
        port: 53
        sources: [local-zone, default]
    sources:
      - name: local-zone
        kind: zone
        match:
          - lan.example.org
        zoneRef:
          - DNSZone/lan
      - name: default
        kind: upstream
        match:
          - "."
        upstreams:
          - https://dns.example.net/dns-query
          - udp://1.1.1.1:53
    cache:
      enabled: true
      maxEntries: 10000
```

resolver は参照先 status から得られる全アドレスで listen します。
PD 更新等で IPv6 アドレスが増えても、再起動なしで対応します。

## 動作確認

```sh
# LAN インターフェースに v4 / v6 が乗っていることを確認
routerctl describe Interface/lan

# DHCP イベントの実時間 tail
routerctl events --topic 'routerd.dhcp.lease.**' --resource DHCPv4Server/lan-dhcpv4

# ローカル resolver で名前解決
dig @<lan-ip> router.lan.example.org
dig @<lan-ip> example.com
```

## 運用上のヒント

- 最初は `routerctl plan` と `--dry-run` から始めます。本番 LAN listener を有効化するのは、管理経路と既知の rollback 経路を確保した後にしてください。
- dnsmasq のリースを手で書き換えた場合は、`routerd-dhcp-event-relay` を再起動して in-memory state を追従させます。可能な限り routerd 経由でリースを変更してください。
- 公共 DNS は fallback として残してください。`routerd-dns-resolver` は health check 失敗の forwarder を降格しますが、健全な代替がない場合に限ります。

## 関連項目

- [WAN 側サービス](./wan-side-services.md)
- [ローカル DNS ゾーン](../how-to/dns-local-zone.md)
- [専用 DNS upstream](../how-to/dns-private-upstream.md)
