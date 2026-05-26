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
| 版本 | **v20260526.1607** |
| 定位 | 推荐稳定版（取代 v20260525.1631） |
| 运行实绩 | 已在正式环境路由器（homert02）验证：routerd 重启与 install 期间 DNS 不中断（NG 0），`/api/v1/config` 返回的原始 secrets 检出数为 0，`gatewayHealth` 汇总 26 components 且 overall=ok，`routerctl doctor` rc=0（pass=32 warn=4 fail=0 skip=1），install 跨过程中 BGP 2/2 与 2-way ECMP 保持 |
| 二进制文件 | 静态链接（`CGO_ENABLED=0`），通过 CI 与 Release workflow |

## 推荐 v20260526.1607 的理由

推荐的理由是**运维成熟度，不是新功能数量**。
v20260526.1607 继承上一推荐版的正式环境安全的 DNS 与 BGP 升级行为，
并新增 4 项已在正式环境路由器（homert02）验证的运维契约：

- **Web Console 不再泄露 secrets。** `/api/v1/config` 与 generation 的
  config / diff 端点会在序列化前对 WireGuard `privateKey` / `preSharedKey`、
  Tailscale `authKey`、BGP/PPPoE/IPsec `password`、WebConsole
  `initialPassword`、bearer/token 字段等执行 redact。键保留并替换为标记
  值，UI 结构不受影响。homert02 实流量验证：**原始 secrets 检出 0**。
- **`gatewayHealth` 汇总整条出口路径。** `/api/v1/summary` 现在统一汇总
  DNSResolver / DSLiteTunnel / DHCPv6PrefixDelegation / EgressRoutePolicy /
  NAT44Rule / HealthCheck。Web Console 横幅显示 selected 与 preferred
  egress path 的对应关系，启用 fallback 候选时会明显警告。homert02 验证：
  **overall=ok / 26 components**。
- **`routerctl doctor` 成为机器可读的稳定契约。** `-o json` 输出作为
  v1alpha1 运维契约（area、status 枚举、summary、退出码）已文档化；
  fail 时以非 0 退出，便于脚本调用。homert02 验证：
  **rc=0（pass=32 warn=4 fail=0 skip=1）**。
- **`ManagementAccess` 声明式 apply 保护。** apply 前的 preflight 在管理
  接口缺失、firewall 会切断 SSH、WebConsole 绑定到所有地址时**中止非
  dry-run apply**（可用 `--allow-mgmt-lockout` 覆盖）。相同检查也由
  `routerctl doctor mgmt` 公开。

**继承事项（来自 v20260525.1631 等）：** DNS 解析器作为独立的长寿命服务
单元运行，routerd 重启或升级期间 DNS 不中断（homert02 验证：
`routerd.service` 重启与 install 中 DNS probe NG 均为 0）。`install.sh`
在二进制升级时不会自动重启 `routerd-bgp`，eBGP 会话与 ECMP 可跨 routerd
binary 更新保持（homert02 验证：2/2 Established、2-way ECMP、HTTP 200 跨
install 维持）。完整 BGP 控制平面（不使用 FRR；#26 next-hop 改写、#28
OpenRC live ISO 启动）。`routerctl ledger` 维护（`integrity-check` /
`vacuum` / `backup` / `prune-events`，非 dry-run prune 发出审计事件）。

## 已知观察（不阻塞发布）

- **DS-Lite doctor 可能在 egress 健康时仍出现 WARN。** 当 AFTR 的 AAAA
  探测或 tunnel device 检测偶发噪声时，doctor 的 `dslite` area 可能给出
  WARN，即便 `gatewayHealth=ok` 且实际 egress（HTTP 200）正常。这属于
  保守型诊断噪声，并非 dataplane 故障。后续调优将让 DS-Lite doctor
  severity 与 `gatewayHealth` 的 selected-path 证据对齐。
- **`install.sh` 后 `routerd-bgp` 可能以旧 executable inode 继续运行。**
  这是预期行为：`install.sh` 在升级时不会自动重启 `routerd-bgp`，从而
  保证已建立的 BGP 会话与 ECMP 跨 routerd binary 更新保留。直到运维人员
  在 Graceful Restart 时机执行 `systemctl restart routerd-bgp` 之前，
  进程将继续持有旧 inode。
- **未声明 `ManagementAccess` 时 `routerctl doctor mgmt` 会 SKIP。**
  这是 live config 的选择，不是发布缺陷——该保护是 opt-in 的。要启用
  apply 锁出保护与 doctor mgmt 的判定，请声明 `ManagementAccess` 资源
  （参见 [`examples/home-router-mgmt-protected.yaml`](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)）。

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
