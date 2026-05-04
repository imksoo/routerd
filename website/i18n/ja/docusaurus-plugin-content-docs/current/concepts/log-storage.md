# ログストレージ

routerd は、長期的な状態と運用ログを分けて保存します。

既定の配置は次のとおりです。

| ファイル | 目的 | 標準の保管期間 |
| --- | --- | --- |
| `/var/lib/routerd/routerd.db` | リソース状態とイベントテーブル | イベントは 30 日 |
| `/var/lib/routerd/dns-queries.db` | `routerd-dns-resolver` の DNS クエリー履歴 | 30 日 |
| `/var/lib/routerd/traffic-flows.db` | conntrack から作った通信フロー履歴 | 30 日 |
| `/var/lib/routerd/firewall-logs.db` | accept、drop、reject のファイアウォールログ | 90 日 |

ログテーブルの列名は、OpenTelemetry のログ属性へ変換しやすい名前にしています。
`traffic-flows.db` には、nDPI と TLS SNI 用の列を予約しています。
現時点では、これらの列へ書く処理は後続の実装で追加します。

`LogRetention` は古い行を削除します。
SQLite の incremental vacuum も実行できます。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: LogRetention
metadata:
  name: default
spec:
  schedule: daily
  incrementalVacuum: true
  targets:
    - file: /var/lib/routerd/routerd.db
      retention: 30d
    - file: /var/lib/routerd/dns-queries.db
      retention: 30d
    - file: /var/lib/routerd/traffic-flows.db
      retention: 30d
    - file: /var/lib/routerd/firewall-logs.db
      retention: 90d
```

確認には次のコマンドを使います。

```sh
routerctl dns-queries --since 1h
routerctl traffic-flows --since 1h
routerctl firewall-logs --since 24h --action drop
```
