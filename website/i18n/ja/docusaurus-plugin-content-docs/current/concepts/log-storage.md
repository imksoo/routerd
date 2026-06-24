# ログストレージ

![routerd の log writer、platform 由来の SQLite store、retention、読み取り専用の運用 view](/img/diagrams/concept-log-storage.png)

routerd は、長期的な状態と運用ログを分けて保存します。

Linux の既定の配置は次のとおりです。

| ファイル | 目的 | 標準の保管期間 |
| --- | --- | --- |
| `/var/lib/routerd/routerd.db` | リソース状態とイベントテーブル | イベントは 30 日 |
| `/var/lib/routerd/dns-queries.db` | `routerd-dns-resolver` の DNS クエリー履歴 | 30 日 |
| `/var/lib/routerd/traffic-flows.db` | conntrack から作った通信フロー履歴 | 30 日 |
| `/var/lib/routerd/firewall-logs.db` | accept、drop、reject のファイアウォールログ | 90 日 |

FreeBSD では、同じデータベース名を `/var/db/routerd` 配下に置きます。

ログテーブルの列名は、OpenTelemetry のログ属性へ変換しやすい名前にしています。
`traffic-flows.db` には、nDPI と TLS SNI 用の列を予約しています。
これらの列へ書き込む処理は、現時点ではまだ実装しておらず、後続の実装で追加します。

`LogRetention` は、signal 単位で古い行を削除します。
SQLite の incremental vacuum も実行できます。DB ファイルのパスは設定には現れず、
routerd が、イベント・DNS クエリー・通信フロー・ファイアウォールイベントを生成するリソースから導出します。

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
    - dnsQueries
    - trafficFlows
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
routerctl dns-queries --since 1h
routerctl traffic-flows --since 1h
routerctl firewall-logs --since 24h --action drop
```
