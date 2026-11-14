# Telemetry

[リソース別の設定](../resources.md#telemetry) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: observability.routerd.net/v1alpha1` · `metadata.name`

生成する管理サービスにOpenTelemetryの環境変数を設定します。LogSinkや独立したcollectorプロセスを定義するリソースではありません。

## 指定条件と参照関係

- 生成するテレメトリー環境の取得元は、TelemetryまたはOTLP endpointが空でないObservabilityPipelineのいずれか1つです。複数が選ばれると生成エラーになります。リソース名による相互参照ではありません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

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

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.otlp` | **必須** | object | 必須のOTLP接続設定です。 |
| `spec.otlp.endpoint` | **必須** | string | 必須の空白でない送信先。OTEL_EXPORTER_OTLP_ENDPOINTに設定し、既定値はありません。この検証では空白以外であることを確認し、送信先への到達性は確認しません。 |
| `spec.otlp.insecure` | *任意* | boolean | 省略時false。trueならOTEL_EXPORTER_OTLP_INSECURE=trueを出力し、falseならその変数を出力しません。 |
| `spec.serviceNamespace` | *任意* | string | OpenTelemetryのサービス名前空間。空白を除去した結果が空ならrouterdです。 |
| `spec.attributes` | *任意* | object | リソース属性の対応表。省略時は空です。キー順に並べ、カンマ区切りのkey=valueとしてOTEL_RESOURCE_ATTRIBUTESへ出力します。この生成処理では値をエスケープしません。 |
| `spec.signals` | *任意* | array | logs・metrics・tracesを指定し、省略時は3種類すべてです。空でない一覧では、省略されたsignalのexporter変数をnoneにします。受信プロセスを作る指定ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/render/telemetry.go](https://github.com/imksoo/routerd/blob/main/pkg/render/telemetry.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `signals`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/telemetry-export.yaml)
