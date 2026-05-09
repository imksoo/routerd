# 状態を持つファイアウォール

routerd は nftables の `inet routerd_filter` テーブルを管理します。
ファイアウォールは 4 種類のリソースで表します。

- `FirewallZone` は、インターフェースをゾーンに割り当てます。
- `FirewallPolicy` は、拒否ログなどの全体設定を表します。
- `FirewallRule` は、役割の組み合わせでは表せない例外を表します。
- `ClientPolicy` は、MAC アドレスで LAN クライアントを分類します。

役割は `untrust`、`trust`、`mgmt` の 3 種類です。`wan`、`lan`、
`management` などはゾーン名です。役割とは分けて考えます。

既定の動作は次の表で決まります。

| 送信元 | self | mgmt | trust | untrust |
| --- | --- | --- | --- | --- |
| `mgmt` | 許可 | 許可 | 許可 | 許可 |
| `trust` | 許可 | 拒否 | 許可 | 許可 |
| `untrust` | 拒否 | 拒否 | 拒否 | 拒否 |

生成される規則は、確立済み通信、関連通信、loopback、必要な ICMPv6
を常に許可します。DHCPv6 PD、DS-Lite、dnsmasq の DHCP、`routerd-dns-resolver`
などに必要な開口も、routerd が内部で生成します。

NAT44 は別の `ip routerd_nat` テーブルを使います。

## ゲスト端末の隔離

`ClientPolicy` は、同じ LAN セグメント上の端末を MAC アドレスで分類するゲストモードです。
信頼済み端末とゲスト端末が同じ DHCP サーバーからアドレスを受け取る構成で使います。
ゲスト端末はルーターの最小サービスだけを使い、プライベートネットワークへ到達させません。

方針は 2 種類です。

| mode | 動作 |
| --- | --- |
| `include` | 一覧に書いた MAC アドレスだけを guest として扱います。残りは trusted です。 |
| `exclude` | 一覧に書いた MAC アドレスだけを trusted として扱います。対象インターフェース上の残りは guest です。 |

ゲスト端末は、既定で DNS、DHCP、NTP を利用できます。
転送先が `10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`、`fc00::/7` の通信は拒否します。
グローバルインターネット向け通信は、通常のゾーンマトリクスと経路方針に従います。

`ClientPolicy` は Linux nftables の Ethernet 送信元アドレス set で実装しています。
FreeBSD pf は routed filter path で同じ MAC 照合モデルを持たないため、routerd はこのリソースを明示的に未対応として扱います。

## ログ

`FirewallPolicy.spec.logDeny` が true で、`FirewallLog` リソースが有効な場合、
生成した nftables ルールは拒否したパケットを設定済みの NFLOG group へ送ります。
Linux では、`routerd-firewall-logger` が nfnetlink から直接その group を読み取り、
`firewall-logs.db` に保存します。別のパケット取得プロセスは起動しません。
NFLOG の prefix、インターフェース、パケットファミリー、プロトコル、
アドレス、ポートをそのまま保存できます。

Web Console の Firewall タブと `routerctl firewall-logs` は、このデータベースを読みます。
logger は管理対象の `SystemdUnit` として有効にしてください。
たとえば `routerd-firewall-logger daemon --path /var/lib/routerd/firewall-logs.db --nflog-group 1` を使います。
