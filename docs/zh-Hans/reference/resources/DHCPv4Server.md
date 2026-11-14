# DHCPv4Server

[各资源的配置](../resources.md#dhcpv4server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定义

此处列出所有字段的结构，不是可直接应用的配置示例。请替换占位值、省略不使用的选项，并选择兼容的组合。显示值仅表示类型；有效默认值及条件列于下方字段说明。必填标记来自JSON Schema，表示在所属对象内必须指定；语义验证可能要求其他字段或引用。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # 选填; object
      end: "<string>" # 选填; string
      leaseTime: "<string>" # 选填; string
      start: "<string>" # 选填; string
    authoritative: true # 选填; boolean
    dns: # 选填; object
      cacheSize: 0 # 选填; integer; minimum=0
      enabled: true # 选填; boolean
      upstreamInterface: "<string>" # 选填; string
      upstreamServers: # 选填; array
        - "<string>" # string
      upstreamSource: "dhcpv4" # 选填; string; enum=["dhcpv4", "static", "system", "none"]
    dnsInterface: "<string>" # 选填; string
    dnsServerFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    dnsServers: # 选填; array
      - "<string>" # string
    dnsSource: "dhcpv4" # 选填; string; enum=["dhcpv4", "static", "self", "none"]
    domain: "<string>" # 选填; string
    domainFrom: # 选填; object
      field: "<string>" # 选填; string
      optional: true # 选填; boolean
      resource: "<string>" # 必填; string
    gateway: "<string>" # 选填; string
    gatewayFrom: # 选填; object
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
    ntpServerFrom: # 选填; array
      - # object
        field: "<string>" # 选填; string
        optional: true # 选填; boolean
        resource: "<string>" # 必填; string
    ntpServers: # 选填; array
      - "<string>" # string
    options: # 选填; array
      - # object
        code: 1 # 选填; integer; minimum=1; maximum=65535
        name: "<string>" # 选填; string
        value: "<string>" # 选填; string
    profiles: # 选填; array
      - # object
        addressPool: # 必填; object
          end: "<string>" # 选填; string
          leaseTime: "<string>" # 选填; string
          start: "<string>" # 选填; string
        dnsServers: # 选填; array
          - "<string>" # string
        gateway: "<string>" # 选填; string
        name: "<string>" # 必填; string
        netmask: "<string>" # 选填; string
        ntpServers: # 选填; array
          - "<string>" # string
        options: # 选填; array
          - # object
            code: 1 # 选填; integer; minimum=1; maximum=65535
            name: "<string>" # 选填; string
            value: "<string>" # 选填; string
    rangeEnd: "<string>" # 选填; string
    rangeStart: "<string>" # 选填; string
    role: "server" # 选填; string; enum=["server", "transit"]
    router: "<string>" # 选填; string
    routerSource: "interfaceAddress" # 选填; string; enum=["interfaceAddress", "static", "none"]
    scopes: # 选填; array
      - # object
        addressPool: # 必填; object
          end: "<string>" # 选填; string
          leaseTime: "<string>" # 选填; string
          start: "<string>" # 选填; string
        dnsServers: # 选填; array
          - "<string>" # string
        gateway: "<string>" # 选填; string
        match: # 必填; object
          macAddresses: # 选填; array
            - "<string>" # string
          ouiPrefixes: # 选填; array
            - "<string>" # string
        name: "<string>" # 必填; string
        netmask: "<string>" # 选填; string
        ntpServers: # 选填; array
          - "<string>" # string
        options: # 选填; array
          - # object
            code: 1 # 选填; integer; minimum=1; maximum=65535
            name: "<string>" # 选填; string
            value: "<string>" # 选填; string
        profileRef: "<string>" # 选填; string
    server: "dnsmasq" # 选填; string; enum=["dnsmasq", "kea", "dhcpd"]
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
