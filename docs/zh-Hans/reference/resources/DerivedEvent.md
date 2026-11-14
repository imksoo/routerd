# DerivedEvent

[各资源的配置](../resources.md#derivedevent) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DerivedEvent
  metadata:
    name: "<name>"
  spec:
    emitInitial: true # 选填; boolean
    emitWhen: "all_true" # 选填; string; enum=["all_true", "any_true"]
    hysteresis: "<string>" # 选填; string
    inputs: # 必填; array
      - # object
        any_of: # 选填; array
          - # array
            - # object
              equals: "<string>" # 选填; string
              field: "<string>" # 选填; string
              not_empty: true # 选填; boolean
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        not_empty: true # 选填; boolean
    retractWhen: "any_false" # 选填; string; enum=["any_false", "all_false"]
    topic: "<string>" # 必填; string
```
