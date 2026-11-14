# Sysctl

[各資源的設定](../resources.md#sysctl) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

協調一項核心sysctl，並可寫入持久sysctl.d檔案；沒有指向其他routerd資源的參照欄位。

## 指定條件與參照關係

- runtime與persistent皆false時記為Skipped/RuntimeDisabled，不套用值；兩者可以同時為true。
- 同一Router不可重複Sysctl鍵。Linux的NAT44SessionSync使用conntrackd時，另須明確宣告net.netfilter.nf_conntrack_tcp_be_liberal，啟用runtime、optional=false，且value與有效expectedValue皆為1。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: Sysctl
  metadata:
    name: "<name>"
  spec:
    [ compare: ("" | "exact" | "atLeast") ]
    [ expectedValue: <string> ]
    key: <string>
    [ optional: <boolean> ]
    [ persistent: <boolean> ]
    [ runtime: <boolean> ]
    value: <string>
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.key` | **必填** | string | 必填核心鍵，例如net.ipv4.ip_forward，無預設值；禁止空格、tab、換行及/，不是資源參照。 |
| `spec.value` | **必填** | string | 傳給sysctl -w的必填非空字串，持久化時也使用此值。YAML中看似數字的值須加引號，無預設值。 |
| `spec.expectedValue` | *選填* | string | 用於執行期偏差比較的預期值，預設為value，可處理寫入值與觀測表示不同的情況。拒絕僅空白的值。 |
| `spec.compare` | *選填* | string; enum=["", "exact", "atLeast"] | 預設exact，將空白正規化後以字串比較。atLeast逐一比較帶正負號整數，要求元素數相同且每個目前值不小於預期值。 |
| `spec.runtime` | *選填* | boolean | 是否套用至目前核心，預設true；false時仍可在persistent=true下寫入持久設定。 |
| `spec.persistent` | *選填* | boolean | 將key=value寫入routerd的sysctl.d檔案，預設false，與runtime獨立。 |
| `spec.optional` | *選填* | boolean | 預設false。true時讀取失敗記為Skipped/ReadFailedOptional並略過此項後續套用；不會忽略任意寫入或持久化錯誤。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/sysctl.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sysctl.go) · [pkg/sysctlprofile/compare.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/compare.go) · [pkg/sysctlprofile/persistent.go](https://github.com/imksoo/routerd/blob/main/pkg/sysctlprofile/persistent.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `key`, `value`, `dryRun`
