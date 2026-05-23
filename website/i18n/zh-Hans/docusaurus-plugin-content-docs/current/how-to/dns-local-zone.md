---
title: 本地 DNS 区域
slug: /how-to/dns-local-zone
---

# 本地 DNS 区域

## 适用场景

当您希望通过名称解析内部主机，但又不想手动同步各设备的 `/etc/hosts` 时，具体而言是希望实现以下目标：

- 拥有少量固定记录（路由器、NAS、打印机）。
- 为取得 DHCP 租约的设备自动生成 A / AAAA / PTR 记录。
- 正向查询与反向查询均正常运作。

## routerd 的解决方式

使用 `DNSZone` 管理单一 DNS 域的本地权威记录。
可以结合**手动记录**（以 YAML 声明）与**来自 DHCP 的记录**（从租约数据库建立）。
`DNSResolver` 将这些记录作为响应来源之一加载，使内部查询在本地响应，外部查询则转发至配置的上游解析器。

DHCP 衍生的记录通过事件总线同步。dnsmasq 在租约变更时调用 `routerd-dhcp-event-relay`，relay 发布 routerd 事件，`routerd-dns-resolver` 则更新内存中的区域数据。
dnsmasq 的租约文件在启动时也会重新读取，因此即使重启守护进程也不会丢失记录。

## 示例

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSZone
  metadata:
    name: lan
  spec:
    zone: lan.example.org
    ttl: 300
    dnssec:
      enabled: false
    records:
      - hostname: router
        ipv4: 192.0.2.1
        ipv6: 2001:db8:1::1
      - hostname: nas
        ipv4: 192.0.2.10
    dhcpDerived:
      sources:
        - DHCPv4Server/lan-dhcpv4
        - DHCPv6Server/lan-dhcpv6
      hostnameSuffix: lan.example.org
      ddns: true
      ttl: 60
      leaseFile: /run/routerd/dnsmasq.leases
    reverseZones:
      - name: 2.0.192.in-addr.arpa
```

应用后，`nas.lan.example.org` 及 `<dhcp-client-name>.lan.example.org` 将解析为本地地址，`192.0.2.x` 的 PTR 查询也会返回对应的名称。

## 补充说明

- 请选择您拥有管理权的域，或为内部使用保留的域（如 `example.org`、`home.arpa`）。请勿使用可能与公众 DNS 冲突的 suffix，例如 `.lan`。
- 启用 DNSSEC（`dnssec.enabled: true`）后，外部的 DNSSEC 验证仍可正常运作。本地区域在设计上不签署。
- 若有多个内部子网，请为每个子网分别撰写一条 `reverseZones` 条目，以确保双向 PTR 查询均可运作。

## 相关文件

- [专用 DNS 上游](./dns-private-upstream.md)
- [DNS 解析器概念](../concepts/dns-resolver.md)
