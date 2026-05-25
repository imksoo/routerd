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
| 版本 | **v20260525.1631** |
| 定位 | 推荐稳定版（取代 v20260525.0112） |
| 运行实绩 | 已在正式环境路由器（homert02）上运行；维持 BGP 2-way ECMP，DNS 解析器可跨 routerd 重启持续应答，并可通过 graceful restart 以零中断升级 |
| 二进制文件 | 静态链接（`CGO_ENABLED=0`），通过 CI 与 Release workflow |

## 推荐 v20260525.1631 的理由

- **DNS 可跨 routerd 重启持续应答。** `DNSResolver` 现在作为独立的长寿命服务单元（`routerd-dns-resolver@<name>.service`）运行：重启或升级 routerd 不再中断 DNS，配置变更（包括 DHCPv6-PD 收敛）通过守护进程的 reload 端点就地生效而无需重启进程，`routerctl restart-dns-resolver` 可显式恢复。它在启动时也会部分拉起：先以已解析的 listen 地址与 source 应答（`phase: Degraded` 与 `waiting` 列表）并收敛为 `Applied`，因此不存在等待前缀委派时拒绝 DNS 的启动窗口。
- **完整具备 BGP 控制平面成果。** routerd 不使用 FRR，由自有的 `routerd-bgp` 守护进程维护 eBGP peer；next-hop 改写修正（#26）即使上游广告第三方 next-hop 也能维持 2-way ECMP，且 Alpine/OpenRC 的 live ISO 会在 OpenRC 下启动 `routerd-bgp`（#28）。
- **升级不再扰动 BGP 与 DNS。** `install.sh` 在二进制升级时不再自动重启 `routerd-bgp` 或 DNS 解析器，因此 eBGP 会话、ECMP 与 DNS 可跨 routerd 更新保持。
- **运维更轻松。** `routerd rollback --list` / `--to <generation>` 可重新应用已保存的配置世代，`routerctl set-log-level` 可在运行时更改日志详细度，`routerctl describe` 会显示 Phase、Reason、Message 及修复提示。
- **非 root 获取 status。** 只读 status socket 以 `root:routerd`、模式 `0o660` 创建，因此属于 `routerd` 组的运维人员无需 sudo 即可执行 `routerctl status`。
- **已在正式环境（homert02）运行**，以静态二进制文件（`CGO_ENABLED=0`）发布，并通过 CI 与 Release workflow。

:::warning 升级注意事项
- **从 v20260523.1542 或更早版本升级：** 已移除 `disabled:` 字段（请改用 `enabled: false`）以及无操作的 `--controller-chain*` / `--observe-interval` 旗标。请在升级前重写相关配置与主机 service unit。
- **DNS 解析器服务单元化：** 解析器现在作为 `routerd-dns-resolver@<name>.service` 运行。首次升级到该模式时会进行一次「子进程 → 单元」切换，期间有一次短暂的 DNS 中断；此后 routerd 的重启与升级不再中断 DNS。
:::

## 「稳定版」的定义与注意事项

:::warning API 仍为 v1alpha1
「稳定版里程碑」代表**此版本具备正式环境所需的质量**，并**不保证 API（资源 schema）的向下兼容性**。
:::

- routerd 的资源 API 目前为 **v1alpha1**。**版本之间可能包含破坏性变更。**
- 升级时，请勿依赖向下兼容性，应以**配合新 schema 重新撰写配置（YAML）**为前提进行。
- 本项目不提供迁移兼容层。各版本的变更内容请参阅[变更记录（Changelog）](./changelog.md)。

## 安装与升级

安装程序请参阅[安装与升级](../install-and-upgrade.md)。建议以推荐里程碑版本为起点进行升级。
