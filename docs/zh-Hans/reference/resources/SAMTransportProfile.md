# SAMTransportProfile

[各资源的配置](../resources.md#samtransportprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMTransportProfile
  metadata:
    name: "<name>"
  spec:
    addressingMode: "" # 选填; string; enum=["", "edge-index", "pair-stable"]
    bgp: # 必填; object
      bfd: # 选填; object
        detectMultiplier: 1 # 选填; integer; minimum=1; maximum=50
        enabled: true # 选填; boolean
        interface: "<string>" # 选填; string
        minRx: "<string>" # 选填; string
        minTx: "<string>" # 选填; string
        profile: "" # 选填; string; enum=["", "fast", "normal", "slow"]
      convergenceProfile: "" # 选填; string; enum=["", "default", "fast", "stable"]
      directLocalPreference: 0 # 选填; integer
      ebgpMultihop: 0 # 选填; integer; minimum=0; maximum=255
      exportPolicy: # 选填; object
        allowedPrefixes: # 选填; array
          - "<string>" # string
      generatePeers: true # 选填; boolean
      importPolicy: # 选填; object
        allowedPrefixLengthMax: 0 # 选填; integer; minimum=0; maximum=128
        allowedPrefixLengthMin: 0 # 选填; integer; minimum=0; maximum=128
        allowedPrefixes: # 选填; array
          - "<string>" # string
        forbiddenCommunities: # 选填; array
          - "<string>" # string
        localPreference: 0 # 选填; integer
        nextHopRewrite: "" # 选填; string; enum=["", "peer-address", "unchanged"]
        requiredCommunities: # 选填; array
          - "<string>" # string
      peerASN: 1 # 必填; integer; minimum=1
      routeReflectorClient: true # 选填; boolean
      routeReflectorClusterID: "<string>" # 选填; string
      routerRef: "<string>" # 必填; string
      timers: # 选填; object
        profile: "" # 选填; string; enum=["", "default", "fast", "slow"]
      timersPreset: "" # 选填; string; enum=["", "default", "fast", "slow"]
    encapDport: 1 # 选填; integer; minimum=1; maximum=65535
    encapSport: 1 # 选填; integer; minimum=1; maximum=65535
    encryption: "" # 选填; string; enum=["", "none", "wireguard"]
    innerPrefix: "<string>" # 必填; string
    localEndpoint: "<string>" # 选填; string
    localEndpointFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    mode: "ipip" # 必填; string; enum=["ipip", "gre", "fou", "gue"]
    peersFrom: # 选填; array
      - # object
        direct: true # 选填; boolean
        nodeRefs: # 选填; array
          - "<string>" # string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    publishPeerGroup: true # 选填; boolean
    selfNodeRef: "<string>" # 必填; string
    underlayInterface: "<string>" # 必填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
