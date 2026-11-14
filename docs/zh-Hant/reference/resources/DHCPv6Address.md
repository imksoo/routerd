# DHCPv6Address

[各資源的設定](../resources.md#dhcpv6address) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

宣告主機DHCPv6 client取得的IPv6位址，與DHCPv6PrefixDelegation不同，不分配LAN前綴。

## 指定條件與參照關係

- 目前用於宣告驗證/索引、觀察與主機網路接管判斷，不產生專屬DHCPv6Address daemon。計畫成功不代表主機client已設定完成。
- 同介面外部PD client須與DHCPv6Address的client選擇相符。設定索引將省略視為networkd，目前observe/plan卻顯示dhcpcd；省略行為不一致，請明確指定client。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Address
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # 選填; string
    interface: "<string>" # 必填; string
    required: true # 選填; boolean
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.interface` | 必填Router介面索引中的邏輯名稱，不加Interface/，必須在宣告設定存在，無預設值。 |
| `spec.client` | 主機DHCPv6 client/程式選擇，解碼預設空。接管判斷識別networkd，其他明確名稱會停用該介面的networkd DHCPv6。驗證器不限制已知列舉，不是資源名稱參照。 |
| `spec.required` | 預設false，可解碼但目前位址觀察/接管處理不使用，不增加等待位址要求。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `interface`, `address`
