# DerivedEvent

[各资源的配置](../resources.md#derivedevent) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

资源状态条件改变时，发送成立或撤回事件。与 EventRule 不同，此处评估当前状态而非事件序列。

## 指定条件与引用关系

- topic 与非空 inputs 列表必填。输入使用 field/equals/not_empty/any_of 结构，不是通用 resource/field 来源、dependsOn 或 when。
- 当前以 Kind/name.status.field（可包在 ${...} 中）读取 net.routerd.net/v1alpha1 状态。不含 .status. 的字符串视为固定值，不是状态引用。普通状态来源验证不会检查这些输入的来源存在性或提供字段兼容性。
- 控制器在启动、收到自身以外的本机 routerd.** 事件及定期处理时评估（默认 5 秒）。进程重启会重置转换状态与滞后计时器，不会从已存状态恢复。
- retractWhen 会验证，但当前评估器不读取。撤回按 emitWhen 判定的反值，因此改变 retractWhen 不会改变行为。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DerivedEvent
  metadata:
    name: "<name>"
  spec:
    [ emitInitial: <boolean> ]
    [ emitWhen: ("all_true" | "any_true") ]
    [ hysteresis: <string> ]
    inputs:
      -
        [ any_of:
            -
              -
                [ equals: <string> ]
                [ field: <string> ]
                [ not_empty: <boolean> ]
        ]
        [ equals: <string> ]
        [ field: <string> ]
        [ not_empty: <boolean> ]
    [ retractWhen: ("any_false" | "all_false") ]
    topic: <string>
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.topic` | **必填** | string | 必填的基础事件主题。成立发送 <topic>.asserted，不成立发送 <topic>.retracted；不发送裸主题事件。 |
| `spec.inputs` | **必填** | array | 必填且非空的条件列表，由 emitWhen 组合。空条件对象会评估为 true，请明确填写条件。 |
| `spec.inputs[].field` | *选填* | string | 可选字符串，默认空。Kind/name.status.field 读取状态顶层成员；来源或成员不存在则为空。其他字符串视为固定值。数组转为 JSON 字符串，equals 比较完整序列化值。 |
| `spec.inputs[].equals` | *选填* | string | 可选、区分大小写的完全匹配。默认空表示不比较。field 非空且未设 equals/not_empty 时，隐含要求值非空白。 |
| `spec.inputs[].not_empty` | *选填* | boolean | 默认 false。true 拒绝空值或纯空白；equals 亦非空时，两个条件均须成立。 |
| `spec.inputs[].any_of` | *选填* | array | 可选的 AND 组 OR 列表。非空时优先于此输入的 field/equals/not_empty。每组至少一个条件且全部成立；空组为 false。 |
| `spec.inputs[].any_of[][].field` | *选填* | string | 组条件字段，状态或固定值规则同 inputs[].field。 |
| `spec.inputs[].any_of[][].equals` | *选填* | string | 组完全匹配；空值与默认语义同 inputs[].equals。 |
| `spec.inputs[].any_of[][].not_empty` | *选填* | boolean | 组非空白检查，默认 false，语义同 inputs[].not_empty。 |
| `spec.emitWhen` | *选填* | string; enum=["all_true", "any_true"] | 默认 all_true 要求所有输入成立；any_true 要求至少一项成立。此目标变为 false 也会决定撤回。 |
| `spec.retractWhen` | *选填* | string; enum=["any_false", "all_false"] | 接受 any_false（默认）与 all_false，但验证后未使用。不可依赖独立的撤回条件。 |
| `spec.hysteresis` | *选填* | string | 可选 Go 时长，默认 0 为立即转换。新目标须持续至计时到期再检查；恢复旧目标会取消。初始状态不等待。验证接受负值，会立即触发。 |
| `spec.emitInitial` | *选填* | boolean | 默认 false，仅保存初始状态而不发送事件。true 在每次新控制器状态建立时发送初始结果，包含重启后。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/derived/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/derived/engine.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

`phase`, `topic`
