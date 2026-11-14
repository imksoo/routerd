# ProviderActionPolicy

[各资源的配置](../resources.md#provideractionpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

控制交给执行 Plugin 的提供商操作。声明策略本身不会执行操作或登录云端。

## 指定条件与引用关系

- 仅使用第一个 ProviderActionPolicy。省略时 enabled=false、dryRunOnly=true、requireApproval=true，无提供商／操作允许条目且 maxActionsPerRun=0。所有条件共同生效，适用条件也约束 dry-run 预览。
- 自动执行需 enabled=true、dryRunOnly=false、requireApproval=false、正上限，并符合允许列表及具 execute.providerAction 能力的合适 Plugin。外部凭证与实际提供商权限由执行器处理。
- 目前 executionWindow 接受所有字符串，不限制执行时间。allowUndo 仅在有合适 undo 数据时允许尽力回滚，不保证原子事务。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: ProviderActionPolicy
  metadata:
    name: "<name>"
  spec:
    allowUndo: true # 选填; boolean
    allowedActions: # 选填; array
      - "<string>" # string
    allowedCIDRs: # 选填; array
      - "<string>" # string
    allowedProviderRefs: # 选填; array
      - "<string>" # string
    allowedProviders: # 选填; array
      - "<string>" # string
    dryRunOnly: true # 选填; boolean
    enabled: true # 选填; boolean
    executionWindow: "<string>" # 选填; string
    maxActionsPerRun: 0 # 选填; integer
    requireApproval: true # 选填; boolean
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.enabled` | 默认 false。允许执行或预览操作需为 true。 |
| `spec.dryRunOnly` | 默认 true。false 仅在其他条件全部通过时允许实际变更；true 的预览仍需 enabled 与允许列表。 |
| `spec.requireApproval` | 默认 true。false 仅在 enabled=true 且 dryRunOnly=false 时允许自动批准，不跳过允许列表或执行器选择。 |
| `spec.allowedProviders` | 精确匹配 aws/azure/oci/gcp 的允许列表，默认空表示全部拒绝。使用无前后空白的标准拼写。 |
| `spec.allowedProviderRefs` | 可选的操作 providerRef 精确允许列表，空表示不限制引用。不解析为状态来源，此验证也不检查配置存在；须符合实际操作的 providerRef 拼写。 |
| `spec.allowedActions` | 精确允许标准操作：assign-secondary-ip、unassign-secondary-ip、assign-route-table-route、unassign-route-table-route、ensure-forwarding-enabled、ensure-forwarding-disabled。空表示全拒绝。 |
| `spec.allowedCIDRs` | 可选 IPv4/IPv6 CIDR。空不加限制；非空要求 target.address 存在，且其 IP（含 CIDR 输入的主机部分）落于任一前缀。不检查执行器可能变更的所有地址。 |
| `spec.maxActionsPerRun` | 非负整数，默认 0 拒绝执行。正值限制自动控制器每轮候选数；单个操作引擎调用仅检查为正值。不是累计或费用预算。 |
| `spec.allowUndo` | 默认 false；true 允许在具备 undo 数据并符合策略检查时尽力回滚。 |
| `spec.executionWindow` | 可选的自由字符串，默认空。目前不解析为计划，也不在运行时强制限制，因此不限制时间。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/provideraction/policy.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/policy.go) · [pkg/provideraction/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/provideraction/engine.go) · [pkg/controller/provideraction/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/provideraction/controller.go)

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
