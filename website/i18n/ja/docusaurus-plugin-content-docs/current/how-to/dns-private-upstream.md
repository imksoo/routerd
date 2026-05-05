---
title: 専用 DNS 上流
slug: /how-to/dns-private-upstream
---

# 専用 DNS 上流

`DNSResolver` は `routerd-dns-resolver` を起動します。
このデーモンは UDP と TCP で待ち受けます。
待ち受けごとに、`spec.sources` を上から順に評価します。

dnsmasq は DNS を担当しません。
DHCPv4、DHCPv6、DHCP 中継、RA の補助に限定します。

## 上流プロトコル

| scheme | プロトコル | 既定ポート |
| --- | --- | --- |
| `https://` | DNS over HTTPS | URL に依存 |
| `tls://` | DNS over TLS | 853 |
| `quic://` | DNS over QUIC | 853 |
| `udp://` | 平文 DNS over UDP | 53 |

`upstreams` の順序が優先順です。
routerd は健康な上流のうち、優先度の高いものから試します。
失敗した場合は次の上流を試します。

上流一覧がほかのリソース状態から来る場合は、`upstreamFrom` を使います。
DHCPv6 情報要求で得たアクセス網 DNS サーバーを使う場合に重要です。

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
          - 172.18.0.1
        port: 53
        sources:
          - local-zone
          - ngn-aftr
          - private-provider-bootstrap
          - default

    sources:
      - name: local-zone
        kind: zone
        match:
          - home.internal
        zoneRef:
          - DNSZone/home

      - name: ngn-aftr
        kind: forward
        match:
          - transix.jp
        upstreamFrom:
          - resource: DHCPv6Information/wan-info
            field: dnsServers

      - name: private-provider-bootstrap
        kind: forward
        match:
          - example-dns-provider.test
        upstreams:
          - udp://1.1.1.1:53
          - udp://8.8.8.8:53

      - name: default
        kind: upstream
        match:
          - "."
        upstreams:
          - https://dns.example.net/dns-query
          - tls://dns.example.net
          - quic://dns.example.net
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

## プロバイダー名の補助解決

一部の専用 DNS プロバイダーでは、リゾルバーの接続先名が同じプロバイダーの
ドメインにあります。
ブラウザーやホストがその名前を同じプロバイダー経由で解決しようとすると、
失敗したり、望まない経路に入ったりすることがあります。

その場合は、プロバイダーのドメインを条件付き転送に追加します。
アクセス網の DNS サーバーや公開リゾルバーへ送ります。
プロバイダーのアカウント ID は、ホストローカルの YAML だけに書きます。
共有する設定例には commit しないでください。

## インターフェース指定

`sources[].viaInterface` は、Linux のインターフェース名へ送信を束縛します。
`ens18` のような OS インターフェース名を固定値で指定します。
インターフェース自体を別リソースで作る場合は、`ownerRefs` やリソース順序で
関係を表し、存在するまでリゾルバーを待機させます。

FreeBSD では Linux の `SO_BINDTODEVICE` と同じ動作を提供できません。
そのため、プラットフォーム固有の文書で同じ強制力を約束しないでください。
