# 状態を持つファイアウォール

![routerd の firewall zone、policy matrix、明示 rule、client policy、生成される nftables filter 出力](/img/diagrams/concept-firewall.png)

routerd は nftables の `inet routerd_filter` テーブルを管理します。
ファイアウォールは 4 種類のリソースで表します。

- `FirewallZone` は、インターフェースをゾーンに割り当てます。
- `FirewallPolicy` は、拒否ログなどの全体設定を表します。
- `FirewallRule` は、役割の組み合わせでは表せない例外を表します。
- `ClientPolicy` は、MAC アドレスで LAN クライアントを分類します。

役割は `untrust`、`trust`、`mgmt` の 3 種類です。`wan`、`lan`、
`management` などはゾーン名であり、役割とは分けて考えます。

既定の動作は次の表で決まります。

| 送信元 | self | mgmt | trust | untrust |
| --- | --- | --- | --- | --- |
| `mgmt` | 許可 | 許可 | 許可 | 許可 |
| `trust` | 許可 | 拒否 | 許可 | 許可 |
| `untrust` | 拒否 | 拒否 | 拒否 | 拒否 |

生成するルールは、確立済みの通信、関連する通信、loopback、必要な ICMPv6
を常に許可します。DHCPv6 PD、DS-Lite、dnsmasq の DHCP、`routerd-dns-resolver`
などに必要な穴も、routerd が内部で生成します。

NAT44 は別の `ip routerd_nat` テーブルを使います。

## ルール式

`FirewallRule` は、CIDR による送信元 / 宛先の照合、再利用できる `IPAddressSet`
への宛先参照、TCP/UDP の `sourcePorts` / `destinationPorts`、ICMP / ICMPv6
の type 名、nftables の `rateLimit`、送信元ごとの `connLimit` を扱えます。
`rateLimit` と `connLimit` は閾値を超えた通信に一致するため、スキャンや
ブルートフォースを緩和する `drop` / `reject` ルールと組み合わせるのが基本です。

## ゲスト端末の隔離

`ClientPolicy` は、同じ LAN セグメント上の端末を MAC アドレスで分類するゲストモードです。
信頼済みの端末とゲスト端末が同じ DHCP サーバーからアドレスを受け取る構成で使います。
ゲスト端末はルーターの最小限のサービスだけを使い、プライベートネットワークへは到達させません。

方針は 2 種類です。

| mode | 動作 |
| --- | --- |
| `include` | 一覧に書いた MAC アドレスだけを guest として扱います。残りは trusted です。 |
| `exclude` | 一覧に書いた MAC アドレスだけを trusted として扱います。対象インターフェース上の残りは guest です。 |

`ClientPolicy.spec.macs` は、よく使う場合の短縮形です。
`interfaces` を省略すると、`trust` の `FirewallZone` に属する全インターフェースが対象になります。
`spec.isolation` では、internet、LAN、management、local discovery の allow / deny を読みやすく表現できます。

ゲスト端末は、既定で DNS、DHCP、NTP を使えます。
転送先が `10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`、`fc00::/7` の通信は拒否します。
グローバルインターネット向けの通信は、通常のゾーンマトリクスと経路方針に従います。

`ClientPolicy` は、Linux nftables の Ethernet 送信元アドレス set で実装しています。
FreeBSD pf は routed filter path で同じ MAC 照合モデルを持たないため、routerd はこのリソースを明示的に未対応として扱います。

## ログ

`FirewallPolicy.spec.logDeny` が true で、`FirewallEventLog` リソースが有効な場合、
生成した nftables ルールは、拒否したパケットを設定済みの NFLOG group へ送ります。
Linux では、`routerd-firewall-logger` が nfnetlink から直接その group を読み取り、
`firewall-logs.db` に保存します。
別のパケット取得プロセスは起動しません。
NFLOG の prefix、インターフェース、パケットファミリー、プロトコル、
アドレス、ポートをそのまま保存できます。

Web 管理画面のファイアウォールタブと `routerctl firewall-logs` は、このデータベースを読みます。
`FirewallEventLog.spec.enabled` が true のとき、routerd は `routerd-firewall-logger` のサービス成果物を導出し、設定済みのデータベースパスと NFLOG グループを渡します。

accept した通信フローを DPI の観測に使う場合は、`FirewallEventLog.spec.log.copyRange`
で、NFLOG が各パケットからコピーするペイロード量に上限を設定できます。
`1536` や `2048` バイト程度にしておくと、TLS/HTTP/DNS の分類に必要な先頭部分は見えつつ、
パケット全体をユーザー空間へコピーし続けることを避けられます。
