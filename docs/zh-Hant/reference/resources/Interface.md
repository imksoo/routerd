# Interface

[各資源的設定](../resources.md#interface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

為既有OS介面提供metadata.name邏輯名稱，供其他資源參照，不建立實體介面。

## 指定條件與參照關係

- 相依資源的interface使用metadata.name邏輯名稱，ifname填實際裝置名。managed=false不會抑制已要求的LinkController啟用/MTU變更。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: Interface
  metadata:
    name: "<name>"
  spec:
    adminUp: true # 選填; boolean
    ifname: "<string>" # 必填; string
    managed: true # 必填; boolean
    mtu: 576 # 選填; integer; minimum=576; maximum=9216
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
| `spec.ifname` | 必填既有核心裝置名稱，例如ens18，無預設值；不存在時不建立，而記為Down/InterfaceNotFound。 |
| `spec.managed` | 管理宣告，解碼預設false，但JSON Schema要求明確填寫。影響管理/觀察資訊及周邊處理，不保證adminUp/MTU只觀察不變更。 |
| `spec.adminUp` | 預設false，不要求啟用；true會啟用已存在但關閉的link。false不表示強制關閉。 |
| `spec.mtu` | 預設0保留目前MTU；非零值須為576–9216，不同時套用。 |
| `spec.when` | 啟用條件，省略不設限制；state/all/any、unknown及時間條件請見共用條件參考。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `ifname`, `addresses`, `ipv4Addresses`, `ipv6Addresses`, `primaryIPv4`, `primaryIPv6`, `macAddress`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)
