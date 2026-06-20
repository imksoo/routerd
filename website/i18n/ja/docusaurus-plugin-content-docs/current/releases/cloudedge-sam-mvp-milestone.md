# CloudEdge / Selective Address Mobility: experimental MVP、マルチクラウドラボ検証済み

ステータス: **experimental**（ラボ検証済み、安定版として非推奨）
ブランチ: `cloudedge-mvp` · 日付: 2026-05-29（2026-05-30 更新: OCI 追加で 3 クラウドパリティ）

## 概要

CloudEdge **Selective Address Mobility**（SAM）MVP は **3 つのクラウド** でマルチクラウドラボ検証済みです。
Azure x PVE、AWS x PVE、OCI x PVE のすべてが同一サブネットの /32 モビリティスモークをパスしました。
クラウド VM（`.7`）とオンプレミス/PVE VM（`.9`）が **routerd 間 WireGuard オーバーレイ経由で双方向に（ping + SSH + 100 MiB scp、ソース保持）NAT なしかつクライアントのデフォルトゲートウェイを変更せずに** 通信し、同一の論理サブネット上にあるように見えました。

これは **完全な L2 拡張ではありません**。
「SAM」は選択された /32 IPv4 アドレスを捕捉し、ソースおよび宛先アドレスを保持したままオーバーレイ経由で配送します。

## 検証結果

| シナリオ | 結果 | エビデンス |
|---|---|---|
| Azure x PVE 同一サブネット /32 モビリティ | PASS / clean | `docs/releases/evidence/cloudedge-sam-azure-pve-20260529.md` |
| AWS x PVE 同一サブネット /32 モビリティ | PASS / clean（Azure パリティ、初回実行） | `docs/releases/evidence/cloudedge-sam-aws-pve-20260529.md` |
| OCI x PVE 同一サブネット /32 モビリティ | PASS / clean（PMTU/MSS clamp 修正 #53 後） | `routerd-labs/cloudedge-sam/evidence/20260530T031247Z-oci-pve-hardening-43a64c55/summary.md` |

3 回の実行すべてがパスしました。
AWS は **AWS 固有のコード変更なし** で初回実行からパスしています。

OCI は当初、低 PMTU アンダーレイで TCP がブラックホールしました（ping は通過、SSH/scp はタイムアウト）。
これは #50 が予測したとおりの障害です。
PMTU/MSS clamp が `FirewallZone` に依存しており、「SAM」（純粋な転送プレーン）は FirewallZone を定義しないため、どのクラウドでも `routerd_mss` clamp が導出されませんでした。
修正（#53）により clamp を **FirewallZone 非依存かつインターフェースタイプ非依存** にしました。
`hybrid.EstimateMTU` 経由の有効オーバーレイ MTU を使用して、オーバーレイトンネル MTU が実質的な低下である転送配送パスに対して MSS clamp を導出します（OCI で MSS 1300）。
ホームルーター（PPPoE/DS-Lite）は変更なし（`RemoteAddressClaim` なしのため転送パスセットが空であり、zone 出力は同一）。
修正後、OCI x PVE は `routerd_mss` が両側で存在し `doctor hybrid` PASS でクリーンです。

## 実証された抽象化

- **捕捉（プロバイダー固有）**: Azure NIC セカンダリプライベート IP + NIC IP フォワーディング、AWS ENI セカンダリプライベート IPv4 + EC2 source/destination check 無効、OCI VNIC セカンダリプライベート IP + `skipSourceDestCheck=true`。
- **配送、claim、doctor（routerd 共通）**: `RemoteAddressClaim` → `wg-hybrid` 経由の `/32` 配送ルート、オンプレミス proxy-ARP リターン捕捉、NAT なし、ソースおよび宛先保持、`routerctl doctor hybrid`。provider-secondary-ip の de-assign 強化と WireGuard stdin apply は両クラウドで汎用化済み。

## このブランチの内容（cloudedge-mvp、main との差分）

- Dynamic-config 基盤: `DynamicConfigPart` / mask ディレクティブ / `DynamicOverridePolicy`。effective-config = startup + active dynamic parts - masks。
- Plugin runner（observe-only, dry-run）: `Plugin` / `DynamicConfigSource` / `PluginResult`。actionPlans は表示専用。
- L3 hybrid: `OverlayPeer` / `HybridRoute`（既存の IPv4Route install に lowered）。
- Selective Address Mobility: `AddressMobilityDomain` / `RemoteAddressClaim` / `CloudProviderProfile`。Linux データプレーン（proxy-ARP 捕捉 + /32 オーバーレイ配送 + provider-secondary-ip OS アドレス de-assign）、`routerctl doctor hybrid`。
- nftables ownership marking（stale-table 診断用）。

## スコープと既知の制限（experimental であり安定版でない理由）

- クラウドプロバイダー API ミューテーションなし（セカンダリ IP 割り当てやルートテーブルはプロビジョニング側または手動。actionPlans は表示専用）。
- 「SAM」ライブデータプレーンは Linux のみ。
- 完全な L2、EVPN、BUM、ブロードキャストドメイン拡張なし。
- GCP は未検証（Azure、AWS、OCI は検証済み。OCI は 2026-05-30 追加）。
- OCI Ubuntu イメージはデフォルトで `iptables` の reject-all FORWARD/INPUT を持ち、WG およびオーバーレイの転送パスをブロック（#52）。`doctor hybrid` で検出、ホスト側で修正（ホストファイアウォールは routerd コアのスコープ外であり、routerd は自動プロビジョニングせず警告のみ）。
- 本番トポロジーのバリエーションは未検証。
- 設定のエルゴノミクスに粗削りな部分と手動ブートストラップおよび鍵手順が残存（例: WG `allowedIPs` はキャプチャ対象の `/32` と手動一致が必要。WireGuard の鍵とホスト package/systemd ブートストラップは手動）。
  完全な一覧はマージ前棚卸し `docs/releases/cloudedge-sam-stocktake-20260529.md` を参照。
  スモーク時の手動修正はすべて routerd ネイティブに移行済み（#41/#42/#43/#45/#47）。
  残りの項目は設計上のプロバイダープロビジョニングまたは追跡される experimental フォローアップです。

## 推奨

**experimental** な CloudEdge/SAM MVP 機能として `main` にマージ（experimental として文書化）。
安定昇格およびリリースタグは追加検証まで保留。
