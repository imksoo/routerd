# Telemetry

[各資源的設定](../resources.md#telemetry) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: observability.routerd.net/v1alpha1` · `metadata.name`

為產生的受管服務設定OpenTelemetry環境變數，不定義LogSink或獨立collector程序。

## 指定條件與參照關係

- 產生的遙測環境只可選一個Telemetry或OTLP endpoint非空的ObservabilityPipeline；同時選到多個會產生錯誤，不透過資源名稱互相參照。

## YAML定義

此處列出所有欄位的結構，不是可直接套用的設定範例。請替換佔位值、省略不使用的選項，並選擇相容的組合。顯示值只表示型別；有效預設值及條件列於下方欄位說明。必填標記來自JSON Schema，表示在所屬物件內必須指定；語意驗證可能要求其他欄位或參照。

```yaml
- apiVersion: observability.routerd.net/v1alpha1
  kind: Telemetry
  metadata:
    name: "<name>"
  spec:
    attributes: # 選填; object
      "<key>": "<string>" # 選填; string
    otlp: # 必填; object
      endpoint: "<string>" # 必填; string
      insecure: true # 選填; boolean
    serviceNamespace: "<string>" # 選填; string
    signals: # 選填; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
```

## 欄位意義與預設值

| YAML | 欄位意義與預設值 |
| --- | --- |
| `spec.otlp` | 必填OTLP連線設定。 |
| `spec.otlp.endpoint` | 必填且不可空白的匯出端點，寫入OTEL_EXPORTER_OTLP_ENDPOINT，無預設值。驗證器只檢查非空白，不確認可達性。 |
| `spec.otlp.insecure` | 預設false；true產生OTEL_EXPORTER_OTLP_INSECURE=true，false不產生此變數。 |
| `spec.serviceNamespace` | OpenTelemetry服務命名空間，去除前後空白後為空時預設routerd。 |
| `spec.attributes` | 資源屬性對照，預設空；按鍵排序並以逗號分隔key=value寫入OTEL_RESOURCE_ATTRIBUTES，此產生器不跳脫值。 |
| `spec.signals` | 接受logs、metrics、traces，預設三者全部。非空清單會將省略訊號的exporter變數設為none，不會建立接收程序。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/render/telemetry.go](https://github.com/imksoo/routerd/blob/main/pkg/render/telemetry.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `signals`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/telemetry-export.yaml)
