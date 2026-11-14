# SelfAddressPolicy

[各資源的設定](../resources.md#selfaddresspolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

描述供支援的靜態 DHCPv6 轉譯使用的有序自身位址選擇策略，不會指派介面位址。

## 指定條件與參照關係

- 目前即時 DHCPv6 LAN 產生不使用 DHCPv6Server.selfAddressPolicy 或 dnsSource: self。策略可影響獨立靜態轉譯器，但宣告不會讓執行中的伺服器公告自身 DNS，即時路徑請用明確 dnsServers/dnsServerFrom。
- 參照為已宣告 Interface/IPv6DelegatedAddress 裸名，即使來源分支不使用仍驗證存在。本資源無 when/dependsOn。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: SelfAddressPolicy
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # 必填; string; enum=["ipv4", "ipv6"]
    candidates: # 必填; array
      - # object
        address: "<string>" # 選填; string
        addressSuffix: "<string>" # 選填; string
        delegatedAddress: "<string>" # 選填; string
        interface: "<string>" # 選填; string
        matchSuffix: "<string>" # 選填; string
        ordinal: 1 # 選填; integer; minimum=1
        source: "delegatedAddress" # 必填; string; enum=["delegatedAddress", "interfaceAddress", "static"]
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.addressFamily` | 必填 ipv4 或 ipv6，驗證接受兩者；目前選擇使用者為舊 DHCPv6 靜態轉譯器，ipv4 不會提供獨立即時位址選擇服務。 |
| `spec.candidates` | 必填非空有序候選清單，支援的靜態轉譯器依序選第一個成功值，全部失敗則報錯；明確參照的策略不自動補候選。 |
| `spec.candidates[].source` | 必填 delegatedAddress/interfaceAddress/static。delegatedAddress 僅限 IPv6，禁止 address/interface；interfaceAddress 需 interface，static 需符合 addressFamily 的 address。後兩者多餘欄位通常不拒絕。 |
| `spec.candidates[].interface` | interfaceAddress 必填的已宣告邏輯介面，delegatedAddress 禁用；即使所選來源不使用，提供時仍驗證存在。 |
| `spec.candidates[].delegatedAddress` | delegatedAddress 來源必填的 IPv6DelegatedAddress 裸名，資源須存在；支援的轉譯器由該資源介面觀測前綴衍生，不是通用狀態 field。 |
| `spec.candidates[].address` | static 來源必填且符合 addressFamily 的純位址，delegatedAddress 禁用，不會自動指派到 OS。 |
| `spec.candidates[].addressSuffix` | delegatedAddress 的選用 IPv6 後綴，省略繼承參照資源 addressSuffix；其他來源不使用此值選擇。 |
| `spec.candidates[].matchSuffix` | interfaceAddress 的選用 IPv6 模式，各非零位元組須相符，零位元組不限制，並非傳統連續後綴遮罩。設定後選第一個符合的全域 IPv6，忽略 ordinal。 |
| `spec.candidates[].ordinal` | matchSuffix 空值時的 interfaceAddress 一起算索引，省略或 0 為 1，結構描述下限 1，負值驗證拒絕。順序依觀測清單，不保證固定身分；超界則嘗試下一候選。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `address`, `source`
