# Web Console

`WebConsole` は、routerd の状態を読むための HTTP 画面です。
管理ネットワークでの運用確認を目的にします。
設定変更、サービス再起動、リソース適用、状態データベースの編集は行いません。

設定変更は YAML ファイルと `routerctl` コマンドだけで行います。
Web Console は次の情報だけを読みます。

- routerd デーモンの状態
- SQLite 状態データベース内のリソース状態
- SQLite イベントテーブル内の bus イベント
- conntrack または pf state から得たコネクション観測値
- `dns-queries.db` に保存した DNS クエリー履歴
- `traffic-flows.db` に保存した通信フロー履歴
- `firewall-logs.db` に保存した拒否ログ

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: WebConsole
metadata:
  name: mgmt
spec:
  enabled: true
  listenAddress: 192.168.123.129
  port: 8080
  title: homert02
```

待ち受けは管理アドレスに限定してください。
信頼しない WAN インターフェースでは公開しないでください。

最初の画面では、次の情報を表示します。

- routerd 全体の phase と generation
- PD、DS-Lite、DNS、NAT、経路、HealthCheck、VPN、firewall リソースの phase
- 直近の routerd イベント
- `routerd.dhcp.lease.renewed` の MAC アドレス、IPv4 アドレス、ホスト名などのイベント属性
- conntrack 件数と IPv4/IPv6 コネクションの一部
- コネクション行の `dst label` 列。直近の DNS 応答から導出します
- クライアント別の通信量
- 送信元と宛先で集計した直近の拒否ログ

JSON エンドポイントも読み取り専用です。
Web Console API は `/api/v1` にだけ公開します。

| Path | 内容 |
| --- | --- |
| `/api/v1/summary` | 状態、リソース phase、直近イベント、コネクション概要 |
| `/api/v1/resources` | 状態データベース内のリソース状態 |
| `/api/v1/events` | 直近の bus イベント |
| `/api/v1/connections` | conntrack または pf state から得たコネクション観測値 |
| `/api/v1/dns-queries?since=1h&client=&qname=&limit=100` | DNS クエリー履歴 |
| `/api/v1/traffic-flows?since=1h&client=&peer=&limit=100` | DNS 履歴で通信先名を補った通信フロー履歴 |
| `/api/v1/firewall-logs?since=24h&action=drop&src=&limit=100` | ファイアウォールログ |
