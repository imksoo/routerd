---
title: WAN 侧服务
sidebar_position: 4
---

# WAN 侧服务

![处理 DHCPv4、DHCPv6-PD、PPPoE、DS-Lite、health check、egress selection、NAT44 与 downstream status input 的 WAN-side routerd services](/img/diagrams/tutorial-wan-side-services.png)

本页介绍处理路由器 WAN 侧的 routerd 资源。
WAN 侧资源负责建立上游链路、从 ISP 获取 IP 地址与前缀、终结隧道，以及向控制器链提供多条上游路由等功能。

LAN 侧（路由器向内侧提供的服务）请参阅 [LAN 侧服务](./lan-side-services.md)。

## 一览

| 功能 | 资源 | 负责守护进程 |
| --- | --- | --- |
| 物理 / 虚拟接口 | `Interface`、`IPv4StaticAddress` | （kernel） |
| 通过 DHCP 从 ISP 获取 IPv4 | `DHCPv4Client` | `routerd-dhcpv4-client` |
| 从 ISP 获取 IPv6 前缀 | `DHCPv6PrefixDelegation`、`IPv6DelegatedAddress` | `routerd-dhcpv6-client` |
| 其他 DHCPv6 选项（DNS、AFTR 等） | `DHCPv6Information` | `routerd-dhcpv6-client` |
| 上游时间服务器 | `NTPClient` | `systemd-timesyncd` 或 `ntpd` |
| PPPoE 会话 | `PPPoESession` | `routerd-pppoe-client` |
| IPv6 上的 IPv4（DS-Lite） | `DSLiteTunnel` | （kernel `ip6tnl`） |
| WAN 路由选择 | `EgressRoutePolicy`、`HealthCheck` | `routerd-healthcheck@<name>` |
| IPv4 NAT（masquerade） | `NAT44Rule` | （nftables） |
| 静态 IPv4 路由 | `IPv4Route` | （kernel） |

请根据 ISP 的提供形态，选择所需资源的组合。

## 模式 A：原生双栈（IPv4 + IPv6）

ISP 在同一 WAN 接口上同时发送 IPv4（DHCPv4）与 IPv6 前缀（DHCPv6-PD）的配置。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata: {name: wan}
  spec:
    ifname: ens18
    role: untrust

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Client
  metadata: {name: wan-v4}
  spec:
    interface: wan

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata: {name: wan-pd}
  spec:
    interface: wan

- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata: {name: lan-base}
  spec:
    pdRef: wan-pd
    interface: lan
    suffix: ::1/64

- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata: {name: lan-to-wan}
  spec:
    type: masquerade
    egressInterface: wan
    sourceRanges:
      - 192.0.2.0/24
```

`DHCPv4Client` 启动 `routerd-dhcpv4-client`，并将租约内容写入 `lease.json`。地址本身由 kernel 持有，routerd 向下游资源发出事件。

`DHCPv6PrefixDelegation` 使用 `routerd-dhcpv6-client` 获取 IA_PD。`IPv6DelegatedAddress` 从获取的前缀中切出分配给 LAN 侧的 `/64`（或其他长度）。

## 上游 NTP / SNTP

`NTPClient` 可从 DHCPv4 option 42 或 DHCPv6 option 31 中提取时间服务器。
若上游不发送时间服务器，则将指定的公共 NTP 服务器配置至 OS 的 NTP 客户端。
Linux / NixOS 使用 `systemd-timesyncd`，FreeBSD 使用 `ntpd`。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPClient
  metadata: {name: system-time}
  spec:
    provider: systemd-timesyncd
    managed: true
    source: auto
    serverFrom:
      - resource: DHCPv4Client/wan-v4
        field: ntpServers
      - resource: DHCPv6Information/wan-info
        field: sntpServers
    fallbackServers:
      - ntp.jst.mfeed.ad.jp
      - ntp.nict.jp
```

若要将路由器本身作为 LAN 客户端的时间参考来源，请并用 LAN 侧的 `ntpServerFrom` 与 `sntpServerFrom`。

## 模式 B：PPPoE（IPv4）+ DHCPv6-PD（IPv6）

旧式 xDSL 系配置，IPv4 通过 PPPoE 获取，IPv6 通过相同物理链路的原生 DHCPv6-PD 获取。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata: {name: wan-pppoe}
  spec:
    interface: wan
    user: "user@isp.example"
    passwordFromSecret: pppoe-password
    mtu: 1454
    mru: 1454

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata: {name: wan-pd}
  spec:
    interface: wan
```

`PPPoESession` 启动 `routerd-pppoe-client`，在 Linux 上封装 `pppd`/`rp-pppoe`，在 FreeBSD 上封装 `ppp(8)`。PPPoE 会话的接口（通常为 `ppp0`）可作为路由或 `NAT44Rule` 的参照对象。

## 模式 C：DS-Lite（在仅 IPv6 的接入网络上隧道 IPv4）

ISP 不提供原生 IPv4，仅提供 IPv6 的配置。IPv4 通过连往 AFTR（Address Family Transition Router）的 DS-Lite 隧道实现。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata: {name: wan-pd}
  spec:
    interface: wan

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Information
  metadata: {name: wan-info}
  spec:
    interface: wan

- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata: {name: ds-lite-primary}
  spec:
    sourceInterface: wan
    aftrFQDN: gw.transix.jp
    aftrFQDNResolverFromResource:
      resource: DHCPv6Information/wan-info
      field: dnsServers
    mtu: 1454
```

`DSLiteTunnel` 在解析到 AFTR 地址后，以 kernel 的 `ip6tnl` 设备建立。
AFTR 记录通常只能通过接入网络内的 DNS 解析，因此请使用 `aftrFQDNResolverFromResource` 指定 ISP 的 DNS。

## 模式 D：多 WAN（主线路 + 备援）

有多条路由时，请将 `EgressRoutePolicy` 与 `HealthCheck` 组合至 WAN 获取资源中使用。详细请参阅[多 WAN 切换](../how-to/multi-wan.md)。

## 状态确认

各 WAN 资源的状况可通过 `routerctl describe <kind>/<name>` 确认。示例：

```sh
routerctl describe DHCPv6PrefixDelegation/wan-pd      # phase: Bound, prefix: 2001:db8:1::/56
routerctl describe DSLiteTunnel/ds-lite-primary       # phase: Up, aftr: 2001:db8:cafe::1
routerctl describe EgressRoutePolicy/ipv4-default     # selectedCandidate: ds-lite-primary
```

亦可从 Web 管理界面的「Overview」「Resources」分页确认相同信息。「Connections」分页显示各 WAN 路由的实际 conntrack/pf 状态。

## 相关项目

- [LAN 侧服务](./lan-side-services.md)
- [多 WAN 切换](../how-to/multi-wan.md)
- [NTT NGN 的 DS-Lite](../how-to/flets-ipv6-setup.md)
- [Path MTU 与 MSS clamping](../concepts/path-mtu.md)
