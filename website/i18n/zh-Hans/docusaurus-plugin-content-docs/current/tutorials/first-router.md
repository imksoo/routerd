---
title: 启动第一台路由器
sidebar_position: 2
---

# 启动第一台路由器

![展示 DHCPv4 WAN、static LAN address、最小 Interface resource 与 validate-plan-dry-run apply loop 的 first router tutorial](/img/diagrams/tutorial-first-router.png)

本教程将启动最小的 routerd 配置。配置为「通过 DHCPv4 获取 IPv4 的 WAN 一条」加上「固定 IPv4 地址的 LAN 一条」。

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: first-router
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: ens18
        adminUp: true
        managed: true

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: ens19
        adminUp: true
        managed: true

    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Client
      metadata:
        name: wan
      spec:
        interface: wan

    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata:
        name: lan-address
      spec:
        interface: lan
        address: 192.0.2.1/24
```

`DHCPv4Client` 由 `routerd-dhcpv4-client` 拥有。
routerd 不依赖 OS 内置的客户端。这个守护进程与其他 routerd 守护进程使用相同的约定（`/v1/status`、`lease.json`、`events.jsonl`）来公开状态。

应用至生产环境前，请先以 validate 和 plan 确认。

```bash
routerctl validate --config first-router.yaml
routerctl plan     --config first-router.yaml
routerctl apply    --config first-router.yaml --dry-run
```

确认管理路径（通过 LAN 的 SSH、控制台、Hypervisor 控制台）在变更后仍可访问，再去掉 `--dry-run` 正式应用。

## 接下来阅读

- [WAN 侧服务](./wan-side-services.md) — DHCPv6-PD、PPPoE、DS-Lite
- [LAN 侧服务](./lan-side-services.md) — DHCP、RA、DNS、本地区域
- [基本 NAT 与防火墙策略](./basic-firewall.md)
