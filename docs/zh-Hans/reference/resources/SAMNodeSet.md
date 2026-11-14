# SAMNodeSet

[各资源的配置](../resources.md#samnodeset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMNodeSet
  metadata:
    name: "<name>"
  spec:
    nodes: # 必填; array
      - # object
        eventEndpoint: "<string>" # 选填; string
        macAddresses: # 选填; array
          - "<string>" # string; pattern="^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$"
        maintenance: # 选填; object
          drain: true # 选填; boolean
        maxSecondaryIPs: 0 # 选填; integer; minimum=0
        nodeRef: "<string>" # 必填; string
        placement: # 选填; object
          group: "<string>" # 选填; string
          priority: 0 # 选填; integer; minimum=0; maximum=1000000
        role: "" # 选填; string; enum=["", "onprem", "cloud"]
        routeReflector: true # 选填; boolean
        samEndpoint: "<string>" # 选填; string
        samEndpointFrom: # 选填; object
          field: "<string>" # 选填; string
          optional: true # 选填; boolean
          resource: "<string>" # 必填; string
        site: "<string>" # 选填; string
        wireGuard: # 选填; object
          allowedIPs: # 选填; array
            - "<string>" # string
          endpoint: "<string>" # 选填; string
          persistentKeepalive: 0 # 选填; integer; minimum=0; maximum=65535
          publicKey: "<string>" # 选填; string
          transportEndpoint: "<string>" # 选填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-rr-a-hub.yaml)
