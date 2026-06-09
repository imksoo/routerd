# CloudEdge SAM Azure x PVE スモークエビデンス

日付: 2026-05-29

ブランチ/ビルド: `cloudedge-mvp`、`routerd v20260528.2308 (439ec316)`

Result: PASS

エビデンスバンドル:
`/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260529T161157Z-439ec316-clean`

## トポロジー

- クラウドクライアント: `10.77.60.7/24`
- オンプレミスクライアント: `10.77.60.9/24`
- クラウドルータープライマリ: `10.77.60.4/24`
- クラウドルーター Azure NIC セカンダリ捕捉アドレス: `10.77.60.9`
- オンプレミスルーター: router06、`10.77.60.1/24` (`ens21`)
- オーバーレイ: `wg-hybrid`、`169.254.110.1/32` <-> `169.254.110.2/32`

## Azure 捕捉

- `ce-router-nic` で IP フォワーディングが有効。
- プライマリプライベート IP は `10.77.60.4`。
- セカンダリプライベート IP は `10.77.60.9`。
- routerd のリコンサイル後、`routerd-cloud` のゲスト OS は `10.77.60.9` をローカルインターフェースアドレスとして保持しなかった。
- `10.77.60.9/32` は `wg-hybrid` 経由で配送。

## クラウド側

- `RemoteAddressClaim/onprem-client-10-77-60-9` は `Ready`。
- 捕捉タイプは `provider-secondary-ip`。
- `captureDeassignedOSAddress.enforced=true`。
- 配送ルートがインストール済み: `10.77.60.9 dev wg-hybrid scope link metric 120`。
- `ip route get 10.77.60.9` は `wg-hybrid` を選択。
- `10.77.60.9/32` はローカルインターフェースに不在。
- `routerctl doctor hybrid` は `overall=pass`、`fail=0`。

## オンプレミス側

- `RemoteAddressClaim/cloud-client-10-77-60-7` は `Ready`。
- 捕捉タイプは `ens21` 上の `proxy-arp`。
- Proxy neighbor が存在: `10.77.60.7 proxy`。
- 配送ルートがインストール済み: `10.77.60.7 dev wg-hybrid scope link metric 120`。
- `ens21.proxy_arp=1`。
- `routerctl doctor hybrid` は `overall=pass`、`fail=0`。

## 接続性

- クラウドクライアントからオンプレミスクライアントへの ping: 3/3 受信、0% loss。
- オンプレミスクライアントからクラウドクライアントへの ping: 3/3 受信、0% loss。
- クラウドクライアントからオンプレミスクライアントへの SSH がソース保持で成功:
  `SSH_CONNECTION=10.77.60.7 ... 10.77.60.9 22`。
- オンプレミスクライアントからクラウドクライアントへの SSH がソース保持で成功:
  `SSH_CONNECTION=10.77.60.9 ... 10.77.60.7 22`。
- NAT は観測されなかった。
- クライアントのデフォルトゲートウェイは変更なし。

## クリーンラン硬化チェック

- Azure Ubuntu が routerd 起動前に `10.77.60.9/24` を `eth0` に再導入。
- routerd `439ec316` が手動の `ip addr del` 回避策なしでそのアドレスを de-assign。
- routerd が以前の手動 `/dev/stdin` 回避策なしで WireGuard を適用。
- エビデンスは Azure VM deallocate 前に取得。
- Azure VM はエビデンス取得後に deallocate; リソースグループは破棄せず。

## 既知のノート

- `routerd_filter` テーブルが利用不可の場合、FORWARD ポリシーの doctor チェックはスキップ; データプレーンスモークはそれでも通過。
- router06 のグローバルステータスは `Pending` のままだが、`doctor hybrid` は通過し SAM データプレーンパスは健全。
- 定常状態での `captureDeassignedOSAddress.deassigned=false` は、そのリコンサイル中に削除すべきアドレスがなかったことを意味する。`enforced=true` + ローカルアドレスの doctor 通過が関連するアサーション。
