---
title: プライベート DNS 上流
slug: /how-to/dns-private-upstream
---

# プライベート DNS 上流

`DNSResolver` は `routerd-dns-resolver` を起動します。
このデーモンは UDP と TCP で待ち受けます。
`spec.sources` を上から順に評価します。
最初に一致した応答元が問い合わせに応答します。

dnsmasq は DNS を配信しません。
dnsmasq は DHCP サーバー、DHCP 中継、RA を担当します。

## 上流プロトコル

| スキーム | プロトコル | 既定ポート |
| --- | --- | --- |
| `https://` | DNS over HTTPS | URL に依存します |
| `tls://` | DNS over TLS | 853 |
| `quic://` | DNS over QUIC | 853 |
| `udp://` | 平文 DNS over UDP | 53 |

`upstreams` の順序が優先順位です。
routerd は正常な上流のうち、もっとも優先度が高いものを使います。
失敗した場合は次の上流へ切り替えます。

## 例

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: lan-resolver
  spec:
    listen:
    - name: lan
      addresses:
      - 192.168.160.5
      - 127.0.0.1
      port: 53
      sources:
      - local
      - ngn-aftr
      - default

    sources:
    - name: local
      kind: zone
      match:
      - lab.example
      zoneRef:
      - DNSZone/lan

    - name: ngn-aftr
      kind: forward
      match:
      - transix.jp
      upstreams:
      - ${DHCPv6Information/wan-info.status.dnsServers}

    - name: default
      kind: upstream
      match:
      - "."
      upstreams:
      - https://cloudflare-dns.com/dns-query
      - tls://dns.google
      - quic://dns.google
      - udp://8.8.8.8:53
      healthcheck:
        interval: 15s
        timeout: 3s
        failThreshold: 3
        passThreshold: 2
      dnssecValidate: true
      viaInterface: ${Interface/wan.status.ifname}
      bootstrapResolver:
      - 2606:4700:4700::1111

    cache:
      enabled: true
      maxEntries: 10000
      minTTL: 60s
      maxTTL: 24h
      negativeTTL: 30s
```

共有する例には、プロバイダー固有のアカウント識別子を入れません。
本番用 URL は、各ホストのローカル YAML にだけ書きます。
