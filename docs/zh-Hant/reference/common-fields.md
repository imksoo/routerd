# 共用參照與條件欄位

## 資源識別與值來源

metadata.name是同一種類內的資源名稱。值來源resource使用Kind/名稱，不是OS介面名稱或URL。一般狀態參照驗證要求目標在同一Router中宣告，且目標種類提供指定欄位。optional: true也不會略過這些檢查。

```yaml
dnsServerFrom:
  - resource: IPv4StaticAddress/lan-address
    field: address
```

| 欄位 | 意義、預設值與條件 |
| --- | --- |
| resource | 來源Kind/名稱。使用來源或指定field時必須提供，不支援萬用字元。 |
| field | 要讀取的address、phase、zone等欄位，須由目標種類提供。底層讀取器雖可預設phase，一般設定驗證仍要求在指定resource時明確提供field，請勿省略。 |
| optional | 預設false。讓支援此選項的使用端容許執行期值尚未解析，不會允許參照設定中不存在的資源。缺值行為請見各資源說明。 |

使用ValuesFromStoreOrRouter的讀取處理，會先從宣告設定取得DNSZone.zone或IPv4StaticAddress.address，再讀取狀態；其他讀取器直接使用狀態。宣告參照不保證動態值已就緒，也不會自行建立執行順序。

## 相依條件

dependsOn是條件清單，全部必須成立；省略或空清單不設相依限制。

```yaml
dependsOn:
  - resource: DHCPv6PrefixDelegation/wan-pd
    phase: Bound
```

| 欄位 | 意義、預設值與條件 |
| --- | --- |
| resource | 相依目標Kind/名稱；設定中須有該資源，且提供所讀欄位。 |
| field | 預設phase；指定phase時不使用此值。 |
| phase | 要求目標phase相符的簡寫；非空時優先於field與equals。 |
| equals | 要求任一回傳值完全相符。省略時要求首值非空白。 |
| notEmpty | 預設false，檢查正規化後首值非空白。目前讀取器已移除空白值，因此此選項通常不增加限制。 |
| optional | 預設false。執行期無值時只有true才通過；不會將已解析但不相符的值視為通過，也不放寬設定參照驗證。 |

## 啟用條件

省略when不設條件。非空條件在每一層必須只用state、all、any其中一種。all與any可巢狀包含條件，但子項不可為空。JSON Schema未列出這兩個遞迴屬性，實作仍接受。

```yaml
when:
  all:
    - state:
        DHCPv6PrefixDelegation/wan-pd.phase:
          equals: Bound
    - state:
        HealthCheck/upstream.phase:
          equals: Healthy
```

state以AND組合所有項目；all以AND、any以OR組合子條件。資源狀態鍵可用Kind/名稱.field、Kind/名稱.status.field或其${...}形式。資源格式的鍵會檢查宣告目標與提供欄位，其他格式當作狀態儲存鍵。

| 狀態條件 | 意義與省略行為 |
| --- | --- |
| exists | 省略則不限制存在性；true要求set，false要求unset。unknown與unset不同。 |
| equals | 非空值要求set與區分大小寫的完全相符；空值不比較。不存在條件請用exists或status。 |
| in | 非空清單要求set且完全符合任一項；空清單不限制。 |
| contains | 非空值要求set且包含區分大小寫的子字串；空值不限制。 |
| status | 選填狀態儲存狀態set、unset或unknown，與資源生命週期phase不同。 |
| for | 選填Go duration，要求觀測狀態經過時間至少達到此值。這不是整個條件的獨立計時器；會驗證語法，但不強制正值。 |
| maxAge | 選填正的Go duration，要求狀態經過時間不超過此值。與for併用時兩項限制皆須成立。 |

同一狀態的多個比較條件必須全部成立。未解析的unknown可能屬於未確定，而非單純false；部分有效設定路徑會保留未確定資源，僅選擇確定結果的路徑則不會。VXLANTunnel拒絕明確接受unknown的條件。等待與撤回行為請見各資源說明。

實作：[參照驗證](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go)、[條件驗證](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)、[值與相依讀取](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go)、[條件評估](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)、[提供欄位](https://github.com/imksoo/routerd/blob/main/pkg/api/provides.go)。
