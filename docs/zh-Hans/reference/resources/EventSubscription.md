# EventSubscription

[各资源的配置](../resources.md#eventsubscription) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

从持久化数据库匹配尚未过期的联合事件，调用本机 Plugin，将结果保存为 DynamicConfigPart。

## 指定条件与引用关系

- groupRef、至少一个 match.types 与 trigger.pluginRef 必填。组与 Plugin 使用单独 metadata 名称，不是 Kind/name。此验证不检查存在性：Plugin 缺失时运行时为 Pending，没有匹配保存事件的组为 Idle。
- 匹配区分大小写。类型、subject 前缀、各 payload 项与源节点限制以 AND 组合；列表选项以 OR 组合。事件类型完全匹配，不是本机主题通配符。
- 控制器通常每 5 秒轮询保存事件。成功的订阅/事件组合跳过，失败最多尝试三次；中断后的 pending 可重试。处理为至少一次，并不保证 Plugin 外部副作用恰好一次。
- 两个批处理选项都非正值时，一次调用接收当前合格批次，不一定逐事件调用。批处理计时器在内存中，事件与执行记录持久化。调度批次达 1024 件或约 4 MiB 的所选数据时提前执行。
- Plugin 接收匹配事件，以及仅由 Plugin.context.resources 明确允许的配置资源；密钥会遮蔽。TTL 与生成资源由 PluginResult 决定，不由此订阅决定。不同事件批次生成不同的有效 DynamicConfigPart 来源。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventSubscription
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # 必填; string
    match: # 必填; object
      payload: # 选填; object
        "<key>": "<string>" # 选填; string
      sourceNodes: # 选填; array
        - "<string>" # string
      subjectPrefixes: # 选填; array
        - "<string>" # string
      types: # 必填; array
        - "<string>" # string
    trigger: # 必填; object
      batchWindow: "<string>" # 选填; string
      debounce: "<string>" # 选填; string
      pluginRef: "<string>" # 必填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.groupRef` | 必填单独 EventGroup 名称，用于选择保存事件的 group_name。无默认，不会自动创建或启动组。 |
| `spec.match` | 必填匹配对象，types 不可为空。其他可选条件为空时不限制。 |
| `spec.match.types` | 必填完整事件类型列表，至少一个非空条目。匹配任一类型即可；通配符视为普通字符串。 |
| `spec.match.subjectPrefixes` | 可选 subject 前缀列表，默认空允许全部。使用字符串前缀匹配，不是路径片段或通配符匹配。 |
| `spec.match.payload` | 可选的键/字符串完全匹配映射，每个条目均须匹配，键不可空白。缺失键读为空字符串，因此要求空值也会匹配缺失键。 |
| `spec.match.sourceNodes` | 可选 sourceNode 完全匹配允许列表，默认空允许全部。不会自动排除自身事件，请指定所需源标识。 |
| `spec.trigger` | 必填的本机 Plugin 调用配置。与 Plugin.triggers 不同，由订阅控制器执行。 |
| `spec.trigger.pluginRef` | 必填单独 Plugin metadata.name，不是 Plugin/name。Plugin 缺失时为 Pending。使用该 Plugin 的可执行文件、超时、环境与 context 限制。 |
| `spec.trigger.batchWindow` | 可选 Go 时长，自首个所选事件起算。默认空/0 无批处理上限；但 debounce 为正时默认上限为 1m。正 batchWindow 限制反复 debounce。负值可通过验证，视为非正值。 |
| `spec.trigger.debounce` | 可选 Go 时长，自最后新增所选事件起算，默认空/0 禁用。重读相同事件不延长。debounce 与 batchWindow 较早期限触发；无正 batchWindow 时上限 1m。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventsubscription/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventsubscription/controller.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/plugin/context.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/context.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

此 Kind 未登记常规状态引用的允许字段。
