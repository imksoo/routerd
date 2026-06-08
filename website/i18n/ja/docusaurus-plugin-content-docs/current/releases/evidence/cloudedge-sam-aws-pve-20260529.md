# CloudEdge SAM AWS x PVE スモークエビデンス

日付: 2026-05-29

ブランチ/ビルド: `cloudedge-mvp`、`routerd f60e7d9a`

Result: PASS (クリーン — 手動回避策なし; Azure パリティ)

これは Selective Address Mobility を 2 番目のパブリッククラウド (AWS VPC/EC2) に対して初回実行で検証したものです。AWS 固有のコード変更なしで、provider-secondary-ip キャプチャ + de-assign 強化と WireGuard stdin apply (Azure サイクルで実装) が設計どおり汎用化されました。プロバイダー固有の作業はプロビジョニング側 (AWS ENI セカンダリ IP + EC2 source/destination check 無効) のみです。

## トポロジー

- クラウドクライアント (AWS EC2): `10.88.60.7/24`
- オンプレミスクライアント (PVE VM): `10.88.60.9/24`
- クラウドルーター (AWS EC2): プライマリ `10.88.60.4/24`、ENI セカンダリキャプチャ `10.88.60.9`
- オンプレミスルーター (PVE、router07): `10.88.60.1/24` (`vmbr470`)
- オーバーレイ: `wg-hybrid`、`169.254.120.1/32` (クラウド) <-> `169.254.120.2/32` (オンプレミス)
- リージョン: ap-northeast-1。WireGuard: オンプレミス -> AWS パブリックエンドポイント、persistent keepalive。

## AWS キャプチャ前提条件 (プロビジョニング側)

- ENI: プライマリ `10.88.60.4`、セカンダリプライベート IPv4 `10.88.60.9`。
- EC2 source/destination check: DISABLED (Azure NIC IP フォワーディングの AWS 相当)。
- routerd-cloud ゲスト OS: routerd により `10.88.60.9` がローカルアドレスから削除
  (`provider-secondary-ip` + `configureOSAddress=false` de-assign 強制)。

## アサーション (すべて PASS)

- クラウド配送ルート: `10.88.60.9 dev wg-hybrid metric 120`。
- オンプレミス: `10.88.60.7` の proxy ARP; 配送ルート `10.88.60.7 dev wg-hybrid metric 120`。
- ステージ A: AWS ルーター NIC の tcpdump が `.7 -> .9` ICMP request/reply をキャプチャ。
- `.7 -> .9` ping 3/3 (0% loss); `.9 -> .7` ping 3/3 (0% loss)。
- SSH 双方向、ソース保持:
  - `SSH_CONNECTION=10.88.60.7 ... 10.88.60.9 22`
  - `SSH_CONNECTION=10.88.60.9 ... 10.88.60.7 22`
- NAT なし; 両クライアントのデフォルトゲートウェイ変更なし。
- doctor hybrid: AWS 側 overall pass (pass 10 / warn 0 / fail 0 / skip 1);
  PVE 側 overall pass (pass 13 / warn 0 / fail 0 / skip 1)。

## ノート

- AWS 固有の障害なし; 新規 issue の起票なし。
- Azure×PVE ペア (router06) は未変更。
- コスト: EC2 インスタンスはエビデンスキャプチャ後に停止 (再実行用に保持); EIP/EBS は完全 teardown まで残存。完全なローカルエビデンスバンドル:
  `routerd-labs/cloudedge-sam/evidence/20260529T233145Z-aws-pve-f60e7d9a`。
