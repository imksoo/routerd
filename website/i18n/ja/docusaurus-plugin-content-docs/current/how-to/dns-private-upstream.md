---
title: 専用 DNS 上流
slug: /how-to/dns-private-upstream
---

# 専用 DNS 上流

## 想定するシーン

ルーター上の resolver に次のような振る舞いを求めたい：

- アクセス網の内部ゾーン (例: ISP の AFTR FQDN、社内ドメイン) を、動的に学習した DNS サーバーへ転送する。
- 主に暗号化 DNS プロバイダー (DoH / DoT) を既定上流として使う。
- 暗号化上流が不調になったら平文 DNS にフォールバックする。
- プロバイダーアカウント ID やプライベートエンドポイントを共有 example に晒さない。

## routerd での解決方法

`DNSResolver` は `routerd-dns-resolver` を起動します。
daemon は UDP/TCP で待ち受けます。resolver に属する `DNSForwarder` が match rule を表し、`DNSUpstream` が再利用可能な上流 endpoint を表します。

| Scheme | 種別 | 既定ポート |
| --- | --- | --- |
| `https://` | DNS over HTTPS | URL 依存 |
| `tls://` | DNS over TLS | 853 |
| `udp://` | 平文 DNS over UDP | 53 |
| `tcp://` | 平文 DNS over TCP | 53 |

`DNSForwarder.spec.upstreams` の順序は優先度です。健全な最高優先度を試し、失敗したらリストを順に下ります。

`DNSUpstream.spec.addressFrom` を使うと、上流 address list を別リソースの status から取れます。
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

## プロバイダー bootstrap

一部の private DNS プロバイダーは、resolver endpoint をそのプロバイダー自身のドメインで配布します。
ホストがそのプロバイダー名を「プロバイダー本体経由」で解決しようとすると、resolver が健全になる前にループまたは失敗します。

そのプロバイダードメインに対する条件付き source を作り、公衆 resolver か access-network DNS に飛ばします。
プロバイダー account ID (例: profile ID) は共有 example に書かず、ホストローカルの secrets ファイルや per-host YAML overlay にだけ置いてください。

`DNSUpstream.spec.bootstrap` は同等の保護をより細かく行います：暗号化 transport を確立する前に、その上流 endpoint 名を解決するための resolver を指定します。

## インターフェース束縛

`DNSUpstream.spec.sourceInterface` は外向き DNS クエリを Linux のインターフェース名に bind します。
リテラルの OS インターフェース名 (例: `ens18`) を使ってください。
そのインターフェースが他のリソース (トンネル等) で作られる場合は、`ownerRefs` または順序で関係を宣言し、インターフェースが存在するまで resolver を pending にしてください。

FreeBSD は同等の `SO_BINDTODEVICE` 強制を持たないので、プラットフォーム固有 docs では同じ挙動を約束しないでください。

## 関連項目

- [ローカル DNS ゾーン](./dns-local-zone.md)
- [DNS resolver コンセプト](../concepts/dns-resolver.md)
