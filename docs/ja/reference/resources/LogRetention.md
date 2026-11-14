# LogRetention

[リソース別の設定](../resources.md#logretention) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

運用ログのSQLiteレコードを削除し、必要なら削除前に保管先へ転送します。任意のOSログファイルを整理する設定ではありません。

## 指定条件と参照関係

- 初回のreconcileで実行し、その後はlastRunAtから23時間以上経過した時点で実行します。schedule省略時もdailyも、この経過時間で判定し、午前0時のcron実行ではありません。dry-runでは外部ログDBを開いたり削除したりしません。
- イベントジャーナルには独立した24時間の保持上限と行数・容量上限があります。ここで長期間を指定しても、そのイベント保持上限は延長しません。
- sinksを指定した場合は各行を削除する前に転送します。保管先への送信が失敗した行は正常削除として扱いません。LogSinkのotlpは現行では送信しないため、機能する保管先として使えません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogRetention
  metadata:
    name: "<name>"
  spec:
    retention: "<string>" # 必須; string
    schedule: "" # 任意; string; enum=["", "daily"]
    signals: # 任意; array
      - "events" # string; enum=["events", "accessLogs", "pluginRuns", "dnsQueries", "trafficFlows", "firewallEvents", "dhcpFingerprints"]
    sinks: # 任意; array
      - "<string>" # string
    vacuum: true # 任意; boolean
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.retention` | 保持期間。7dや24hなどの指定が必須で既定値はありません。dは整数日数×24時間、それ以外はGo duration形式です。検証は構文の解析までで、正の値であることは要求しません。 |
| `spec.signals` | 削除対象のログ区分。省略時はevents、accessLogs、pluginRuns、dnsQueries、trafficFlows、firewallEvents、dhcpFingerprintsの7種類すべてです。格納DBは関連ログリソースのパスまたはOS既定値から決めます。 |
| `spec.sinks` | 削除前の保管先。省略時は保管しません。LogSink/名前または名前だけを受理し、有効なsystem.routerd.net/v1alpha1のLogSinkを選びます。存在しない名前は選択されずエラーにもならないため、指定を確認してください。 |
| `spec.vacuum` | 削除後に対象SQLiteファイルをVACUUMする指定。省略時falseです。保持対象の選択ではなく、DBファイルの領域整理を行います。 |
| `spec.schedule` | 省略またはdaily。いずれも初回実行と、その後23時間以上経過してからの実行です。時刻指定やcron式は受理しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go) · [pkg/logstore/retention.go](https://github.com/imksoo/routerd/blob/main/pkg/logstore/retention.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go)
