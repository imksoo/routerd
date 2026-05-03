---
title: 暗号化 DNS 上流
slug: /how-to/dns-private-upstream
---

# 暗号化 DNS 上流

routerd は、ローカル DNS 代理を起動できます。
管理対象の dnsmasq は、LAN からの通常の DNS 問い合わせをローカル代理へ転送します。
代理は、設定された優先順で上流 DNS を選びます。

デーモン名は当面 `routerd-doh-proxy` のままです。
native backend は、次の URL 形式を扱います。

| URL 形式 | プロトコル | 既定ポート |
| --- | --- | --- |
| `https://` | DoH | URL に従います |
| `tls://` | DoT | 853 |
| `quic://` | DoQ | 853 |
| `udp://` | 平文 UDP DNS | 53 |

## 設定例

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DoHProxy
  metadata:
    name: public-dns
  spec:
    backend: native
    listenAddress: 127.0.0.1
    listenPort: 5053
    upstreams:
    - https://1.1.1.1/dns-query
    - tls://dns.google
    - quic://dns.google
    - udp://8.8.8.8:53
    healthcheck:
      interval: 15s
      timeout: 3s
      failThreshold: 3
      passThreshold: 2

- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolverUpstream
  metadata:
    name: default-resolver
  spec:
    zones:
    - zone: .
      servers:
      - type: doh
        proxyRef: public-dns
        listenAddress: 127.0.0.1
        listenPort: 5053
```

`spec.upstreams` の並び順が優先順位です。
routerd は、正常な上流のうち最も上にあるものを使います。
その上流が失敗すると、次の上流へ切り替えます。
定期確認で連続失敗すると、上流は停止扱いになります。
確認が連続成功すると、優先順位の候補へ戻ります。

## 注意

cloudflared 2026.2.0 では `proxy-dns` が削除されました。
新しい設定では `backend: native` を使います。
`cloudflared` と `dnscrypt` の外部 backend は、過去の実験用として API に残しています。
router05 のラボでは native backend を使います。

プロバイダー固有のアカウント識別子は、共有する設定例に書かないでください。
本番用の URL は、そのホストだけの YAML に書きます。
