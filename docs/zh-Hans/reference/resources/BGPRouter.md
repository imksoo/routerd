# BGPRouter

[各资源的配置](../resources.md#bgprouter) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPRouter
  metadata:
    name: "<name>"
  spec:
    asn: 1 # 必填; integer; minimum=1
    backend: "" # 选填; string; enum=["", "gobgp"]
    communities: # 选填; object
      accept: # 选填; array
        - "<string>" # string
      send: "" # 选填; string; enum=["", "standard", "extended", "both"]
      set: # 选填; object
        in: # 选填; array
          - "<string>" # string
        out: # 选填; array
          - "<string>" # string
    convergenceProfile: "" # 选填; string; enum=["", "default", "fast", "stable"]
    exportPolicy: # 选填; object
      allowedPrefixes: # 选填; array
        - "<string>" # string
    gracefulRestart: # 选填; object
      enabled: true # 选填; boolean
      restartTime: "<string>" # 选填; string
      stalePathTime: "<string>" # 选填; string
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
    listen: # 选填; object
      address: "<string>" # 选填; string
      port: 1 # 选填; integer; minimum=1; maximum=65535
    redistribute: # 选填; object
      connected: # 选填; object
        allowedPrefixes: # 选填; array
          - "<string>" # string
      static: # 选填; object
        allowedPrefixes: # 选填; array
          - "<string>" # string
    routerID: "<string>" # 必填; string
    timers: # 选填; object
      profile: "" # 选填; string; enum=["", "default", "fast", "slow"]
    vrf: "<string>" # 选填; string
    watcher: # 选填; object
      maxPrefixes: 1 # 选填; integer; minimum=1; maximum=999999
      peerStateChangeThrottle: "<string>" # 选填; string
      pollInterval: "<string>" # 选填; string
    when: # 选填; object
      state: # 选填; object
        "<key>": # 选填; object
          contains: "<string>" # 选填; string
          equals: "<string>" # 选填; string
          exists: true # 选填; boolean
          for: "<string>" # 选填; string
          in: # 选填; array
            - "<string>" # string
          maxAge: "<string>" # 选填; string
          status: "set" # 选填; string; enum=["set", "unset", "unknown"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
