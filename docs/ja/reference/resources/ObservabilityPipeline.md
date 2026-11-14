# ObservabilityPipeline

[リソース別の設定](../resources.md#observabilitypipeline) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

管理サービス向けOpenTelemetry環境の宣言と、独立した組込み運用イベントログconsumerをまとめた設定です。

## 指定条件と参照関係

- OTLP環境の選択と組込みログ転送は別経路です。signalsは前者、logs.enabledは後者を制御します。出力先が空でもstdoutには送りません。
- sampling.rate=0は1として扱います。この実装ではKafka出力先とOTLPの証明書ファイル項目は宣言だけで、対応する実行処理にはつながっていません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ObservabilityPipeline
  metadata:
    name: "<name>"
  spec:
    attributes: # 任意; object
      "<key>": "<string>" # 任意; string
    logs: # 任意; object
      enabled: true # 任意; boolean
      sinks: # 任意; array
        - # object
          enabled: true # 任意; boolean
          kafka: # 任意; object
            brokers: # 任意; array
              - "<string>" # string
            topic: "<string>" # 任意; string
          labels: # 任意; object
            "<key>": "<string>" # 任意; string
          loki: # 任意; object
            headers: # 任意; object
              "<key>": "<string>" # 任意; string
            tenant: "<string>" # 任意; string
            url: "<string>" # 任意; string
          minLevel: "debug" # 任意; string; enum=["debug", "info", "warning", "error"]
          name: "<string>" # 任意; string
          syslog: # 任意; object
            address: "<string>" # 任意; string
            facility: "kern" # 任意; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
            network: "" # 任意; string; enum=["", "unix", "unixgram", "tcp", "udp"]
            tag: "<string>" # 任意; string
          type: "stdout" # 必須; string; enum=["stdout", "syslog", "loki", "kafka"]
    otlp: # 任意; object
      endpoint: "<string>" # 任意; string
      headers: # 任意; object
        "<key>": "<string>" # 任意; string
      insecure: true # 任意; boolean
      tls: # 任意; object
        caFile: "<string>" # 任意; string
        certFile: "<string>" # 任意; string
        insecureSkipVerify: true # 任意; boolean
        keyFile: "<string>" # 任意; string
    sampling: # 任意; object
      rate: 0 # 任意; number
    serviceNamespace: "<string>" # 任意; string
    signals: # 任意; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
    when: # 任意; object
      state: # 任意; object
        "<key>": # 任意; object
          contains: "<string>" # 任意; string
          equals: "<string>" # 任意; string
          exists: true # 任意; boolean
          for: "<string>" # 任意; string
          in: # 任意; array
            - "<string>" # string
          maxAge: "<string>" # 任意; string
          status: "set" # 任意; string; enum=["set", "unset", "unknown"]
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.serviceNamespace` | OpenTelemetryのサービス名前空間。空白を除去した結果が空ならrouterdです。 |
| `spec.attributes` | リソース属性の対応表。省略時は空です。キー順に並べ、カンマ区切りのkey=valueとしてOTEL_RESOURCE_ATTRIBUTESへ出力します。この生成処理では値をエスケープしません。 |
| `spec.otlp` | 生成するサービス向けの任意のOTLP環境設定。endpointが空白でなければテレメトリー環境の取得元として選びます。組込みログ出力先は別に動作します。 |
| `spec.otlp.endpoint` | 送信先。省略時は空です。空白でない値はURIとして解析できる必要があり、OTEL_EXPORTER_OTLP_ENDPOINTへ出力します。Telemetryまたはendpoint付きpipelineが複数あれば環境生成に失敗します。 |
| `spec.otlp.insecure` | 省略時false。trueでOTEL_EXPORTER_OTLP_INSECURE=trueを出力します。tls.insecureSkipVerify=trueでも同じinsecure変数を出力します。 |
| `spec.otlp.headers` | exporterの要求ヘッダー。省略時は空です。キー順のカンマ区切りkey=valueとしてOTEL_EXPORTER_OTLP_HEADERSへ出力し、値はエスケープしません。Lokiのヘッダーとは別です。 |
| `spec.otlp.tls` | 読み込むTLSオプションです。現行の環境生成に影響するのはinsecureSkipVerifyだけで、3つのファイル指定はexporter設定に接続されていません。 |
| `spec.otlp.tls.caFile` | CAファイルの宣言。省略時は空です。現行の生成処理と組込みexporterは読み取らず、指定しても信頼する証明書の設定には反映しません。 |
| `spec.otlp.tls.certFile` | クライアント証明書の宣言。省略時は空で、現行のOTLP設定経路では使いません。 |
| `spec.otlp.tls.keyFile` | クライアント鍵の宣言。省略時は空で、現行では使いません。ここでcertFileとkeyFileを組で指定しても相互TLSは有効になりません。 |
| `spec.otlp.tls.insecureSkipVerify` | 省略時false。現行処理ではtrueをOTEL_EXPORTER_OTLP_INSECURE=trueへ変換します。証明書検証だけを省くTLS clientの設定ではありません。 |
| `spec.signals` | 生成するOpenTelemetry環境では省略時logs/metrics/tracesすべてです。空でない一覧から除いたsignalはexporter=noneにします。組込みログconsumerはこの一覧を使わず、logs.enabledで制御します。 |
| `spec.sampling` | 組込みイベントログの抽出設定。生成するOTLP環境とは独立しています。 |
| `spec.sampling.rate` | 0〜1を指定します。省略/0は1になり、対象イベントをすべて送ります。小数値では連番の1000周期で抽出し、無作為抽出ではありません。consumerを止める場合はlogs.enabled=falseを使います。 |
| `spec.logs` | DBに保存されたイベントログを転送する組込み処理の設定。OTLP環境とは別です。 |
| `spec.logs.enabled` | 省略時true。falseで組込みログconsumerを止めますが、生成サービスのOTLP変数は無効にしません。 |
| `spec.logs.sinks` | 出力先一覧。省略時は空で、組込みの出力先はありません。stdoutを暗黙には追加しません。Kafkaはenabledでもexporterが除外します。 |
| `spec.logs.sinks[].name` | 任意の出力先ラベル。省略時は空です。LogSinkリソースの参照ではなく、現行exporterは選択に使いません。 |
| `spec.logs.sinks[].type` | stdout・syslog・loki・kafkaの指定が必須です。kafkaは宣言を受理しますが実行しません。選択した種類の設定だけを使います。 |
| `spec.logs.sinks[].enabled` | 省略時true。falseならこの出力先を除外します。kafkaはこの値とは別に除外します。 |
| `spec.logs.sinks[].minLevel` | 最低重要度。省略時infoでdebug/info/warning/errorを受理し、sampling後に判定します。 |
| `spec.logs.sinks[].labels` | 追加のLoki streamラベル。省略時は空です。キーを正規化してから同名の全体属性・組込みラベルを上書きします。stdoutやsyslogでは使いません。 |
| `spec.logs.sinks[].loki` | type=lokiで使う設定です。 |
| `spec.logs.sinks[].loki.url` | lokiでは必須で既定値はありません。Lokiのpush先URL全体を指定し、exporterはpush用のパスを付け足しません。成功扱いはHTTP 2xxだけです。 |
| `spec.logs.sinks[].loki.tenant` | 任意のテナントID。省略時は空で、空でなければX-Scope-OrgIDを設定します。 |
| `spec.logs.sinks[].loki.headers` | 追加HTTPヘッダー。省略時は空です。Content-Typeとテナントのヘッダーを含め、同名の値を上書きします。 |
| `spec.logs.sinks[].syslog` | type=syslogで使う設定です。 |
| `spec.logs.sinks[].syslog.network` | 転送方式。unix、unixgram、tcp、udpを指定します。省略時はGoのsyslog実装がローカルソケットを探索します。 |
| `spec.logs.sinks[].syslog.address` | ソケットパスまたはhost:port。省略時は空です。転送方式を明示した場合は対応する宛先も指定します。この組合せは設定検証では必須化されず、接続エラーは実際の出力時に生じます。 |
| `spec.logs.sinks[].syslog.tag` | syslogのタグ。省略時routerd。リソース参照ではありません。 |
| `spec.logs.sinks[].syslog.facility` | 省略時local6。このexporterが対応づけるのはlocal0〜local7で、それ以外を指定してもlocal6を使います。独立したLogSinkのsyslog実装とは異なります。 |
| `spec.logs.sinks[].kafka` | Kafkaの意図を記述する設定。検証は行いますが、組込みexporterではこの出力先を除外します。 |
| `spec.logs.sinks[].kafka.brokers` | type=kafkaでは空白でないbrokerが1件以上必要です。現行exporterは接続しません。 |
| `spec.logs.sinks[].kafka.topic` | type=kafkaでは空白でないtopicが必須です。EventRuleのtopicやリソース参照ではなく、現行ではKafka送信もしません。 |
| `spec.when` | 有効化条件。省略時は条件を設けません。state/all/any、unknown、経過時間の条件は共通の条件指定を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/render/telemetry.go](https://github.com/imksoo/routerd/blob/main/pkg/render/telemetry.go) · [pkg/observabilitypipeline/exporter.go](https://github.com/imksoo/routerd/blob/main/pkg/observabilitypipeline/exporter.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/observability-loki.yaml)
