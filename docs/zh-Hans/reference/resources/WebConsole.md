# WebConsole

[各资源的配置](../resources.md#webconsole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: WebConsole
  metadata:
    name: "<name>"
  spec:
    basePath: "<string>" # 选填; string
    enabled: true # 选填; boolean
    links: # 选填; array
      - # object
        description: "<string>" # 选填; string
        label: "<string>" # 必填; string
        url: "<string>" # 必填; string
    listenAddress: "<string>" # 选填; string
    listenAddressFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    port: 1 # 选填; integer; minimum=1; maximum=65535
    title: "<string>" # 选填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
