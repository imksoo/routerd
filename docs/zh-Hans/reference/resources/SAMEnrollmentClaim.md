# SAMEnrollmentClaim

[各资源的配置](../resources.md#samenrollmentclaim) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClaim
  metadata:
    name: "<name>"
  spec:
    bgp: # 选填; object
      asn: 1 # 选填; integer; minimum=1
      routerID: "<string>" # 选填; string
    directMesh: true # 选填; boolean
    endpoint: "<string>" # 选填; string
    expiresAt: "<string>" # 选填; string
    joinAudience: "<string>" # 选填; string
    joinHMAC: "<string>" # 选填; string
    joinNonce: "<string>" # 选填; string
    joinTimestamp: "<string>" # 选填; string
    leafID: "<string>" # 必填; string
    mobility: # 选填; object
      ownedAddresses: # 选填; array
        - "<string>" # string
    policyRef: "<string>" # 必填; string
    revoked: true # 选填; boolean
    rrSetRef: "<string>" # 选填; string
    tunnelAddress: "<string>" # 必填; string
    wireGuard: # 选填; object
      allowedIPs: # 选填; array
        - "<string>" # string
      endpoint: "<string>" # 选填; string
      persistentKeepalive: 0 # 选填; integer; minimum=0; maximum=65535
      publicKey: "<string>" # 必填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
