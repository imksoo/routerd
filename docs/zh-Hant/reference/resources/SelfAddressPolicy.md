# SelfAddressPolicy

[各資源的設定](../resources.md#selfaddresspolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

描述供支援的靜態 DHCPv6 轉譯使用的有序自身位址選擇策略，不會指派介面位址。

## 指定條件與參照關係

- 目前即時 DHCPv6 LAN 產生不使用 DHCPv6Server.selfAddressPolicy 或 dnsSource: self。策略可影響獨立靜態轉譯器，但宣告不會讓執行中的伺服器公告自身 DNS，即時路徑請用明確 dnsServers/dnsServerFrom。
- 參照為已宣告 Interface/IPv6DelegatedAddress 裸名，即使來源分支不使用仍驗證存在。本資源無 when/dependsOn。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: SelfAddressPolicy
  metadata:
    name: "<name>"
  spec:
    addressFamily: ("ipv4" | "ipv6")
    candidates:
      -
        [ address: <string> ]
        [ addressSuffix: <string> ]
        [ delegatedAddress: <string> ]
        [ interface: <string> ]
        [ matchSuffix: <string> ]
        [ ordinal: <integer> ]
        source: ("delegatedAddress" | "interfaceAddress" | "static")
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.addressFamily` | **必填** | string; enum=["ipv4", "ipv6"] | 必填 ipv4 或 ipv6，驗證接受兩者；目前選擇使用者為舊 DHCPv6 靜態轉譯器，ipv4 不會提供獨立即時位址選擇服務。 |
| `spec.candidates` | **必填** | array | 必填非空有序候選清單，支援的靜態轉譯器依序選第一個成功值，全部失敗則報錯；明確參照的策略不自動補候選。 |
| `spec.candidates[].source` | **必填** | string; enum=["delegatedAddress", "interfaceAddress", "static"] | 必填 delegatedAddress/interfaceAddress/static。delegatedAddress 僅限 IPv6，禁止 address/interface；interfaceAddress 需 interface，static 需符合 addressFamily 的 address。後兩者多餘欄位通常不拒絕。 |
| `spec.candidates[].interface` | *選填* | string | interfaceAddress 必填的已宣告邏輯介面，delegatedAddress 禁用；即使所選來源不使用，提供時仍驗證存在。 |
| `spec.candidates[].delegatedAddress` | *選填* | string | delegatedAddress 來源必填的 IPv6DelegatedAddress 裸名，資源須存在；支援的轉譯器由該資源介面觀測前綴衍生，不是通用狀態 field。 |
| `spec.candidates[].address` | *選填* | string | static 來源必填且符合 addressFamily 的純位址，delegatedAddress 禁用，不會自動指派到 OS。 |
| `spec.candidates[].addressSuffix` | *選填* | string | delegatedAddress 的選用 IPv6 後綴，省略繼承參照資源 addressSuffix；其他來源不使用此值選擇。 |
| `spec.candidates[].matchSuffix` | *選填* | string | interfaceAddress 的選用 IPv6 模式，各非零位元組須相符，零位元組不限制，並非傳統連續後綴遮罩。設定後選第一個符合的全域 IPv6，忽略 ordinal。 |
| `spec.candidates[].ordinal` | *選填* | integer; minimum=1 | matchSuffix 空值時的 interfaceAddress 一起算索引，省略或 0 為 1，結構描述下限 1，負值驗證拒絕。順序依觀測清單，不保證固定身分；超界則嘗試下一候選。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `address`, `source`
