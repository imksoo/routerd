# Web Console

`WebConsole` は、routerd の状態を読むための HTTP 画面です。
管理ネットワークでのローカル運用を想定しています。
設定変更、サービス再起動、リソース適用、状態データベース編集は行いません。

設定変更は YAML ファイルと `routerctl` コマンドに限定します。
ブラウザーは観測専用です。

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
untrust WAN インターフェースでは公開しないでください。

## 読み取る情報

Web Console は次の情報を読み取ります。

- routerd デーモン状態
- SQLite 状態データベース内のリソース状態
- SQLite イベントテーブル内のバスイベント
- conntrack または pf state から得たコネクション観測
- `dns-queries.db` の DNS クエリー履歴
- `traffic-flows.db` の通信フロー履歴
- `firewall-logs.db` のファイアウォール拒否履歴
- 現在の YAML 設定。読み取り専用で表示します。

## 現在の画面

現在の Fluent UI 版 Web アプリケーションでは、次を表示します。

- PD、DS-Lite、DNS、NAT、経路、ヘルスチェック、VPN、パッケージ、sysctl、
  systemd ユニット、ログリソースの状態概要
- フェーズや観測値が変わったリソースの強調表示
- 選択したイベントの詳細ペイン。大きな属性でイベント表を崩しません。
- DHCP リースイベントの詳細。MAC アドレス、IP アドレス、ホスト名、リソース名を表示します。
- アドレスファミリーとプロトコルで分けた Connections 画面。ページネーションと行数選択があります。
- 分離されたログデータベースに基づく DNS クエリー、通信フロー、ファイアウォールログ画面
- シンタックスハイライトと折りたたみを持つ読み取り専用 Config 画面

コネクション行は、基本的に往路を表示します。
conntrack は同じ通信を往復方向で報告するため、復路を主要行として重ねて表示しません。

## API 境界

Web Console API は読み取り専用です。
エンドポイントは `/api/v1` 配下だけです。

| パス | 内容 |
| --- | --- |
| `/api/v1/summary` | 状態、リソースフェーズ、直近イベント、コネクション概要 |
| `/api/v1/resources` | 状態データベース内のリソース状態 |
| `/api/v1/events` | 直近のバスイベント |
| `/api/v1/connections` | conntrack または pf state から得たコネクション観測 |
| `/api/v1/dns-queries?since=1h&client=&qname=&limit=100` | DNS クエリーログ行 |
| `/api/v1/traffic-flows?since=1h&client=&peer=&limit=100` | DNS 由来ホスト名を含む通信フローログ行 |
| `/api/v1/firewall-logs?since=24h&action=drop&src=&limit=100` | ファイアウォールログ行 |
| `/api/v1/config` | 現在の YAML 設定 |
