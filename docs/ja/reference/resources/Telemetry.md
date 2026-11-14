# Telemetry

[リソース別の設定](../resources.md#telemetry) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: observability.routerd.net/v1alpha1` · `metadata.name`

生成する管理サービスにOpenTelemetryの環境変数を設定します。LogSinkや独立したcollectorプロセスを定義するリソースではありません。

## 指定条件と参照関係

- 生成するテレメトリー環境の取得元は、TelemetryまたはOTLP endpointが空でないObservabilityPipelineのいずれか1つです。複数が選ばれると生成エラーになります。リソース名による相互参照ではありません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: observability.routerd.net/v1alpha1
  kind: Telemetry
  metadata:
    name: "<name>"
  spec:
    attributes: # 任意; object
      "<key>": "<string>" # 任意; string
    otlp: # 必須; object
      endpoint: "<string>" # 必須; string
      insecure: true # 任意; boolean
    serviceNamespace: "<string>" # 任意; string
    signals: # 任意; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.otlp` | 必須のOTLP接続設定です。 |
| `spec.otlp.endpoint` | 必須の空白でない送信先。OTEL_EXPORTER_OTLP_ENDPOINTに設定し、既定値はありません。この検証では空白以外であることを確認し、送信先への到達性は確認しません。 |
| `spec.otlp.insecure` | 省略時false。trueならOTEL_EXPORTER_OTLP_INSECURE=trueを出力し、falseならその変数を出力しません。 |
| `spec.serviceNamespace` | OpenTelemetryのサービス名前空間。空白を除去した結果が空ならrouterdです。 |
| `spec.attributes` | リソース属性の対応表。省略時は空です。キー順に並べ、カンマ区切りのkey=valueとしてOTEL_RESOURCE_ATTRIBUTESへ出力します。この生成処理では値をエスケープしません。 |
| `spec.signals` | logs・metrics・tracesを指定し、省略時は3種類すべてです。空でない一覧では、省略されたsignalのexporter変数をnoneにします。受信プロセスを作る指定ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/render/telemetry.go](https://github.com/imksoo/routerd/blob/main/pkg/render/telemetry.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/telemetry-export.yaml)
