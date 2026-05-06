---
title: ローカル DNS ゾーン
slug: /how-to/dns-local-zone
---

# ローカル DNS ゾーン

## 想定するシーン

社内ホストを名前で解決したいが、各端末の `/etc/hosts` を手作業で揃えたくない。具体的には：

- 一握りの固定レコード (router、NAS、プリンタ)
- DHCP リースを取得した端末ごとの A / AAAA / PTR を自動生成
- 順引きと逆引きの両方が動く

## routerd での解決方法

`DNSZone` で 1 つの DNS ドメインのローカル権威レコードを管理します。
**手書きレコード** (YAML 中で宣言) と **DHCP 由来レコード** (リース DB から構築) を組み合わせられます。
`DNSResolver` がこれらをソースの 1 つとして読み込むため、内部問い合わせはローカルで応答し、外部問い合わせは設定済みの上流に送られます。

DHCP 由来レコードはイベントバス経由で同期されます：dnsmasq がリース変更で `routerd-dhcp-event-relay` を呼び、relay が routerd イベントを発行し、`routerd-dns-resolver` が in-memory ゾーンを更新します。
dnsmasq のリースファイルは起動時にも読み直すので、デーモン再起動でレコードは失われません。

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

これを apply すると、`nas.lan.example.org` や `<dhcp-client-name>.lan.example.org` がローカルアドレスに解決され、`192.0.2.x` の PTR ルックアップも同じ名前を返します。

## 補足

- 管理権を持つドメインか、社内向けに予約された (`example.org`、`home.arpa` 等) ものを選んでください。`.lan` のように公衆 DNS と衝突する suffix は使わないでください。
- DNSSEC を有効 (`dnssec.enabled: true`) にしておけば、外部 DNSSEC 検証は引き続き動きます。ローカルゾーンは設計上 unsigned です。
- 内部サブネットが複数ある場合は、`reverseZones` をサブネット数分書いてください。これで両方向の PTR が動きます。

## 関連項目

- [専用 DNS upstream](./dns-private-upstream.md)
- [DNS resolver コンセプト](../concepts/dns-resolver.md)
