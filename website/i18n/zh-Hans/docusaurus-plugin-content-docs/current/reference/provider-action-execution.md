# Provider Action Execution（实验性）

> **实验性。** 此功能带有门控，默认禁用，正在积极开发中。
> 关于设计和安全性依据，请参见 [ADR 0007](../adr/0007-provider-action-execution.md)。

routerd 可以通过 **executor plugin** 执行经审批的云提供商变更（例如：为
[选择性地址移动性](./selective-address-mobility)分配 secondary IP）。routerd
不持有云凭据。

![展示 actionPlans 作为不活跃提案保存、导入到 action journal、通过策略门控审批、由持有凭据的 executor plugin 执行的 Provider action execution 示意图](/img/diagrams/dynamic-config-provider-actions.png)

## 凭据模型

- **持有凭据的是 executor plugin，而非 routerd。**
  executor（capability `execute.providerAction`）在自己的进程中运行，通过云原生
  identity（AWS instance profile、Azure managed identity、OCI instance principal）
  或自身环境进行认证。
- **routerd 核心不持有、读取或传递提供商凭据。**
  routerd 仅向 executor 传递已审批的 action plan（不含密钥）和经过允许列表化、
  脱敏处理的 plugin 上下文。
- `action_executions` journal 仅记录计划及其结果，不包含任何密钥。

## `ProviderActionPolicy`

`apiVersion: hybrid.routerd.net/v1alpha1`，`kind: ProviderActionPolicy`。零值为
安全的锁定状态。执行禁用，仅 dry-run，需要审批，无允许列表。

| 字段 | 类型 | 默认值 | 含义 |
| --- | --- | --- | --- |
| `enabled` | bool | `false` | 非 `true` 时执行被禁用。 |
| `dryRunOnly` | bool (pointer) | 省略时 `true` | 仅允许 dry-run。拒绝实际变更。 |
| `requireApproval` | bool (pointer) | 省略时 `true` | 执行前需要运维人员审批。 |
| `allowedProviders` | list | 空 = 无 | 允许的提供商：`aws`、`azure`、`oci`、`gcp`。 |
| `allowedProviderRefs` | list | 空 = 不限制 | 限制为指定的 `CloudProviderProfile` ref。 |
| `allowedActions` | list | 空 = 无 | 规范动词：`assign-secondary-ip`、`unassign-secondary-ip`、`ensure-forwarding-enabled`、`ensure-forwarding-disabled`。 |
| `allowedCIDRs` | list | 空 = 不限制 | action 的目标地址必须在某个 CIDR 范围内。 |
| `maxActionsPerRun` | int | `0` = 无 action | 每次执行的 action 上限。设置正值以允许。 |
| `allowUndo` | bool | `false` | 允许尽力回滚。 |
| `executionWindow` | string | 空 = 无窗口 | 可选的时间窗口。宽松验证。 |

示例（除单个允许列表化的 action 外全部锁定）：

```yaml
apiVersion: hybrid.routerd.net/v1alpha1
kind: ProviderActionPolicy
metadata:
  name: sam-execution
spec:
  enabled: true
  dryRunOnly: false
  requireApproval: true
  allowedProviders: [aws]
  allowedActions: [assign-secondary-ip, unassign-secondary-ip]
  allowedCIDRs: [10.88.60.0/24]
  maxActionsPerRun: 4
  allowUndo: true
```

## action 生命周期

planner plugin 提议的 action plan 被导入到 journal 中，经历以下状态转换。

```text
pending  ->  approved  ->  succeeded
                       ->  failed
                       ->  skipped
                            (succeeded) -> rolledBack
```

- **pending** — 从 `actionPlan` 导入，以 `idempotencyKey` 为键，等待审批。
- **approved** — 运维人员已审批，或策略自动审批
  （当 `requireApproval: false` 且 `enabled` 且非 `dryRunOnly` 时）。
- **succeeded / failed / skipped** — executor 报告的结果。`skipped` 表示已
  succeeded 的 `idempotencyKey` 的重复，或策略拒绝的 action。
- **rolledBack** — 对先前 succeeded 的 action 应用尽力 undo
  （仅在 `allowUndo` 为 true 时）。

导入是幂等的。重新导入相同的 `idempotencyKey` 不会创建第二行，因此已 succeeded
的 action 不会被执行两次。

## `routerctl action` 命令

当前面向运维人员的接口有意采用 journal 导向。先导入或审批 action，仅执行通过策略
的已审批条目。

| 命令 | 用途 |
| --- | --- |
| `routerctl action list` | 列出 journal 条目（按状态/提供商过滤）。 |
| `routerctl action show ID` | 显示单个 journal 条目。 |
| `routerctl action approve ID` | 运维人员审批：从 `pending` 变为 `approved`。 |
| `routerctl action execute --dry-run` | 验证和预览。无变更。 |
| `routerctl action execute --approved` | 执行策略允许的已审批 action。 |
| `routerctl action journal` | 输出执行 journal / 审计跟踪。 |
| `routerctl action rollback ID --dry-run` | 尽力 undo 的预览（无变更）。 |

## dry-run vs 执行

- **dry-run** 是默认行为，是 `dryRunOnly` 为 true（或 `enabled` 为 false）期间
  唯一允许的路径。它验证计划、检查策略并预览效果，但**不进行**任何提供商变更。
- **执行** 通过 executor 进行实际变更，仅在所有硬安全停止条件满足时才执行：
  `enabled`、非 `dryRunOnly`、已审批（或策略自动审批）、允许列表匹配、
  在 `maxActionsPerRun` 以内。

关于硬安全停止条件的完整列表，请参见
[ADR 0007](../adr/0007-provider-action-execution.md)。
