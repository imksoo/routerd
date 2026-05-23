---
title: ローカル DNS ゾーン
slug: /how-to/dns-local-zone
---

# ローカル DNS ゾーン

## 想定するシーン

社内ホストを名前で解決したいが、各端末の `/etc/hosts` を手作業でそろえたくない場合です。具体的には次を実現したいときです。

- ひと握りの固定レコード（router、NAS、プリンタ）を持つ。
- DHCP リースを取得した端末ごとに、A / AAAA / PTR を自動生成する。
- 順引きと逆引きの両方を動かす。

## routerd での解決方法

`DNSZone` で、1 つの DNS ドメインのローカル権威レコードを管理します。
**手書きのレコード**（YAML で宣言）と、**DHCP 由来のレコード**（リース DB から構築）を組み合わせられます。
`DNSResolver` がこれらを応答元の 1 つとして読み込むため、内部の問い合わせはローカルで応答し、外部の問い合わせは設定済みの上流へ送ります。

DHCP 由来のレコードはイベントバスを経由して同期します。dnsmasq がリース変更で `routerd-dhcp-event-relay` を呼び、relay が routerd のイベントを発行し、`routerd-dns-resolver` がメモリ上のゾーンを更新します。
dnsmasq のリースファイルは起動時にも読み直すので、デーモンを再起動してもレコードは失われません。

## 例

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: lan
  spec:
    zone: lan.example.org
    ttl: 300
    dnssec:
      enabled: false
    records:
      - hostname: router
        ipv4: 192.0.2.1
        ipv6: 2001:db8:1::1
      - hostname: nas
        ipv4: 192.0.2.10
    dhcpDerived:
      sources:
        - DHCPv4Server/lan-dhcpv4
        - DHCPv6Server/lan-dhcpv6
      hostnameSuffix: lan.example.org
      ddns: true
      ttl: 60
      leaseFile: /run/routerd/dnsmasq.leases
    reverseZones:
      - name: 2.0.192.in-addr.arpa
```

これを適用すると、`nas.lan.example.org` や `<dhcp-client-name>.lan.example.org` がローカルアドレスに解決され、`192.0.2.x` の PTR ルックアップも同じ名前を返します。

## 補足

- 管理権を持つドメインか、社内向けに予約されたドメイン（`example.org`、`home.arpa` など）を選んでください。`.lan` のように公衆 DNS と衝突する suffix は使わないでください。
- DNSSEC を有効（`dnssec.enabled: true`）にしておけば、外部の DNSSEC 検証は引き続き動きます。ローカルゾーンは設計上 unsigned です。
- 内部サブネットが複数ある場合は、`reverseZones` をサブネットの数だけ書いてください。これで両方向の PTR が動きます。

## 関連項目

- [専用 DNS 上流](./dns-private-upstream.md)
- [DNS リゾルバのコンセプト](../concepts/dns-resolver.md)
