# FederationSLO

[各資源的設定](../resources.md#federationslo) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

設定 routerctl doctor 診斷聯邦配送與訂閱時使用的每群組門檻，不改變配送重試或 Plugin 執行策略。

## 指定條件與參照關係

- groupRef 必填且須為既有 EventGroup 名稱。每個群組最多一個 FederationSLO。所有數值須非負；套用零值預設後，有效 lagWarnSeconds 必須嚴格小於有效 lagFailSeconds。
- 延遲達到門檻（>=）即 WARN/FAIL；訂閱數量嚴格超過允許最大值才 WARN/FAIL。即將到期使用剩餘 TTL 嚴格小於門檻；待送或失敗配送可使該診斷由 WARN 升為 FAIL。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: FederationSLO
  metadata:
    name: "<name>"
  spec:
    delivery: # 選填; object
      expiresSoonSeconds: 0 # 選填; integer; minimum=0
      lagFailSeconds: 0 # 選填; integer; minimum=0
      lagWarnSeconds: 0 # 選填; integer; minimum=0
    groupRef: "<string>" # 必填; string
    subscription: # 選填; object
      maxFailedRuns: 0 # 選填; integer; minimum=0
      maxPendingRuns: 0 # 選填; integer; minimum=0
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.groupRef` | 必填單純 EventGroup metadata.name，不是 EventGroup/name。驗證存在性與每群組唯一性。 |
| `spec.delivery` | 選用的配送診斷門檻，省略欄位使用下列預設。不是傳輸逾時或重試設定。 |
| `spec.delivery.lagWarnSeconds` | 非負秒數，省略或 0 為 60。最大測量配送延遲 >= 此值時為 WARN，除非已達 FAIL 門檻。 |
| `spec.delivery.lagFailSeconds` | 非負秒數，省略或 0 為 180。最大測量配送延遲 >= 此值時為 FAIL，須大於有效 WARN 門檻。 |
| `spec.delivery.expiresSoonSeconds` | 非負秒數，省略或 0 為 120。事件剩餘 TTL 低於此值視為即將到期。0 無法停用檢查，無期限事件另行處理。 |
| `spec.subscription` | 選用的訂閱執行診斷容許數，預設 pending/failed 容許皆為 0。不限制控制器佇列或重試次數。 |
| `spec.subscription.maxPendingRuns` | 非負數量，預設 0。待處理數大於此值時為 WARN，但若失敗數已導致 FAIL 則優先 FAIL。 |
| `spec.subscription.maxFailedRuns` | 非負數量，預設 0。失敗數大於此值時為 FAIL，不改變訂閱最多三次的嘗試策略。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [cmd/routerctl/doctor_federation.go](https://github.com/imksoo/routerd/blob/main/cmd/routerctl/doctor_federation.go)
