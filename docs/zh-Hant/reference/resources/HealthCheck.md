# HealthCheck

[各資源的設定](../resources.md#healthcheck) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: HealthCheck
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # 選填; string; enum=["ipv4", "ipv6"]
    enabled: true # 選填; boolean
    healthyThreshold: 1 # 選填; integer; minimum=1
    interface: "<string>" # 選填; string
    interval: "<string>" # 選填; string
    port: 0 # 選填; integer; minimum=0; maximum=65535
    protocol: "" # 選填; string; enum=["", "icmp", "tcp", "dns", "http"]
    role: "link" # 選填; string; enum=["link", "next-hop", "internet", "service", "policy"]
    target: "<string>" # 選填; string
    targetSource: "auto" # 選填; string; enum=["auto", "static", "defaultGateway", "dsliteRemote"]
    timeout: "<string>" # 選填; string
    type: "ping" # 選填; string; enum=["ping"]
    unhealthyThreshold: 1 # 選填; integer; minimum=1
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
