---
title: 定位
slug: /concepts/positioning
---

# 定位

routerd 是一个本地控制平面，用来构建可以从配置理解、也可以从运行状态解释的路由器。

routerd 不是完整网络操作系统的替代品。它也不是从外部管理大量路由器的云控制器。routerd 在每台路由器主机本地运行，把带类型的 YAML 资源转换为主机网络、服务、路由、隧道、防火墙、日志和状态。

## routerd 重视什么

routerd 重视以下运维方式。

- 可以放入 git 管理的声明式路由器配置
- 不依赖托管控制器的本地运行
- 对生成的主机文件和服务有明确所有权
- 通过事件说明状态，而不是隐藏在守护进程内部
- 在危险变更前检查管理路径
- 能解释路由、隧道和防火墙判断存在的原因

典型用户包括家庭实验室、小型办公室、使用 Proxmox VE 或 KVM 的开发者，以及想把手写 Linux 路由脚本替换为可重复机制的人。

## 覆盖范围

| 领域 | 示例 |
| --- | --- |
| WAN 接入 | DHCPv4、DHCPv6-PD、DHCPv6 信息请求、PPPoE |
| IPv4 过渡 | DS-Lite、NAT44、多阶段 WAN fallback |
| LAN 服务 | DHCPv4、DHCPv6、RA、DNS、NTP |
| 路由 | 静态路由、策略路由、EgressRoutePolicy、健康检查 |
| 安全 | 三角色防火墙模型、访客模式、拒绝日志 |
| Overlay | WireGuard、Tailscale 集成、VXLAN 基础、VRF |
| 运维 | Web Console、`routerctl`、OpenTelemetry、日志存储 |
| 初始构建 | 软件包、sysctl profile、systemd unit、live ISO |

这个范围的两端相距很远。

- **虚拟 SDN/VNET 间路由：** 连接 Proxmox VE SDN、WireGuard overlay、VRF、VXLAN 实验和实验室策略路由的路由器 VM。
- **无盘 PC 路由器：** 小型 x86 mini PC 从 live ISO 启动，从 USB 恢复 `router.yaml`，把日志保存在 RAM 中，并提供物理 LAN。

很少有路由器项目把这两端当成同一个配置问题。routerd 会这样做。差异主要在生成的主机成果物，而不是意图模型。

范围广是有意义的。路由器故障常常发生在边界处。DNS 选择可能依赖 DHCPv6 information option。DS-Lite 隧道可能依赖只能通过特定 upstream 解析的 AFTR 记录。路由应该在健康检查确认以后才成为 primary。routerd 把这些关系放在同一个资源图中。

## 与 shell 脚本的区别

shell 脚本容易开始，但后续很难审计。它们常常能回答“执行了什么命令”，却不能回答“现在应该存在什么状态”。

routerd 把期望状态保存在 YAML 中，保存观测状态，发出事件，并通过 API、CLI 和 Web Console 暴露结果。这样更容易检查漂移、比较世代，以及调试真实流量。

## 与设备固件的区别

当用途符合 UI 设计时，设备固件很方便。可是如果需要精确组合 DS-Lite、PPPoE fallback、本地 DNS、自定义防火墙、OpenTelemetry 或实验室 overlay 网络，操作会变得困难。

routerd 把这些功能作为资源处理。UI 用于读取和排查。配置变更仍然以 CLI 和 YAML 为准。

## 与 Kubernetes 风格控制器的区别

routerd 借用了资源和控制器的思想，但不需要集群。边界是主机。被调整的是内核、本地守护进程和本地文件。

这种形态足够小，可以作为家庭路由器运行，同时仍然允许 DHCP、DNS、隧道、健康检查、路由、防火墙日志和 telemetry 通过事件协同。

## 非目标

routerd 当前不以以下目标为主。

- 托管型 SDN 控制器
- 远程插件市场
- 通用防火墙语言
- 替代所有企业路由器功能
- GUI 优先的配置系统

routerd 重视明确的 YAML、本地控制和高质量运维信息，而不是广泛的点击式管理界面。
