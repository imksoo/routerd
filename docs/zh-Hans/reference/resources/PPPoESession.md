# PPPoESession

[各资源的配置](../resources.md#pppoesession) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata:
    name: "<name>"
  spec:
    acName: "<string>" # 选填; string
    authMethod: "chap" # 选填; string; enum=["chap", "pap", "both"]
    defaultRoute: true # 选填; boolean
    enabled: true # 选填; boolean
    ifname: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    ipv6: true # 选填; boolean
    lcpEchoFailure: 0 # 选填; integer; minimum=0
    lcpEchoInterval: 0 # 选填; integer; minimum=0
    lcpFailure: 0 # 选填; integer; minimum=0
    lcpInterval: 0 # 选填; integer; minimum=0
    managed: true # 选填; boolean
    mru: 576 # 选填; integer; minimum=576; maximum=1500
    mtu: 576 # 选填; integer; minimum=576; maximum=1500
    password: "<string>" # 选填; string
    passwordFile: "<string>" # 选填; string
    persist: true # 选填; boolean
    secretEncoding: "plain" # 选填; string; enum=["plain"]
    serviceName: "<string>" # 选填; string
    usePeerDNS: true # 选填; boolean
    username: "<string>" # 必填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/example-pppoe-ipv4-nat.yaml)
