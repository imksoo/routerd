# ログストレージ

![routerd の log writer、platform 由来の SQLite store、retention、読み取り専用の運用 view](/img/diagrams/concept-log-storage.png)

routerd は、長期的な状態と運用ログを分けて保存します。

Linux の既定の配置は次のとおりです。

| ファイル | 目的 | 標準の保管期間 |
| --- | --- | --- |
| `/var/lib/routerd/routerd.db` | リソース状態、イベント、アクセスログ、プラグイン実行ログ | イベントは既定24時間、その他は明示した保持ポリシー |
| `/var/lib/routerd/dns-queries.db` | `routerd-dns-resolver` の DNS クエリー履歴 | 30 日 |
| `/var/lib/routerd/traffic-flows.db` | conntrack から作った通信フロー履歴 | 30 日 |
| `/var/lib/routerd/firewall-logs.db` | accept、drop、reject のファイアウォールログ | 90 日 |
| `/var/lib/routerd/dhcp-fingerprints.db` | DHCP クライアントのフィンガープリント観測 | 30 日 |

FreeBSD では、同じデータベース名を `/var/db/routerd` 配下に置きます。

ログテーブルの列名は、OpenTelemetry のログ属性へ変換しやすい名前にしています。
`traffic-flows.db` には、nDPI と TLS SNI 用の列を予約しています。
これらの列へ書き込む処理は、現時点ではまだ実装しておらず、後続の実装で追加します。

イベント履歴には、`LogRetention`を設定していない場合にも常に働く安全上限があります。
24時間、10万行、論理ペイロード64MiBのうち最初に達した上限を使い、古いイベントを
分割して削除します。状態テーブルは削除しません。これは小容量Live ISOのCOW領域を
保護する安全装置であり、長期アーカイブの代わりではありません。

`LogRetention` は、設定可能なローカル SQLite の保持ポリシーです。signal 単位で古い行を削除し、
SQLite の incremental vacuum も実行できます。DB ファイルのパスや書き込み元ごとの保持期間は設定に現れません。
routerd は登録済みのログテーブル一覧から、イベント・アクセスログ・プラグイン実行ログ・DNS クエリー・通信フロー・ファイアウォールイベント・DHCP フィンガープリントを導出します。
新しい運用ログテーブルは、この一覧に signal と時刻列を登録しない限り保持対象にできません。

`dhcp-sticky.db`、リース同期データ、設定世代、連携状態はログではなく運用状態です。
削除によりルーターの挙動が変わるため、意図的に `LogRetention` の対象外です。

`LogRetention.spec.sinks` へ `LogSink` を指定すると、ローカル削除前に行を外部へアーカイブします。
転送に失敗した場合は削除しないため、外部保管の確認前にローカルの記録を失いません。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: LogRetention
metadata:
  name: default
spec:
  retention: 30d
  schedule: daily
  vacuum: true
  signals:
    - events
    - accessLogs
    - pluginRuns
    - dnsQueries
    - trafficFlows
    - dhcpFingerprints
  sinks:
    - LogSink/local-syslog
---
apiVersion: system.routerd.net/v1alpha1
kind: LogRetention
metadata:
  name: firewall-events
spec:
  retention: 90d
  schedule: daily
  vacuum: true
  signals:
    - firewallEvents
```

確認には次のコマンドを使います。

```sh
routerctl get dns-queries --limit 100
routerctl get traffic-flows --limit 100
routerctl get firewall-logs --limit 100
routerctl doctor disk
```

SQLiteがディスク満杯を返した場合、routerdは調査用イベントをDBへ書けなくても、既存の
ルーティングとローカルイベント配送を可能な限り継続します。全体状態を`Degraded`にし、
runtime statsと`routerctl doctor`へ`EventJournalReadOnly`を表示し、
`/run/routerd/storage-critical.json`と`systemctl status routerd`にも復旧方法を出します。
揮発性Live ISOではルーターOSを再起動してCOW領域を初期化し、再起動後に保持設定を
確認してください。永続ディスクは再起動だけでは空かないため、イベント履歴の整理・圧縮、
または状態領域の拡張・移動が必要です。
