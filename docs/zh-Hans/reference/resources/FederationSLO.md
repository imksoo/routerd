# FederationSLO

[各资源的配置](../resources.md#federationslo) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: FederationSLO
  metadata:
    name: "<name>"
  spec:
    delivery: # 选填; object
      expiresSoonSeconds: 0 # 选填; integer; minimum=0
      lagFailSeconds: 0 # 选填; integer; minimum=0
      lagWarnSeconds: 0 # 选填; integer; minimum=0
    groupRef: "<string>" # 必填; string
    subscription: # 选填; object
      maxFailedRuns: 0 # 选填; integer; minimum=0
      maxPendingRuns: 0 # 选填; integer; minimum=0
```
