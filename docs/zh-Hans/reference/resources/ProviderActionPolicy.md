# ProviderActionPolicy

[各资源的配置](../resources.md#provideractionpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: ProviderActionPolicy
  metadata:
    name: "<name>"
  spec:
    allowUndo: true # 选填; boolean
    allowedActions: # 选填; array
      - "<string>" # string
    allowedCIDRs: # 选填; array
      - "<string>" # string
    allowedProviderRefs: # 选填; array
      - "<string>" # string
    allowedProviders: # 选填; array
      - "<string>" # string
    dryRunOnly: true # 选填; boolean
    enabled: true # 选填; boolean
    executionWindow: "<string>" # 选填; string
    maxActionsPerRun: 0 # 选填; integer
    requireApproval: true # 选填; boolean
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/aws.yaml)
