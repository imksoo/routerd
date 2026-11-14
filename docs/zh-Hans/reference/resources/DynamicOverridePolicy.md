# DynamicOverridePolicy

[各资源的配置](../resources.md#dynamicoverridepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: config.routerd.net/v1alpha1` · `metadata.name`

授权指定动态来源在动态部分有效期间屏蔽特定启动配置资源，不改写已保存的启动配置。

## 指定条件与引用关系

- 以来源、操作及目标apiVersion/kind/name完全匹配判定。没有匹配allow时拒绝屏蔽；同一目标的多个有效屏蔽合并至最晚到期时间。

## 配置语法

这是说明用语法，不是可直接复制的YAML。未加括号的字段在父条目内必填；`[ ... ]`表示按条件可省略，`<类型>`为替换值，`(a | b)`表示选项。选填父条目内仍可能有必填子条目。条件必填、互斥与实际默认值请见下表；常规YAML请使用链接示例。

```text
- apiVersion: config.routerd.net/v1alpha1
  kind: DynamicOverridePolicy
  metadata:
    name: "<name>"
  spec:
    allow:
      -
        operations:
          - <string>
        source: <string>
        targets:
          -
            apiVersion: <string>
            kind: <string>
            name: <string>
```

## 字段含义与默认值

选填父条目的必填子条目仅在使用父条目时需要。其他具体条件列于说明；`[ ... ]`选项仍可能需要择一。

| YAML | 指定要求 | 类型与限制 | 字段含义与默认值 |
| --- | --- | --- | --- |
| `spec.allow` | **必填** | array | 允许规则列表；空列表无效果且不授权。 |
| `spec.allow[].source` | **必填** | string | 必填来源标识，须与DynamicConfigPart.spec.source完全一致；不一定是Plugin或DynamicConfigSource名称，无通配符与默认值。 |
| `spec.allow[].operations` | **必填** | array | 允许操作；仅接受mask，空列表不允许任何操作。 |
| `spec.allow[].targets` | **必填** | array | 可屏蔽的启动配置资源完整标识列表；空列表不允许任何目标。 |
| `spec.allow[].targets[].apiVersion` | **必填** | string | 目标API组与版本；每项必填，无默认值。 |
| `spec.allow[].targets[].kind` | **必填** | string | 目标资源类型，必填；不展开通配符。 |
| `spec.allow[].targets[].name` | **必填** | string | 必填，与目标metadata.name完全匹配；不展开通配符。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_config.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_config.go) · [pkg/dynamicconfig/merge.go](https://github.com/imksoo/routerd/blob/main/pkg/dynamicconfig/merge.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

此 Kind 未登记常规状态引用的允许字段。
