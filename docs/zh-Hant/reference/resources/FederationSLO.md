# FederationSLO

[各資源的設定](../resources.md#federationslo) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

設定 routerctl doctor 診斷聯邦配送與訂閱時使用的每群組門檻，不改變配送重試或 Plugin 執行策略。

## 指定條件與參照關係

- groupRef 必填且須為既有 EventGroup 名稱。每個群組最多一個 FederationSLO。所有數值須非負；套用零值預設後，有效 lagWarnSeconds 必須嚴格小於有效 lagFailSeconds。
- 延遲達到門檻（>=）即 WARN/FAIL；訂閱數量嚴格超過允許最大值才 WARN/FAIL。即將到期使用剩餘 TTL 嚴格小於門檻；待送或失敗配送可使該診斷由 WARN 升為 FAIL。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: federation.routerd.net/v1alpha1
  kind: FederationSLO
  metadata:
    name: "<name>"
  spec:
    [ delivery:
        [ expiresSoonSeconds: <integer> ]
        [ lagFailSeconds: <integer> ]
        [ lagWarnSeconds: <integer> ]
    ]
    groupRef: <string>
    [ subscription:
        [ maxFailedRuns: <integer> ]
        [ maxPendingRuns: <integer> ]
    ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.groupRef` | **必填** | string | 必填單純 EventGroup metadata.name，不是 EventGroup/name。驗證存在性與每群組唯一性。 |
| `spec.delivery` | *選填* | object | 選用的配送診斷門檻，省略欄位使用下列預設。不是傳輸逾時或重試設定。 |
| `spec.delivery.lagWarnSeconds` | *選填* | integer; minimum=0 | 非負秒數，省略或 0 為 60。最大測量配送延遲 >= 此值時為 WARN，除非已達 FAIL 門檻。 |
| `spec.delivery.lagFailSeconds` | *選填* | integer; minimum=0 | 非負秒數，省略或 0 為 180。最大測量配送延遲 >= 此值時為 FAIL，須大於有效 WARN 門檻。 |
| `spec.delivery.expiresSoonSeconds` | *選填* | integer; minimum=0 | 非負秒數，省略或 0 為 120。事件剩餘 TTL 低於此值視為即將到期。0 無法停用檢查，無期限事件另行處理。 |
| `spec.subscription` | *選填* | object | 選用的訂閱執行診斷容許數，預設 pending/failed 容許皆為 0。不限制控制器佇列或重試次數。 |
| `spec.subscription.maxPendingRuns` | *選填* | integer; minimum=0 | 非負數量，預設 0。待處理數大於此值時為 WARN，但若失敗數已導致 FAIL 則優先 FAIL。 |
| `spec.subscription.maxFailedRuns` | *選填* | integer; minimum=0 | 非負數量，預設 0。失敗數大於此值時為 FAIL，不改變訂閱最多三次的嘗試策略。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [cmd/routerctl/doctor_federation.go](https://github.com/imksoo/routerd/blob/main/cmd/routerctl/doctor_federation.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

此 Kind 未登錄一般狀態參照的允許欄位。
