---
title: 専用 DNS 上流
slug: /how-to/dns-private-upstream
---

# 専用 DNS 上流

## 想定するシーン

ルーター上のリゾルバに、次のような振る舞いをさせたい場合です。

- アクセス網の内部ゾーン（例: ISP の AFTR FQDN、社内ドメイン）を、動的に学習した DNS サーバーへ転送する。
- ふだんは暗号化 DNS プロバイダー（DoH / DoT）を既定の上流として使う。
- 暗号化上流が不調になったら、平文 DNS にフォールバックする。
- プロバイダーのアカウント ID やプライベートなエンドポイントを、共有する example に書かない。

## routerd での解決方法

`DNSResolver` は `routerd-dns-resolver` を起動します。
デーモンは UDP/TCP で待ち受けます。リゾルバに属する `DNSForwarder` が match ルールを表し、`DNSUpstream` が再利用できる上流エンドポイントを表します。

| Scheme | 種別 | 既定ポート |
| --- | --- | --- |
| `https://` | DNS over HTTPS | URL 依存 |
| `tls://` | DNS over TLS | 853 |
| `udp://` | 平文の DNS over UDP | 53 |
| `tcp://` | 平文の DNS over TCP | 53 |

`DNSForwarder.spec.upstreams` の順序が優先度です。まず優先度が最も高く健全なものを試し、失敗したらリストを順に下ります。

`DNSUpstream.spec.addressFrom` を使うと、上流アドレスの一覧を別リソースの status から取れます。
DHCPv6 information-request で得た DNS サーバーを使うときの仕組みです。

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
    cache:
      enabled: true
      maxEntries: 10000
      minTTL: 60s
      maxTTL: 24h
      negativeTTL: 30s
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: local-zone
  spec:
    resolver: DNSResolver/lan-resolver
    match: [lan.example.org]
    zoneRefs: [DNSZone/lan]
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: access-network
  spec:
    resolver: DNSResolver/lan-resolver
    match: [transix.jp, corp.example.com]
    upstreams: [DNSUpstream/access-network]
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: access-network
  spec:
    protocol: udp
    addressFrom:
      - resource: DHCPv6Information/wan-info
        field: dnsServers
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: provider-bootstrap
  spec:
    resolver: DNSResolver/lan-resolver
    match: [dns.example-provider.net]
    upstreams: [DNSUpstream/cloudflare-udp, DNSUpstream/google-udp]
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSForwarder
  metadata:
    name: default
  spec:
    resolver: DNSResolver/lan-resolver
    match: ["."]
    upstreams: [DNSUpstream/provider-doh, DNSUpstream/provider-dot, DNSUpstream/cloudflare-udp]
    healthcheck:
      interval: 15s
      timeout: 3s
      failThreshold: 3
      passThreshold: 2
    dnssecValidate: true
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: provider-doh
  spec:
    protocol: doh
    address: dns.example-provider.net
    path: /dns-query
    sourceInterface: ens18
    bootstrap: [1.1.1.1, 2606:4700:4700::1111]
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: provider-dot
  spec:
    protocol: dot
    address: dns.example-provider.net
    sourceInterface: ens18
    bootstrap: [1.1.1.1, 2606:4700:4700::1111]
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: cloudflare-udp
  spec:
    protocol: udp
    address: 1.1.1.1
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: google-udp
  spec:
    protocol: udp
    address: 8.8.8.8
```

## プロバイダーの bootstrap

一部のプライベート DNS プロバイダーは、リゾルバのエンドポイントを、そのプロバイダー自身のドメインで配布します。
ホストがそのプロバイダー名を「プロバイダー本体を経由して」解決しようとすると、リゾルバが健全になる前にループするか、失敗します。

そのプロバイダーのドメインに対する条件付き source を作り、公衆リゾルバか access-network の DNS へ向けます。
プロバイダーのアカウント ID（例: profile ID）は、共有する example には書かず、ホストローカルの secrets ファイルやホストごとの YAML overlay にだけ置いてください。

`DNSUpstream.spec.bootstrap` は、同じ保護をより細かく行います。暗号化 transport を確立する前に、その上流エンドポイント名を解決するためのリゾルバを指定します。

## インターフェース束縛

`DNSUpstream.spec.sourceInterface` は、外向きの DNS クエリを Linux のインターフェース名に束縛します。
リテラルの OS インターフェース名（例: `ens18`）を使ってください。
そのインターフェースをほかのリソース（トンネルなど）が作る場合は、`ownerRefs` か順序で関係を宣言し、インターフェースができるまでリゾルバを pending にしてください。

FreeBSD には同等の `SO_BINDTODEVICE` の強制がないため、プラットフォーム固有のドキュメントでは同じ挙動を約束しないでください。

## 関連項目

- [ローカル DNS ゾーン](./dns-local-zone.md)
- [DNS リゾルバのコンセプト](../concepts/dns-resolver.md)
