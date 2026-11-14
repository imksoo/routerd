# LogSink

[リソース別の設定](../resources.md#logsink) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

typeで選ぶ運用イベントログの出力先です。ObservabilityPipeline内のログ出力先とは別の設定です。

## 指定条件と参照関係

- typeで実装を1つ選び、その種類のオブジェクトだけを使います。他の種類のオブジェクトは排他エラーにはせず、統合もしません。otlpは検証上telemetryRefとendpointのどちらか1つだけが必須ですが、現行OTLPSink.Emitはイベントを送信しません。
- journaldの実装はGoのlog.Printfへ出力します。journaldへの到達はプロセスのログ出力先とサービス管理側に依存し、journald APIへ直接送信する実装ではありません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogSink
  metadata:
    name: "<name>"
  spec:
    enabled: true # 任意; boolean
    file: # 任意; object
      name: "<string>" # 任意; string
    journald: # 任意; object
      identifier: "<string>" # 任意; string
    minLevel: "debug" # 任意; string; enum=["debug", "info", "warning", "error"]
    otlp: # 任意; object
      endpoint: "<string>" # 任意; string
      telemetryRef: "<string>" # 任意; string
    syslog: # 任意; object
      address: "<string>" # 任意; string
      facility: "kern" # 任意; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
      network: "" # 任意; string; enum=["", "unix", "unixgram", "tcp", "udp"]
      tag: "<string>" # 任意; string
    type: "syslog" # 必須; string; enum=["syslog", "otlp", "webhook", "file", "journald"]
    webhook: # 任意; object
      headers: # 任意; object
        "<key>": "<string>" # 任意; string
      timeout: "<string>" # 任意; string
      url: "<string>" # 任意; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.type` | 出力先の区分。syslog、otlp、webhook、file、journaldの指定が必須で既定値はありません。 |
| `spec.enabled` | ログ出力先として選択するかの指定。省略時true。falseならLogRetentionから参照していても選びません。 |
| `spec.minLevel` | 最低重要度。省略時info。debug、info、warning、errorを指定し、下位の重要度のイベントを除外します。 |
| `spec.syslog` | type=syslogで使う設定。省略時はローカルsyslog、facility=local6、tag=routerdです。 |
| `spec.syslog.network` | 転送方式。unix、unixgram、tcp、udpを指定します。省略時はGoのsyslog実装がローカルソケットを探索します。 |
| `spec.syslog.address` | ソケットパスまたはhost:port。省略時は空です。転送方式を明示した場合は対応する宛先も指定します。この組合せは設定検証では必須化されず、接続エラーは実際の出力時に生じます。 |
| `spec.syslog.facility` | syslogのfacility。省略時local6。受理する値はYAML内の列挙を参照してください。 |
| `spec.syslog.tag` | syslogのタグ。省略時routerd。リソース参照ではありません。 |
| `spec.otlp` | OTLPの宣言。type=otlpではtelemetryRefとendpointのどちらか1つが必須です。この出力先の実送信処理は実装されていません。 |
| `spec.otlp.telemetryRef` | Telemetry/名前またはTelemetryの名前だけを指定します。endpointと排他で省略時は空です。他のtypeを選んでいても、Router全体の検証で参照先の存在を確認します。現行OTLPSinkはこの参照を送信に使いません。 |
| `spec.otlp.endpoint` | 意図する送信先URI。telemetryRefと排他で既定値はありません。URI構文は検証しますが、このLogSinkではexporterを生成しません。 |
| `spec.webhook` | type=webhookで使う、JSONをHTTP POSTする送信先の設定です。 |
| `spec.webhook.url` | type=webhookでは必須で既定値はありません。HTTP(S)の絶対URLを指定してください。検証はURI構文までです。送信成功と扱うのはHTTP 2xxだけです。 |
| `spec.webhook.headers` | 追加の要求ヘッダー。省略時は空です。既定のContent-Type: application/jsonを含め、同名ヘッダーを上書きします。 |
| `spec.webhook.timeout` | HTTP要求の制限時間。省略時5sでGo duration形式です。検証は0や負値も受理し、HTTP clientでは0以下が時間制限なしになります。 |
| `spec.file` | type=fileの保存先設定。JSON Lines形式で追記します。 |
| `spec.file.name` | 論理ファイル名。省略時routerd-eventsで、保存先は/var/lib/routerd/logs/<name>.jsonlです。パス区切り、NUL、改行は使えません。任意のファイルパスを指定する項目ではありません。 |
| `spec.journald` | type=journaldの設定。プロセスのloggerを通して出力します。 |
| `spec.journald.identifier` | プロセスのログメッセージに付ける接頭辞。省略時routerd。journaldのSYSLOG_IDENTIFIERを直接設定する実装ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `type`
