# DHCPv6Information

[各資源的設定](../resources.md#dhcpv6information) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

從使用同介面的DHCPv6-PD daemon取得AFTR、DNS、SNTP與搜尋網域資訊，不啟動獨立Information-request client。

## 指定條件與參照關係

- 同一邏輯interface的DHCPv6PrefixDelegation須先達Bound，才讀取其daemon socket；無對應PD資源就沒有來源。Info-request於bootstrap/轉成Bound時觸發，不是獨立租約生命週期。
- 資訊controller使用宣告Router而非when篩選後的有效檢視，也不評估dependsOn。Ready表示讀取daemon狀態，不證明收到新資訊回覆。ready_when會被拒絕，須注意以下執行限制。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Information
  metadata:
    name: "<name>"
  spec:
    dependsOn: # 選填; array
      - # object
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        notEmpty: true # 選填; boolean
        optional: true # 選填; boolean
        phase: "<string>" # 選填; string
        resource: "<string>" # 必填; string
    interface: "<string>" # 必填; string
    request: # 選填; array
      - "<string>" # string
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
| `spec.interface` | 必填介面識別，與DHCPv6PrefixDelegation.spec.interface完全相符，無預設值；兩個宣告使用相同邏輯介面名稱。 |
| `spec.request` | 請求選項宣告預設空，目前controller不使用；daemon InfoRequest固定以ORO要求AFTR名稱、DNS、SNTP與搜尋網域。 |
| `spec.when` | 條件宣告預設未指定，依共用when規則驗證，但目前資訊controller不用它篩選自身工作，不能作為資訊請求停止開關。 |
| `spec.dependsOn` | 相依宣告預設空；驗證目標/field，但此controller不在讀取或要求資訊前強制這些條件。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/pdclient/client.go](https://github.com/imksoo/routerd/blob/main/pkg/pdclient/client.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `source`, `aftrName`, `dnsServers`, `sntpServers`, `domainSearch`
