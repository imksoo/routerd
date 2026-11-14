# EgressRoutePolicy

[各資源的設定](../resources.md#egressroutepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EgressRoutePolicy
  metadata:
    name: "<name>"
  spec:
    candidates: # 必填; array
      - # object
        dependsOn: # 選填; array
          - # object
            equals: "<string>" # 選填; string
            field: "<string>" # 選填; string
            notEmpty: true # 選填; boolean
            optional: true # 選填; boolean
            phase: "<string>" # 選填; string
            resource: "<string>" # 必填; string
        device: "<string>" # 選填; string
        deviceFrom: # 選填; object
          field: "<string>" # 選填; string
          optional: true # 選填; boolean
          resource: "<string>" # 必填; string
        enabled: true # 選填; boolean
        gateway: "<string>" # 選填; string
        gatewayFrom: # 選填; object
          field: "<string>" # 選填; string
          optional: true # 選填; boolean
          resource: "<string>" # 必填; string
        gatewaySource: "" # 選填; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
        healthCheck: "<string>" # 選填; string
        interface: "<string>" # 選填; string
        mark: 0 # 選填; integer; minimum=0; maximum=4294967295
        metric: 0 # 選填; integer; minimum=0
        name: "<string>" # 選填; string
        preferredSource: "" # 選填; string; enum=["", "interface"]
        priority: 0 # 選填; integer; minimum=0; maximum=32765
        routeMetric: 0 # 選填; integer; minimum=0
        routeTable: 0 # 選填; integer; minimum=0; maximum=4294967295
        source: "<string>" # 選填; string
        table: 0 # 選填; integer; minimum=0; maximum=4294967295
        targets: # 選填; array
          - # object
            gateway: "<string>" # 選填; string
            gatewayFrom: # 選填; object
              field: "<string>" # 選填; string
              optional: true # 選填; boolean
              resource: "<string>" # 必填; string
            gatewaySource: "" # 選填; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
            healthCheck: "<string>" # 選填; string
            interface: "<string>" # 選填; string
            mark: 0 # 選填; integer; minimum=0; maximum=4294967295
            metric: 0 # 選填; integer; minimum=0
            name: "<string>" # 選填; string
            outboundInterface: "<string>" # 選填; string
            priority: 0 # 選填; integer; minimum=0; maximum=32765
            routeMetric: 0 # 選填; integer; minimum=0
            routeTable: 0 # 選填; integer; minimum=0; maximum=4294967295
            table: 0 # 選填; integer; minimum=0; maximum=4294967295
        weight: 0 # 選填; integer; minimum=0
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
    destinationCIDRs: # 選填; array
      - "<string>" # string
    destinationSetRefs: # 選填; array
      - "<string>" # string
    excludeDestinationCIDRs: # 選填; array
      - "<string>" # string
    excludeDestinationSetRefs: # 選填; array
      - "<string>" # string
    family: "ipv4" # 選填; string; enum=["ipv4", "ipv6"]
    hashFields: # 選填; array
      - "<string>" # string
    hostTraffic: true # 選填; boolean
    hysteresis: "<string>" # 選填; string
    mode: "priority" # 選填; string; enum=["priority", "mark", "hash"]
    selection: "highest-weight-ready" # 選填; string; enum=["highest-weight-ready", "weighted-ecmp"]
    sourceCIDRs: # 選填; array
      - "<string>" # string
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

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
