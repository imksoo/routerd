# DynamicOverridePolicy

[各资源的配置](../resources.md#dynamicoverridepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: config.routerd.net/v1alpha1` · `metadata.name`

授权指定动态来源在动态部分有效期间屏蔽特定启动配置资源，不改写已保存的启动配置。

## 指定条件与引用关系

- 以来源、操作及目标apiVersion/kind/name完全匹配判定。没有匹配allow时拒绝屏蔽；同一目标的多个有效屏蔽合并至最晚到期时间。

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: config.routerd.net/v1alpha1
  kind: DynamicOverridePolicy
  metadata:
    name: "<name>"
  spec:
    allow: # 必填; array
      - # object
        operations: # 必填; array
          - "<string>" # string
        source: "<string>" # 必填; string
        targets: # 必填; array
          - # object
            apiVersion: "<string>" # 必填; string
            kind: "<string>" # 必填; string
            name: "<string>" # 必填; string
```

## 字段含义与默认值

| YAML | 字段含义与默认值 |
| --- | --- |
| `spec.allow` | 允许规则列表；空列表无效果且不授权。 |
| `spec.allow[].source` | 必填来源标识，须与DynamicConfigPart.spec.source完全一致；不一定是Plugin或DynamicConfigSource名称，无通配符与默认值。 |
| `spec.allow[].operations` | 允许操作；仅接受mask，空列表不允许任何操作。 |
| `spec.allow[].targets` | 可屏蔽的启动配置资源完整标识列表；空列表不允许任何目标。 |
| `spec.allow[].targets[].apiVersion` | 目标API组与版本；每项必填，无默认值。 |
| `spec.allow[].targets[].kind` | 目标资源类型，必填；不展开通配符。 |
| `spec.allow[].targets[].name` | 必填，与目标metadata.name完全匹配；不展开通配符。 |

实现: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_config.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_config.go) · [pkg/dynamicconfig/merge.go](https://github.com/imksoo/routerd/blob/main/pkg/dynamicconfig/merge.go)

## 可引用的状态字段

常规状态引用验证接受此 Kind 的下列字段名称。列于允许列表不保证目前有值，请检查实际状态。这些是输出，不是 YAML spec 配置。

此 Kind 未登记常规状态引用的允许字段。
