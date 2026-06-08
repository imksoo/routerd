# ADR 0010: Capture 所有权仲裁（多实例所有权映射 + ownershipEpoch fencing）

![ADR 0010 的示意图。从重复持有者的风险出发，到 ownershipEpoch 和所有权映射的设计决策，VRRP 或单一路由器的 capture 护栏](/img/diagrams/adr-0010-capture-ownership-arbitration.png)

## 状态

已提议。批准为实验性实现 — 2026-06-01。

以 [ADR 0008: 基于 fencing token 的 Capture 协调](../adr/0008-capture-coordination-fencing.md)
和 [Selective Address Mobility](../reference/selective-address-mobility) 数据平面为基础。
对应 issue #76。消费者为
[ADR 0011: 通用故障切换](../adr/0011-generalized-failover.md)（#74）。
实验性。

## 背景

规模化时，单一云端路由器无法持有所有 capture 的 secondary IP
（ENI/NIC/VNIC 槽位限制），因此 `N+1` 配置的同 provider 路由器需要
**分散** capture 的地址。当前 routerd **没有跨节点所有权映射，
也没有互斥控制**：

- 协调是**单节点本地投影**：每个节点从相同的 federation 事件流
  独立投影到相同的 `AddressLease` 状态
  （`pkg/controller/mobility/controller.go`）。**无分布式锁、无仲裁、无共识**。
- "单一所有者"是*隐式的*（capturePolicy `all-non-owner-sites` + 确定性
  `evaluatePlacement`），而 `captureEpoch`
  （`pkg/state/mobility_capture_epoch.go`）是**每节点、每
  (pool, address, captureDomain)** 的单调递增 token，在导入/执行门控处
  围栏 stale 的 provider action（ADR 0008）。
- 预留字段 `MobilityPoolSpec.Authority` 未使用。

#76 要求集中式所有权映射、竞争排除和脑裂防止。
ADR 0008 有意**回避共识**（Paxos/Raft/etcd），
从单调递增 fencing token + provider 的结构性单一分配 + 幂等收敛
构建安全性。此 ADR 延续该理念。

### "所有权"在无共识下能保证什么、不能保证什么（诚实的范围）

这**不是 linearizable 的分布式锁**。事件顺序仲裁 +
fencing + 云端的单一分配语义保证以下：

1. 看到同一事件流的所有节点**收敛到相同的 owner 映射**；
2. 看到 ownershipEpoch *N+1* 的节点不会执行 epoch-*N* 的 action
   （在门控处围栏）；
3. 云端 secondary IP 恰好属于一个 NIC，因此 provider 状态
   **收敛到单一分配**。

**不能保证的**：从 federation 分区的（未看到 *N+1* 的）
旧 owner，如果仍然存活，通过 provider API 重新获取地址 —
排除这一点需要共识 / STONITH / provider 的条件式 fencing，
但不添加。因此属性为**"围栏式 eventual 所有权 +
provider 强制的单一分配"**，而非"脑裂防止"。
On-prem 的 **proxy-ARP** 更弱（无 provider 单一分配）：
上限为 VRRP master 权限 + fail-closed（遵循 ADR 0008）。

## 决策

### `ownershipEpoch` — 每 (pool, address) 的集群围栏 token

引入 **`ownershipEpoch`**。比 `captureEpoch` 更高层次的概念：
每 (pool, address) 的单调递增 token，**仅在确认的 owner 变更时**递增
（lease 在 candidate/holding 阶段不递增）。跨云端 / on-prem /
provider / action 的围栏 token。`captureEpoch`
作为兼容性/派生注解保留。正本迁移到 `ownershipEpoch`。

### 所有权映射 — 无 leader 的确定性收敛

**没有选举的 leader**（leader 选举需要共识）。所有权映射是
每个节点从 federation 事件流确定性构建的**收敛视图**：

- 每个 `(pool, address)` 的 owner 通过确定性仲裁选择：
  **preferNodes → 放置优先级 → 稳定 tie-break** 对*合格*成员应用
  （合格性由 ADR 0011 定义：未排空、健康、存活、适用时 VRRP master）。
- 多实例分散：placement group 内每个地址仲裁到一个 owner。
  地址集分散到合格成员（未来：最小负载）。1 IP → 同时 1 个 owner。
- 映射**可视化**（status DB + 指标 + control/`routerctl`），
  操作员可以看到"哪个 IP 被哪个节点所有" —
  以收敛视图而非单一写入者存储实现 #76 要求的"集中式所有权映射"。

### `MobilityPool` 的 `ipOwnershipPolicy`

```yaml
spec:
  ipOwnershipPolicy:
    type: centralized          # 收敛的确定性映射（唯一模式）
    epochLocking: true         # 用 ownershipEpoch 为 action 打戳+围栏
    preferNodes: [aws-router-a, aws-router-b]
    autoFailover: true         # ADR 0011（活性驱动 seize）消费
```

`preferNodes` 为仲裁施加偏向。`epochLocking` 启用
ownershipEpoch fencing。`autoFailover` 是 ADR 0011 使用的钩子。
`type` 当前仅一个模式（`centralized` = 收敛的确定性）。

### Action 幂等性 key

provider action 的幂等性 key 至少包含 `pool / address / ownerNode /
ownershipEpoch / actionVerb / provider / nicRef`。stale epoch 或
错误 owner 的 action 被确定性围栏。

## 阶段划分（此 ADR）

- **Phase 1（此 ADR 的最小范围）**: `ownershipEpoch` token、
  确定性所有权记录 + 仲裁（preferNodes/priority/tie-break）、
  `ipOwnershipPolicy` spec + 验证、**所有权映射的可视化**（status +
  指标 + `routerctl`）。**无自动 seizure** — Phase 1 仅
  *计算并发布* desired 所有权，用 ownershipEpoch 围栏 action。
  现有静态放置继续驱动谁来执行。
- 活性驱动的故障切换/seize 在 **ADR 0011**。

## 结论

- routerd 获得了一个集群收敛、围栏式的所有权模型，用于在 N+1 同 provider 路由器间
  分散 capture 的 IP，而无需添加共识存储。
- 安全性范围被诚实地陈述（"围栏式 eventual 所有权"，而非分布式锁）。
  云端结构性地强，on-prem 是 VRRP 权限的尽力而为。
- `ownershipEpoch` 是单一的跨切面围栏 token，供 ADR 0011 的 seize 和
  Phase 4 的云端清单/漂移检测构建其上。
