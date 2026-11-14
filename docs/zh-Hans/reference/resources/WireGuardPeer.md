# WireGuardPeer

[各资源的配置](../resources.md#wireguardpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardPeer
  metadata:
    name: "<name>"
  spec:
    allowedIPs: # 必填; array
      - "<string>" # string
    endpoint: "<string>" # 选填; string
    interface: "<string>" # 必填; string
    persistentKeepalive: 0 # 选填; integer; minimum=0; maximum=65535
    presharedKey: "<string>" # 选填; string
    presharedKeyFile: "<string>" # 选填; string
    publicKey: "<string>" # 必填; string
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-wireguard.yaml)
