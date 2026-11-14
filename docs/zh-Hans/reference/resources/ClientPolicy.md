# ClientPolicy

[各资源的配置](../resources.md#clientpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: "<name>"
  spec:
    classification: # 选填; array
      - # object
        ipv4Reservation: "<string>" # 选填; string
        ipv6Addresses: # 选填; array
          - "<string>" # string
        match: # 必填; object
          dhcpFingerprints: # 选填; array
            - "<string>" # string
          hostnamePatterns: # 选填; array
            - "<string>" # string
          macs: # 选填; array
            - "<string>" # string
          ouiPrefixes: # 选填; array
            - "<string>" # string
        mode: "trusted" # 必填; string; enum=["trusted", "guest", "isolated"]
        name: "<string>" # 选填; string
    guestEgressAllow: # 选填; array
      - "<string>" # string
    guestEgressDeny: # 选填; array
      - "<string>" # string
    guestServices: # 选填; array
      - "<string>" # string
    interfaces: # 选填; array
      - "<string>" # string
    isolation: # 选填; object
      lanInternet: "" # 选填; string; enum=["", "allow", "deny"]
      lanLAN: "" # 选填; string; enum=["", "allow", "deny"]
      lanMgmt: "" # 选填; string; enum=["", "allow", "deny"]
      mDNSBroadcast: "" # 选填; string; enum=["", "allow", "deny"]
    macs: # 选填; array
      - "<string>" # string
    mode: "include" # 必填; string; enum=["include", "exclude"]
```

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)
