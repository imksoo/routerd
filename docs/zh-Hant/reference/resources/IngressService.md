# IngressService

[各資源的設定](../resources.md#ingressservice) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: IngressService
  metadata:
    name: "<name>"
  spec:
    backends: # 必填; array
      - # object
        address: "<string>" # 選填; string
        addressFrom: # 選填; object
          field: "<string>" # 選填; string
          optional: true # 選填; boolean
          resource: "<string>" # 必填; string
        name: "<string>" # 選填; string
        port: 1 # 必填; integer; minimum=1; maximum=65535
        weight: 0 # 選填; integer; minimum=0
    externalDNS: true # 選填; boolean
    hairpin: # 選填; object
      enabled: true # 選填; boolean
      interfaces: # 選填; array
        - "<string>" # string
      mode: "" # 選填; string; enum=["", "auto", "manual", "off"]
    healthCheck: # 選填; object
      expectedBody: "<string>" # 選填; string
      expectedStatus: # 選填; array
        - 0 # integer
      healthyThreshold: 1 # 選填; integer; minimum=1
      host: "<string>" # 選填; string
      interval: "<string>" # 選填; string
      path: "<string>" # 選填; string
      protocol: "" # 選填; string; enum=["", "tcp", "http", "https"]
      timeout: "<string>" # 選填; string
      tlsSkipVerify: true # 選填; boolean
      unhealthyThreshold: 1 # 選填; integer; minimum=1
    hostname: "<string>" # 選填; string
    listen: # 必填; object
      address: "<string>" # 選填; string
      addressFrom: # 選填; object
        field: "<string>" # 選填; string
        optional: true # 選填; boolean
        resource: "<string>" # 必填; string
      interface: "<string>" # 必填; string
      port: 1 # 必填; integer; minimum=1; maximum=65535
      protocol: "tcp" # 必填; string; enum=["tcp", "udp"]
    policy: # 選填; object
      onNoHealthyBackends: "" # 選填; string; enum=["", "drop", "reject"]
      selection: "" # 選填; string; enum=["", "failover", "sourceHash", "random"]
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

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/k8s-api-vip-dualstack.yaml)
