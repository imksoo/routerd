# ADR 0006: CloudEdge Event Federation（routerd 间的类型化事件）

![ADR 0006 Event Federation 示意图。从手动描述 claim 的问题出发，到 EventGroup、EventPeer、EventSubscription 的设计决策，以及 observed-fact 不变量](/img/diagrams/adr-0006-event-federation.png)

## 状态

已批准。实验性实现进行中 — 2026-05-30。
Phase 1、1.5、2、3 已在 **`event-federation` 分支实现**：

- **Phase 1**（事件信封 + `EventGroup` Kind + SQLite 本地存储 + `routerctl
  federation event emit/list`）— 完成。
- **Phase 1.5**（`EventPeer`/`EventSubscription` Kind + 验证）— 完成。
- **Phase 2**（经由 overlay 的 peer 投递、`routerd-eventd`、HMAC、重试、
  retention 清理）— 完成。**lab-smoke PASS**
  （[传输证据](../releases/evidence/cloudedge-event-federation-transport-20260530.md)）。
- **Phase 3**（subscription → plugin → `RemoteAddressClaim` `DynamicConfigPart`）—
  完成。**lab-smoke PASS**
  （[subscription 证据](../releases/evidence/cloudedge-event-federation-subscription-20260530.md)、
  [how-to](../how-to/event-federation-subscription.md)）。

Phase 4（provider `actionPlan` 插件、dry-run）**下一阶段尚未开始**。
Phase 5（provider action 执行）**不在 MVP 范围内**。

## 背景

SAM（[参考](../reference/selective-address-mobility)、
[里程碑](../releases/cloudedge-sam-mvp-milestone.md)）已在
Azure×PVE、AWS×PVE、OCI×PVE 上完成清洁验证（3 云对等）。SAM 证明了
**capture（provider 特定）/ delivery+claim（routerd 通用）** 的分离。但是，
驱动它的 `RemoteAddressClaim` **目前仍是手动描述的**。下一步是通过
**事件驱动**来发现、传播和实体化 claim：

> on-prem 的 routerctl 检测到客户端 IPv4（ARP/Clients/DHCP）→ 发出类型化事件 →
> federation 总线将其投递到云端 routerd → subscription 启动 provider 插件 →
> 插件以 `DynamicConfigPart` 形式返回 `RemoteAddressClaim`
> （+ provider secondary-IP `actionPlan`）→ **无需人工编辑云端配置**，
> 云端即准备好执行 `provider-secondary-ip` capture。

### 现有资产（MVP 并非从零开始）

设计基于当前代码树。大多数构建块已经存在，
真正全新的工作是**节点间 federation 传输**和
**事件→插件 subscription 触发器**：

- **类型化事件信封**: `pkg/daemonapi` 的 `DaemonEvent{Type,Time,Daemon,Resource,
  Severity,Reason,Message,Attributes}` + `NewEvent(...)`。当前是 daemon→main 流程，
  但已经是带类型和 topic 的信封。
- **daemon→routerd 传输模式**: daemon 通过 UNIX 套接字上的
  HTTP POST 到控制套接字（`cmd/routerd-dhcp-event-relay` → `controlapi.Prefix +
  /dhcp-lease-event` via `unix:/run/routerd/routerd.sock`）。已有*事件中继 daemon 的先例*。
- **分离的长生命周期 daemon 先例**: 13 个 `cmd/routerd-*` daemon
  （`routerd-bgp`、`routerd-ra-observer`、`routerd-dhcp-event-relay` 等）。
  gobgp pivot（ADR 0004）确立了"为避免重启导致的中断，使用分离进程而非进程内嵌入"。
- **Plugin → DynamicConfigPart 管线**: `pkg/plugin/runner.go`、
  `pkg/plugin/dynamic_config.go`、`pkg/dynamicconfig/{types,merge}.go`、
  `PluginRequest`/`PluginResult`。effective = startup + active dynamic − masks。
- **状态**: SQLite（`pkg/state/sqlite.go`）。
- **Provider profile + 外部认证**: `CloudProviderProfile`、
  `auth.mode=external-command`（specs.go:1193）— provider 特定插件的钩子。
  `provider: oci|aws|azure|gcp` 已通过验证。

## 决策

将 **CloudEdge Event Federation** 作为下一个实验性 MVP，构建在已合并的实验性 SAM 之上的新分支中。**不缩减范围，而是分解为有序的、可独立验收的阶段，每个阶段作为一个工作流驱动。** 每个阶段交付一个可工作、可演示的切片，并作为下一阶段的准入门控。

### 设计原则

1. **事件是观测事实，不是配置。** 节点发送
   `routerd.client.ipv4.observed`，而不发送原始的 `RemoteAddressClaim`。接收端的
   *受信任本地插件*决定是否以及如何将其转换为类型化 claim + actionPlan。线路上不传输命令。
2. **at-least-once + 幂等**，而非 exactly-once。存储的幂等性以事件 `id` 为键
   （重复 `id` 为 no-op insert）。`dedupeKey` 是 subscription 端的分组键，
   用于聚合同一事实的重复观测，在 Phase 1 中**不是** DB 的唯一约束。动态资源名称是确定性的
   （`onprem-10-88-60-9`）。provider action 如已满足则为 no-op。无共识、无 gossip、无全序。
3. **复用，不重新发明。** 复用 `DaemonEvent` 信封、控制套接字 HTTP 传输惯例、
   Plugin→DynamicConfigPart 管线、SQLite 状态、`CloudProviderProfile`/`Plugin`
   （不需要新的 `CloudProviderPlugin` Kind）。
4. **新增 Kind 最少化。** MVP 引入 **3 个**：`EventGroup`（总线标识符 + 认证 + retention）、
   `EventPeer`（投递目标 + 内联的 push/receive 过滤器）、
   `EventSubscription`（接收事件 → 本地插件触发器）。原提案中独立的
   `EventFilter` 已合并到 `EventPeer`，仅在需要跨 peer 共享过滤器时才提升为独立 Kind。
5. **分离的 daemon。** Federation 的收发放在新的
   `cmd/routerd-eventd` 长生命周期 daemon 中（遵循 ADR 0004 先例）。不在 reconcile 循环内。
   仅绑定到 overlay（`wg-hybrid`）。
6. **MVP 中 provider mutation 保持 dry-run。** 插件发出 `actionPlan`。
   执行放在后续阶段，位于明确的 approval/auto-apply 策略之后。

### 传输与安全（MVP）

- 接收端 = **仅绑定到 WireGuard overlay 接口/地址**的 HTTP 监听器
  （例：`169.254.x.y:9443`）。WG 隧道是机密性边界。为完整性和防止误投递添加
  **消息级 HMAC**（来自文件的共享密钥）。
  **TLS 延后** — TLS 监听器需要证书配置，会重新引入
  SAM stocktake 指出的引导摩擦。（未来：mTLS / 每 peer 的 Ed25519 / 云 KMS 签名。）
- MVP 中仅 push（`onprem→cloud` 的观测，`cloud→onprem` 的 claim/result ack）。
- 带退避的重试。每 (event, peer) 的投递状态保存在 SQLite。

### 应在状态机层面审查的关键不变量（而非仅审查差分）

遵循项目对进程外有状态 daemon 的规则，
将正确性条件描述为不变量：

- **禁止反馈循环。** 节点不得对自身正在 *capture* 的地址（provider-secondary-ip
  或 proxy-arp）重新发出 `*.observed`。观测以 `ownerSide` + `domain` 为范围，
  capture 的/secondary 地址从观察者的源集合中排除。
  否则，云端自身的 secondary `.9` 会被重新观测 → 重新传播 → 震荡。
- **provision 与 de-provision 的不对称性。** provisioning（claim 出现）可以即时。
  **de-provisioning（TTL 过期 / `*.expired`）必须具有滞后性** —
  远大于 300 秒 observe TTL 的宽限期 + 去抖。震荡的客户端不得反复驱动
  云端 secondary-IP 的 assign/unassign（API 速率限制 + 成本 +
  数据平面扰动）。TTL→teardown 策略应明确且保守。
- **(domain, address) 单写入者。** 拥有方具有权威。接收方仅为
  `ownerSide` 是*发送方*的地址提议 claim。
- **幂等的 provider action。** "already assigned" ⇒ 跨 aws/azure/oci 均为 success/no-op。

### Provider 插件框架

调用 OS CLI 的本地可执行文件。**不是**将 SDK 静态链接到 routerd
（将 SDK 的变更/认证排除在核心之外，启用云原生身份，便于调试）：

- **AWS**: `aws ec2 assign-private-ip-addresses` — 认证：优先 **IAM 实例配置文件**，
  `AWS_PROFILE`/env 回退。
- **Azure**: `az network nic ip-config …` — 认证：优先**托管身份**，
  `az login`/SP env 回退。
- **OCI**: `oci network private-ip create` / `vnic` — 认证：优先**实例主体**，
  OCI config profile 回退。

`Plugin.capabilities` 对插件权限进行门控
（`observe.events`/`propose.dynamicConfig`/`propose.providerAction`）。

## 阶段分解（每阶段 1 个工作流，顺序执行）

每个阶段 = 可独立验收的切片。后续阶段以先行阶段的验收为门控。
实现委托给 codex，claude 负责编排 + 审查。

- **已完成 — Phase 1 — 事件模型 + 本地存储。** `EventGroup` Kind。将 `DaemonEvent`
  复用/扩展为外部 `Event` 信封（id, group, sourceNode, type, subject, ttl, dedupeKey, payload）。
  SQLite `federation_events` 表。`routerctl federation event emit/list`。
  *验收条件:* emit→stored（带 TTL）。重复 id 幂等。过期被忽略。
- **已完成（lab-smoke PASS）— Phase 1.5 — `EventPeer`/`EventSubscription` Kind + 验证。**
- **已完成（lab-smoke PASS）— Phase 2 — 经由 overlay 的 peer 投递。** `EventPeer` Kind。
  `routerd-eventd` 接收端绑定到 `wg-hybrid`。HMAC。push + 退避。`event_deliveries`。
  *验收条件:* on-prem 经由 `wg-hybrid` push 到云端。重复 push 幂等。错误 HMAC 被拒绝。
  `routerctl event deliveries`。`routerd-eventd` 按 `EventGroup` retention（`maxAge`/`maxEvents`）
  定期清理 `federation_events`。`routerctl federation event prune --dry-run` 报告
  待删除项。
- **已完成（lab-smoke PASS）— Phase 3 — subscription 触发的插件 → DynamicConfigPart。**
  `EventSubscription` Kind。事件批次 → `PluginRequest`。`PluginResult` →
  `DynamicConfigPart`（带 `routerd.net/dynamic-source`、`event-id`、`event-group`
  注解）。去抖/batchWindow。`event_subscription_runs`。
  *验收条件:* 云端收到 `10.88.60.9/32` 的 `client.ipv4.observed` → 插件 →
  `RemoteAddressClaim` DynamicConfigPart 可通过 `routerctl dynamic render` 确认。
  actionPlan 仅显示，不执行。
- **下一步（未开始）— Phase 4 — provider actionPlan 插件（dry-run）。** `aws/azure/oci-address-claim`
  示例插件。标准化 `actionPlan` 格式。实例 ID 认证。
  *验收条件:* 插件提议 assign-secondary-IP。无 mutation。计划可通过
  `routerctl plugin`/`dynamic` 确认。
- **Phase 5 —（MVP 后）provider action 执行。** approval/auto-apply 策略、
  action 日志、尽力 undo、身份文档。不在 MVP 范围内。

首个端到端冒烟测试为 **手动 `routerctl federation event emit` →
federation → DynamicConfigPart**（Phase 1-3）。ARP/Clients 观察者插件在
该冒烟测试*之后*引入（以 `routerd-ra-observer` 为模型），以便隔离故障。

### MVP 事件类型

`routerd.client.ipv4.observed`、`…ipv4.expired`、`…dynamic.part.accepted/rejected`、
`…provider.action.planned/succeeded/failed`。首次冒烟测试仅需 `observed`+`expired`。

## 结论

- **正面影响:** 将 SAM 从手动描述转变为事件驱动。小而可演示的阶段。
  复用现有信封/传输/插件/状态。新增 Kind 不膨胀（3 个）。
  provider mutation 有门控。从第一天起支持云原生身份。
- **负面影响 / 风险:** 新增网络监听器（通过 overlay 绑定 + HMAC 缓解）。
  循环/震荡和 provision/de-provision 的不对称性需作为不变量强制执行（见上文）。
  at-least-once 将幂等性推到了插件和命名上。TLS/mTLS 延后。
  de-provisioning 的自动化被有意*最后*启用。
- **不在 MVP 范围内:** 共识、exactly-once、gossip mesh、任意远程命令执行、
  provider mutation 自动化、完整的 IP 生命周期自动化、远程插件注册表、
  跨节点配置重写。

## 已知限制（实验性）

- **`routerd-eventd` 的 supervision 为 systemd 和 FreeBSD `rc.d` 生成。**
  其他服务管理器需要渲染器的显式支持才能自动管理 eventd。
- **`EventSubscription` 的 `batchWindow`/`debounce` 被接受但是粗粒度的。**
  字段经过验证，以轮询粒度生效 — 控制器在
  **每次轮询 tick** 时批处理事件，而不是以精确的亚 tick 计时器运行。短的去抖窗口
  实际上会被向上取整到 tick 间隔。

## 范围外 / 未来的开放问题

- 是否需要从 `cloud→onprem` 传递 ack 以外的内容（例如：云端 secondary 存在后
  切换 on-prem proxy-arp 的 capture-ready 信号）。
- 跨 peer 共享过滤器的功能（将 `EventFilter` 提升为独立 Kind）。
- 多 peer / 3 节点以上的 group（MVP 针对已验证的成对拓扑）。
