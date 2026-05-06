---
title: 専用 DNS 上流
slug: /how-to/dns-private-upstream
---

# 専用 DNS 上流

## 想定するシーン

ルーター上の resolver に次のような振る舞いを求めたい：

- アクセス網の内部ゾーン (例: ISP の AFTR FQDN、社内ドメイン) を、動的に学習した DNS サーバーへ転送する。
- 主に暗号化 DNS プロバイダー (DoH / DoT / DoQ) を既定上流として使う。
- 暗号化上流が不調になったら平文 DNS にフォールバックする。
- プロバイダーアカウント ID やプライベートエンドポイントを共有 example に晒さない。

## routerd での解決方法

`DNSResolver` は `routerd-dns-resolver` を起動します。
daemon は UDP/TCP で待ち受け、各 listen profile の `spec.sources` を順に評価します。
source は `zone` (ローカル権威)、`forward` (ドメインに一致するクエリを転送)、`upstream` (デフォルト forwarder) のいずれかです。

| Scheme | 種別 | 既定ポート |
| --- | --- | --- |
| `https://` | DNS over HTTPS | URL 依存 |
| `tls://` | DNS over TLS | 853 |
| `quic://` | DNS over QUIC | 853 |
| `udp://` | 平文 DNS over UDP | 53 |

`upstreams` の順序は優先度です。健全な最高優先度を試し、失敗したらリストを順に下ります。

`upstreamFrom` を使うと、上流リストを別リソースの status から取れます。
DHCPv6 information-request で得られた DNS サーバーを使うときの仕組みです。

## 条件付き転送の例

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: lan-resolver
  spec:
    listen:
      - name: lan
        addresses:
          - 192.0.2.1
        port: 53
        sources:
          - local-zone
          - access-network
          - provider-bootstrap
          - default

    sources:
      - name: local-zone
        kind: zone
        match:
          - lan.example.org
        zoneRef:
          - DNSZone/lan

      - name: access-network
        kind: forward
        match:
          - transix.jp
          - corp.example.com
        upstreamFrom:
          - resource: DHCPv6Information/wan-info
            field: dnsServers

      - name: provider-bootstrap
        kind: forward
        match:
          - dns.example-provider.net
        upstreams:
          - udp://1.1.1.1:53
          - udp://8.8.8.8:53

      - name: default
        kind: upstream
        match:
          - "."
        upstreams:
          - https://dns.example-provider.net/dns-query
          - tls://dns.example-provider.net
          - quic://dns.example-provider.net
          - udp://1.1.1.1:53
        healthcheck:
          interval: 15s
          timeout: 3s
          failThreshold: 3
          passThreshold: 2
        dnssecValidate: true
        viaInterface: ens18
        bootstrapResolver:
          - 1.1.1.1
          - 2606:4700:4700::1111

    cache:
      enabled: true
      maxEntries: 10000
      minTTL: 60s
      maxTTL: 24h
      negativeTTL: 30s
```

## プロバイダー bootstrap

一部の private DNS プロバイダーは、resolver endpoint をそのプロバイダー自身のドメインで配布します。
ホストがそのプロバイダー名を「プロバイダー本体経由」で解決しようとすると、resolver が健全になる前にループまたは失敗します。

そのプロバイダードメインに対する条件付き source を作り、公衆 resolver か access-network DNS に飛ばします。
プロバイダー account ID (例: profile ID) は共有 example に書かず、ホストローカルの secrets ファイルや per-host YAML overlay にだけ置いてください。

source の `bootstrapResolver` は同等の保護をより細かく行います：暗号化 transport を確立する前に、その上流 URL 自体を解決するための resolver を指定します。

## インターフェース束縛

`sources[].viaInterface` は外向き DNS クエリを Linux のインターフェース名に bind します。
リテラルの OS インターフェース名 (例: `ens18`) を使ってください。
そのインターフェースが他のリソース (トンネル等) で作られる場合は、`ownerRefs` または順序で関係を宣言し、インターフェースが存在するまで resolver を pending にしてください。

FreeBSD は同等の `SO_BINDTODEVICE` 強制を持たないので、プラットフォーム固有 docs では同じ挙動を約束しないでください。

## 関連項目

- [ローカル DNS ゾーン](./dns-local-zone.md)
- [DNS resolver コンセプト](../concepts/dns-resolver.md)
