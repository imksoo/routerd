# Telemetry

[各資源的設定](../resources.md#telemetry) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: observability.routerd.net/v1alpha1` · `metadata.name`

為產生的受管服務設定OpenTelemetry環境變數，不定義LogSink或獨立collector程序。

## 指定條件與參照關係

- 產生的遙測環境只可選一個Telemetry或OTLP endpoint非空的ObservabilityPipeline；同時選到多個會產生錯誤，不透過資源名稱互相參照。

## 設定語法

這是說明用語法，不是可直接複製的YAML。未加括號的欄位在父項目內必填；`[ ... ]`表示依條件可省略，`<型別>`為替換值，`(a | b)`表示選項。選填父項目內仍可能有必填子項目。條件必填、互斥與實際預設值請見下表；一般YAML請使用連結範例。

```text
- apiVersion: observability.routerd.net/v1alpha1
  kind: Telemetry
  metadata:
    name: "<name>"
  spec:
    [ attributes:
        [ <name>: <string> ]
    ]
    otlp:
      endpoint: <string>
      [ insecure: <boolean> ]
    [ serviceNamespace: <string> ]
    [ signals:
        - ("logs" | "metrics" | "traces")
    ]
```

## 欄位意義與預設值

選填父項目的必填子項目僅在使用父項目時需要。其他具體條件列於說明；`[ ... ]`選項仍可能需要擇一。

| YAML | 指定要求 | 型別與限制 | 欄位意義與預設值 |
| --- | --- | --- | --- |
| `spec.otlp` | **必填** | object | 必填OTLP連線設定。 |
| `spec.otlp.endpoint` | **必填** | string | 必填且不可空白的匯出端點，寫入OTEL_EXPORTER_OTLP_ENDPOINT，無預設值。驗證器只檢查非空白，不確認可達性。 |
| `spec.otlp.insecure` | *選填* | boolean | 預設false；true產生OTEL_EXPORTER_OTLP_INSECURE=true，false不產生此變數。 |
| `spec.serviceNamespace` | *選填* | string | OpenTelemetry服務命名空間，去除前後空白後為空時預設routerd。 |
| `spec.attributes` | *選填* | object | 資源屬性對照，預設空；按鍵排序並以逗號分隔key=value寫入OTEL_RESOURCE_ATTRIBUTES，此產生器不跳脫值。 |
| `spec.signals` | *選填* | array | 接受logs、metrics、traces，預設三者全部。非空清單會將省略訊號的exporter變數設為none，不會建立接收程序。 |

實作: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/render/telemetry.go](https://github.com/imksoo/routerd/blob/main/pkg/render/telemetry.go)

## 可參照的狀態欄位

一般狀態參照驗證接受此 Kind 的下列欄位名稱。列於允許清單不保證目前有值，請檢查實際狀態。這些是輸出，不是 YAML spec 設定。

`phase`, `signals`

[設定範例](https://github.com/imksoo/routerd/blob/main/examples/telemetry-export.yaml)
