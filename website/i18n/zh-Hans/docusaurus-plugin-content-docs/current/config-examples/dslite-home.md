---
title: DS-Lite 家用网关
sidebar_position: 30
---

# DS-Lite 家用网关

![IPv6 WAN、DS-Lite tunnel、LAN IPv4 和 delegated IPv6 service 的架构](/img/diagrams/config-example-dslite-home.png)

DS-Lite 用于 IPv6 为主的线路：IPv4 数据包经 ISP 的 tunnel 出网。这不是第一台路由器的
教程，而是需要 ISP 信息的进阶示例。WAN、tunnel 或 DNS 配错会中断连接，请只在有控制台或
独立管理路径的实验环境操作。

完整且已验证的 YAML 位于
[`examples/example-dslite-home.yaml`](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)。
其中接近 Transix 的 AFTR 值只是示例，必须替换为自己线路的信息。

## 此示例创建什么

| 工作 | YAML 中实际的资源名 |
| --- | --- |
| 从 WAN 获得 IPv6 delegated prefix | `DHCPv6PrefixDelegation/wan-pd` |
| 派生 LAN 的 IPv6 地址 | `IPv6DelegatedAddress/lan-v6` |
| 在 LAN 响应 DNS | `DNSResolver/lan`、`DNSZone/home` |
| 创建 IPv4-over-IPv6 tunnel | `DSLiteTunnel/transix` |
| 向 LAN 分发 IPv4、DNS 和 IPv6 router 信息 | `DHCPv4Server/lan`、`IPv6RouterAdvertisement/lan` |

`lan-v4`、`lan-v6`、`lan`、`transix` 是此文件选择的名字，并非每个 routerd 配置都必须使用的名字。

## 配置如何连接

WAN prefix delegation 与由它派生的 LAN IPv6 地址通过名字连接：

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: wan-pd
  spec:
    interface: wan
    profile: ntt-hgw-lan-pd

- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: lan-v6
  spec:
    prefixDelegation: wan-pd
    interface: lan
    subnetID: "0"
    addressSuffix: "::1"
    announce: true
```

DS-Lite tunnel 使用同一个 `lan-v6` 作为本地端地址：

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: transix
  spec:
    interface: wan
    tunnelName: ds-transix
    aftrFQDN: gw.transix.jp
    aftrDNSServers: [2404:1a8:7f01:a::3, 2404:1a8:7f01:b::3]
    localAddressSource: delegatedAddress
    localDelegatedAddress: lan-v6
    localAddressSuffix: "::100"
    defaultRoute: true
```

若你的 ISP 要求使用 WAN Router Advertisement 地址作为 tunnel source，请按照 ISP 指定选择
`localAddressSource`，不要直接复制此值。

DNS、DHCPv4、RA 也使用同一组本地资源名：

```yaml
- kind: DNSResolver
  metadata:
    name: lan
  # 在 IPv4StaticAddress/lan-v4 和 IPv6DelegatedAddress/lan-v6 监听。

- kind: DHCPv4Server
  metadata:
    name: lan
  # 向客户端公告 IPv4StaticAddress/lan-v4 为 gateway 和 DNS。

- kind: IPv6RouterAdvertisement
  metadata:
    name: lan
  # 公告 IPv6DelegatedAddress/lan-v6 和 DNSZone/home。
```

短摘录只说明资源名的连接；需要的 `spec` 字段请阅读完整 YAML。

## daemon 未启动时先检查

复制文件并替换所有 ISP 专属值后，以具有 `sudo` 权限的本地用户运行独立检查。它们不会启动服务或应用网络变更。

```sh
cp examples/example-dslite-home.yaml router.yaml
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config router.yaml
sudo routerd apply --config router.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
rm -rf "$LAB_DIR"
```

确认 WAN/LAN 名称、AFTR FQDN、resolver 地址和管理路径都是自己的信息。只要有一项不对，
就不要继续。

## 应用后观察

只能从控制台或独立管理路径执行 live 操作。服务启动后可检查；首次安装请保留 `sudo`。若管理员通过 `routerd` 组授予本地 socket 访问，加入后必须重新登录才会生效：

```sh
sudo routerctl get status
sudo routerctl describe DHCPv6PrefixDelegation/wan-pd
sudo routerctl describe IPv6DelegatedAddress/lan-v6
sudo routerctl describe DSLiteTunnel/transix
sudo routerctl describe FirewallZone/wan
sudo ip -6 tunnel show
sudo ip route show default
```

在 LAN 客户端上检查 IPv6、路由和本地 DNS：

```sh
ip -6 addr
ip route
curl https://1.1.1.1/
dig router.home.example
```

## 相关页面

- [WAN 侧服务](../tutorials/wan-side-services.md)
- [LAN 侧服务](../tutorials/lan-side-services.md)
- [基本 IPv4 NAT 网关](./basic-ipv4-nat.md)
