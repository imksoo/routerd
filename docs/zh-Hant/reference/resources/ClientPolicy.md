# ClientPolicy

[各資源的設定](../resources.md#clientpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: "<name>"
  spec:
    classification: # 選填; array
      - # object
        ipv4Reservation: "<string>" # 選填; string
        ipv6Addresses: # 選填; array
          - "<string>" # string
        match: # 必填; object
          dhcpFingerprints: # 選填; array
            - "<string>" # string
          hostnamePatterns: # 選填; array
            - "<string>" # string
          macs: # 選填; array
            - "<string>" # string
          ouiPrefixes: # 選填; array
            - "<string>" # string
        mode: "trusted" # 必填; string; enum=["trusted", "guest", "isolated"]
        name: "<string>" # 選填; string
    guestEgressAllow: # 選填; array
      - "<string>" # string
    guestEgressDeny: # 選填; array
      - "<string>" # string
    guestServices: # 選填; array
      - "<string>" # string
    interfaces: # 選填; array
      - "<string>" # string
    isolation: # 選填; object
      lanInternet: "" # 選填; string; enum=["", "allow", "deny"]
      lanLAN: "" # 選填; string; enum=["", "allow", "deny"]
      lanMgmt: "" # 選填; string; enum=["", "allow", "deny"]
      mDNSBroadcast: "" # 選填; string; enum=["", "allow", "deny"]
    macs: # 選填; array
      - "<string>" # string
    mode: "include" # 必填; string; enum=["include", "exclude"]
```

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)
