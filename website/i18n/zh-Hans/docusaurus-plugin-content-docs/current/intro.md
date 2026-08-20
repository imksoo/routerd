---
title: 从这里开始
slug: /
sidebar_position: 0
sidebar_label: 开始
---

# 欢迎使用 routerd

![routerd 文档地图：从安装、第一台路由器，到概念、配置示例、教程、运维和 API 参考](/img/diagrams/intro.png)

routerd 用一份 YAML 描述“这台机器应该怎样当路由器”，再把这个目标转换为主机上的网络设置和服务。把它想成给路由器写一张清楚的清单：哪个网口接上游，哪个网口给本地设备使用，以及要提供哪些服务。

第一次练习请选一台**隔离的 Ubuntu Server 虚拟机或备用电脑**。Ubuntu Server + systemd 是目前最适合跟随本入门路线的平台。NixOS 和 FreeBSD 有集成基础设施，但功能和操作体验并不与 Ubuntu 路径完全相同；请在实际部署前阅读[支持的平台](./platforms.md)。

## 先分清两个命令

- `routerd` 是主程序。它可以直接检查本地文件：`routerd validate --config …`；也可以做一次不提交网络变更的试运行：`routerd apply --once --dry-run …`。
- `routerctl` 是给**已经运行的** `routerd serve` 服务发请求的客户端。它通过本机 Unix socket 读取状态、提交候选配置或请求计划；它不是离线 YAML 检查器。

这一区别很重要：在服务尚未启动时，请使用 `routerd validate`，不要先运行 `routerctl validate` 或 `routerctl plan`。

## 最安全的第一条路线

1. 准备 VM 控制台、串口控制台，或一张完全独立的管理网卡。
2. 按[安装与升级](./install-and-upgrade.md)在 Ubuntu Server 上安装程序。
3. 读[网络基础](./tutorials/network-basics.md)，再完成[安全起步](./tutorials/getting-started.md)：先验证，再用临时路径执行 dry-run。
4. 确认不会断开管理连接后，才让 `routerd serve` 或 systemd 服务运行真实配置。
5. 服务运行后，用 `routerctl get status` 和 `routerctl describe …` 观察结果。

:::caution 不要拿生产网络做第一次实验
`routerd apply --once`（不带 `--dry-run`）和 `routerd serve` 都可能改变主机网络。不要在唯一承载家庭、学校或工作网络的路由器上边远程 SSH 边试验。先保留控制台或独立管理路径。
:::

:::caution 防火墙不是安全认证
routerd 仍处于预发布阶段。防火墙资源还在基础实现阶段，不是通用防火墙语言，也不是安全认证。不要把本网站的一份示例当成面向互联网设备的唯一防线。
:::

## 按目标选页面

| 想做什么 | 从这里开始 |
| --- | --- |
| 认识 WAN、LAN、DHCP、DNS 和 NAT | [网络基础](./tutorials/network-basics.md) |
| 安装或升级 Ubuntu Server 上的 routerd | [安装与升级](./install-and-upgrade.md) |
| 不改网络地检查第一份 YAML | [安全起步](./tutorials/getting-started.md) |
| 让 WAN 通过 DHCP 获取 IPv4，给 LAN 设置网关 | [第一台实验路由器](./tutorials/first-router.md) |
| 看一个完整的 IPv4 NAT 形状 | [基本 IPv4 NAT 网关](./config-examples/basic-ipv4-nat.md) |
| 了解“应用”“渲染”“调和”分别是什么意思 | [应用与渲染](./concepts/apply-and-render.md) |
| 了解受支持的平台与边界 | [支持的平台](./platforms.md) |
| 服务已经运行，想检查健康状况 | [routerctl doctor](./operations/routerctl-doctor.md) |

## 接下来

- [安装 routerd](./tutorials/install.md)
- [安全起步：验证和 dry-run](./tutorials/getting-started.md)
- [配置示例集](./config-examples/index.md)
