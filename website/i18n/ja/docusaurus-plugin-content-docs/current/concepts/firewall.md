# 状態を持つファイアウォール

routerd は nftables の `inet routerd_filter` テーブルを管理します。
ファイアウォールは 3 種類のリソースで表します。

- `FirewallZone` は、インターフェースをゾーンに割り当てます。
- `FirewallPolicy` は、拒否ログなどの全体設定を表します。
- `FirewallRule` は、役割の組み合わせでは表せない例外を表します。

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

## ログ

`FirewallPolicy.spec.logDeny` が true で、`FirewallLog` リソースが有効な場合、
生成した nftables ルールは拒否したパケットを設定済みの NFLOG group へ送ります。
`routerd-firewall-logger` は `tcpdump` 経由でその group を読み取り、
`firewall-logs.db` に保存します。

Web Console の Firewall タブと `routerctl firewall-logs` は、このデータベースを読みます。
logger は管理対象の `SystemdUnit` として有効にしてください。
たとえば `routerd-firewall-logger daemon --path /var/lib/routerd/firewall-logs.db --nflog-group 1` を使います。
