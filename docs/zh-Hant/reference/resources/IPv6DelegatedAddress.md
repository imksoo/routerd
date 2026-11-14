# IPv6DelegatedAddress

[各資源的設定](../resources.md#ipv6delegatedaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

從 DHCPv6PrefixDelegation 前綴衍生並安裝本機 IPv6 位址。

## 指定條件與參照關係

- prefixDelegation 須為既有 DHCPv6PrefixDelegation 裸名，interface 亦須存在於 Router 索引。一般主用安裝需要可用 Bound 前綴、就緒鏈路及成立的相依條件。HA VMAC 預置可在主用取得前使用新鮮同步快照。
- 衍生前綴長為 max(64,委派長度)。subnetID 以十六進位移至該邊界後與基底 OR，suffix 僅取剩餘主機位元再 OR。目前不檢查 subnetID 是否落在委派區塊內，請選有效範圍。
- Linux 使用 ip address、FreeBSD 使用 ifconfig。主用套用後讀回位址才公布 Applied，暫定或 DAD 中可維持 Pending。前綴變更可將舊位址保留為已棄用直到有效期限，/128 作主機位址而非直連子網。
- sendRA/announce 不會讓目前位址控制器啟動 RA 服務，須另設 IPv6RouterAdvertisement 或 DHCPv6Server。已移除的 prefixSource/ready_when 會被拒絕。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: "<name>"
  spec:
    addressSuffix: "<string>" # 必填; string
    announce: true # 選填; boolean
    dependsOn: # 選填; array
      - # object
        equals: "<string>" # 選填; string
        field: "<string>" # 選填; string
        notEmpty: true # 選填; boolean
        optional: true # 選填; boolean
        phase: "<string>" # 選填; string
        resource: "<string>" # 必填; string
    interface: "<string>" # 必填; string
    prefixDelegation: "<string>" # 必填; string
    prefixLength: 1 # 選填; integer; minimum=1; maximum=128
    sendRA: true # 選填; boolean
    subnetID: "<string>" # 選填; string
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
| `spec.prefixDelegation` | 必填 DHCPv6PrefixDelegation metadata.name 裸名，不是 Kind/name 或狀態運算式，無預設值。 |
| `spec.interface` | 必填已宣告的目標邏輯介面，最終 OS 名稱由 Router 解析。 |
| `spec.subnetID` | 選用十六進位字串，預設 0；"10" 為 0x10 而非十進位十，可加 0x。未嚴格檢查容納範圍，請選委派區塊內編號。 |
| `spec.addressSuffix` | 必填 IPv6 後綴如 ::1，僅使用衍生前綴邊界以下的主機位元，不作完整絕對位址，無自動 ::1 預設。 |
| `spec.prefixLength` | 省略或 0 保留衍生長度，通常 /64。執行時接受相同長度或 128；其他明確長度雖可通過 1–128 結構描述，套用仍失敗。/128 避免建立通常的直連前綴路由。 |
| `spec.sendRA` | 布林值預設 false，部分主機相依或轉送準備路徑使用，但不是即時位址控制器的自動 RA 開關。 |
| `spec.announce` | 布林值預設 false，即時 RA 服務限制同 sendRA，不保證主動鄰居或 RA 公告。 |
| `spec.when` | 選用共用條件，預設無條件；條件為假可經明確清理路徑移除衍生位址。 |
| `spec.dependsOn` | 選用共用 AND 相依清單，預設空。不成立時為 Pending/DependsOnFalse。HA 預置對 PD/VMAC 前提有特定處理，請用實際資源 phase。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/pdclient/derive.go](https://github.com/imksoo/routerd/blob/main/pkg/pdclient/derive.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go)

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
