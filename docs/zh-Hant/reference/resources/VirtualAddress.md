# VirtualAddress

[各資源的設定](../resources.md#virtualaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VirtualAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 必填; string
    addressFrom: # 選填; object
      field: "<string>" # 選填; string
      optional: true # 選填; boolean
      resource: "<string>" # 必填; string
    externalDNS: true # 選填; boolean
    family: "ipv4" # 必填; string; enum=["ipv4", "ipv6"]
    gratuitousARP: true # 選填; boolean
    hostname: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    mode: "" # 選填; string; enum=["", "static", "vrrp"]
    track: # 選填; array
      - # object
        confirmConsecutiveHealthy: 1 # 選填; integer; minimum=1; maximum=255
        confirmConsecutiveUnhealthy: 1 # 選填; integer; minimum=1; maximum=255
        resource: "<string>" # 必填; string
        unhealthyPenalty: 0 # 選填; integer; minimum=0; maximum=254
    vrrp: # 選填; object
      additionalFailoverVMACs: # 選填; array
        - # object
          interface: "<string>" # 必填; string
          linkLocalAddress: "<string>" # 選填; string
          macAddress: "<string>" # 必填; string
          parentInterface: "<string>" # 必填; string
          withdrawRouterAdvertisement: true # 選填; boolean
      authentication: "<string>" # 選填; string
      authenticationFrom: # 選填; object
        base64: true # 選填; boolean
        env: "<string>" # 選填; string
        file: "<string>" # 選填; string
      failoverVMAC: # 選填; object
        interface: "<string>" # 必填; string
        linkLocalAddress: "<string>" # 選填; string
        macAddress: "<string>" # 必填; string
        parentInterface: "<string>" # 必填; string
        withdrawRouterAdvertisement: true # 選填; boolean
      gracefulActivation: # 選填; object
        readyWhen: # 必填; object
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
        timeout: "<string>" # 選填; string
      peers: # 選填; array
        - "<string>" # string
      preempt: true # 選填; boolean
      priority: 1 # 選填; integer; minimum=1; maximum=254
      virtualRouterID: 1 # 必填; integer; minimum=1; maximum=255
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
