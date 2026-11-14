# FirewallEventLog

[各資源的設定](../resources.md#firewalleventlog) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallEventLog
  metadata:
    name: "<name>"
  spec:
    enabled: true # 選填; boolean
    events: # 選填; array
      - "deny" # string; enum=["deny", "allow", "rateLimit", "connLimit"]
    fromZones: # 選填; array
      - "<string>" # string
    log: # 選填; object
      acceptSampleRate: 0 # 選填; integer; minimum=0
      copyRange: 0 # 選填; integer; minimum=0
      dropSampleRate: 0 # 選填; integer; minimum=0
      reject: true # 選填; boolean
    nflogGroup: 0 # 選填; integer; minimum=0; maximum=65535
    path: "<string>" # 選填; string
    retention: "<string>" # 選填; string
    rules: # 選填; array
      - "<string>" # string
    sampleRate: 0 # 選填; integer; minimum=0
    sinks: # 選填; array
      - "<string>" # string
    toZones: # 選填; array
      - "<string>" # string
```
