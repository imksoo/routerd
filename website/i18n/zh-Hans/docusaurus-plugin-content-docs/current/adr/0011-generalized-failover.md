# ADR 0011: 通用故障切换（活性驱动 seize、跨 provider action 对等）

![ADR 0011 的示意图。从 active 标记和 standby 合格性输入，经过 routerd 的 seize 决策，到 provider 或 on-prem 的 capture 恢复](/img/diagrams/adr-0011-generalized-failover.png)

## 状态

已提议。批准为实验性实现 — 2026-06-01。

消费 [ADR 0010: Capture 所有权仲裁](../adr/0010-capture-ownership-arbitration.md)
（所有权映射 + `ownershipEpoch`），实现
[ADR 0008](../adr/0008-capture-coordination-fencing.md) Phase C 中延期的
故障切换。对应 issue #74。实验性。

## 背景

CloudEdge 当前仅通过**协调式排空**（`maintenance.drain`）移动 capture。
**没有活性/健康驱动的提升**，各 provider 的 action
（secondary IP 的 assign/unassign、转发）仅 AWS 完整，Azure/OCI/on-prem 薄弱或缺失。
#74 要求一个跨 AWS / Azure / OCI / on-prem（VRRP/keepalived）的
统一故障切换框架。以统一的脑裂/震荡防御实现 L3 连续性
（standby 提升后 capture 的地址继续提供服务）。

ADR 0010 提供所有权原语（收敛的 owner 映射 + `ownershipEpoch` fencing）。
此 ADR 添加**活性 → desired-owner → seize** 循环和
**provider 无关的 action 层**。

### Provider 的 reassignment 语义（已调研 — 反映在 seize 设计中）

- **AWS**: `assign-private-ip-addresses --allow-reassignment` 将 secondary IP
  移动到另一个 ENI。**异步**（通过实例元数据 `local-ipv4s` 确认），
  last-writer-wins，关联的 EIP 也会移动。
- **OCI**: `assign-private-ip --unassign-if-already-assigned` 在同一子网内
  强制 reassign 到另一个 VNIC。last-writer-wins。公共 IP 也会移动。
- **Azure**: 没有单一原子 reassign — **从旧 NIC 删除 ipConfig +
  在新 NIC 添加**（2 步操作。可使用 ETag/If-Match 的乐观并发控制）。

因此 reassignment **并非普遍原子的**（AWS 异步、Azure 2 步）。
故障切换是**实验性的，依赖 provider 的 assign 语义 + `ownershipEpoch`
fencing +（Phase 4）云端清单的漂移 reconciliation** —
不依赖锁。

## 决策

### 统一的合格性与活性模型

desired owner（ADR 0010 的仲裁）对**合格的**成员计算。
合格性是以下条件的交集：

- `maintenance.drain == false`（已排空 → 立即排除）；
- **心跳新鲜** — 每个成员定期发出活性/心跳 federation 事件。
  过期的心跳（TTL）在**提升保持期后**标为不合格（见下文）；
- `HealthCheck` 未失败（按策略）；
- On-prem：**VRRP master** 权限信号（`activeWhen{vrrp-master}`、
  `sam.EvaluateCaptureGate`）— 非 master fail-closed。

活性以**流相对**方式评估。不使用每个节点的 wall clock：
"now"是在 pool 的 federation 流中观测到的**最大事件时间**
（`streamMaxObservedAt`），当
`lastHeartbeat(node) + heartbeatTTL + promotionHoldDuration <= streamMaxObservedAt`
时成员为 stale。看到同一流的所有节点计算相同的判定，因此
合格集 — 从而 owner 映射（ADR 0010）— 在加入活性后仍
**确定性收敛**。发送端的时钟偏斜被
`heartbeatTTL + promotionHoldDuration` 吸收。投影不会
对本地时钟**钳制**未来的时间戳（会变成非确定性）— 未来偏斜通过
status/`doctor` 可视化。完全停止的流也会停止故障切换，但
这是正确的（"无观测则不宣告故障"）。存活成员所在的连通分量
持续推进流时间。**提升保持期**吸收临时间隙，
抑制震荡。`maintenance.drain` 保持**立即**排除
（协调式，无需保持期）。

### Phase 2 的实现决策（2026-06-01 确定）

- **心跳事件**: 类型 `routerd.mobility.member.heartbeat`，group =
  `MobilityPool.groupRef`，payload `{pool, node, emittedAt, seq}`。
  **mobility 控制器**在 reconcile tick 发出。**仅对 `autoFailover: true` 的
  pool**，且仅自节点（云端 `provider-secondary-ip` 角色）。
  以 `heartbeatInterval` 做速率限制。staleness 判定使用事件的 `ObservedAt`。
  `lastHeartbeat` 从与 lease 相同的投影事件流导出
  （无 wall-clock 混入）。
- **保持期字段**平铺在 `ipOwnershipPolicy` 下：
  `heartbeatInterval` / `heartbeatTTL` / `promotionHoldDuration`（duration 字符串）。
  与 lease 的 owner 变更保持期分开。无专用状态表 — 合格性是纯粹的
  `lastHeartbeat + ttl + hold <= streamMaxObservedAt` 测试。验证在
  `autoFailover` 为 true 时要求 `heartbeatInterval`/`heartbeatTTL` 必填，
  并要求 `heartbeatTTL >= heartbeatInterval`。
- **Seize action**: 在现有 `assign-secondary-ip` verb 上增加 `allowReassignment`
  参数（而非新 verb）。当 stale/dead 的前 owner 无法自行 `unassign` 时，
  新 owner 设置此参数来获取地址。
  AWS executor 将其映射为 `--allow-reassignment`。`ActionPlan` 的
  description/risk 可读为 seize/reassign。`ownershipEpoch` 的
  打戳/fencing 与 ADR 0010 相同。
- **`autoFailover` 门控**: 心跳 staleness **仅当 `autoFailover: true`
  时**才进入仲裁合格性。未设置/false 的 pool 保持现行行为
  （仅排空驱动 owner 变更）。对 #76 Phase 1 / SAM / captureEpoch 路径
  无影响。心跳仅在 `autoFailover: true` 的 pool 中发出/消费。
- **范围**: Phase 2 仅覆盖云端 `provider-secondary-ip` + **AWS** seize。
  On-prem（proxy-ARP / VRRP master）和 Azure/OCI reassign executor 在 Phase 3。
- **已知后续**: 心跳事件没有 TTL/expiry，因此
  停止成员的最后一个心跳为 staleness 判定保持可观测。
  结果是心跳行会累积不被清理
  （后续 hygiene pass 追踪 — 不得清理 stale 判定依赖的最后心跳）。

### 活性驱动 seize

当合格 owner 变更时（排空、心跳过期、健康故障），
`ownershipEpoch` 递增，**新 owner seize**：向 provider 发出带
reassignment 的 secondary IP acquire，启用转发。
旧 owner 的 action 持有 stale epoch，在门控处被围栏。
`autoFailover`（ADR 0010 `ipOwnershipPolicy`）门控是否自动化。

### Provider 无关的 action 层

- **planner 发出 provider 无关的所有权/action 意图**（desired 的
  `(owner, address, verb)` 集 + `ownershipEpoch`）。**executor 持有 provider
  差异**（AWS `--allow-reassignment`、OCI
  `--unassign-if-already-assigned`、Azure remove+add）。这是将已用于 AWS 的
  通用 `ActionPlan` + executor 契约泛化。
- **On-prem 不是云 provider**：其"action"是本地数据平面
  （proxy-ARP/GARP/VIP），作为 on-prem executor / SAM-GARP 桥接处理，
  而非 provider API 调用。

## 阶段划分（此 ADR）

- **Phase 2**: 云端活性故障切换 — 心跳事件 + TTL +
  提升保持期 + 统一合格性、`ownershipEpoch` 递增、
  **云端 secondary-IP seize**（AWS 先行，已验证路径）、`autoFailover` 门控。
  L3 不中断（提升后 standby 提供地址）的
  强制故障 CI/lab 测试。
- **Phase 3**: Provider action 对等 — Azure（remove+add ipConfig）和
  OCI（`--unassign-if-already-assigned`）executor。On-prem executor /
  SAM 桥接的 VRRP/GARP 集成，以同一策略覆盖 VRRP/keepalived 故障切换。
- **Phase 4**: 云端清单 observe capability（`describe-secondary-ips`）→
  漂移/孤立/冲突检测在 status + `doctor` 可视化，
  将实验性 seize 硬化为 reconcile 过的所有权。所有权映射的管理 API。

## 结论

- 一个故障切换框架跨越 provider：活性/健康/维护/VRRP 作为
  统一合格性模型的输入。planner 与 provider 无关。
  每个 provider 的现实封装在 executor 中。
- L3 连续性通过 standby 提升 + capture IP 的 seize 实现，
  由 `ownershipEpoch` 围栏。诚实的限制（无共识、
  provider reassignment 并非普遍原子）被记录，
  云端清单（Phase 4）填补漂移缺口。
- On-prem 被集成而不被强塞入云 provider 的模型中。
