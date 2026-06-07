---
title: 专用 DNS 上游
slug: /how-to/dns-private-upstream
---

# 专用 DNS 上游

![DNSForwarder 与 DNSUpstream 将 local、provider、default DNS query 路由到 UDP、TCP、DoT、DoH 和 addressFrom source 的流程](/img/diagrams/how-to-dns-private-upstream.png)

## 适用场景

当您希望路由器上的解析器具备以下行为时：

- 将接入网络的内部区域（例如：ISP 的 AFTR FQDN、企业内部域）转发至动态学习到的 DNS 服务器。
- 平时以加密 DNS 提供商（DoH / DoT）作为默认上游。
- 加密上游异常时，回退至明文 DNS。
- 不在共享示例中写入提供商账号 ID 或私有 endpoint。

## routerd 的解决方式

`DNSResolver` 会启动 `routerd-dns-resolver` 守护进程。
守护进程监听 UDP/TCP。隶属于解析器的 `DNSForwarder` 表示 match 规则，`DNSUpstream` 表示可复用的上游 endpoint。

| Scheme | 类别 | 默认端口 |
| --- | --- | --- |
| `https://` | DNS over HTTPS | 依 URL 而定 |
| `tls://` | DNS over TLS | 853 |
| `udp://` | 明文 DNS over UDP | 53 |
| `tcp://` | 明文 DNS over TCP | 53 |

`DNSForwarder.spec.upstreams` 的顺序即为优先级。优先尝试优先度最高且健康的上游，失败后依次往下。

使用 `DNSUpstream.spec.addressFrom`，可从其他资源的 status 获取上游地址列表。
这是利用 DHCPv6 information-request 获取的 DNS 服务器时所使用的机制。

## 条件式转发示例

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

## 提供商的 bootstrap 处理

部分私有 DNS 提供商以该提供商自身的域名发布解析器 endpoint。
若主机试图「通过该提供商本身」解析其提供商名称，在解析器健全之前将会发生循环或失败。

请为该提供商的域名建立条件式 source，并将查询导向公众解析器或 access-network 的 DNS。
提供商的账号 ID（例如 profile ID）请勿写入共享示例，应仅置于主机本地的 secrets 文件或各主机专属的 YAML overlay 中。

`DNSUpstream.spec.bootstrap` 以更精细的方式提供相同保护。它指定在建立加密传输之前，用于解析该上游 endpoint 名称的解析器。

## 接口绑定

`DNSUpstream.spec.sourceInterface` 将对外的 DNS 查询绑定至 Linux 的接口名称。
请使用 OS 接口的实际名称（例如 `ens18`）。
若该接口由其他资源（如 tunnel）建立，请通过 `ownerRefs` 或顺序声明依赖关系，使解析器在接口建立前保持 pending 状态。

FreeBSD 没有对等的 `SO_BINDTODEVICE` 强制机制，因此在平台专属文档中不保证相同的行为。

## 相关文件

- [本地 DNS 区域](./dns-local-zone.md)
- [DNS 解析器概念](../concepts/dns-resolver.md)
