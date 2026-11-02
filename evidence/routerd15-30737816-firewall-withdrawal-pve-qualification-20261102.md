# routerd 1.5 Linux ファイアウォール設定撤回の PVE 実通信検証（2026-11-02）

## 対象

- ソース改訂: 30737816d1a55235b749d6dede1e573218d0e5e6
- 対象: Linux の FirewallZone、FirewallPolicy、NAT44Rule
- 構成: PVE の VM219（候補ルーター）、VM220（WAN・管理網の独立送信元）、VM224（LAN 側の独立クライアント名前空間）
- 分離: VLAN 3131（LAN）、3132（WAN）、3133（管理網）
- 新規 1.5 設定: 外部管理の三つのインターフェース、IPv4 転送、LAN から WAN への変換、信頼・非信頼・管理の三つのゾーン、既定ポリシー。旧設定の読取り、変換、移行機能は使用していない。

## 実通信の観測

有効な設定では、次を確認した。

| 経路 | 結果 |
| --- | --- |
| LAN クライアントから WAN の HTTP | 200。WAN 側の記録で送信元は変換後の 198.18.16.2 |
| 管理網から LAN クライアントの HTTP | 200 |
| LAN クライアントから管理網の HTTP | 000、時間切れ |
| WAN から LAN クライアントの HTTP | 000、時間切れ |
| WAN からルーターの HTTP | 000、時間切れ |

ルーターの規則カウンターは、許可した LAN→WAN と管理→LAN、遮断した LAN→管理・WAN→LAN・WAN→ルーターの各経路で増加した。

## 撤回時に見つけた不具合と修正

改訂 0c3cfe88 では、空の 1.5 設定を適用すると routerd_filter と NAT の規則表は削除された一方、net.ipv4.ip_forward=1 は残った。その結果、LAN→WAN と管理→LAN の HTTP がともに 200 のまま通った。この観測は不合格であり、合格証跡には含めない。

改訂 30737816 では、最後のファイアウォール設定を撤回すると、所有者印を保持した最小の routerd_filter 規則表へ原子的に置換する。入力と転送は既定拒否、出力は既定許可である。したがって、転送設定値が残っていても無許可転送はできない。

修正版で空の設定を適用した結果は、世代 4 が Healthy、routerd_filter の入力・転送がともに policy drop、LAN→WAN と管理→LAN の HTTP がともに 000 だった。

元の新規 1.5 設定を再適用した世代 5 では、上表の許可・遮断状態へ復旧した。さらに routerd を再起動した後も、LAN→WAN と管理→LAN の HTTP は 200 だった。

## 実行済みの確認

- go test ./pkg/firewallbackend ./pkg/controller/firewall
- go test ./pkg/render
- go test ./cmd/routerd ./cmd/routerctl -run "^$"
- sudo -n unshare -n による TestFirewallNativeWithdrawal。隔離したネットワーク名前空間で、撤回後の転送既定拒否を確認。
- PVE VLAN の実通信、設定撤回、再適用、routerd 再起動後の復旧。

## 未検証範囲

この証跡は Linux のゾーン分離、IPv4 変換、設定撤回・復旧に限る。IPv6、PPPoE、DNS、経路制御、冗長化、詳細な規則選択子、FreeBSD を含む全機能の合格を意味しない。

検証後、VM219 の実行ファイル・設定・状態・起動用 ISO を試験前の控えへ戻した。VM220 と VM224 の試験用名前空間と候補 ISO も削除した。
