# NAT44 の PVE 分離 VLAN 実通信記録

対象リビジョンは `d467ab70ffff3e1b7763f084a945ea1ba9939db3` です。Linux の PVE 分離 VLAN で、NAT44 の実通信、構成撤回、再適用、`routerd` 再起動後の復旧を確認しました。

## 構成

- VM219: `routerd`。VLAN 3131 を LAN、VLAN 3132 を WAN として使用。
- VM220: VLAN 3132 の `198.18.16.1:18080` で待ち受ける独立 HTTP サーバー。
- VM224: VLAN 3131 の独立クライアント。`198.18.15.100/24` を使用。
- VM219 の NAT44: `198.18.15.0/24` から `rd15-wan` への `masquerade`。

## 観測結果

1. NAT44 の nftables 表に `rd15-wan` と `198.18.15.0/24` を対象とする `masquerade` 規則が存在しました。`routerctl doctor nat` は NAT44 資源と表をともに合格と報告しました。
2. VM224 から VM220 の HTTP サーバーへ接続すると HTTP 200 でした。VM220 のアクセスログに記録された送信元は `198.18.16.2` であり、VM224 の元アドレスではありませんでした。VM219 の接続追跡表も、この変換済み送信元を示しました。
3. NAT44 資源を含まない同一構成へ `routerctl apply --replace` で置換すると、NAT44 表は消え、VM224 からの新規 HTTP 接続はタイムアウトしました。VM220 のアクセスログに新しい要求はありませんでした。
4. NAT44 資源を再適用すると表と診断が復帰し、新規 HTTP 接続は HTTP 200 へ復帰しました。VM220 の送信元観測は再び `198.18.16.2` でした。
5. `systemctl restart routerd` 後にも表、診断、VM224 からの新規 HTTP 接続が復帰しました。

## 復帰

VM219 は元の実行物、構成、状態領域、`routerd` と `routerd-dnsmasq` の稼働状態へ復元しました。VM220 は試験用 VLAN と HTTP サーバーを撤去し、元の `routerd` を再稼働しました。VM224 は試験用 VLAN を撤去して停止しました。候補 ISO は元の ISO に差し戻して削除しました。

## 範囲外

- NAT44FlowDNATPinhole、NAT44SessionSync、PortForward、LocalServiceRedirect、既存接続の撤回時の挙動は未確認です。
- 複数送信元範囲、静的送信元 NAT、IPv6 NAT、物理回線は未確認です。
- この記録は上記リビジョンと PVE 構成に限定され、NAT 機能全体または製品全体の合格を示しません。
