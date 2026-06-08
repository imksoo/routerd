# ADR 0007: Provider Action 执行（门控式、executor 分离）

![ADR 0007 的示意图。从非活性 planner 的 actionPlan 出发，经过 ProviderActionPolicy 的门控和审批，到分离的 executor 插件的日志记录](/img/diagrams/adr-0007-provider-action-execution.png)

## 状态

已提议。批准为实验性实现 — 2026-05-30。

此 ADR 直接以 [ADR 0006: CloudEdge Event Federation](../adr/0006-event-federation.md) 和
[Selective Address Mobility](../reference/selective-address-mobility) 数据平面为基础。
**属实验性质**。

Phase 5.0（此区块）仅引入**设计、`ProviderActionPolicy` Kind 和
`action_executions` 日志**。Phase 5.0 **不包含执行状态机、
`routerctl action` 命令、executor 调用或实际的 provider CLI/SDK 调用** —
伪 executor 和执行路径将在后续区块到来。

## 背景

- **Phase 4.1 已引入 dry-run 的 `actionPlans`。** planner 插件（capability
  `propose.providerAction`）将仅供显示的 provider 操作记录到 `DynamicConfigPart`。
  routerd **从不执行** `actionPlan`，也不从中调用 provider CLI/SDK。
  `pkg/plugin.ValidateActionPlan` 拒绝 `mode=execute`。这些仅用于
  让 EventSubscription 驱动的执行可审查。
- **SAM 数据平面已在真实云端验证。** Selective Address Mobility 已在
  AWS、Azure、OCI 上通过干净冒烟测试（3 云对等）。on-prem 端通过 overlay
  投递 claim 的地址。云端仍然需要 provider 实际将 secondary IP attach/detach 到 NIC。
  目前该 attach/detach 是操作员手动完成的。
- **缺少的是门控式执行。** 我们希望 routerd 能驱动已审批的 provider mutation，
  但 provider 凭证不得进入 routerd 核心，执行必须默认关闭、明确审批且完全记录日志。

## 决策

### 两个插件角色

- **Planner**（Phase 4.1、capability `propose.providerAction`）：发出 dry-run 的
  `actionPlans`。**不持有**凭证。
- **Executor**（Phase 5、capability `execute.providerAction` — `PluginSpec.Capabilities`
  的新枚举值）：**在自身进程中使用自身凭证**执行 action。
  使用云原生身份（AWS 实例配置文件、Azure 托管身份、
  OCI 实例主体）或自身环境。

### 凭证模型（硬性不变量）

**routerd 核心绝不持有、读取或传递 provider 凭证。**
routerd 仅向 executor 传递已审批的 `actionPlan`（不含秘密）和 Phase 4.0 的
allowlist/脱敏上下文。executor 自行向云端认证。凭证不经过 routerd 核心或
`action_executions` 日志。

### 流程

1. planner 在 `DynamicConfigPart` 上发出 `actionPlan`（dry-run，与当前相同）。
2. 计划以 `status=pending` **导入**到 `action_executions` 日志。
   以 `idempotencyKey` 为键。
3. **审批**: 操作员审批，或策略自动审批
   （仅当 `requireApproval=false` 且 `enabled=true` 且非 `dryRunOnly`，且
   allowlist 匹配时）。
4. **执行**: routerd 调用匹配的 executor 插件，
   传递已审批的计划（不含秘密）。
5. **结果记入日志**: `succeeded` / `failed` / `skipped` / `rolledBack`。

### `ProviderActionPolicy` Kind

新 Kind（`apiVersion: hybrid.routerd.net/v1alpha1`）对执行进行门控。
与 `RemoteAddressClaim` 和 `CloudProviderProfile` 定义在同一 `hybrid` 组中，
由它们管理。零值为安全的锁定状态：

- `enabled`（bool，默认 false）— 除非为 true，否则执行被禁用。
- `dryRunOnly`（`*bool`，nil 时默认 true）— 仅允许 dry-run。
- `requireApproval`（`*bool`，nil 时默认 true）。
- `allowedProviders` / `allowedProviderRefs` / `allowedActions` — 空表示 none
  （默认拒绝）。
- `allowedCIDRs` — action 目标地址必须包含在其中。
- `maxActionsPerRun`（int，默认 0 = 无 action。操作员需设置
  正数上限）。
- `allowUndo`（bool，默认 false）。
- `executionWindow`（string，宽松验证）。

### `routerctl action` UX 界面（后续区块，此处记录）

`routerctl action list`、`show`、`approve`、`execute --dry-run|--approved`、
`journal`、`rollback --dry-run`。这些是操作员界面。Phase 5.0
**均不交付**。

### 阶段划分

- **Phase 5.0** — 框架 + 数据模型: `ProviderActionPolicy` Kind、
  `action_executions` 日志、schema/验证。**伪 executor**
  （无真实云端）在 Phase 5.0 后半区块端到端验证路径。
  **Phase 5.0 不调用实际的 provider CLI/SDK。**
- **实际 mutation 冒烟测试** — 门控式，逐 provider，
  针对 SAM 验证过的云端执行。
- **Phase 5.x** — 加固（窗口、速率限制、更丰富的回滚、审计）。

## 硬性安全停止措施

1. **执行默认禁用。** `ProviderActionPolicy.enabled` 默认为 false。
   `dryRunOnly` 默认为 true。
2. **需要明确审批。** action 仅在审批后执行（操作员审批，
   或策略的 `requireApproval=false` 且 `enabled` 且非 `dryRunOnly` 且
   allowlist 匹配）。
3. **`mode=execute` 被拒绝** — 除非存在策略许可的已审批
   `action_execution`。
4. **`idempotencyKey` 必需**。已 succeeded 的 key 不会重新执行（skipped / duplicate）。
   导入时 `ON CONFLICT DO NOTHING`，重复 key 不会创建第二个执行行。
5. **所有执行结果均记入日志** — `succeeded` / `failed` / `skipped` /
   `rolledBack`，以及 `pending` / `approved` 的生命周期状态。
6. **Undo/回滚是尽力而为** — executor 可能不支持。
   回滚受 `allowUndo` 门控。
7. **Provider 凭证不经过 routerd 核心** — executor 持有并使用自身的
   云原生身份。
8. **Phase 5.0 不调用实际的 provider CLI/SDK** — 仅伪 executor。

## 结论

- routerd 获得了一条可审查的、默认关闭的路径，用于驱动云端 SAM 的 attach/detach，
  而无需持有云端凭证。
- 日志既是审计记录也是幂等性保障。它是已执行内容的唯一正本。
- provision 与 de-provision 的不对称性（遵循 ADR 0006 的 TTL teardown 滞后）
  通过保持执行的门控和日志记录来遵守，而非对每个事件做出反应式执行。
