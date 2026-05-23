---
title: 專用 DNS 上游
slug: /how-to/dns-private-upstream
---

# 專用 DNS 上游

## 適用情境

當您希望路由器上的解析器具備以下行為時：

- 將接取網路的內部區域（例如：ISP 的 AFTR FQDN、企業內部網域）轉送至動態學習到的 DNS 伺服器。
- 平時以加密 DNS 提供商（DoH / DoT）作為預設上游。
- 加密上游異常時，退回至明文 DNS。
- 不在共享範例中寫入提供商帳號 ID 或私有 endpoint。

## routerd 的解決方式

`DNSResolver` 會啟動 `routerd-dns-resolver` 常駐程式。
常駐程式監聽 UDP/TCP。隸屬於解析器的 `DNSForwarder` 表示 match 規則，`DNSUpstream` 表示可重複使用的上游 endpoint。

| Scheme | 類別 | 預設埠 |
| --- | --- | --- |
| `https://` | DNS over HTTPS | 依 URL 而定 |
| `tls://` | DNS over TLS | 853 |
| `udp://` | 明文 DNS over UDP | 53 |
| `tcp://` | 明文 DNS over TCP | 53 |

`DNSForwarder.spec.upstreams` 的順序即為優先順序。優先嘗試優先度最高且健康的上游，失敗後依序往下。

使用 `DNSUpstream.spec.addressFrom`，可從其他資源的 status 取得上游位址清單。
這是利用 DHCPv6 information-request 取得的 DNS 伺服器時所使用的機制。

## 條件式轉送範例

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

## 提供商的 bootstrap 處理

部分私有 DNS 提供商以該提供商自身的網域發布解析器 endpoint。
若主機試圖「透過該提供商本身」解析其提供商名稱，在解析器健全之前將會發生迴圈或失敗。

請為該提供商的網域建立條件式 source，並將查詢導向公眾解析器或 access-network 的 DNS。
提供商的帳號 ID（例如 profile ID）請勿寫入共享範例，應僅置於主機本地的 secrets 檔案或各主機專屬的 YAML overlay 中。

`DNSUpstream.spec.bootstrap` 以更精細的方式提供相同保護。它指定在建立加密傳輸之前，用於解析該上游 endpoint 名稱的解析器。

## 介面綁定

`DNSUpstream.spec.sourceInterface` 將對外的 DNS 查詢綁定至 Linux 的介面名稱。
請使用 OS 介面的實際名稱（例如 `ens18`）。
若該介面由其他資源（如 tunnel）建立，請透過 `ownerRefs` 或順序宣告依賴關係，使解析器在介面建立前保持 pending 狀態。

FreeBSD 沒有對等的 `SO_BINDTODEVICE` 強制機制，因此在平台專屬文件中不保證相同的行為。

## 相關文件

- [本地 DNS 區域](./dns-local-zone.md)
- [DNS 解析器概念](../concepts/dns-resolver.md)
