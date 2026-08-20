---
title: 分步教程
slug: /tutorials
---

# 分步教程

![routerd 教程路线：网络基础、安装、安全检查、第一台实验路由器，再到 WAN、LAN 和 NAT](/img/diagrams/tutorial-index.png)

这些教程按“先看懂、再检查、最后接入真实网络”的顺序编排。第一个实验建议使用隔离的 Ubuntu Server VM，并保留虚拟机控制台、串口或独立管理网卡。

| 如果你的目标是 | 建议阅读 |
| --- | --- |
| 先弄清 WAN、LAN、DHCP、DNS、NAT | [网络基础](./network-basics.md) |
| 从发布归档安装程序 | [安装](./install.md) |
| 写第一份 YAML，但不改动网络 | [安全起步](./getting-started.md) |
| 设置 WAN DHCP 和 LAN 网关地址 | [第一台实验路由器](./first-router.md) |
| 添加 WAN 获取、PPPoE、DS-Lite 或 DHCPv6-PD | [WAN 侧服务](./wan-side-services.md) |
| 添加 LAN DHCP、RA、DNS 或 NTP | [LAN 侧服务](./lan-side-services.md) |
| 学习 NAT 与防火墙资源的边界 | [基本 NAT 与防火墙](./basic-firewall.md) |
| 查看完整配置的结构 | [配置示例集](../config-examples/index.md) |

## 牢记这条命令路线

第一次检查本地文件时：

```text
routerd validate  →  routerd apply --once --dry-run
```

前者检查 YAML，后者在不提交网络变更的前提下走一次控制器和渲染流程。真实的 `routerd serve` 会管理主机，因此必须等到你确认管理路径安全之后才启动。

服务已经运行时，才进入另一条路线：

```text
routerd serve  →  routerctl get / describe / validate / plan / apply
```

`routerctl` 是运行中 daemon 的客户端；不要把它当作离线配置检查命令。

:::caution 有线和无线隔离不是同一回事
后面的 NAT、防火墙和访客设备示例帮助理解资源模型，但防火墙仍处于预发布的基础实现阶段。尤其是访客设备若和受信任设备处于同一个二层网络，真正的隔离通常需要 VLAN、独立 SSID 或独立交换机端口。
:::

## 下一步

从[网络基础](./network-basics.md)或[安全起步](./getting-started.md)开始。
