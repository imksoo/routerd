---
title: 稳定版里程碑
sidebar_label: 稳定版里程碑
sidebar_position: 0
---

# 稳定版里程碑

routerd 以 `vYYYYMMDD.HHmm` 格式频繁发布版本，其中经过评估**可供正式环境使用**的版本，会在每个里程碑时选定为「稳定版里程碑」。初次部署时，请使用本页所列的版本。

## 当前推荐版本

| 项目 | 内容 |
| --- | --- |
| 版本 | **v20260523.1542** |
| 定位 | 推荐稳定版（取代 v20260522.1334） |
| 运行实绩 | 已在正式环境路由器（homert02）上运行；维持 BGP 2-way ECMP，并可通过 graceful restart 以零中断升级 |
| 二进制文件 | 静态链接（`CGO_ENABLED=0`），通过 CI 与 Release workflow |

## 推荐 v20260523.1542 的理由

- **完整承袭 v20260522.1334 的 BGP 控制平面成果。** routerd 不使用 FRR，由自有的 `routerd-bgp` 守护进程维护 eBGP peer；next-hop 改写修正（#26）即使上游广告第三方 next-hop，也能维持 2-way ECMP。
- **修正了 live ISO 的 BGP（#28）。** 在 Alpine/OpenRC 的 live ISO 上，受管理的 GoBGP 守护进程（`routerd-bgp`）现在会在 OpenRC 下启动，因此可从 live ISO 使用 BGP。v20260522.1334 此处有问题，因此不再推荐 1334，尤其是在 live ISO 上使用 BGP 时。
- **新增了内置 DPI classifier 与 NixOS renderer 修正。**
- **已在正式环境运行**，以静态二进制文件发布，并通过 CI。

## 「稳定版」的定义与注意事项

:::warning API 仍为 v1alpha1
「稳定版里程碑」代表**此版本具备正式环境所需的质量**，并**不保证 API（资源 schema）的向下兼容性**。
:::

- routerd 的资源 API 目前为 **v1alpha1**。**版本之间可能包含破坏性变更。**
- 升级时，请勿依赖向下兼容性，应以**配合新 schema 重新撰写配置（YAML）**为前提进行。
- 本项目不提供迁移兼容层。各版本的变更内容请参阅[变更记录（Changelog）](./changelog.md)。

## 安装与升级

安装程序请参阅[安装与升级](../install-and-upgrade.md)。建议以推荐里程碑版本为起点进行升级。
