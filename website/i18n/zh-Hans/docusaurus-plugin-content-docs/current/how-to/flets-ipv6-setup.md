---
title: DHCPv6-PD 上的 DS-Lite（仅 IPv6 接入网络）
slug: /how-to/flets-ipv6-setup
---

# DHCPv6-PD 上的 DS-Lite（仅 IPv6 接入网络）

![IPv6-only access 中 DHCPv6-PD、delegated LAN IPv6、AFTR DNS forwarding、DS-Lite tunnel egress 与安全 RA 的构成流程](/img/diagrams/how-to-flets-ipv6-setup.png)

## 适用场景

ISP 提供仅 IPv6 的接入网络，IPv4 连接通过 AFTR（Address Family Transition Router）的 DS-Lite 隧道实现。在这种配置中，路由器负责以下工作：

- 通过 DHCPv6-PD 获取 IPv6 前缀，并分配给 LAN。
- 建立通往 AFTR 的 DS-Lite（IPv4-in-IPv6 / `ip6tnl`）隧道。
- AFTR 的 FQDN 有时只有接入网络的 DNS 才能解析，因此使用条件式转发。
- 在 IPv6 RA 中加入 RDNSS，让 SLAAC 客户端（包含 Android）自动获取 DNS 配置。

此模式在日本 FLET'S 系列线路（NTT NGN + `gw.transix.jp` 等）中最为典型，但同样适用于类似的 DS-Lite 部署。

## 前提条件

- WAN 接口已通过 HGW 或 ONU 连接至仅 IPv6 的接入网络。
- 该接口可使用 DHCPv6-PD。
- AFTR 的 DNS 是否会通过 DHCPv6 information-request 返回，因 ISP 或 HGW 而异，请针对两种情况做好准备。

## DHCPv6-PD

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: wan-pd
  spec:
    interface: wan
```

租约存储于：

```text
/var/lib/routerd/dhcpv6-client/wan-pd/lease.json
```

可通过 Unix socket 确认守护进程状态：

```bash
curl --unix-socket /run/routerd/dhcpv6-client/wan-pd.sock http://unix/v1/status
```

## LAN 地址推导与 RA

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: lan-from-pd
  spec:
    interface: lan
    prefixDelegation: wan-pd
    dependsOn:
      - resource: DHCPv6PrefixDelegation/wan-pd
        phase: Bound
    addressSuffix: "::1"

- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: lan-ra
  spec:
    interface: lan
    prefixFrom:
      resource: IPv6DelegatedAddress/lan-from-pd
      field: address
    oFlag: true
    rdnssFrom:
      - resource: IPv6DelegatedAddress/lan-from-pd
        field: address
```

RA 广播的 RDNSS 使用从委派前缀推导出的 LAN 侧地址。
SLAAC 客户端会自动获取此解析器地址。

## AFTR 的条件式 DNS 转发

AFTR 的 FQDN 通常只有 ISP 接入网络的 DNS 才能解析。
只将该域名转发至接入网络的解析器，其余流量交由一般上游处理。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: resolver
  spec:
    listen:
      - name: local
        addresses: [127.0.0.1]
        port: 53
    sources:
      - name: aftr
        kind: forward
        match: [transix.jp]
        upstreams:
          - udp://[2404:8e00::feed:101]:53
      - name: default
        kind: upstream
        match: ["."]
        upstreams:
          - udp://1.1.1.1:53
```

请将 `transix.jp` 及上游 IPv6 地址替换为 ISP 公告的实际值。

## DS-Lite 隧道

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: ds-lite
  spec:
    interface: wan
    tunnelName: ds-routerd
    localAddressSource: interface
    aftrFQDN: gw.transix.jp
    dependsOn:
      - resource: DNSResolver/resolver
        phase: Applied
```

`localAddressSource: interface` 使用 WAN 侧通过 SLAAC/RA 获取的 IPv6 地址作为隧道的本地端点。
此地址比 LAN 侧推导更早获取，因此隧道可更快建立。

若 ISP 公告了稳定的 AFTR 地址且希望省略 DNS 解析，可直接指定 `aftrIPv6`：

```yaml
spec:
  aftrIPv6: 2001:db8:cafe::1
```

在 NTT NGN 的 HGW 等不通过 DHCPv6 information-request 返回 AFTR 的环境中，静态指定 `aftrFQDN` 或 `aftrIPv6` 是正确的备援方式。

隧道内侧的 IPv4 地址通常使用 RFC 6333 的 B4-AFTR 连接范围 `192.0.0.0/29`。
若要使用从 LAN 范围切出的地址，请以 `IPv4StaticAddress` 资源定义，
并从 `DSLiteTunnel.localAddressFrom` 与 `NAT44Rule.snatAddressFrom` 参照该值。
自定义示例请参阅 `examples/dslite-lan-range-snat.yaml`。

## 验证

```bash
routerd apply --config router.yaml --once --dry-run
routerctl status

ip -6 tunnel show
ip route show default
nft list table ip routerd_nat

# 确认可通过隧道获取 IPv4 连接
curl --interface ds-routerd https://1.1.1.1/
```

请先以 dry-run 确认计划无误、且已备妥回滚路径后，再正式应用。

## 相关项目

- [WAN 侧服务](../tutorials/wan-side-services.md)
- [多 WAN 切换](./multi-wan.md)
- [Path MTU 与 MSS clamping](../concepts/path-mtu.md)
