# IPv4StaticAddress

[各資源的設定](../resources.md#ipv4staticaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

在邏輯介面宣告靜態IPv4位址，由controller協調設定，不是DHCP租約。

## 指定條件與參照關係

- interface參照與address/prefix必填。同邏輯介面及遮罩後網路前綴的兩個靜態資源，即使主機位址不同也拒絕；與相同組合的VirtualAddress重疊也拒絕。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 必填; string
    allowOverlap: true # 選填; boolean
    allowOverlapReason: "<string>" # 選填; string
    exclusive: true # 選填; boolean
    interface: "<string>" # 必填; string
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

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.interface` | 必填Router介面索引中的邏輯名稱，不加Interface/，必須在宣告設定存在，無預設值。 |
| `spec.address` | 必填含前綴長度的IPv4主機位址，例如192.0.2.1/24，無預設值或隱含/32。解析器檢查前綴語法，執行期使用IPv4操作。 |
| `spec.exclusive` | 預設false。目前觀察器警告exclusive清理未實作，執行期位址controller也不讀取此旗標。額外位址的整體協調獨立，false不保證保留手動位址。 |
| `spec.allowOverlap` | 預設false，保留供解碼但目前驗證與位址協調不使用，不會略過重複/衝突檢查。 |
| `spec.allowOverlapReason` | 預設空，僅保留宣告，不授權重疊或改變allowOverlap效果。 |
| `spec.when` | 啟用條件，省略不設限制；state/all/any、unknown及時間條件請見共用條件參考。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/basic-static.yaml)
