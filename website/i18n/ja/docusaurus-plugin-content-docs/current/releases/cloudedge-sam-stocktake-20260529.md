# CloudEdge / SAM — マージ前棚卸し (Azure×PVE + AWS×PVE + OCI×PVE スモーク)

日付: 2026-05-29 (2026-05-30 OCI×PVE で更新) · ブランチ `cloudedge-mvp` · 目的:
3 回のクリーンスモーク中に観測された手動介入、設定エルゴノミクス、routerd の機能ギャップの
棚卸し。experimental な main マージとフォローアップのスコープを定める。

## 1. スモーク中の手動回避策 — すべて routerd ネイティブで解決済み

| 回避策 (当時は手動) | 解決 |
|---|---|
| Azure: セカンダリ `/32` がゲスト OS に自動追加 (cloud-init/netplan) → `ip addr del` + suppress | **#41 / 439ec316** — provider-secondary-ip de-assign 強制 |
| Azure: `wg setconf <tempfile>` EACCES → `/dev/stdin` | **#43 / 439ec316** — WireGuard の stdin 経由適用 |
| Azure: 古い `routerd_filter` nft テーブルが転送をドロップ → 手動削除 | **#42 / 439ec316** doctor 警告 + ドキュメント; **#47 / f60e7d9a** nft ownership 診断 |
| `routerctl describe` に `-o` なし → プレーン出力 | **#45 / 40a99208** |
| AWS: セカンダリ `.9` が一時的に OS で見えた | **手動ステップなし** — routerd de-assign (#41) が自動処理 (修正がプロバイダー間で汎用化されることを検証) |
| OCI: 低 PMTU アンダーレイで TCP ブラックホール (ping OK、SSH/scp タイムアウト) | **#53 / 3c540656** — PMTU/MSS clamp を FirewallZone 非依存 + タイプ非依存に変更; SAM 転送パスに対して `routerd_mss` を導出 (`hybrid.EstimateMTU` 経由で MSS 1300)。#50 が予測。 |
| OCI: Ubuntu イメージのデフォルト `iptables` reject-all FORWARD/INPUT が WG/オーバーレイ転送をブロック | **#52** — `doctor hybrid` が検出 + 必要なホストルールを提示; ホストファイアウォールはホスト側で対処 (routerd は自動プロビジョニングせず警告のみ) |

→ スモーク時の routerd レベルの修正はすべて routerd 自身が処理するようになりました。AWS の実行では不要でした。OCI の実行では #53 PMTU/MSS ギャップ (実際のバグ、routerd コアで修正済み) と #52 ホストファイアウォール前提条件 (設計上ホスト側、doctor で検出) が発見されました。

## 2. ホスト/クラウドブートストラップ — 手動 (デプロイメントギャップ、大部分は routerd コア外)

- routerd tarball のビルド/コピー/インストール、systemd ユニットの作成/有効化、ライブ設定の配置、
  validate/dry-run/apply の実行 — 手動。将来: ラボブートストラップスクリプト / ゴールデンイメージ;
  既存の OS ブートストラップ自動化の発見に関連。(フォローアップ。)
- ランタイム前提条件 (`wireguard-tools`、`tcpdump`、`jq`、`curl`) のインストール — 手動;
  routerd のランタイム前提条件としてドキュメント化 / パッケージングで対処すべき。(フォローアップ。)
- AWS: user-data apt がミラー同期失敗にヒット → 手動 `apt` リトライ (ラボブートストラップの脆弱性)。
- AWS: PVE router07 の DHCP/guest-agent 前提が失敗 → 静的 mgmt IP で再作成
  (PVE ラボ自動化、routerd ではない)。

## 3. 設定エルゴノミクス (設定記述の粗削りな部分) — アクション可能

- **WireGuardPeer.allowedIPs を捕捉対象の `/32` (+ オーバーレイ `/32`) と手動一致させる必要がある** —
  `RemoteAddressClaim` との暗黙的結合; 間違えやすい (広い allowedIPs の問題)。
  候補: WG peer の allowedIPs が各配送 `/32` をカバーしているかの validation / `doctor` クロスチェック
  (または自動導出)。**最も価値の高いエルゴノミクス修正。** (フォローアップ。)
- `nicRef`: Azure のフル ARM ID vs AWS の ENI ID — プロバイダー形式の違い、手動ルックアップ、
  エラーを起こしやすい。候補: プロバイダー別ドキュメント + 軽量バリデーション。(フォローアップ。)
- `capture.interface` (proxy-arp) は実際の OS NIC 名 (ens21/eth1) でなければならない — 手動確認。
- オーバーレイ `/32`、共有サブネット、`ownerSide`、`domain.peerRef` vs `delivery.peerRef` は
  手動で整合させる必要がある; 2 つの peerRef は部分的に冗長。(フォローアップ: 簡素化/明確化。)
- `configureOSAddress=false` のセマンティクスは #41 以前は曖昧だった (現在は "routerd が
  OS ローカルでの不在を強制" として明確化)。
- `doctor` の FORWARD ポリシースキップは Azure では読みにくかった (`exit status 1`); AWS では改善。

## 4. WireGuard 鍵プロビジョニング — 手動

- private/public 鍵の生成、配置、公開鍵の交換はすべて手動; routerd は `privateKeyFile` のみ読み取り。
  候補: 不在時の自動生成 + 交換用の公開鍵公開。(フォローアップ。)
- (ラボ SSH 鍵はクライアント発信の SSH エビデンス用にクライアントに一時配置後、削除 — テストハーネスのみ、
  routerd スコープ外。)

## 5. プロバイダープロビジョニング — 設計上手動 (routerd MVP のスコープ外)

- Azure: RG/VNet/サブネット/NSG/パブリック IP/NIC/VM/ディスク、NIC セカンダリ `.9`、NIC IP フォワーディング、
  起動/deallocate — 設計上手動 (MVP ではクラウド API ミューテーションなし; actionPlan /
  CloudProviderProfile が将来のフック)。
- AWS: VPC/サブネット/IGW/ルートテーブル/SG/EIP/EC2/ENI セカンダリ `.9`、source/dest check 無効、
  停止 — 設計上手動。
- PVE: VM/ブリッジ/NIC — ラボインフラ、設計上手動。

## experimental マージに向けた要点

- データプレーンとスモーク時の修正は routerd ネイティブであり、**3 つのクラウド**
  (Azure / AWS / OCI) で検証済み、すべてクリーン。
- マルチクラウドテストの効果: OCI の低 PMTU アンダーレイが **routerd コアの実際のバグ** を発見
  (#53 — PMTU/MSS clamp が FirewallZone にゲートされていたため、SAM はどのクラウドでもクランプなし;
  アンダーレイ PMTU が十分に低い場合にのみブラックホールとして顕在化)。修正は汎用的
  (FirewallZone 非依存 + インターフェースタイプ非依存) かつホームルーターでも安全。
- 残りの手動作業は **設計上の手動 (プロバイダープロビジョニング、MVP スコープ外)** か
  **experimental の粗削りな部分** (allowedIPs/nicRef/peerRef/鍵に関する設定エルゴノミクス、
  ホストブートストラップ、OCI ホストファイアウォール前提条件 #52)。これらは
  **experimental** ラベルの根拠であり、マージブロッカーではなくフォローアップとして追跡。
