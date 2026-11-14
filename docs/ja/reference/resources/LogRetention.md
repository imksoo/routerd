# LogRetention

[リソース別の設定](../resources.md#logretention) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

運用ログのSQLiteレコードを削除し、必要なら削除前に保管先へ転送します。任意のOSログファイルを整理する設定ではありません。

## 指定条件と参照関係

- 初回のreconcileで実行し、その後はlastRunAtから23時間以上経過した時点で実行します。schedule省略時もdailyも、この経過時間で判定し、午前0時のcron実行ではありません。dry-runでは外部ログDBを開いたり削除したりしません。
- イベントジャーナルには独立した24時間の保持上限と行数・容量上限があります。ここで長期間を指定しても、そのイベント保持上限は延長しません。
- sinksを指定した場合は各行を削除する前に転送します。保管先への送信が失敗した行は正常削除として扱いません。LogSinkのotlpは現行では送信しないため、機能する保管先として使えません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: LogRetention
  metadata:
    name: "<name>"
  spec:
    retention: <string>
    [ schedule: ("" | "daily") ]
    [ signals:
        - ("events" | "accessLogs" | "pluginRuns" | "dnsQueries" | "trafficFlows" | "firewallEvents" | "dhcpFingerprints")
    ]
    [ sinks:
        - <string>
    ]
    [ vacuum: <boolean> ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.retention` | **必須** | string | 保持期間。7dや24hなどの指定が必須で既定値はありません。dは整数日数×24時間、それ以外はGo duration形式です。検証は構文の解析までで、正の値であることは要求しません。 保持期間には正の値を指定してください。 |
| `spec.signals` | *任意* | array | 削除対象のログ区分。省略時はevents、accessLogs、pluginRuns、dnsQueries、trafficFlows、firewallEvents、dhcpFingerprintsの7種類すべてです。格納DBは関連ログリソースのパスまたはOS既定値から決めます。 |
| `spec.sinks` | *任意* | array | 削除前の保管先。省略時は保管しません。LogSink/名前または名前だけで指定します。Router全体の検証で各LogSinkの存在を確認し、実行時には無効な出力先を除きます。 |
| `spec.vacuum` | *任意* | boolean | 削除後に対象SQLiteファイルをVACUUMする指定。省略時falseです。保持対象の選択ではなく、DBファイルの領域整理を行います。 |
| `spec.schedule` | *任意* | string; enum=["", "daily"] | 省略またはdaily。いずれも初回実行と、その後23時間以上経過してからの実行です。時刻指定やcron式は受理しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/chain/logretention.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/logretention.go) · [pkg/logstore/retention.go](https://github.com/imksoo/routerd/blob/main/pkg/logstore/retention.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `targets`, `updatedAt`
