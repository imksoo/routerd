# DHCPv4Server

[各資源的設定](../resources.md#dhcpv4server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # 選填; object
      end: "<string>" # 選填; string
      leaseTime: "<string>" # 選填; string
      start: "<string>" # 選填; string
    authoritative: true # 選填; boolean
    dns: # 選填; object
      cacheSize: 0 # 選填; integer; minimum=0
      enabled: true # 選填; boolean
      upstreamInterface: "<string>" # 選填; string
      upstreamServers: # 選填; array
        - "<string>" # string
      upstreamSource: "dhcpv4" # 選填; string; enum=["dhcpv4", "static", "system", "none"]
    dnsInterface: "<string>" # 選填; string
    dnsServerFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    dnsServers: # 選填; array
      - "<string>" # string
    dnsSource: "dhcpv4" # 選填; string; enum=["dhcpv4", "static", "self", "none"]
    domain: "<string>" # 選填; string
    domainFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    gateway: "<string>" # 選填; string
    gatewayFrom: # 選填; object
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
    ntpServerFrom: # 選填; array
      - # object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
    ntpServers: # 選填; array
      - "<string>" # string
    options: # 選填; array
      - # object
        code: 1 # 選填; integer; minimum=1; maximum=65535
        name: "<string>" # 選填; string
        value: "<string>" # 選填; string
    profiles: # 選填; array
      - # object
        addressPool: # 必填; object
          end: "<string>" # 選填; string
          leaseTime: "<string>" # 選填; string
          start: "<string>" # 選填; string
        dnsServers: # 選填; array
          - "<string>" # string
        gateway: "<string>" # 選填; string
        name: "<string>" # 必填; string
        netmask: "<string>" # 選填; string
        ntpServers: # 選填; array
          - "<string>" # string
        options: # 選填; array
          - # object
            code: 1 # 選填; integer; minimum=1; maximum=65535
            name: "<string>" # 選填; string
            value: "<string>" # 選填; string
    rangeEnd: "<string>" # 選填; string
    rangeStart: "<string>" # 選填; string
    role: "server" # 選填; string; enum=["server", "transit"]
    router: "<string>" # 選填; string
    routerSource: "interfaceAddress" # 選填; string; enum=["interfaceAddress", "static", "none"]
    scopes: # 選填; array
      - # object
        addressPool: # 必填; object
          end: "<string>" # 選填; string
          leaseTime: "<string>" # 選填; string
          start: "<string>" # 選填; string
        dnsServers: # 選填; array
          - "<string>" # string
        gateway: "<string>" # 選填; string
        match: # 必填; object
          macAddresses: # 選填; array
            - "<string>" # string
          ouiPrefixes: # 選填; array
            - "<string>" # string
        name: "<string>" # 必填; string
        netmask: "<string>" # 選填; string
        ntpServers: # 選填; array
          - "<string>" # string
        options: # 選填; array
          - # object
            code: 1 # 選填; integer; minimum=1; maximum=65535
            name: "<string>" # 選填; string
            value: "<string>" # 選填; string
        profileRef: "<string>" # 選填; string
    server: "dnsmasq" # 選填; string; enum=["dnsmasq", "kea", "dhcpd"]
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
