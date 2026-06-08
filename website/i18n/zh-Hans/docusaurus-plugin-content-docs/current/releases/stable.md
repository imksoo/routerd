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
| 版本 | **v20260608.2325** |
| 定位 | 推荐稳定版（取代 v20260608.0642。pair-stable SAM transport addressing — `addressingMode: pair-stable` 实现紧凑的 leaf-spine 配置编写） |
| 运行实绩 | 在 lab 环境（7 份紧凑配置验证通过）、k8s 集群（10 节点: 2 RR + 8 leaf，全部 BGP Established，FIB 正确，连通性通过）和生产路由器（homert02，未受影响）上验证完毕。发现 0 个问题 |
| 二进制 | 静态链接（`CGO_ENABLED=0`），通过 CI 和 Release 工作流 |

## 推荐 v20260608.2325 的理由

本版在 v20260608.0642 的基础上添加了 **pair-stable SAM transport addressing**。

### Pair-stable addressing（#330, #331）

`SAMTransportProfile` 新增 `spec.addressingMode: pair-stable`，使用 inner prefix 和 canonical peer key 的 fnv64a 哈希实现确定性的 /31 slot 分配。

- **紧凑配置编写。** leaf 节点不再需要 `topologyNodeRefs`，消除了逐节点重复的拓扑声明。svnet1 配置减少约 100 行。
- **拓扑变更稳定性。** 添加或删除节点不会重新分配现有 peer 的地址（与依赖排序顺序的 `edge-index` 不同）。
- **向后兼容。** 现有的 `edge-index`（默认）配置不受影响。
- **碰撞检测。** `routerd validate` / `routerctl validate` 在配置时检测 /31 slot 哈希碰撞。

### 从 v20260608.0642 继承的事项

继承 v20260608.0642 的全部特性：ADR 0014 CLI 重新设计、OpenRC 可靠性提升、DNS VRRP VIP 支持、forcefrag prerouting 修复、BGP peer watch 稳定化及所有先前的生产安全修复。

## 已知观测（非发布阻塞项）

- **`install.sh` 后 `routerd-bgp` 可能仍以旧 inode 运行。** 这是设计如此。`install.sh` 在升级时不自动重启 `routerd-bgp`，以便已建立的 BGP 会话和 ECMP 在 routerd 二进制更新后存活。
- **未声明 `ManagementAccess` 的配置中 `routerctl doctor mgmt` 显示 SKIP。** 这是运行配置的选择，非发布缺陷。

:::warning 升级注意
- **从 v20260528.2308 升级时：** ADR 0014 变更了 CLI verb 体系。`routerd apply` → `routerctl apply`、`routerd validate` → `routerctl validate` 等。如果服务单元或脚本中使用了旧命令，请重写。`install.sh` 会自动部署新的服务单元，因此 systemd 管理的单元会自动更新。
- **务必先 `cd` 到解压后的发布目录再执行 `install.sh`。**
- **从 v20260523.1542 及更早版本升级时：** `disabled:` 字段（请用 `enabled: false`）和 `--controller-chain*` / `--observe-interval` 标志已删除。
- **DNS 解析器服务单元化：** 解析器以 `routerd-dns-resolver@<name>.service` 运行。首次升级时会有短暂的 DNS 中断。
:::

## 「稳定版」的含义与注意

:::warning API 仍为 v1alpha1
「稳定版里程碑」表示**该版本的质量达到了生产可用的水准**，但**不承诺 API（资源 schema）的向后兼容**。
:::

- routerd 的资源 API 目前为 **v1alpha1**。版本间**可能出现破坏性变更**。
- 升级时请勿依赖向后兼容。请以**按照新 schema 重写配置（YAML）**为前提进行。
- 策略上不提供迁移兼容层。各版本的变更请查阅[变更日志](./changelog.md)。

## 安装与升级

安装步骤请参阅[安装与升级](../install-and-upgrade.md)。建议以推荐的里程碑版本为升级起点。
