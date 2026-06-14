---
title: LAN 侧服务
sidebar_position: 5
---

# LAN 侧服务

![处理 LAN address、DHCPv4/DHCPv6、router advertisement、local DNS、lease event 与 client option 的 LAN-side routerd services](/img/diagrams/tutorial-lan-side-services.png)

本页介绍处理路由器 LAN 侧的 routerd 资源。
LAN 侧资源负责内侧接口的地址、DHCPv4 / DHCPv6 分配、IPv6 Router Advertisement，以及本地 DNS 解析器等功能。

WAN 侧（从上游获取地址）请参阅 [WAN 侧服务](./wan-side-services.md)。

## 服务分工

routerd 将 LAN 侧服务明确划分给两个守护进程：

- **dnsmasq** 负责 DHCPv4、DHCPv6、DHCP relay 及 IPv6 Router Advertisement。
- **`routerd-dns-resolver`** 负责 DNS 区域、条件式转发、缓存及查询日志。

采用经过验证的 dnsmasq 直接处理 DHCP，DNS 策略则以具类型的 routerd 资源（`DNSResolver`、`DNSZone`）表达，两者各司其职。

## 一览

| 功能 | 资源 | 负责守护进程 |
| --- | --- | --- |
| LAN 接口地址 | `IPv4StaticAddress`、`IPv6DelegatedAddress` | （kernel） |
| DHCPv4 范围 | `DHCPv4Server` | dnsmasq |
| DHCPv4 固定分配 | `DHCPv4Reservation` | dnsmasq |
| DHCPv6（stateless / stateful） | `DHCPv6Server` | dnsmasq |
| IPv6 Router Advertisement | `IPv6RouterAdvertisement` | dnsmasq（RA 模式） |
| LAN 侧时间服务器通告 | `DHCPv4Server`、`DHCPv6Server` | dnsmasq |
| DNS 区域（本地权威） | `DNSZone` | `routerd-dns-resolver` |
| DNS 解析器监听 | `DNSResolver` | `routerd-dns-resolver` |
| DHCP 租约事件中继 | （内置） | `routerd-dhcp-event-relay` |

## DHCPv4 范围

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    interface: lan
    addressPool:
      start: 192.0.2.64
      end: 192.0.2.191
      leaseTime: 12h
    gatewayFrom:
      resource: IPv4StaticAddress/lan-base
      field: address
    dnsServerFrom:
      - resource: IPv4StaticAddress/lan-base
        field: address
    ntpServerFrom:
      - resource: IPv4StaticAddress/lan-base
        field: address
    domainFrom:
      resource: DNSZone/lan
      field: zone
    stickyHoldDays: 3
```

将自动分配的客户端范围与固定地址范围分开，可使操作更清晰易读。
`stickyHoldDays` 为可选项目。指定大于 0 的值后，routerd 会短期保留 DHCP 租约历史，并在租约释放或到期后，临时生成 dnsmasq 的 `dhcp-host` hold 条目。相同 MAC 地址可在 hold 期间内重新获取相同地址，该地址不会立即分配给其他客户端。

## DHCPv4 静态预约

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: smart-meter
  spec:
    server: lan-dhcpv4
    macAddress: "02:00:00:00:00:01"
    hostname: smart-meter
    ipAddress: 192.0.2.10
```

`DHCPv4Reservation` 会展开为 dnsmasq 的 host reservation 条目。
在 Web 管理界面与事件日志中，会以不依赖设备当前 IP 的稳定资源名称显示。

FreeBSD 上，dnsmasq 的租约文件存放于 `/var/db/routerd/dnsmasq` 目录下。
若仅存放于 `/var/run`，重启后租约将丢失。
rc.d 脚本会在启动前创建运行时目录与租约目录。
`routerctl apply` 会在重启 dnsmasq 前先执行 `dnsmasq --test`。
同时也会自动生成 DHCP、DHCPv6、RA、DNS 所需的 pf 通道。

## IPv6 RA 与 DHCPv6

在 IPv6 LAN 中，请在 Router Advertisement 中包含 RDNSS 一起发送。
Android 不会通过 DHCPv6 获取 DNS，因此 RDNSS 是必要的。
Windows 客户端还需要额外提供 DHCPv6 stateless 服务器。

Router Advertisement 没有标准的 NTP 服务器通告。
若要将路由器本身设为 LAN 的时间参考来源，请使用 DHCPv4 option 42 与 DHCPv6 option 31（SNTP）。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: lan-ra
  spec:
    interface: lan
    prefixFrom:
      resource: IPv6DelegatedAddress/lan-base
      field: address
    mFlag: false
    oFlag: true
    rdnssFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    dnsslFrom:
      - resource: DNSZone/lan
        field: zone
    mtu: 1454

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: lan-dhcpv6
  spec:
    interface: lan
    mode: stateless
    dnsServerFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    sntpServerFrom:
      - resource: IPv6DelegatedAddress/lan-base
        field: address
    domainSearchFrom:
      - resource: DNSZone/lan
        field: zone
```

若要通过 DHCPv6 同时分配地址，请使用 `mode: stateful` 或 `mode: both`。
若要让 LAN 的 DNS suffix 与 `DNSZone` 一致，请使用 `domainFrom`、`dnsslFrom`、`domainSearchFrom`。
DHCPv4 的 domain-name、RA 的 DNSSL、DHCPv6 的 domain-search 均参照相同的本地区域，因此无需重复编写域名字符串。

## 本地 DNS 区域

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: lan
  spec:
    zone: lan.example.org
    ttl: 300
    records:
      - hostname: router
        ipv4From:
          resource: IPv4StaticAddress/lan-base
          field: address
        ipv6From:
          resource: IPv6DelegatedAddress/lan-base
          field: address
    dhcpDerived:
      sources:
        - DHCPv4Server/lan-dhcpv4
        - DHCPv6Server/lan-dhcpv6
      ddns: true
      ttl: 60
```

固定记录写在 `records:` 中，DHCP 租约派生的记录写在 `dhcpDerived.sources` 中。
两者在查询时会合并。
若 DHCP 派生的 hostname 为相对名称，会发布于 DNSZone 本身之下，通常无需编写 `hostnameSuffix`。

## DNS 解析器监听

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSResolver
  metadata:
    name: lan-resolver
  spec:
    listen:
      - name: lan
        addressFrom:
          - resource: IPv4StaticAddress/lan-base
            field: address
          - resource: IPv6DelegatedAddress/lan-base
            field: address
        port: 53
        sources: [local-zone, default]
    sources:
      - name: local-zone
        kind: zone
        match:
          - lan.example.org
        zoneRef:
          - DNSZone/lan
      - name: default
        kind: upstream
        match:
          - "."
        upstreams:
          - https://dns.example.net/dns-query
          - udp://1.1.1.1:53
    cache:
      enabled: true
      maxEntries: 10000
```

解析器会在参照资源的 status 中获取的所有地址上监听。
即使因 PD 更新等原因新增 IPv6 地址，也无需重启即可自动跟进。

## 动作确认

```sh
# 确认 LAN 接口已加载 v4 / v6
routerctl describe Interface/lan

# 实时追踪 DHCP 事件
routerctl events --topic 'routerd.dhcp.lease.**' --resource DHCPv4Server/lan-dhcpv4

# 以本地解析器进行名称解析
dig @<lan-ip> router.lan.example.org
dig @<lan-ip> example.com
```

## 操作提示

- 请先从 `routerctl plan` 开始。在确保管理路径与已知的回滚路径后，再启用生产环境的 LAN 监听。
- 若手动修改了 dnsmasq 的租约文件，请重启 `routerd-dhcp-event-relay` 以使内存内状态同步。租约的变更请尽量通过 routerd 进行。
- 请保留公共 DNS 作为备援。`routerd-dns-resolver` 会降低健康检查失败的转发器优先级，但仅在没有其他健全替代方案时才会如此。

## 相关项目

- [WAN 侧服务](./wan-side-services.md)
- [本地 DNS 区域](../how-to/dns-local-zone.md)
- [专用 DNS 上游](../how-to/dns-private-upstream.md)
