# ADR 0008: 基于 fencing token 的 Capture 协调（epoch 围栏级别投影）

![ADR 0008 的示意图。capture 协调的风险、captureEpoch fencing、带戳的 provider action、stale action 的拒绝、幂等的级别投影](/img/diagrams/adr-0008-capture-coordination-fencing.png)

## 状态

已提议。批准为实验性实现 — 2026-05-31。

此 ADR 以 [ADR 0006: CloudEdge Event Federation](../adr/0006-event-federation.md)、
[ADR 0007: Provider Action Execution](../adr/0007-provider-action-execution.md) 和
[Selective Address Mobility](../reference/selective-address-mobility) 数据平面为基础。
**属实验性质**。

替换"持久化 de-provision 标记"修复（commit 26f2a729、issue #70）中引入的
de-provision 机制。该修复将 unassign 设为**持久化的**，但保留了
**命令式 cancel** 路径（当地址重新变为 desired 时，取消进行中的 de-provision）。
该 cancel 路径是非确定性的 — reconcile 时序与执行竞争 — 打补丁修复状态汇合点
无法消除 flaky。此 ADR 用 **epoch 围栏级别投影**替换之。

## 背景

Selective Address Mobility 的移动 `/32` 是**具有唯一性约束的共享资源，
在任意时刻恰好有一个 capture 持有者**（any-origin 对称仲裁的单一所有者不变量）。
"持有"地址意味着拥有物理 capture：云端 NIC 上的 provider **secondary IP**
分配（AWS ENI / Azure NIC / OCI VNIC），或 on-prem 的 **proxy-ARP + GARP**。

capture 以两种方式在持有者之间转移：

- **协调式 / 计划式** — 维护排空。活跃持有者配合。
- **突发式 / 故障** — 持有者的主机停止或分区。*无法配合*，
  备用方需要 seize（夺取）capture。

de-provision（secondary IP 的 unassign / 转发的禁用）是 capture 的**释放**，
assign 是**获取**。此 bug 表现为 flaky 测试
（`TestServeChainMobilityCancelsPendingDeprovisionWhenDesiredAgain`，
无 `-race` 约 3/30 失败）：
re-capture 时进行中的 de-provision 有时未被取消，
遗留了孤立的标记 / pending action。补丁修复 cancel 的汇合点无法消除 flaky。
**对进行中工作的命令式取消是 level-trigger reconciler 的错误抽象**。

### 参考的理论（分布式协调）

- **Fencing token**（Kleppmann, *How to do distributed locking*）：带 TTL 的
  lease/lock 是*活性*所必需的（停止的持有者 lease 过期，备用方可接管），
  但***安全性*不充分** — 暂停/延迟/复活的（"僵尸"）旧持有者在 lease 过期后仍可能
  执行操作。"在写入前检查过期时间无法修复。"唯一的修复是
  **受保护资源**检查的**单调递增 fencing token**，
  拒绝 token 低于已见最大值的操作。
- **Generation / term / epoch**：Raft 的 *term*、ZooKeeper 的 *epoch* / *zxid* 等是同样的
  单调递增 fencing token，用于**僵尸围栏**和偏离状态的 reconcile。
  "下游系统必须拒绝带有 stale epoch 的操作。"
- **Level-trigger reconciliation**（Kubernetes 控制器）：每 tick 从观测状态
  reconcile 到 desired 状态。**幂等**。不在边沿上运作。
  嫁接到 level 循环上的边沿逻辑（"re-desire 时取消 X"）会产生竞争。
- **脑裂 / HA 故障切换**（Pacemaker STONITH、keepalived VRRP + EC2
  `AssignPrivateIpAddresses`）：浮动 IP 恰好由 1 个 master 持有
  （IPaddr2 + GARP）。STONITH 在接管前保证旧节点停止。
  心跳间隔权衡检测延迟和脑裂风险 — 但**不提供安全性**。
  安全性来自 fencing/仲裁。

### routerd 特定的约束

此处的"受保护资源"是**云 provider API 和 on-prem 的 ARP 表**，
两者都不原生检查 fencing token — AWS 不会因为 epoch 34 已发生而拒绝
"带 epoch 33 的 unassign"。**无法将 fencing 推到实际资源层面。**
routerd 需要在**自身控制的最后一道门**强制执行 fence：
action 导入 / executor 边界（"fencing proxy"模式）。

## 决策

### 1. `captureEpoch` — 每 (pool, address, captureDomain) 的单调递增 fencing token

持久化的**严格单调递增本地计数器**。
以 `(pool, address, captureDomain)` 为键，每当 **desired capture 持有者**变更时递增
— 包括向之前的持有者 re-capture。与 `AddressLease` 的 epoch **不同**：

- `AddressLease` epoch = **位置所有者**（拥有地址者）的 epoch。
- `captureEpoch` = **物理 capture 持有者**（attach secondary IP /
  响应 proxy-ARP 者）的 epoch。

这是不同的生命周期，不得混淆。**wall-clock time（`now`）
不得用作 token** — 跨节点非单调，会导致 churn。
这是被替换修复的潜在缺陷。`captureDomain` 是 placement group 的
范围（`provider:<ref>:placement:<group>`），同一 provider group 内
争夺同一地址的所有 routerd 共享一条 epoch 线。

### 2. 所有 provider action 打上 `(captureEpoch, captureKey, holder)` 戳

planner 为 `assign-secondary-ip`、`unassign-secondary-ip`、转发 action 打上
`captureEpoch`、`captureKey`、action 的目标持有者（acquire → desired 持有者，
release → 退出节点）戳。`idempotencyKey` 以 `:epoch:<N>` 为后缀，因此
capture epoch N 的 action 与 epoch N+1 的 action 具有不同的稳定 key — 且
**在同一 epoch 内的 reconcile 间保持稳定**（无 churn）。

### 3. de-provision 意图是级别投影，不是工作队列

de-provision 工作集 = 以当前 `captureEpoch` 评估的
*(之前 capture 过的 − 当前 desired)* 的**投影**，每次 reconcile 重新计算。
re-capture 不"取消"任何东西：地址重新进入 desired 状态因此从投影中移除，
`captureEpoch` 递增。不存在命令式 cancel 路径。

**持久化标记表作为 outbox 保留**（仅靠 `DynamicConfigPart`
在导入前会丢失意图 — 原始 #70 故障）。但标记是
**epoch 键控的投影项**，而非可取消的边沿状态。
stale 标记由同一 fence（`dropStaleDeprovisionMarkers`）清除。

### 4. 在导入 / executor 门控处围栏

导入地址 X 的 provider action 前、以及扫描日志时，
将其 `captureEpoch`/holder 与 X 的**当前** `captureEpoch` 比较：

- epoch 与当前不匹配，**或** holder 不再是当前者的 acquire，
  **或** holder 仍是当前者的 release → action 为 **stale** → 跳过（围栏），
  已导入的 pending/approved stale action 标记为 `skipped`。
  试图复活被替换标记的旧 reconcile 因持有旧 epoch 而在此门控处被终止。

此单一确定性门控**替换**了分散的
`cancelMarkerPlansForDesired` / `CancelActionByIdempotencyKey` 取消逻辑。

### 5. 为何安全 — 以及诚实的限制

- **节点内**: 本地 `captureEpoch` 门控在节点的 reconcile 循环内是单调且串行的。
  确定性地围栏 stale 的本地 reconcile。这是消除 #70 flaky 的机制。
- **节点间**（对先前过度声明的纠正 — 每节点 DB 门控在跨节点时
  **不是 linearizable 的**）：安全性是**结构性的**，来自
  (a) provider 的**单一分配语义** — secondary IP 恰好存在于一个 NIC 上 —
  与 (b) **带 reassignment 的 acquire**（AWS `assign-private-ip --allow-reassignment`
  将 IP *原子地移动*，不等待停止的持有者释放 — release-before-acquire 会在
  主机故障时丧失活性）和
  (c) **NIC 范围**的 stale 操作（旧持有者的 `unassign` 仅针对自身 NIC，
  无法剥离新持有者 NIC 上的 IP）的组合。
- **On-prem 的 proxy-ARP 更弱**。不得伪装成与云端等价：
  没有原子的 reassignment。此处的安全性依赖于
  **作为 capture 权限的 VRRP/keepalived master 状态** — 非活跃节点 **fail-closed**
  （无 proxy-ARP、无 route lowering），仅 master 发出 proxy-ARP + GARP — 。
  分区下的完全安全性在无 STONITH / 仲裁时不可实现，
  超出范围。
- **活性与安全性预算**: lease TTL / 心跳间隔调节*检测延迟*
  （太短 → 震荡，太长 → 恢复慢）。对应 keepalived 的 `advert_int` 和
  现有的 `deprovisionHoldDuration` 滞后。**安全性不得依赖此旋钮**
  — 提供安全性的仅有单调递增 `captureEpoch`。Kleppmann 教训的具体化。

## 阶段划分

- **Phase A（此 ADR 的最小范围 — 确定性修复 #70）**: 引入 `captureEpoch`。
  为 action 打戳。将标记改为 epoch 键控的级别投影。
  epoch stale / holder 不匹配时在导入处围栏。**移除** cancel 路径和
  wall-clock 生命周期 key。验收条件：
  `TestServeChainMobilityCancelsPendingDeprovisionWhenDesiredAgain`
  以 `-count=100`（及 `-race`）确定性通过，断言放宽（`< 2`）
  替换为精确的确定性计数，re-emit 测试保持 green，
  不通过放宽测试来通过。
- **Phase B（后续）**: 用于突发 seize 的 execute-time 门控（在 import-time 之外）。
- **Phase C（后续 — 故障切换功能）**: **活性驱动的放置** —
  不仅是 `maintenance.drain` 标志，还通过 lease TTL / 心跳驱动激活。
  突发主机故障触发备用方的
  **seize**（带 reassignment 的 acquire），并对僵尸复活围栏。
  这是 D4（on-prem VRRP 故障切换）的云版本，
  将仅限排空的 migration（D5）转变为 AWS / Azure / OCI 上的透明主机维护/
  物理主机故障切换。

## 结论

- Flaky 的 de-provision/re-capture 竞争在抽象层面消除，而非通过覆盖：
  一个确定性的 epoch 围栏计算替换了分散的命令式取消。
- routerd 获得了原则性的 fencing token（`captureEpoch`），同一门控
  后续也可用于突发故障切换的 seize — #70 修复和故障切换功能
  共享一个机制。
- 设计明确说明**云端 capture 是强安全的**（provider 的单一分配 + reassignment +
  NIC 范围 + epoch），**on-prem 的 proxy-ARP 是尽力而为的**
  （VRRP master 权限 + fail-closed + GARP），而非暗示两者等价。
- 保持 simplicity-first 范围：不引入共识协议（Paxos/Raft）。
  每地址的单调递增计数器 + 单一围栏门控就是协调面的全部。
- `-race` 验收标准的修复还发现并修复了现有事件总线的数据竞争（publish 与
  unsubscribe 的 channel close 竞争）。参见伴随的 `fix(bus)` commit。
