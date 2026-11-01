# IPsec PVE 実通信・ファイアウォール撤去試験

実施日: 2026-11-02
対象リビジョン: `ccdc2aa51ccc006347007d1371dc2e3a1c0d86bd`
実通信時の routerd バイナリ: `65d1f768e6f950fe8d2cf1b824cd65a39c0cb3880ae45e95aa935cfbfe06207c`

## 対象と構成

- PVE VM219: routerd 1.5 の隔離ルーター
- PVE VM220: 独立した routerd 対向ルーター
- 専用 VLAN 3132 上の IKEv2 と ESP
- 保護対象: `10.254.96.1/32` と `10.254.96.2/32`
- Ubuntu の製品依存として `charon-systemd` と `strongswan-swanctl` を導入

対向側は、外部管理 `Interface` と信頼領域を一時的に宣言しました。ルーター側には、対向保護アドレスから自身への ICMP だけを許可する一時 `FirewallRule` を加えました。広い許可規則や直接の `nft` 操作は使用していません。

## 観測結果

1. IPsec 接続を含む構成は検証に通り、VM219 は 23 資源で `HEALTHY` になりました。
2. 両端で IKEv2 と CHILD SA の確立を確認しました。
3. VM219 から VM220 への ICMP は 3/3 応答でした。往復時間は 0.252–0.427 ms でした。
4. VM220 から VM219 への ICMP は 3/3 応答でした。往復時間は 0.252–0.291 ms でした。
5. 両端の SA と Linux XFRM の送受信バイト・パケット数が増加しました。
6. VM219 を元の構成へ戻し、21 資源の `HEALTHY`、一時 IPsec 接続と保護アドレスの不在を確認しました。
7. VM220 を元の構成へ戻した直後、未参照の試験用 `nftables` 集合と空の連鎖が残ることを確認しました。

## 修正と再検証

`ccdc2aa5` は、`inet routerd_filter` を `flush table` だけで更新する処理を、原子的な `destroy table` と再作成へ変更しました。これにより、削除済みファイアウォール領域の名前付き集合・連鎖も撤去します。

- `go test ./pkg/render ./pkg/controller/firewall ./pkg/firewallbackend -count=1`: 成功
- 修正バイナリを VM219/220 へ配備し、両方の `routerd.service` が稼働中であることを確認
- VM220 の元の 13 資源構成を再適用後、`if_ipsec_test` と関連連鎖が不在であることを確認

## 未検証範囲

証明書認証、複数対向、再鍵交換、対向喪失時の回復、外部事業者装置との相互接続は、この試験の対象外です。
