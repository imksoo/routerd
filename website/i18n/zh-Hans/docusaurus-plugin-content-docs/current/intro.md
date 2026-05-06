---
title: 文档
slug: /
sidebar_position: 0
sidebar_label: 总览
---

# routerd 文档

routerd 把类型化的 YAML 资源,变成在 Linux / NixOS / FreeBSD 主机上运行、可观测的路由器。请按目的选择章节。

## 按目的选择

| 想做什么 | 从这里开始 |
| --- | --- |
| 了解 routerd 是什么、为何存在 | [概念 → routerd 是什么](./concepts/what-is-routerd.md) |
| 第一次架路由器 | [教程 → 入门](./tutorials/getting-started.md) |
| 解决特定部署场景 | [How-to 指南](./how-to/multi-wan.md) |
| 查阅资源 Kind 或字段 | [参考 → Resource API](./reference/api-v1alpha1.md) |
| 运维运行中的路由器 | [运维 → Reconcile](./operations/reconcile.md) |
| 看实战场景的背景笔记 | [知识库](./knowledge-base/dhcpv6-pd-clients.md) |
| 了解最近变动 | [版本 → Changelog](./releases/changelog.md) |

## 章节列表

- **概念 (Concepts)** — 愿景、设计理念、资源模型、所有权语义
- **教程 (Tutorials)** — 安装、第一台路由器、WAN/LAN 服务、基本防火墙、NixOS 快速上手
- **How-to** — 多 WAN、FLET'S 初设、PVE overlay、OpenTelemetry 导出、疑难排查
- **知识库 (Knowledge base)** — 实际部署现场笔记 (DHCPv6-PD client、NTT NGN PD 获取)
- **参考 (Reference)** — 资源 API、控制 API、插件协议、支持平台、所有权规则
- **运维 (Operations)** — Reconcile 与删除、状态数据库、主机 inventory
- **设计笔记 (Design notes)** — 架构未决事项与设计依据
- **版本 (Releases)** — 变更记录
