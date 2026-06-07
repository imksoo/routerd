---
title: LAN 側サービス
sidebar_position: 5
---

# LAN 側サービス

![LAN address、DHCPv4/DHCPv6、router advertisement、local DNS、lease event、client option を扱う LAN-side routerd services](/img/diagrams/tutorial-lan-side-services.png)

このページでは、ルーターの LAN 側を扱う routerd リソースを紹介します。
LAN 側のリソースは、内側インターフェースのアドレス、DHCPv4 / DHCPv6 の配布、IPv6 Router Advertisement、ローカル DNS リゾルバといった役割を担います。

WAN 側 (上流からのアドレス取得) は [WAN 側サービス](./wan-side-services.md) を参照してください。

## サービス分担

routerd は LAN 側サービスを 2 つのデーモンに明確に分けます。

- **dnsmasq** が DHCPv4、DHCPv6、DHCP relay、IPv6 Router Advertisement を担当します。
- **`routerd-dns-resolver`** が DNS ゾーン、条件付き転送、キャッシュ、クエリログを担当します。

実績のある dnsmasq をそのまま DHCP に使い、DNS のポリシーは型付き routerd リソース (`DNSResolver`、`DNSZone`) で表現する、という分担です。

## 一覧

| 役割 | リソース | 担当デーモン |
| --- | --- | --- |
| LAN インターフェースのアドレス | `IPv4StaticAddress`、`IPv6DelegatedAddress` | (kernel) |
| DHCPv4 スコープ | `DHCPv4Server` | dnsmasq |
| DHCPv4 固定割り当て | `DHCPv4Reservation` | dnsmasq |
| DHCPv6 (stateless / stateful) | `DHCPv6Server` | dnsmasq |
| IPv6 Router Advertisement | `IPv6RouterAdvertisement` | dnsmasq (RA mode) |
| LAN 側の時刻サーバー広告 | `DHCPv4Server`、`DHCPv6Server` | dnsmasq |
| DNS ゾーン (ローカル権威) | `DNSZone` | `routerd-dns-resolver` |
| DNS リゾルバの待ち受け | `DNSResolver` | `routerd-dns-resolver` |
| DHCP リースイベントの中継 | (組み込み) | `routerd-dhcp-event-relay` |

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
    domainFrom:
      resource: DNSZone/lan
      field: zone
    stickyHoldDays: 3
```

自動割り当てするクライアント用と固定アドレス用で範囲を分けると、運用が読みやすくなります。
`stickyHoldDays` は任意の項目です。0 より大きい値を指定すると、routerd は DHCP リース履歴を短期間保持し、リースの解放または期限切れの後に一時的な dnsmasq の `dhcp-host` hold を生成します。同じ MAC は hold 期間内に同じアドレスを再取得でき、そのアドレスはすぐには別のクライアントへ割り当てられません。

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
Web 管理画面と event log には、デバイスの現在の IP に依存しない安定したリソース名で現れます。

FreeBSD では、dnsmasq のリースファイルを `/var/db/routerd/dnsmasq` 配下に置きます。
`/var/run` だけに置くと、再起動でリースが失われるためです。
rc.d スクリプトは、起動前にランタイムディレクトリとリースディレクトリを作成します。
`routerctl apply` は、dnsmasq を再起動する前に `dnsmasq --test` を実行します。
あわせて、DHCP、DHCPv6、RA、DNS に必要な pf の穴も自動で生成します。

## IPv6 RA と DHCPv6

IPv6 LAN では、Router Advertisement に RDNSS を含めて配布してください。
Android は DHCPv6 で DNS を取得しないため、RDNSS が必要です。
Windows クライアントには、加えて DHCPv6 stateless サーバーも用意します。

Router Advertisement には、標準の NTP サーバー広告がありません。
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
    dnsslFrom:
      - resource: DNSZone/lan
        field: zone
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
    domainSearchFrom:
      - resource: DNSZone/lan
        field: zone
```

DHCPv6 でアドレス自体も配布したい場合は、`mode: stateful` または `mode: both` を使います。
LAN の DNS suffix を `DNSZone` に合わせたい場合は、`domainFrom`、`dnsslFrom`、`domainSearchFrom` を使います。
DHCPv4 の domain-name、RA の DNSSL、DHCPv6 の domain-search がいずれも同じローカルゾーンを参照するため、ドメイン文字列を重複して書かずに済みます。

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
      ddns: true
      ttl: 60
```

固定レコードは `records:` に、DHCP リース由来のレコードは `dhcpDerived.sources` に書きます。
両者は問い合わせ時に統合されます。
DHCP 由来の hostname が相対名の場合は DNSZone 自身の下に公開されるため、通常は `hostnameSuffix` を書く必要はありません。

## DNS リゾルバの待ち受け

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

リゾルバは、参照先の status から得られるすべてのアドレスで待ち受けます。
PD 更新などで IPv6 アドレスが増えても、再起動なしで追従します。

## 動作確認

```sh
# LAN インターフェースに v4 / v6 が乗っていることを確認
routerctl describe Interface/lan

# DHCP イベントをリアルタイムに tail
routerctl events --topic 'routerd.dhcp.lease.**' --resource DHCPv4Server/lan-dhcpv4

# ローカルリゾルバで名前解決
dig @<lan-ip> router.lan.example.org
dig @<lan-ip> example.com
```

## 運用上のヒント

- 最初は `routerctl plan` と `--dry-run` から始めます。本番の LAN 待ち受けを有効化するのは、管理経路と既知のロールバック経路を確保した後にしてください。
- dnsmasq のリースを手で書き換えた場合は、`routerd-dhcp-event-relay` を再起動してメモリ上の状態を追従させます。リースの変更は、できる限り routerd 経由で行ってください。
- 公共 DNS はフォールバックとして残してください。`routerd-dns-resolver` はヘルスチェックに失敗したフォワーダーを降格しますが、これは健全な代替がない場合に限ります。

## 関連項目

- [WAN 側サービス](./wan-side-services.md)
- [ローカル DNS ゾーン](../how-to/dns-local-zone.md)
- [専用 DNS upstream](../how-to/dns-private-upstream.md)
