# SAMEnrollmentClient

[各资源的配置](../resources.md#samenrollmentclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClient
  metadata:
    name: "<name>"
  spec:
    bootstrapEndpoints: # 选填; array
      - "<string>" # string
    claimRef: "<string>" # 必填; string
    controlAPITLS: # 选填; object
      caFile: "<string>" # 选填; string
      certFile: "<string>" # 选填; string
      insecureSkipVerify: true # 选填; boolean
      keyFile: "<string>" # 选填; string
      serverName: "<string>" # 选填; string
    controlAPITokenFrom: # 选填; object
      base64: true # 选填; boolean
      env: "<string>" # 选填; string
      file: "<string>" # 选填; string
    retryBackoff: # 选填; object
      max: "<string>" # 选填; string
      min: "<string>" # 选填; string
    rrSocket: "<string>" # 选填; string
    stateTTLRefreshBefore: "<string>" # 选填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
