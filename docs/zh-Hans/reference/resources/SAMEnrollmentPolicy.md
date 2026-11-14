# SAMEnrollmentPolicy

[各资源的配置](../resources.md#samenrollmentpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentPolicy
  metadata:
    name: "<name>"
  spec:
    allowedLeafIDs: # 选填; object
      pattern: "<string>" # 选填; string
    directMesh: # 选填; object
      peerGroupRef: "<string>" # 选填; string
    endpointPrefixes: # 选填; array
      - "<string>" # string
    joinAudience: "<string>" # 选填; string
    joinTokenFrom: # 选填; object
      base64: true # 选填; boolean
      env: "<string>" # 选填; string
      file: "<string>" # 选填; string
    mobilityPoolRefs: # 选填; array
      - "<string>" # string
    mobilityPrefixes: # 选填; array
      - "<string>" # string
    rrNodeSetRef: "<string>" # 选填; string
    rrSetRef: "<string>" # 选填; string
    transportProfileRef: "<string>" # 必填; string
    ttl: "<string>" # 选填; string
    tunnelAddressPrefixes: # 必填; array
      - "<string>" # string
    wireGuard: # 选填; object
      allowedExtraIPPrefixes: # 选填; array
        - "<string>" # string
      endpointPrefixes: # 选填; array
        - "<string>" # string
      interface: "<string>" # 必填; string
      persistentKeepalive: 0 # 选填; integer; minimum=0; maximum=65535
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
