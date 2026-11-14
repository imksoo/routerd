# Sysctl

[各資源的設定](../resources.md#sysctl) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

協調一項核心sysctl，並可寫入持久sysctl.d檔案；沒有指向其他routerd資源的參照欄位。

## 指定條件與參照關係

- runtime與persistent皆false時記為Skipped/RuntimeDisabled，不套用值；兩者可以同時為true。
- 同一Router不可重複Sysctl鍵。Linux的NAT44SessionSync使用conntrackd時，另須明確宣告net.netfilter.nf_conntrack_tcp_be_liberal，啟用runtime、optional=false，且value與有效expectedValue皆為1。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: Sysctl
  metadata:
    name: "<name>"
  spec:
    compare: "" # 選填; string; enum=["", "exact", "atLeast"]
    expectedValue: "<string>" # 選填; string
    key: "<string>" # 必填; string
    optional: true # 選填; boolean
    persistent: true # 選填; boolean
    runtime: true # 選填; boolean
    value: "<string>" # 必填; string
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.key` | 必填核心鍵，例如net.ipv4.ip_forward，無預設值；禁止空格、tab、換行及/，不是資源參照。 |
| `spec.value` | 傳給sysctl -w的必填非空字串，持久化時也使用此值。YAML中看似數字的值須加引號，無預設值。 |
| `spec.expectedValue` | 用於執行期偏差比較的預期值，預設為value，可處理寫入值與觀測表示不同的情況。拒絕僅空白的值。 |
| `spec.compare` | 預設exact，將空白正規化後以字串比較。atLeast逐一比較帶正負號整數，要求元素數相同且每個目前值不小於預期值。 |
| `spec.runtime` | 是否套用至目前核心，預設true；false時仍可在persistent=true下寫入持久設定。 |
| `spec.persistent` | 將key=value寫入routerd的sysctl.d檔案，預設false，與runtime獨立。 |
| `spec.optional` | 預設false。true時讀取失敗記為Skipped/ReadFailedOptional並略過此項後續套用；不會忽略任意寫入或持久化錯誤。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/sysctl.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sysctl.go) · [pkg/sysctlprofile/compare.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/compare.go) · [pkg/sysctlprofile/persistent.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/persistent.go)
