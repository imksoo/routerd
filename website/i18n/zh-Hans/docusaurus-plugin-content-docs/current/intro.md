---
title: 文档说明
slug: /
sidebar_position: 0
sidebar_label: 概览
---

# routerd 文档

![Diagram showing the routerd documentation map from install and first router goals through concepts, examples, tutorials, how-to guides, operations, API references, platforms, plugins, and schemas](/img/diagrams/intro.png)

routerd 是一个声明式路由器，通过以类型化 YAML 描述的期望状态，在 Linux / NixOS / FreeBSD 上构建可运作的路由器。无需以程序步骤堆叠配置，只需声明您想要的状态，routerd 便会将实际系统收敛至该状态。

请从符合您目的的章节开始阅读。

:::tip 建议的稳定版本
若是全新导入，请从建议的稳定版里程碑 **v20260528.2308** 开始。详情请参阅[稳定版里程碑](./releases/stable.md)。
:::

## 依目的查找

| 想做的事 | 起点 |
| --- | --- |
| 导入或更新 routerd | [导入 → 安装与升级](./install-and-upgrade.md) |
| 了解 routerd 是什么、为何存在 | [入门 → routerd 是什么](./concepts/what-is-routerd.md) |
| 了解与其他产品和方式的定位差异 | [入门 → 定位](./concepts/positioning.md) |
| 第一次构建路由器 | [导入 → 快速入门](./tutorials/getting-started.md) |
| 在浏览器中生成初始配置 | [routerd config wizard](https://routerd.net/wizard) |
| 启用 editor 补全与验证 | [How-to → VS Code YAML schema](./how-to/vscode-yaml-schema.md) |
| 将无磁盘 mini PC 作为路由器 | [导入 → 无磁盘 mini PC](./tutorials/diskless-minipc-walkthrough.md) |
| 理解声明式模型（资源、应用、调和） | [功能说明 → 资源模型](./concepts/resource-model.md) |
| 从已验证的配置示例组建配置 | [配置示例集](./config-examples/index.md) |
| 解决特定部署问题 | [How-to 指南](./how-to/multi-wan.md) |
| 查询资源种类或字段 | [参考文档 → 资源 API](./api-v1alpha1.md) |
| 运维运行中的路由器 | [功能说明 → 调和（reconcile）](/docs/operations/reconcile) |
| 追踪变更内容 | [发行版与稳定版 → 变更记录](./releases/changelog.md) |
| 了解复杂案例的背景 | [知识库](./knowledge-base/dhcpv6-pd-clients.md) |

## 章节一览

- **入门** — routerd 是什么、定位、设计理念
- **导入（快速入门）** — 安装与升级、第一台路由器、各 OS 入门（NixOS / FreeBSD）、无磁盘 mini PC
- **功能说明（声明式模型）** — 词汇表、资源模型、应用与生成、状态与拥有权、调和（reconcile）、Web 管理界面
- **配置参考文档（依功能）** — DNS 解析器、防火墙、Egress・多 WAN、BGP、Tailscale、OpenTelemetry 等各功能的配置方式
- **配置示例集（依场景）** — NAT、LAN 的 DHCP/DNS、DS-Lite、PPPoE、端口转发、访客隔离、多 WAN 故障切换等已验证的配置示例
- **How-to 指南** — Flets 初始配置、IPv6 双协议栈、访客模式、OS 启动配置（bootstrap）、VS Code YAML schema、PVE overlay、故障排查
- **知识库（实际环境知识）** — 从实际环境获得的现场笔记（DHCPv6-PD 客户端、NTT NGN 的前缀委派获取）
- **运维** — 状态数据库、设备清单、USB 持久化、Alpine 部署、密钥、可观测性、备援等
- **参考文档（API・协议・支持环境）** — 资源 API、控制 API、插件协议、支持平台、硬件
- **发行版与稳定版** — 稳定版里程碑、变更记录、发行程序
- **设计笔记** — 架构上的讨论点与设计依据
- **项目** — 贡献方式、许可与法律事务
