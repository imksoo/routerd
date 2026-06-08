# ADR 0012: BGP /32 地址移动性

![ADR 0012 的示意图。将 lease 和 epoch 所有权替换为 BGP best-path /32 通告，活性标记、Route Reflector 路径、FIB 导入、后台 provider capture](/img/diagrams/adr-0012-bgp-address-mobility.png)

## 状态

已批准。Phase 1 的 Clean Option B 已实现至 B6/B7 — 2026-06-03。

[ADR 0006](../adr/0006-event-federation.md)、
[ADR 0008](../adr/0008-capture-coordination-fencing.md)、
[ADR 0010](../adr/0010-capture-ownership-arbitration.md)、
[ADR 0011](../adr/0011-generalized-failover.md) 为 CloudEdge 移动性数据平面
引入的自定义 overlay 可达性正本被替换。旧有的 provider action、VRRP、
doctor 安全机制作为后台 reconciliation 和本地 capture 守卫保留在范围内。

## 背景

CloudEdge 的 Selective Address Mobility 最初从 routerd 专有控制平面
构建 overlay 可达性：

- Event Federation 传递 observed/expired/heartbeat 事实；
- mobility 控制器将这些事件投影为 `AddressLease` 行；
- planner 将 lease 下降为 `AddressMobilityDomain`、`RemoteAddressClaim`、
  provider `ActionPlan`、`captureEpoch`、`ownershipEpoch` 状态；
- SAM 将生成的 claim 下降为路由、proxy-ARP、provider secondary-IP action；
- provider action 控制器审批/执行云端 mutation。

这证明了产品路径，但也使故障切换依赖于一条长的 routerd 专有链。
实际 4-site 测试中，overlay/云端故障切换受限于 reconcile tick、
lease/epoch 投影、action 导入/自动执行、provider API 行为、
云端 fabric 传播。最近的冒烟结果显示 AWS/OCI 云端故障切换约需 120 秒，
而目标是 60 秒以下，overlay 流量最好在秒级。

routerd 已随附基于 GoBGP 的 `routerd-bgp` daemon 和 BGP 控制器。
现有接口可完成 GoBGP 启动、peer 和策略配置、通过 `AddPath` 通告
静态 IPv4/IPv6 unicast 前缀、通过 `DeletePath` 撤回、
观测 best path / 导入 Linux IPv4 FIB。GoBGP v3.37.0 也支持
EVPN Type-2/Type-5 和 MAC mobility 序列号，但 routerd
当前的 BGP 资源模型和 FIB syncer 仅公开 IPv4/IPv6 unicast。
最快的有用切入点是普通的 IPv4 unicast `/32` 移动性，而非 EVPN。

云 provider fabric 是另一个约束。AWS VPC 路由表、Azure UDR/Route Server、
OCI VCN 路由表不会自动跟随 VM 私有 GoBGP overlay 通告，除非
配置了显式的云端路由集成。provider 的 secondary-IP 分配、路由表目标变更、
Azure Route Server 等 provider 服务在云原生入口时可能仍然需要。
BGP 可以将 provider API 调用从 overlay 可达性的关键路径中移除，
但并不消除 provider 入口问题。

## 决策

将 CloudEdge 移动性的**overlay 可达性正本**迁移到 BGP RIB：

- `MobilityPool` 中的每个所拥有地址表示为 IPv4 unicast `/32` BGP 通告。
- 地址的 owner 是在该 `/32` 的 BGP best-path 选择中胜出的节点。
- 非 owner 从 BGP best path 学习远程所拥有地址，通过 BGP FIB importer
  而非生成的 SAM 投递路由安装 overlay 投递路由。
- 移动性转移表示为 BGP withdraw/advertise 和路径优先级变更。
  操作员意图通过 `MobilityPool` 保持声明式。操作员无需
  手动记述 lease、claim 或 provider action。
- best-path 仲裁优先使用标准 unicast 属性：
  `LOCAL_PREF`/`MED`/communities + 确定性路由策略。可能添加
  路由序列 community 以提高可观测性，但普通 BGP 不将
  "新序列胜出"作为原生规则。
- EVPN 明确延后。EVPN Type-2 MAC/IP 移动性是未来的互操作选项，
  不是 Phase 1 的机制。

Provider secondary-IP 和转发 action **降级为后台 reconciliation**：

- 对通过 VPC/VNet/VCN 进入的云端 fabric 入口路径仍然需要。
  作为已建立的 routerd overlay 路径的替代。
- 从相同的 BGP 移动性视图和 provider 清单/action 日志
  进行 eventual reconcile。
- 不得成为 overlay 可达性的正本。

On-prem LAN capture 保持本地：

- VRRP master 门控、proxy-ARP、GARP、非 master 的 fail-closed 行为、
  重复持有者 doctor 检查维持不变。
- BGP 决定远程 overlay 可达性。不替换本地 L2/ARP 权限守卫。

## Clean Option B 的最终状态

预发布实现直接以 BGP 作为移动性的正本：

- **所有权:** 移动 `/32` 的 owner 是该前缀当前的 BGP best path。
  没有单独的 `AddressLease`、ownership epoch、capture epoch 注册表。
- **投递:** 非 owner 将 BGP best path 导入本地 FIB，
  通过 overlay next hop 路由 `/32`。MobilityPool 的
  route-mode 规划和生成的 SAM 投递 claim 不在主线中。
- **Capture/trap:** 云端 provider secondary-IP action 从 BGP best-path 视图和
  本地放置派生。不是 overlay 可达性的前提，而是
  后台 fabric 入口 reconciliation。
- **Fencing:** provider action 携带当前移动性路径签名
  （`mobilityPathSig`）+ desired 持有者和 observed provider/日志转换。
  当 desired BGP path 不再匹配时 stale action 被跳过。
  旧有 ownership/capture epoch 表已删除。
- **活性:** 移动性故障切换依赖 BGP withdrawal 和 best-path 收敛。
  快速故障检测由渲染到 FRR `bfdd` 的 `BFD` 资源提供。
  BGP hold 定时器作为 BFD 不稳定时路由 withdrawal 的非破坏性权威保留。
  自定义移动性心跳/staleness 投影已删除。
- **On-prem LAN 权限:** VRRP master 门控、proxy-ARP、GARP、
  非 master fail-closed 行为、重复 proxy-ARP doctor 检查作为本地安全机制维持。
- **删除的状态:** B6 中物理删除了移动性 lease、ownership epoch、capture epoch、
  deprovision 标记的表和 API。该阶段净减约 6,200 行。

## 非目标

- Phase 1 不实现 EVPN。
- Phase 1 不删除 provider executor。
- 不声称仅 BGP 即可解决云原生入口。
- 不添加共识、etcd、Raft、单写入者 lease 数据库。
- 不要求操作员为每个地址记述动态 BGP path 资源。
- 不全局删除 Event Federation。在 BGP path 证明后
  仅退役移动性专有的使用。

## 模型

预期稳态映射：

| 现有概念 | BGP 移动性概念 |
| --- | --- |
| `AddressLease` 活跃 owner | `pool/address/32` 的 BGP best path |
| observed owner 事件 | 本地 `/32` advertise |
| expired/released 事件 | 本地 `/32` withdraw |
| `staticOwnedAddresses` | 所有成员的静态本地 `/32` advertise |
| F3 交接 | release/withdraw 屏障，随后新 owner advertise |
| `RemoteAddressClaim` 投递路由 | 导入的 BGP `/32` FIB 路由 |
| capture 放置的活跃成员 | 路径优先级 / origin 合格性 |
| overlay 路由的 `ownershipEpoch`/`captureEpoch` | best-path 视图和可选路由元数据 |
| provider secondary-IP action | 后台 fabric 入口 reconciliation |
| on-prem proxy-ARP 权限 | 不变的 VRRP master 门控 |

## Phase 1 范围

Phase 1 构建了 BGP unicast path，并在发布前删除了被替换的自定义
移动性 planner/状态路径。

1. 为 routerd 生成的 `/32` 通告添加源感知的动态 BGP path 管理。
2. 将 `MobilityPool` 的 owner 状态投影到 BGP 通告。
3. 消费 BGP best path 作为远程地址投递视图。
4. 将故障切换和静态交接的 overlay 可达性迁移到 BGP withdraw/advertise。
5. 将 provider secondary-IP 处理转换为后台 reconciliation。
6. 对等证明后删除旧有 lease/planner/epoch 路径。

## 结论

正面影响：

- Overlay 故障切换变成路由收敛问题，而非 routerd 专有的
  lease/action/provider 串行工作流。
- 设计与 BGP 服务 VIP 和 pod/服务路由通告等
  Kubernetes 边缘模式对齐。
- 最复杂的自定义状态（`AddressLease` 投影、capture 放置、
  capture/ownership epoch 规划、deprovision 标记）可在
  迁移后大幅精简。
- D3/D5/D6/D7 的 overlay 可达性可在云 provider secondary-IP reconciliation
  仍挂起时收敛。

负面影响 / 风险：

- 普通 BGP 需要显式策略以避免相同前缀通告的歧义。
  序列 community 不是原生 fencing token。
- 除非部署也配置了云端路由集成，否则在后台 provider 状态追上之前
  provider fabric 入口可能不可用。
- 现有的实际演示和 acceptance 探针需要区分 overlay 可达性与
  云原生入口。
- routerd 的 GoBGP 观测当前基于轮询。Phase 1 可能需要添加事件驱动的
  `WatchEvent` 路径，否则 BGP 路由安装循环会残留轮询延迟。
- 脑裂防止仍依赖 VRRP/provider fencing/doctor 检查。
  BGP best path 选择一条转发路径，但仅靠它不能移除 stale 的本地
  proxy-ARP 或 stale 的 provider 分配。

## 迁移规则

- 维持 `MobilityPool` 作为操作员记述的唯一移动性意图。
- 将 MobilityPool 的默认投递设为 BGP。旧 MobilityPool route-mode planner
  是迁移辅助，干净的预发布 API 不接受。
- 不在没有确定性优先级规则的情况下，对同一 `(pool, address)` 同时运行
  两个路由下降源。
- 在生成的 BGP path 上标记源元数据，使静态 BGP 通告不被
  移动性 reconciliation 误撤回。
- 在 provider reconciliation 存续期间，维持 provider action 的幂等性和
  path 签名 fencing。

## 退出条件

- 4-site 演示使用 BGP 学习的 `/32` overlay 路由通过定向 SSH 矩阵。
- 协调排空和 stale owner 故障切换通过 BGP 收敛，无需在 overlay 路径上
  手动审批/执行 provider action。
- Provider secondary-IP action 的延迟或失败不破坏 overlay 可达性。
- VRRP/proxy-ARP on-prem 的 fail-closed 语义未改变。
- 旧有移动性 lease/planner 路径在测试和实际证据覆盖 BGP 路径后已删除。
