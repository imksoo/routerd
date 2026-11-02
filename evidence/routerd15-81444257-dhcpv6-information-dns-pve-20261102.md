# DHCPv6 情報 DNS 撤回・復帰の PVE 実通信記録

Linux 配布物 `81444257` で、VM219 は独立 VM220 の Kea から `/60` と DNS `2001:db8:16::53` を取得しました。VM224 の `router.info.test AAAA` は候補の DNS リゾルバーを通り同じ応答を得ました。

Kea の DNS オプションだけを撤回すると、`DHCPv6Information/wan-info` は `dnsServers: null`、候補の DNS 実行時設定は `noUsableSources: true` となりました。VM224 の同じ問い合わせは応答なしになりました。PD、候補の LAN アドレス、PVE 管理経路は維持しました。

Kea の DNS オプションを復帰すると、候補は DNS 上流を再取得し、VM224 の問い合わせは `2001:db8:16::53` へ復帰しました。候補機の再起動や設定再適用は、撤回・復帰の段階で行っていません。

対象は PVE VLAN 3131 の VM224、VLAN 3132 の VM219・VM220 です。構成は新規 routerd 1.5 形式で、旧設定や移行処理は使用していません。準備中に VM220 の旧 `routerd_filter` が UDP/547 を遮断したため除去しました。この準備段階の未到達結果は判定に含めません。

試験後は三 VM をスナップショットへ戻し、ISO、一時配置、スナップショットを削除しました。VM219 と VM220 は稼働、VM224 は停止です。

範囲外は複数 DNS サーバー、DNS 有効期限の長期挙動、WAN ルーター広告、PPPoE、FreeBSD、および全機能認定です。
