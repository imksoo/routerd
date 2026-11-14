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

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: EventRule
  metadata:
    name: "<name>"
  spec:
    emit:
      [ attributes:
          [ <name>: <string> ]
      ]
      topic: <string>
    pattern:
      [ allow_missing_correlation: <boolean> ]
      [ correlate_by: <string> ]
      [ duration: <string> ]
      [ expected: <string> ]
      [ interval: <string> ]
      operator: ("all_of" | "any_of" | "sequence" | "window" | "absence" | "throttle" | "debounce" | "count")
      [ quiet: <string> ]
      [ rate: <integer> ]
      [ strict: <boolean> ]
      [ threshold: <integer> ]
      [ topic: <string> ]
      [ topics:
          - <string>
      ]
      [ trigger: <string> ]
      [ window: <string> ]
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.pattern` | **必填** | object | 必填的模式对象；不会拒绝所选运算符未使用的字段。 |
| `spec.pattern.operator` | **必填** | string; enum=["all_of", "any_of", "sequence", "window", "absence", "throttle", "debounce", "count"] | 必填：all_of、any_of、sequence、window、absence、throttle、debounce 或 count，无默认值。 |
| `spec.pattern.topic` | *选填* | string | 可选的单一输入主题，附加于 topics 后。absence 的 trigger 为空时以此作为启动主题。默认空。 |
| `spec.pattern.topics` | *选填* | array | 可选的有序输入列表，默认空。sequence 按此顺序；all_of 要求每个完整主题都曾出现。 |
| `spec.pattern.trigger` | *选填* | string | 可选的 absence 启动主题，优先于 topic，会启动或重新启动计时器。其他运算符的输入列表也会在 topic 后加入此值。默认空。 |
| `spec.pattern.expected` | *选填* | string | 可选的 absence 取消主题，同键匹配时取消待处理计时器，优先于 trigger 匹配。不是普通输入主题。默认空，表示不取消。 |
| `spec.pattern.duration` | *选填* | string | 可选 Go 时长；window/throttle/absence 默认 1m，debounce 默认 1s。专用 window/interval/quiet 优先。验证也接受零或负值，延迟行为请使用正值。 |
| `spec.pattern.window` | *选填* | string | window 的回溯期间，优先于 duration，均省略时为 1m。含起始边界；达阈值后清空时间桶。其他运算符不使用。 |
| `spec.pattern.quiet` | *选填* | string | debounce 的静默期间，优先于 duration，均省略时为 1s。每次匹配输入都重启计时器，按最后输入生成事件。 |
| `spec.pattern.interval` | *选填* | string | throttle 每个关联键的最短发送间隔，优先于 duration，均省略时为 1m。第一个匹配事件立即发送。 |
| `spec.pattern.rate` | *选填* | integer; minimum=0 | 验证接受的非负整数，默认 0，但当前引擎不读取。无法控制 throttle；请使用 interval。 |
| `spec.pattern.threshold` | *选填* | integer; minimum=0 | window/count 的事件阈值，非负整数；省略或 0 表示 1。其他运算符不使用。 |
| `spec.pattern.correlate_by` | *选填* | string | 可选 attributes.<key>、resource.name/kind/apiVersion 或 daemon.instance/kind 以分开状态。空值使用共享键 default。输入主题是字符串，并非 Kind/name 引用。 |
| `spec.pattern.allow_missing_correlation` | *选填* | boolean | 默认 false：跳过缺少关联值的输入并增加 warningCount。true 将缺值归入空键。但 resource 对象存在而所选成员为空时，false 也会接受。 |
| `spec.pattern.strict` | *选填* | boolean | 默认 false，仅供 sequence。列表内主题乱序时进度归零，该事件不会重新作为首项；忽略列表外主题。 |
| `spec.emit` | **必填** | object | 必填的输出事件对象。严重性固定 info，resource 为此 EventRule。 |
| `spec.emit.topic` | **必填** | string | 必填且不可为空的输出事件类型，无默认值。规则引擎不会消费自身生成的事件。 |
| `spec.emit.attributes` | *选填* | object | 可选字符串映射。展开 ${event.type}、${correlation}、${count}、${resource.name/kind/apiVersion}、${attributes.<key>}；无法解析的占位符保持原样。正 count 与非空 correlation 会覆盖同名输出键。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/eventrule/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/eventrule/engine.go) · [pkg/eventconsumer/consumer.go](https://github.com/imksoo/routerd/blob/main/pkg/eventconsumer/consumer.go) · [pkg/bus/bus.go](https://github.com/imksoo/routerd/blob/main/pkg/bus/bus.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `topic`
