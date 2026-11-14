# EventRule

[各资源的配置](../resources.md#eventrule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

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
