# EventRule

[各资源的配置](../resources.md#eventrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

从持久化的本机事件记录匹配模式并生成事件，可按关联键分别维护状态。

## 指定条件与引用关系

- pattern.operator 与 emit.topic 必填。topic、topics、trigger、expected 至少提供一项，但宽松的验证不保证模式可运行：只有 expected 不构成输入列表，absence 需要 trigger/topic。不会检查主题对应的资源是否存在。
- 输入顺序为 topics、topic、trigger。all_of 与 sequence 使用完整主题字符串匹配；any_of、window、count、throttle、debounce、absence 支持以点分隔的 *（一段）与 **（零段以上）。
- all_of 触发后仍保留已见主题，全部见过后每个符合事件都会再次触发。sequence 完成后重置；strict 时列表内乱序事件也会重置。window 达阈值后清空时间桶，count 达阈值后重置计数。
- 计数、未完成序列与计时器仅存于内存，进程重启后从空状态开始。事件读取位置会持久化；新读取者从当前记录末尾开始。位置保存失败可能重复处理输入。所有规则都排除 routerd-eventrule 生成的事件，以避免连锁循环。
- 每条规则最多保留 4096 个关联键，会移除最久未观测键的计数与计时器。window/throttle 比较输入事件时间；absence/debounce 使用进程经过时间计时。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EventRule
  metadata:
    name: "<name>"
  spec:
    emit: # 必填; object
      attributes: # 选填; object
        "<key>": "<string>" # 选填; string
      topic: "<string>" # 必填; string
    pattern: # 必填; object
      allow_missing_correlation: true # 选填; boolean
      correlate_by: "<string>" # 选填; string
      duration: "<string>" # 选填; string
      expected: "<string>" # 选填; string
      interval: "<string>" # 选填; string
      operator: "all_of" # 必填; string; enum=["all_of", "any_of", "sequence", "window", "absence", "throttle", "debounce", "count"]
      quiet: "<string>" # 选填; string
      rate: 0 # 选填; integer; minimum=0
      strict: true # 选填; boolean
      threshold: 0 # 选填; integer; minimum=0
      topic: "<string>" # 选填; string
      topics: # 选填; array
        - "<string>" # string
      trigger: "<string>" # 选填; string
      window: "<string>" # 选填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.pattern` | 必填的模式对象；不会拒绝所选运算符未使用的字段。 |
| `spec.pattern.operator` | 必填：all_of、any_of、sequence、window、absence、throttle、debounce 或 count，无默认值。 |
| `spec.pattern.topic` | 可选的单一输入主题，附加于 topics 后。absence 的 trigger 为空时以此作为启动主题。默认空。 |
| `spec.pattern.topics` | 可选的有序输入列表，默认空。sequence 按此顺序；all_of 要求每个完整主题都曾出现。 |
| `spec.pattern.trigger` | 可选的 absence 启动主题，优先于 topic，会启动或重新启动计时器。其他运算符的输入列表也会在 topic 后加入此值。默认空。 |
| `spec.pattern.expected` | 可选的 absence 取消主题，同键匹配时取消待处理计时器，优先于 trigger 匹配。不是普通输入主题。默认空，表示不取消。 |
| `spec.pattern.duration` | 可选 Go 时长；window/throttle/absence 默认 1m，debounce 默认 1s。专用 window/interval/quiet 优先。验证也接受零或负值，延迟行为请使用正值。 |
| `spec.pattern.window` | window 的回溯期间，优先于 duration，均省略时为 1m。含起始边界；达阈值后清空时间桶。其他运算符不使用。 |
| `spec.pattern.quiet` | debounce 的静默期间，优先于 duration，均省略时为 1s。每次匹配输入都重启计时器，按最后输入生成事件。 |
| `spec.pattern.interval` | throttle 每个关联键的最短发送间隔，优先于 duration，均省略时为 1m。第一个匹配事件立即发送。 |
| `spec.pattern.rate` | 验证接受的非负整数，默认 0，但当前引擎不读取。无法控制 throttle；请使用 interval。 |
| `spec.pattern.threshold` | window/count 的事件阈值，非负整数；省略或 0 表示 1。其他运算符不使用。 |
| `spec.pattern.correlate_by` | 可选 attributes.<key>、resource.name/kind/apiVersion 或 daemon.instance/kind 以分开状态。空值使用共享键 default。输入主题是字符串，并非 Kind/name 引用。 |
| `spec.pattern.allow_missing_correlation` | 默认 false：跳过缺少关联值的输入并增加 warningCount。true 将缺值归入空键。但 resource 对象存在而所选成员为空时，false 也会接受。 |
| `spec.pattern.strict` | 默认 false，仅供 sequence。列表内主题乱序时进度归零，该事件不会重新作为首项；忽略列表外主题。 |
| `spec.emit` | 必填的输出事件对象。严重性固定 info，resource 为此 EventRule。 |
| `spec.emit.topic` | 必填且不可为空的输出事件类型，无默认值。规则引擎不会消费自身生成的事件。 |
| `spec.emit.attributes` | 可选字符串映射。展开 ${event.type}、${correlation}、${count}、${resource.name/kind/apiVersion}、${attributes.<key>}；无法解析的占位符保持原样。正 count 与非空 correlation 会覆盖同名输出键。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/eventrule/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/eventrule/engine.go) · [pkg/eventconsumer/consumer.go](https://github.com/imksoo/routerd/blob/main/pkg/eventconsumer/consumer.go) · [pkg/bus/bus.go](https://github.com/imksoo/routerd/blob/main/pkg/bus/bus.go)
