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
| 版本 | **v20260608.0642** |
| 定位 | 推荐稳定版（取代 v20260528.2308。ADR 0014 CLI 体系重新设计 — `routerd` 仅作为守护进程，`routerctl` 作为管理 CLI。OpenRC 管理可靠性提升、DNS 解析器支持 VRRP VIP 监听、forcefrag 移至 prerouting、BGP peer watch 稳定化） |
| 运行实绩 | 在 lab 环境（router06/router07/k8s-rt-01/k8s-rt-02）和生产路由器（homert02）上验证完毕。Cloud VM 测试（lab + k8s）全部 PASS。解决 12 个 issue，合并 12 个 PR |
| 二进制 | 静态链接（`CGO_ENABLED=0`），通过 CI 和 Release 工作流 |

## 推荐 v20260608.0642 的理由

本版继承了 v20260528.2308 的所有生产安全特性，在此基础上以 **CLI 体系重新设计**（ADR 0014）和 **OpenRC / init 脚本可靠性提升**为核心，加入了 40 个提交的改进。

### ADR 0014 — CLI 体系重新设计

routerd 的 CLI 被清晰地拆分为「守护进程」和「管理工具」。

- **`routerd`** 仅作为守护进程。唯一的子命令是 `routerd serve`。
- **`routerctl`** 是管理 CLI：`validate` / `plan` / `apply` / `doctor` / `get` / `describe` / `status` / `ledger` / `dns-queries` / `traffic-flows` 等全部管理操作。
- 旧有的 `routerd apply` / `routerd validate` / `routerd run` 已移除。`--once` 标志也已废弃。
- 文档和脚本中的命令引用已全部更新为新的 verb 体系（#254–#262）。

### OpenRC / init 脚本可靠性

针对 FreeBSD 和 OpenRC 环境的 init 脚本管理应用了 6 项修复。

- **消除 OpenRC DNS 解析器的双重管理**（#306）— 此前 `routerd serve` 和 OpenRC 同时尝试管理 DNS 解析器，导致双重启动。
- **OpenRC 升级时停止旧的 `routerd serve`**（#311, #313）— 修复升级过程中旧进程残留的问题。
- **OpenRC 重启时清理托管的 helper**（#315）— 防止孤儿 helper 进程积累。
- **DNS 解析器 helper 监控**（#283）— OpenRC 现在能正确监控和启动 DNS 解析器的 helper 进程。
- **残留 helper 更新**（#280）和 **OpenRC 重启的 nodeps 化**（#278）— 解决升级时的服务依赖问题。

### 网络功能改进

- **DNS 解析器可在 VRRP VIP 上监听**（#319）— `IP_FREEBIND` / `IPV6_FREEBIND` 套接字选项允许绑定尚未分配的地址。DNS 服务可在 VRRP 备份节点上预先启动。
- **forcefrag DF 清除移至 prerouting hook**（#328）— forward hook 使用的 `oifname` 在 prerouting 中不可用，改为使用 `fib daddr oifname` 查询路由表。修复了 MSS clamp 未正确应用的情况。
- **消除 BGP peer watch 的不必要更新**（#329）— `desiredPeerMatches()` 使用 `reflect.DeepEqual` 导致每次 reconcile 都因 `dynamicExportPrefixes` 变化和 GracefulRestart 格式不一致（`"2m"` vs `"120s"`）而触发 `UpdatePeer`。引入稳定比较函数 `stableDesiredPeerEqual`，在配置语义相同时抑制更新。
- **`routerd serve` 启动时自动启用 loopback**（#321）— 在 Live ISO 和容器环境中 `lo` 可能处于 down 状态时，自动执行 `ip link set lo up`。

### 安装器改进

- **bootstrap 安装器可靠清理临时目录**（#324）— `exec sh ./install.sh` 导致 EXIT trap 不触发的问题已修复。
- **安装器 apply state 警告修复**（#327）— 将 `routerctl get status` 输出格式改为 `-o json` 以准确判定 `lastApplyTime`。
- **BGP peer state watch 实现 status 即时更新**（#304）— BGP 会话状态变化即时反映到 status。
- **重启不活跃的 keepalived 以修复 VRRP**（#299）— 修复某些情况下 VRRP 故障转移不正常的问题。

### 文档

- **添加日语正本翻译 37 篇 + 中文翻译 80 篇**（#322）— 覆盖所有分类：ADR / explainer / how-to / ops / reference / releases / evidence / slides。日语作为正本，zh-Hans / zh-Hant 作为翻译。
- **所有文档图表以 gpt-image-2 重新生成**（#261）— 统一视觉风格。

### 从 v20260528.2308 继承的事项

v20260528.2308 的所有生产安全特性均已继承。

- fd 泄漏修复（#39 SQLite ledger、#40 Unix socket / BGP gobgp client）
- heap 泄漏修复（OTel instrument singleton、bounded reverse DNS cache）
- `routerctl doctor runtime` 持续资源监控
- BGP 会话跨 routerd 二进制升级存活
- `doctor dslite` selectedSource 对齐
- 网关健康度独立画面
- `install.sh` 即时失败（载荷缺失检测）
- 密钥脱敏
- `ManagementAccess` 应用守卫
- 机器可读 `routerctl doctor`（`-o json`）

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
