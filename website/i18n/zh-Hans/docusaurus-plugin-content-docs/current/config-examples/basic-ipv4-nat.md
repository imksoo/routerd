---
title: 基本 IPv4 NAT 路由器
sidebar_position: 10
---

# 基本 IPv4 NAT 路由器

![由 DHCP WAN、routerd 管理的 LAN address、DHCPv4 server、NAT44 和 firewall zone 组成的基本 IPv4 gateway](/img/diagrams/config-example-basic-ipv4-nat.png)

这是一个接近最小配置的家用路由器示例，让 LAN 客户端通过 DHCP 取得的 WAN 端 IPv4 地址连上互联网。

完整的已验证 YAML 位于 `examples/example-basic-ipv4-nat.yaml`。

## 架构图

```mermaid
flowchart LR
  internet((Internet))
  upstream["[1] ISP / upstream router"]
  wan["[2] wan<br/>DHCPv4 client"]
  router["[3] routerd host"]
  lan["[4] lan<br/>192.168.10.1/24"]
  clients["[5] LAN clients<br/>192.168.10.100-199"]

  internet --- upstream --- wan --- router --- lan --- clients
```

## 图示对应表

| 编号 | 说明 | 主要资源 |
| --- | --- | --- |
| [1] | 分配 WAN 端 IPv4 租约的上游网络。 | routerd 管理范围外 |
| [2] | 物理 WAN 接口，在此执行 DHCPv4 客户端。 | `Interface/wan`、`DHCPv4Client/wan-dhcpv4` |
| [3] | 应用推导出的 forwarding sysctl 和 nftables 规则的 Linux 主机。 | Derived host runtime |
| [4] | routerd 持有的 LAN 网关地址。 | `Interface/lan`、`IPv4StaticAddress/lan-base` |
| [5] | 将路由器作为网关 / DNS 使用的 LAN 客户端。 | `DHCPv4Server/lan-dhcpv4` |

## 此示例管理的项目

| 领域 | routerd 资源 |
| --- | --- |
| WAN 地址 | `Interface/wan`、`DHCPv4Client/wan-dhcpv4` |
| LAN 地址 | `Interface/lan`、`IPv4StaticAddress/lan-base` |
| LAN DHCPv4 | `DHCPv4Server/lan-dhcpv4` |
| IPv4 互联网连接 | `NAT44Rule/lan-to-wan` |
| 基本过滤器 | `FirewallZone/wan`、`FirewallZone/lan`、`FirewallPolicy/home` |

此示例尽量简化 DNS。向 DHCPv4 客户端分发路由器的 LAN 地址作为 DNS 服务器。在基本路由运作后，可视需要再添加 `DNSResolver` 和 `DNSZone`。

## 配置要点

```yaml
# [2] WAN 地址从上游网络通过 DHCPv4 取得。
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Client
  metadata:
    name: wan-dhcpv4
  spec:
    interface: wan

# [4] routerd 持有 LAN 网关地址。
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: lan-base
  spec:
    interface: lan
    address: 192.168.10.1/24

# [5] 向 LAN 客户端分发地址、网关、DNS、搜索域。
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    interface: lan
    addressPool:
      start: 192.168.10.100
      end: 192.168.10.199
      leaseTime: 12h
    gatewayFrom:
      resource: IPv4StaticAddress/lan-base
      field: address
    dnsServerFrom:
      - resource: IPv4StaticAddress/lan-base
        field: address

# [2] -> [5] LAN IPv4 流量在出往 WAN 时进行 masquerade。
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: lan-to-wan
  spec:
    type: masquerade
    egressInterface: wan
    sourceRanges:
      - 192.168.10.0/24
```

`NAT44Rule` 会反映至 routerd 的 nftables NAT 表格。在防火墙资源中，
将 WAN 接口加入 `untrust` 区域，LAN 接口加入 `trust` 区域。

## 应用步骤

```bash
cp examples/example-basic-ipv4-nat.yaml router.yaml
routerctl validate -f router.yaml --replace
routerctl plan -f router.yaml --replace
```

确认管理访问并非依赖即将变更地址的 LAN 接口，或已具备控制台访问权限后再执行应用。

```bash
routerctl apply -f router.yaml --replace
```

## 确认

```bash
routerctl status
routerctl describe DHCPv4Client/wan-dhcpv4
routerctl describe IPv4StaticAddress/lan-base
routerctl describe NAT44Rule/lan-to-wan
nft list table ip routerd_nat
nft list table inet routerd_filter
```

在 LAN 客户端端确认以下项目。

```bash
ip route
ping 192.168.10.1
curl https://1.1.1.1/
```

## 常见的修改点

- 将 `ens18` 和 `ens19` 改为实际的接口名称。
- 若与上游、VPN 或管理网络重叠，请变更 `192.168.10.0/24`。
- 在分发路由器作为 DNS 服务器之前，视需要先添加 `DNSResolver`。
