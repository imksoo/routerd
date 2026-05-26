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
| 版本 | **v20260526.2335** |
| 定位 | 推荐稳定版（取代 v20260526.2241；docs / CI 一致性 follow-up，无运行时行为变化） |
| 运行实绩 | 已在正式环境路由器（homert02）通过 **三次连续的 in-place 升级**（1607 → 2152 → 2241 → 2335）验证：每次 routerd 重启 `routerd-bgp` 都未被触碰（MainPID 2394269 跨四次升级保持不变），BGP 始终维持 2/2 Established，uptime 跨越每次重启持续增长（1h19m → 1h27m → 2h0m → 2h15m → 3h7m → 3h10m），2-way ECMP（.38/.53）保持在 kernel 中，`routerctl doctor dslite` 结果为 pass=12 warn=0，Web Console Gateway Health 画面 180s / 90 samples 观察为 good=90 / bad=0，`install.sh` 以正规 cd-into-package-dir 模式 rc=0 |
| 二进制文件 | 静态链接（`CGO_ENABLED=0`），通过 CI 与 Release workflow |

## 推荐 v20260526.2335 的理由

推荐的理由是**运维成熟度，不是新功能数量**。v20260526.2335 完整继承
v20260526.2241 的正式环境安全特性（而 v20260526.2241 又继承自
v20260526.1607 的 Web Console secrets redaction、`gatewayHealth` 汇总、
可机读 `routerctl doctor`、`ManagementAccess` apply 保护），并新增一项
docs / CI 加固：

- **推荐稳定版的展示不会再静默漂移。** 新增的 CI 守护脚本
  (`scripts/check-active-stable.sh`) 将 `website/src/pages/index.tsx`
  的 `STABLE_VERSION` 作为 source of truth，当 homepage hero、各 locale
  的 intro tip、announcement bar、`docusaurus.config.ts` 指向不同的
  `vYYYYMMDD.HHmm` 时在 CI 中 fail。这是为防止 v20260526.2241 promote
  时出现的 5 处遗留为 `v20260526.1607` 类问题在未来 promote 中再次出现。

从 v20260526.2241 继承并在 2335 的 homert02 apply 中再次验证的 5 项
运维契约：

- **routerd 二进制升级不再使 BGP 会话断开。** BGP 控制器在 reconcile 入口
  会先 hydrate 已 applied 的策略状态，因此 routerd 重启不会再次 PUT 内容
  未变的 import-policy 赋值并重置 BGP 会话。在 homert02 上通过 **两次连续
  的 routerd 重启** 验证（PID 3368318 → 3407972 → 3428160）：BGP 始终维持
  2/2 Established，uptime 跨越每次重启持续增长而非重置，2-way ECMP
  （.38/.53）保持在 kernel 中无需重新安装。
- **`routerctl doctor dslite` 与现实对齐。** Doctor 现在将 DSLiteTunnel
  `phase=Up` 视为健康，并通过 `status.selectedSource = "DSLiteTunnel/<name>"`
  识别 EgressRoutePolicy 的选择（同时保留旧的 `selectedCandidate` 名称匹配）。
  使用 `dslite-pd-balanced` 等聚合候选名称的正式环境配置不再让
  `gatewayHealth=ok` 的 DSLiteTunnel 显示为 WARN。验证结果：warn=4 →
  pass=12 warn=0。
- **Gateway Health UI 拥有独立画面且渲染稳定。** Web Console 将 Gateway
  Health 从 Overview 拆分到独立画面（与 Connections / Clients 一致），
  并显示完整的 `selectedPath` / `preferredPath` / `fallbackReason` /
  `failedProbes` / `lastTransition` 证据。Overview 仅保留汇总卡片。
  partial refresh 期间瞬时显示 `Components 0 / Unknown` 的 flap 问题已修复：
  `reconcileSummary` 在新 snapshot 的 components 为空但旧的有数据时
  保留旧 `gatewayHealth`。验证结果：**180s / 90 samples 中 good=90 /
  bad=0，确认 26 components**。
- **`install.sh` 不能再 silent no-op。** 此前从 release tree 之外执行
  （例如 `cd /tmp/release && ./pkg/install.sh ...`）会让 cwd 相对的
  `bin/*` 通配符一次也不展开，仅 `--with-ndpi-archive` 的 payload 会被
  装上，但脚本仍以 `routerd upgrade completed` 退出 0。现在若 cwd 不
  含 `bin/routerd` payload，则以明确诊断信息 `exit 2` 立即终止。新增的
  CI 回归 smoke (`scripts/install-sh-cwd-smoke.sh`) 覆盖缺失 payload 与
  正规 cwd 两种情况。homert02 验证：cwd-mismatch antipattern **rc=2
  立即 fail**，正规 cd-into-package-dir 模式返回 rc=0。

**继承事项（来自 v20260526.1607 等）：** Web Console 的 `/api/v1/config`
与 generation 端点会在序列化前对 WireGuard `privateKey` / `preSharedKey`、
Tailscale `authKey`、BGP/PPPoE/IPsec `password`、WebConsole
`initialPassword`、bearer/token 字段执行 redact。`/api/v1/summary` 汇总
DNSResolver / DSLiteTunnel / DHCPv6PrefixDelegation / EgressRoutePolicy /
NAT44Rule / HealthCheck 到 `gatewayHealth`。`routerctl doctor` 是 v1alpha1
的可机读契约（`-o json`、文档化的 area / status enum / summary，fail 时
以非 0 退出）。`ManagementAccess` apply preflight 在 `--allow-mgmt-lockout`
之外阻止 lockout。DNS 解析器作为独立长寿命服务单元运行，routerd 重启或
升级期间 DNS 不中断。`install.sh` 在二进制升级时不会自动重启
`routerd-bgp`，eBGP 会话与 ECMP 可跨 routerd binary 更新保持。
`routerctl ledger` 维护（`integrity-check` / `vacuum` / `backup` /
`prune-events`，非 dry-run prune 发出审计事件）。

## 已知观察（不阻塞发布）

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
- **运行 `install.sh` 前请先 `cd` 进入解压后的 release 目录。** 从其他目录执行（例如 `cd /tmp && sudo ./routerd-release-vYYYYMMDD.HHmm/install.sh ...`）会以 `exit 2` 立即终止。这是有意的设计——此前同样的调用会 silent no-op，仅安装 `--with-ndpi-archive` 的 payload。
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
