# Web Console

`WebConsole` は、routerd の状態を読むための HTTP 画面です。
管理ネットワークでの運用確認を目的にします。
設定変更、サービス再起動、リソース適用、状態データベースの編集は行いません。

設定変更は YAML ファイルと `routerctl` コマンドだけで行います。
Web Console は次の情報だけを読みます。

- routerd デーモンの状態
- SQLite 状態データベース内のリソース状態
- SQLite イベントテーブル内の bus イベント
- conntrack と NAPT の観測値

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
- conntrack 件数と NAPT エントリーの一部

JSON endpoint も読み取り専用です。

| Path | 内容 |
| --- | --- |
| `/api/summary` | 状態、リソース phase、直近イベント、NAPT 概要 |
| `/api/resources` | 状態データベース内のリソース状態 |
| `/api/events` | 直近の bus イベント |
| `/api/napt` | conntrack と NAPT の観測値 |

