---
title: 词汇表
sidebar_label: 词汇表
sidebar_position: 1
---

# 词汇表

![将 routerd 术语按声明式资源、runtime evidence、host artifact 和 networking behavior 组织的关系图](/img/diagrams/concept-glossary.png)

routerd 文档中使用的主要术语与译词。

## 网络术语

| 英文 | 译词（本文档） | 备注 |
| --- | --- | --- |
| interface | 接口 | 主机上的网络接口 |
| route / routing | 路由 | 转发条目及其选择 |
| gateway | 网关 | 离开网络时使用的下一跳路由器 |
| NAT | NAT | |
| NAPT | NAPT | 动态的多对一转换 |
| firewall | 防火墙 | routerd 的区域式有状态过滤功能 |
| filter / rule | 过滤条件 / 规则 | 单条允许或拒绝规则 |
| prefix delegation (PD) | 前缀委派（PD） | DHCPv6 前缀委派 |
| upstream | 上游 | DNS 或路由的上游侧 |
| egress / ingress | egress / ingress | 出向 / 入向，保留英文 |

## 声明式模型术语

| 英文 | 译词（本文档） | 备注 |
| --- | --- | --- |
| declarative | 声明式 | 描述期望状态而非步骤 |
| resource | 资源 | |
| Kind | Kind（类别） | 保留大写 Kind |
| spec | spec | 期望状态 |
| status | status | 实际观测状态 |
| apply | 应用 | `routerctl apply` 的动作 |
| reconcile | 调和（reconcile） | 使实际状态趋近期望状态的处理 |
| controller | 控制器 | |
| render | 生成（render） | 由资源组装出配置文件等产物 |
| daemon | 守护进程 | |
| generation | 世代（generation） | SQLite 的世代编号 |
| ownership | 拥有 / 拥有权 | |
| bootstrap | 引导配置（bootstrap） | |
| Tier (H/S/C/E) | Tier H / Tier S … | 功能阶段的专有名词 |

## 其他标记

- **Web Console**（routerd 的网页 UI）写作「Web 管理界面」。但 `WebConsole`（启用该 UI 的 Kind 名称）是代码标识符，保留原文。
- **HGW** 保留为「HGW」，不展开。
