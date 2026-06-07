---
title: 定位
slug: /concepts/positioning
---

# 定位

![routerd 作为 local declarative router control plane 的适用范围及其边界](/img/diagrams/concept-positioning.png)

routerd 是一个本地控制平面，用来构建可以从配置理解、也可以从运行状态说明的路由器。

routerd 不是完整网络操作系统的替代品，也不是从外部统一管理大量路由器的云端控制器。routerd 在每台路由器主机本地运行，将类型化的 YAML 资源转换为主机的网络、服务、路由、隧道、防火墙、日志与状态。

## 重视的事

routerd 重视以下几点运维方式。

- 可以纳入 git 管理的声明式路由器配置
- 不依赖托管控制器、可本地自主运作
- 对生成的主机产物有明确的拥有关系
- 以事件而非隐藏在守护进程内部的状态来说明现况
- 在危险变更前确认管理路径
- 能追溯路由、隧道与防火墙判断依据的可观测性

主要对象包含：家庭实验室、小型办公室、使用 Proxmox VE 或 KVM 的开发者，以及想把手写 Linux 路由脚本替换为可重现机制的人。

## 覆盖范围

| 领域 | 示例 |
| --- | --- |
| WAN 接入 | DHCPv4、DHCPv6-PD、DHCPv6 信息请求、PPPoE |
| IPv4 过渡 | DS-Lite、NAT44、多段 WAN 故障切换 |
| LAN 服务 | DHCPv4、DHCPv6、RA、DNS、NTP |
| 路由 | 静态路由、策略路由、EgressRoutePolicy、健康检查 |
| 安全 | 三角色防火墙、访客模式、拒绝日志 |
| Overlay | WireGuard、Tailscale 整合、VXLAN 基础、VRF |
| 运维 | Web 管理界面、`routerctl`、OpenTelemetry、日志存储 |
| 初始构建 | 软件包管理、sysctl profile、systemd unit、live ISO |

routerd 所覆盖的范围，两端相距颇远。

- **虚拟 SDN / VNET 间的路由：** 连接 Proxmox VE SDN、WireGuard overlay、VRF、VXLAN 实验及实验室策略路由的路由器 VM。
- **无磁盘 PC 路由器：** 小型 x86 mini PC 从 live ISO 启动，从 USB 还原 `router.yaml`，将日志保存在 RAM 中，并提供实体 LAN。

很少有路由器项目把这两种场景视为同一个配置问题。routerd 选择这样做。差异主要在于生成的主机产物，而非意图模型本身。

覆盖范围广是有意义的。路由器故障往往发生在功能边界处。DNS 的选择可能依赖 DHCPv6 信息选项；DS-Lite 隧道可能依赖只能通过特定上游解析的 AFTR 记录；路由应在健康检查确认后才晋升为主要路由。routerd 将这些关系统一置于同一份资源图中。

## 与 shell 脚本的差异

shell 脚本容易上手，但事后很难审计。它们只能回答「执行了什么命令」，却无法保留「现在应该存在什么状态」。

routerd 将期望状态保存在 YAML 中，记录观测状态，发出事件，并通过 API、CLI 与 Web 管理界面呈现结果。这使得差异比较、世代回溯及实际流量的调试都更加容易。

## 与设备固件的差异

设备固件在用途符合 UI 设计时相当方便。然而，当需要精确组合 DS-Lite、PPPoE 故障切换、本地 DNS、自定义防火墙、OpenTelemetry 或实验室 overlay 网络时，操作往往变得困难。

routerd 将这些功能视为资源来处理。Web 管理界面用于查阅与排查，配置变更则以 CLI 和 YAML 为准。

## 与 Kubernetes 式控制器的差异

routerd 借鉴了资源与控制器的概念，但不需要集群。边界是主机本身，调和（reconcile）的对象是内核、本地守护进程与本地文件。

这种形式让 routerd 足够精简，可作为家庭路由器运行，同时仍能让 DHCP、DNS、隧道、健康检查、路由、防火墙日志与遥测以事件驱动方式协同运作。

## 非目标

routerd 目前不以下列为目标。

- 托管型 SDN 控制器
- 远程插件市场
- 通用防火墙语言
- 取代所有企业路由器功能
- GUI 优先的配置系统

routerd 重视明确的 YAML、本地控制与高质量的运维信息，而非功能繁多的点击式管理界面。
