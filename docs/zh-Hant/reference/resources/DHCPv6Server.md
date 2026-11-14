# DHCPv6Server

[各資源的設定](../resources.md#dhcpv6server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # 選填; object
      end: "<string>" # 選填; string
      leaseTime: "<string>" # 選填; string
      start: "<string>" # 選填; string
    configPath: "<string>" # 選填; string
    defaultRoute: true # 選填; boolean
    delegatedAddress: "<string>" # 選填; string
    dependsOn: # 選填; array
      - # object
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        notEmpty: true # 選填; boolean
        optional: true # 選填; boolean
        phase: "<string>" # 選填; string
        resource: "<string>" # 必填; string
    dnsServerFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    dnsServers: # 選填; array
      - "<string>" # string
    dnsSource: "self" # 選填; string; enum=["self", "static", "none"]
    domainSearch: # 選填; array
      - "<string>" # string
    domainSearchFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    interface: "<string>" # 選填; string
    leaseFile: "<string>" # 選填; string
    leaseTime: "<string>" # 選填; string
    listenInterfaces: # 選填; array
      - "<string>" # string
    logDHCP: true # 選填; boolean
    managed: true # 選填; boolean
    mode: "stateless" # 選填; string; enum=["stateless", "stateful", "both", "ra-only"]
    pidFile: "<string>" # 選填; string
    rapidCommit: true # 選填; boolean
    role: "server" # 選填; string; enum=["server", "transit"]
    selfAddressPolicy: "<string>" # 選填; string
    server: "dnsmasq" # 選填; string; enum=["dnsmasq"]
    sntpServerFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    sntpServers: # 選填; array
      - "<string>" # string
    stickyHoldDays: 0 # 選填; integer; minimum=0
    when: # 選填; object
      state: # 選填; object
        "<key>": # 選填; object
          contains: "<string>" # 選填; string
          equals: "<string>" # 選填; string
          exists: true # 選填; boolean
          for: "<string>" # 選填; string
          in: # 選填; array
            - "<string>" # string
          maxAge: "<string>" # 選填; string
          status: "set" # 選填; string; enum=["set", "unset", "unknown"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
