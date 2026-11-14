# LogSink

[リソース別の設定](../resources.md#logsink) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

typeで選ぶ運用イベントログの出力先です。ObservabilityPipeline内のログ出力先とは別の設定です。

## 指定条件と参照関係

- typeで実装を1つ選び、その種類のオブジェクトだけを使います。他の種類のオブジェクトは排他エラーにはせず、統合もしません。otlpは検証上telemetryRefとendpointのどちらか1つだけが必須ですが、現行OTLPSink.Emitはイベントを送信しません。
- journaldの実装はGoのlog.Printfへ出力します。journaldへの到達はプロセスのログ出力先とサービス管理側に依存し、journald APIへ直接送信する実装ではありません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: LogSink
  metadata:
    name: "<name>"
  spec:
    [ enabled: <boolean> ]
    [ file:
        [ name: <string> ]
    ]
    [ journald:
        [ identifier: <string> ]
    ]
    [ minLevel: ("debug" | "info" | "warning" | "error") ]
    [ otlp:
        [ endpoint: <string> ]
        [ telemetryRef: <string> ]
    ]
    [ syslog:
        [ address: <string> ]
        [ facility: ("kern" | "user" | "mail" | "daemon" | "auth" | "syslog" | "lpr" | "news" | "uucp" | "cron" | "authpriv" | "ftp" | "local0" | "local1" | "local2" | "local3" | "local4" | "local5" | "local6" | "local7") ]
        [ network: ("" | "unix" | "unixgram" | "tcp" | "udp") ]
        [ tag: <string> ]
    ]
    type: ("syslog" | "otlp" | "webhook" | "file" | "journald")
    [ webhook:
        [ headers:
            [ <name>: <string> ]
        ]
        [ timeout: <string> ]
        [ url: <string> ]
    ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.type` | **必須** | string; enum=["syslog", "otlp", "webhook", "file", "journald"] | 出力先の区分。syslog、otlp、webhook、file、journaldの指定が必須で既定値はありません。 |
| `spec.enabled` | *任意* | boolean | ログ出力先として選択するかの指定。省略時true。falseならLogRetentionから参照していても選びません。 |
| `spec.minLevel` | *任意* | string; enum=["debug", "info", "warning", "error"] | 最低重要度。省略時info。debug、info、warning、errorを指定し、下位の重要度のイベントを除外します。 |
| `spec.syslog` | *任意* | object | type=syslogで使う設定。省略時はローカルsyslog、facility=local6、tag=routerdです。 |
| `spec.syslog.network` | *任意* | string; enum=["", "unix", "unixgram", "tcp", "udp"] | 転送方式。unix、unixgram、tcp、udpを指定します。省略時はGoのsyslog実装がローカルソケットを探索します。 |
| `spec.syslog.address` | *任意* | string | ソケットパスまたはhost:port。省略時は空です。転送方式を明示した場合は対応する宛先も指定します。この組合せは設定検証では必須化されず、接続エラーは実際の出力時に生じます。 |
| `spec.syslog.facility` | *任意* | string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"] | syslogのfacility。省略時local6。受理する値はYAML内の列挙を参照してください。 |
| `spec.syslog.tag` | *任意* | string | syslogのタグ。省略時routerd。リソース参照ではありません。 |
| `spec.otlp` | **条件付き必須** | object | OTLPの宣言。type=otlpではtelemetryRefとendpointのどちらか1つが必須です。この出力先の実送信処理は実装されていません。 |
| `spec.otlp.telemetryRef` | **条件付き必須** | string | Telemetry/名前またはTelemetryの名前だけを指定します。endpointと排他で省略時は空です。他のtypeを選んでいても、Router全体の検証で参照先の存在を確認します。現行OTLPSinkはこの参照を送信に使いません。 |
| `spec.otlp.endpoint` | **条件付き必須** | string | 意図する送信先URI。telemetryRefと排他で既定値はありません。URI構文は検証しますが、このLogSinkではexporterを生成しません。 |
| `spec.webhook` | **条件付き必須** | object | type=webhookで使う、JSONをHTTP POSTする送信先の設定です。 |
| `spec.webhook.url` | **条件付き必須** | string | type=webhookでは必須で既定値はありません。HTTP(S)の絶対URLを指定してください。検証はURI構文までです。送信成功と扱うのはHTTP 2xxだけです。 |
| `spec.webhook.headers` | *任意* | object | 追加の要求ヘッダー。省略時は空です。既定のContent-Type: application/jsonを含め、同名ヘッダーを上書きします。 |
| `spec.webhook.timeout` | *任意* | string | HTTP要求の制限時間。省略時5sでGo duration形式です。検証は0や負値も受理し、HTTP clientでは0以下が時間制限なしになります。 |
| `spec.file` | *任意* | object | type=fileの保存先設定。JSON Lines形式で追記します。 |
| `spec.file.name` | *任意* | string | 論理ファイル名。省略時routerd-eventsで、保存先は/var/lib/routerd/logs/<name>.jsonlです。パス区切り、NUL、改行は使えません。任意のファイルパスを指定する項目ではありません。 |
| `spec.journald` | *任意* | object | type=journaldの設定。プロセスのloggerを通して出力します。 |
| `spec.journald.identifier` | *任意* | string | プロセスのログメッセージに付ける接頭辞。省略時routerd。journaldのSYSLOG_IDENTIFIERを直接設定する実装ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `type`
