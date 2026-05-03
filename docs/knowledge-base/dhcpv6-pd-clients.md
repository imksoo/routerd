# DHCPv6-PD クライアントの現在方針

routerd の現在方針では、DHCPv6-PD は `routerd-dhcpv6-client` が担当します。
過去に評価した OS 付属クライアントの経路は、現在の設定例としては使いません。

## なぜ専用デーモンにしたか

DHCPv6-PD は、取得だけでなく更新、再起動復元、イベント記録が重要です。
OS 付属クライアントへ設定を生成するだけでは、routerd の状態管理と LAN 側反映をきれいにつなげませんでした。

専用デーモンにしたことで、次をそろえられます。

- lease を `lease.json` に保存します。
- 起動時に lease を復元します。
- Renew の結果を events に記録します。
- `/v1/status` で `Bound` や `Pending` を返します。
- routerd 本体がイベントを受け、LAN 側リソースを調整します。

## 現在のバイナリ

```text
routerd-dhcpv6-client
```

代表的なパスは次の通りです。

```text
/run/routerd/dhcpv6-client/<name>.sock
/var/lib/routerd/dhcpv6-client/<name>/lease.json
/var/lib/routerd/dhcpv6-client/<name>/events.jsonl
```

## 旧実装の扱い

過去には、systemd-networkd、WIDE/KAME 系クライアント、別の DHCP クライアントを比較しました。
その調査は設計判断の背景として残しますが、現在の本線ではありません。

現在の Kind は `DHCPv6PrefixDelegation` です。
OS クライアントを選ぶ `client` フィールドは使いません。

## 実機確認済みの状態

router01、router02、router03、router04、router05 の 5 台で、`routerd-dhcpv6-client` による DHCPv6-PD Bound を確認済みです。
router02 は NixOS の宣言設定でユニットを管理しています。
router01 と router04 は FreeBSD 上で動作しています。

## 運用上の注意

同じ WAN インターフェースで、複数の DHCPv6-PD クライアントを同時に動かさないでください。
HGW 側の状態が壊れ、Reply が返らなくなることがあります。

routerd 管理へ移行するときは、古いクライアント、古い lease、古い systemd または rc.d の設定を止めます。
その後で `routerd-dhcpv6-client` を起動します。
