# Web 管理画面

`WebConsole` は、routerd の状態を読むための HTTP 画面です。
管理ネットワークでのローカル運用を想定しています。
設定の変更、サービスの再起動、リソースの適用、状態データベースの編集は行いません。

設定の変更は、YAML ファイルと `routerctl` コマンドに限定します。
ブラウザーは観測専用です。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: WebConsole
metadata:
  name: mgmt
spec:
  enabled: true
  listenAddressFrom:
    resource: Interface/mgmt
    field: ipv4Addresses
  port: 8080
  title: edge-router
```

待ち受けは、管理アドレスに限定してください。
untrust の WAN インターフェースでは公開しないでください。
管理アドレスを OS や IPAM から受け取る場合は、`listenAddressFrom` を使います。
routerd は、起動時にリソースの状態から値を解決します。
固定の予備アドレスが必要な場合は、`listenAddress` も併用できます。

## 読み取る情報

Web 管理画面は次の情報を読み取ります。

- routerd デーモン状態
- SQLite 状態データベース内のリソース状態
- SQLite イベントテーブル内のバスイベント
- conntrack または pf の state から得たコネクションの観測値
- `dns-queries.db` の DNS クエリー履歴
- `traffic-flows.db` の通信フロー履歴
- `firewall-logs.db` のファイアウォール拒否履歴
- 現在の dnsmasq の DHCP リースファイル。端末名、MAC アドレス、ローカルな
  ベンダー候補を表示します。
- 現在の YAML 設定。読み取り専用で表示します。

## 現在の画面

現在の Fluent UI 版 Web アプリケーションでは、次を表示します。

- PD、DS-Lite、DNS、NAT、経路、ヘルスチェック、VPN、パッケージ、sysctl、
  systemd ユニット、ログリソースの状態概要
- フェーズや観測値が変わったリソースの強調表示
- 選択したイベントの詳細ペイン。大きな属性でイベント表を崩しません。
- DHCP リースイベントの詳細。MAC アドレス、IP アドレス、ホスト名、リソース名を表示します。
- アドレスファミリーとプロトコルで分けた Connections 画面。
  絞り込み、並べ替え、ページネーション、行数選択ができます。
- 分離したログデータベースに基づく、DNS クエリー、通信フロー、ファイアウォールログの画面
- `/bgp`、`/vrrp`、`/ingress` の BGP、VRRP、IngressService 専用の運用ページ。
  これらは Server-Sent Events でリソース表を更新し、ブラウザー内に
  5/15/60 分の軽量な SVG トレンドを保持し、関連リソースだけのイベントログを表示します。
- Firewall 行では、ファイアウォールログ、DNS 応答、DHCP リース、
  MAC ベンダー候補、現在の conntrack の復路タプルをまとめて表示します。
  これにより、拒否されたパケットが不要な外部通信なのか、既存の NAT 変換に近い別経路の応答なのかを判断しやすくします。
- 構造化した折りたたみツリーと Raw YAML 表示を持つ、読み取り専用の Config 画面

コネクション行は、基本的に往路を表示します。
conntrack は同じ通信を往復の両方向で報告しますが、復路を主要行として重ねて表示することはしません。

## API 境界

Web 管理画面 API は読み取り専用です。
JSON エンドポイントは `/api/v1` 配下にあり、SSE ストリームは短い
`/api/events/stream` という別名でも利用できます。

| パス | 内容 |
| --- | --- |
| `/api/v1/summary` | 状態、リソースフェーズ、直近イベント、コネクションの概要 |
| `/api/v1/resources` | 状態データベース内のリソース状態 |
| `/api/v1/events?limit=200&resourceKind=&resourceName=&q=` | 任意の絞り込み条件付きの直近バスイベント |
| `/api/v1/events/stream` または `/api/events/stream` | `routerd.*` バスイベントの Server-Sent Events ストリーム |
| `/api/v1/connections` | conntrack または pf の state から得たコネクションの観測値 |
| `/api/v1/dns-queries?since=1h&client=&qname=&limit=100` | DNS クエリーログの行 |
| `/api/v1/traffic-flows?since=1h&client=&peer=&limit=100` | DNS 由来のホスト名を含む通信フローログの行 |
| `/api/v1/firewall-logs?since=24h&action=drop&src=&limit=100` | ファイアウォールログの行 |
| `/api/v1/bgp`、`/api/v1/vrrp`、`/api/v1/ingress` | Kubernetes edge の経路 / VIP リソース向けの運用状態 |
| `/api/v1/config` | 現在の YAML 設定 |
| `/api/v1/generations?limit=100` | 完了した適用世代と、YAML スナップショットの有無 |
| `/api/v1/generations/<id>/config` | 1 つの適用世代に保存された YAML |
| `/api/v1/generations/<from>/diff/<to>` | 2 つの YAML 世代の差分（unified diff） |

## シークレットの redaction

config を返すエンドポイント — `/api/v1/config`、
`/api/v1/generations/<id>/config`、
`/api/v1/generations/<from>/diff/<to>` — は、**シリアライズ前に secrets を
redact します**。WireGuard `privateKey` / `preSharedKey`、Tailscale
`authKey`、BGP/PPPoE/IPsec `password`、WebConsole `initialPassword`、
および bearer / token / API key 系のフィールドは、マーカ値
（`***REDACTED***`）に置き換えられます。キーは残るため UI の構造は壊れません。

読み取り専用の Web Console から生の secrets が見える経路はありません。
特権ローカル経路（routerd の control socket、`routerctl describe`）は意図的に
変更しておらず、必要なら生の intent を表示します。これらの経路はローカル
socket の権限と `routerd` グループメンバーシップで守ってください。
