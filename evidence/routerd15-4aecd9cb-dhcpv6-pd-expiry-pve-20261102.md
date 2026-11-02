# DHCPv6 PD 失効時の DNS 撤回・復旧

## 結果

`4aecd9cb4f609241ef9edc59f6dfde326f4ca61c` では、DHCPv6 の委譲プレフィックス（PD）が失効すると、`DHCPv6Information/wan-info` の DNS 情報が撤回されました。独立クライアント VM224 の名前解決は停止し、独立 DHCPv6 サーバの復帰後には候補機を再起動せず自動復旧しました。

## 構成

- PVE の隔離 VLAN 3131: VM219 の候補機と VM224 の独立クライアント
- PVE の隔離 VLAN 3132: VM219 の WAN と VM220 の独立 Kea DHCPv6・DNS
- 候補機: `DHCPv6PrefixDelegation`、`DHCPv6Information`、`IPv6DelegatedAddress`、`DNSResolver`、`DNSUpstream`
- Kea の PD: `2001:db8:1500::/60`、有効期限 30 秒、DNS `2001:db8:16::53`
- 候補パッケージ SHA-256: `4c57cdca882fe40cee97c06365e82a1eaa53e5c7c6b1e5e3b1fb4dd5998fd2aa`

各 VM は開始前に `rd15-pd-expiry-fix` スナップショットを取得しました。試験後は VM219 と VM220 を起動中、VM224 を停止中の基準状態へ戻し、スナップショット、ISO、作業ディレクトリを削除しました。

## 観測

1. VM219 は PD `2001:db8:1500::/60` と DNS `2001:db8:16::53` を取得しました。LAN アドレス `2001:db8:1500:d0::1/64` が設定されました。
2. VM224 から `dig +short AAAA router.info.test @2001:db8:1500:d0::1` を実行すると、`2001:db8:16::53` が返りました。
3. VM220 で Kea DHCPv6 だけを停止しました。PD は `PrefixExpired` の後に `Acquiring` へ移行し、LAN のグローバル IPv6 アドレスは削除されました。
4. `DHCPv6Information/wan-info` は DNS 値を持たない `Pending` へ遷移しました。理由は `DHCPv6PrefixDelegationNotBound` です。更新通知 `routerd.dhcpv6.info.updated` も記録されました。
5. VM224 の同じ問い合わせは到達不能となり、`DIG_EXIT=9` を返しました。
6. VM220 の Kea DHCPv6 を再起動すると、候補機は再起動・設定再適用なしで PD、DNS、LAN アドレスを再取得しました。VM224 の同じ問い合わせは再び `2001:db8:16::53` を返しました。

## 修正内容と確認

修正前の `81444257` では、PD と LAN アドレスは失効時に撤回されましたが、`DHCPv6Information` の DNS 値が残りました。`4aecd9cb` は PD が `Bound` でない場合に、当該情報資源を DNS 値なしの `Pending` 状態へ更新します。

実行済み確認:

- `go test ./pkg/controller/chain -run '^TestDHCPv6InformationWithdrawsStaleDNSWhenPDIsNotBound$' -count=1`
- `go test ./pkg/controller/dnsresolver -count=1`
- `make dist`（終了状態 0）

## 未確認範囲

この結果は隔離 PVE VLAN 上の Linux 実通信に限ります。実回線、NTT DHCPv6、物理 WAN 障害、複数プレフィックスの同時失効、FreeBSD は別途確認が必要です。製品全体または Linux 全機能の合格判定ではありません。
