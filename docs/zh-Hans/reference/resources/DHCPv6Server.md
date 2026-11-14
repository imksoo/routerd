# DHCPv6Server

[各资源的配置](../resources.md#dhcpv6server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # 选填; object
      end: "<string>" # 选填; string
      leaseTime: "<string>" # 选填; string
      start: "<string>" # 选填; string
    configPath: "<string>" # 选填; string
    defaultRoute: true # 选填; boolean
    delegatedAddress: "<string>" # 选填; string
    dependsOn: # 选填; array
      - # object
        equals: "<string>" # 选填; string
        field: "<string>" # 选填; string
        notEmpty: true # 选填; boolean
        optional: true # 选填; boolean
        phase: "<string>" # 选填; string
        resource: "<string>" # 必填; string
    dnsServerFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    dnsServers: # 选填; array
      - "<string>" # string
    dnsSource: "self" # 选填; string; enum=["self", "static", "none"]
    domainSearch: # 选填; array
      - "<string>" # string
    domainSearchFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    interface: "<string>" # 选填; string
    leaseFile: "<string>" # 选填; string
    leaseTime: "<string>" # 选填; string
    listenInterfaces: # 选填; array
      - "<string>" # string
    logDHCP: true # 选填; boolean
    managed: true # 选填; boolean
    mode: "stateless" # 选填; string; enum=["stateless", "stateful", "both", "ra-only"]
    pidFile: "<string>" # 选填; string
    rapidCommit: true # 选填; boolean
    role: "server" # 选填; string; enum=["server", "transit"]
    selfAddressPolicy: "<string>" # 选填; string
    server: "dnsmasq" # 选填; string; enum=["dnsmasq"]
    sntpServerFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    sntpServers: # 选填; array
      - "<string>" # string
    stickyHoldDays: 0 # 选填; integer; minimum=0
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

[配置示例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
