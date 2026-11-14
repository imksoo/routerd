# DynamicConfigSource

[各资源的配置](../resources.md#dynamicconfigsource) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: plugin.routerd.net/v1alpha1` · `metadata.name`

声明通过插件获取的动态配置来源。当前有类型解码与验证，但没有读取DynamicConfigSourceSpec的runtime处理，声明本身不会执行插件。

## 指定条件与引用关系

- pluginRef仅声明名称，验证时不检查目标是否存在。此处ttl必填，但实际插件结果合并另用PluginResult.status.ttl，不能依此资源覆盖结果TTL。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: plugin.routerd.net/v1alpha1
  kind: DynamicConfigSource
  metadata:
    name: "<name>"
  spec:
    [ mergePolicy:
        [ conflict: ("" | "reject") ]
    ]
    pluginRef: <string>
    [ triggers:
        -
          [ every: <string> ]
          [ topic: <string> ]
          type: ("interval" | "event")
    ]
    ttl: <string>
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.pluginRef` | **必填** | string | 预定来源Plugin的metadata.name；必填且不可为空白，无默认值。不加Plugin/；当前没有此声明的运行时解析器。 |
| `spec.ttl` | **必填** | string | 必须为正的Go duration，无默认值。当前仅验证此声明，没有runtime将它应用于插件执行或结果期限。 |
| `spec.mergePolicy` | *选填* | object | 选填合并策略对象；当前没有读取此来源声明的runtime处理。 |
| `spec.mergePolicy.conflict` | *选填* | string; enum=["", "reject"] | 验证仅接受空值或reject；两者都不会通过此声明启用现有资源替换。 |
| `spec.triggers` | *选填* | array | 触发声明列表，默认空；当前runtime仅验证与显示，请参照上方限制。 |
| `spec.triggers[].type` | **条件必填** | string; enum=["interval", "event"] | 触发类型；必须指定interval或event，无默认值。 |
| `spec.triggers[].every` | *选填* | string | interval时必填，须为正的Go duration；event时此字段不产生周期调度，无默认值。 |
| `spec.triggers[].topic` | *选填* | string | 事件主题声明；选填，默认空，event时验证器也不强制要求。这不是资源名称。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/resource_kinds.go](https://github.com/imksoo/routerd/blob/main/pkg/api/resource_kinds.go) · [pkg/config/validate_plugin.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_plugin.go) · [pkg/plugin/dynamic_config.go](https://github.com/imksoo/routerd/blob/main/pkg/plugin/dynamic_config.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

此 Kind 未登记常规状态引用的允许字段。
