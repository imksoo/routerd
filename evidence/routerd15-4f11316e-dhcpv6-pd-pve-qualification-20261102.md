# DHCPv6-PD、ルーター広告、IPv6 経路の PVE 実通信記録

対象リビジョンは `4f11316e84c63fc67574df4a7bee03a8d4ff395d` です。Linux の PVE 分離 VLAN で、DHCPv6-PD、LAN へのアドレス適用、ルーター広告、IPv6 静的経路、独立クライアントの到達性、再起動、プレフィックス変更、上流消失時の撤回、復旧を確認しました。

## 構成

- VM219: `routerd`。VLAN 3131 を LAN、VLAN 3132 を WAN とする。
- VM220: 独立した Kea DHCPv6 サーバーと IPv6 HTTP 上流。VLAN 3132 上で DHCPv6 を待ち受ける。
- VM224: VLAN 3131 だけへ接続する独立クライアント。
- 初期委任: `2001:db8:1500::/60`。変更後: `2001:db8:1510::/60`。
- 上流 HTTP: `http://[2001:db8:17::1]:18080/`。

## 観測結果

1. VM219 は DHCPv6-PD を `Bound` として取得しました。`IPv6DelegatedAddress/lan-v6` は `2001:db8:1500::1/64` を適用しました。
2. `IPv6RouterAdvertisement/lan-pd-ra` は `Applied` となり、`2001:db8:1500::/64` を広告しました。`IPv6StaticRoute/pd-upstream-route` は `Installed` となりました。
3. VM224 は SLAAC により `2001:db8:1500::/64` のアドレスと VM219 のリンクローカルアドレスを経由する既定経路を取得しました。HTTP は `200` であり、上流が観測した送信元は VM224 の委任プレフィックスでした。
4. `systemctl restart routerd` 後も DHCPv6-PD は `Bound`、ルーター広告は `Applied`、IPv6 静的経路は `Installed` でした。VM224 の HTTP は `200` でした。
5. Kea を同一プロセスの設定再読込みで `2001:db8:1510::/60` へ変更しました。VM219 の LAN アドレスとルーター広告は新しい `/64` へ切り替わりました。VM224 は新しい SLAAC アドレスと既定経路を取得し、戻り経路更新後の HTTP は `200`、送信元は `2001:db8:1510::/64` でした。
6. Kea を停止して有効期間を満了させると、DHCPv6-PD は `Acquiring`、LAN アドレスとルーター広告は `Pending` へ遷移しました。VM224 からグローバル IPv6 アドレスと既定経路が消え、HTTP は失敗しました。
7. Kea を再起動すると DHCPv6-PD は再び `Bound`、LAN アドレスとルーター広告は `Applied`、IPv6 静的経路は `Installed` になりました。VM224 は `2001:db8:1510::/64` のアドレスと既定経路を再取得し、HTTP は `200` でした。

## 範囲外

- DHCPv6 の DNS サーバー情報、DNS の有効期間・撤回はこの DHCPv6-PD サーバーが配布していないため未確認です。
- 他の IPv6 取得方式、`SelfAddressPolicy`、FreeBSD は未確認です。
- この記録は上記リビジョンと PVE 構成に限定されます。製品全体または他の機能の合格を示すものではありません。
