# ADR 0011: 汎用フェイルオーバー（活性駆動 seize、クロスプロバイダーアクションパリティ）

![ADR 0011 の図。アクティブマーカーとスタンバイ適格性の入力から、routerd の seize 判断を経て、プロバイダーまたはオンプレミスの捕捉復旧まで](/img/diagrams/adr-0011-generalized-failover.png)

## ステータス

提案済み。実験的実装として承認（2026-06-01）。

[ADR 0010: 捕捉所有権アービトレーション](../adr/0010-capture-ownership-arbitration.md)
（所有権マップ + `ownershipEpoch`）を消費し、
[ADR 0008](../adr/0008-capture-coordination-fencing.md) の Phase C として延期されていた
フェイルオーバーを実現する。issue #74 に対応。実験的。

## 背景

CloudEdge は現在、**協調的ドレイン**（`maintenance.drain`）でのみ捕捉を移動する。
**活性/ヘルス駆動のプロモーション**はなく、プロバイダーごとのアクション
（secondary IP の assign/unassign、フォワーディング）は AWS のみで、Azure/OCI/オンプレミスは
薄いか存在しない。
#74 は、AWS / Azure / OCI / オンプレミス（VRRP/keepalived）を横断する
1 つのフェイルオーバーフレームワークを求めている。
L3 の継続性（スタンバイのプロモーションにより捕捉されたアドレスが引き続き提供される）を、
統一されたスプリットブレイン/フラップ防御で実現する。

ADR 0010 が所有権のプリミティブ（収束したオーナーマップ + `ownershipEpoch` フェンシング）を
提供する。
この ADR は**活性 → desired-owner → seize** ループと
**プロバイダー非依存のアクション層**を追加する。

### プロバイダーの reassignment セマンティクス（調査済み、seize の設計に反映）

- **AWS**: `assign-private-ip-addresses --allow-reassignment` が secondary IP を
  別の ENI に移動する。**非同期**（インスタンスメタデータ `local-ipv4s` で確認）、
  last-writer-wins、関連 EIP も移動する。
- **OCI**: `assign-private-ip --unassign-if-already-assigned` が同一サブネット内の
  別 VNIC に強制的に reassign する。last-writer-wins。パブリック IP も移動する。
- **Azure**: 単一のアトミック reassign はない。**旧 NIC から ipConfig を削除 +
  新 NIC に追加**（2 操作。ETag/If-Match による楽観的同時実行制御が利用可能）。

したがって reassignment は**普遍的にアトミックではない**（AWS は非同期、Azure は 2 操作）。
フェイルオーバーは**実験的であり、プロバイダーの assign セマンティクス + `ownershipEpoch`
フェンシング +（Phase 4）クラウドインベントリのドリフト reconciliation に依存する**。
ロックには依存しない。

## 決定

### 統一された適格性と活性モデル

desired オーナー（ADR 0010 のアービトレーション）は**適格な**メンバーに対して計算される。
適格性は以下の交差で決まる。

- `maintenance.drain == false`（ドレイン済みなら即座に除外）
- **ハートビートが新鮮であること**: 各メンバーが定期的に活性/ハートビートフェデレーションイベントを発行する。
  期限切れのハートビート（TTL）は**プロモーションホールド後**に不適格とする（後述）。
- `HealthCheck` が失敗していないこと（ポリシーに従う）
- オンプレミス：**VRRP マスター**権限シグナル（`activeWhen{vrrp-master}`、
  `sam.EvaluateCaptureGate`）で、非マスターは fail-closed。

活性は**ストリーム相対**で評価する。各ノードの wall clock ではない。
「now」はプールのフェデレーションストリームで観測された**最大イベント時刻**
（`streamMaxObservedAt`）であり、メンバーは
`lastHeartbeat(node) + heartbeatTTL + promotionHoldDuration <= streamMaxObservedAt`
のとき stale となる。
同じストリームを見た全ノードが同じ判定を計算するため、
適格セット（したがってオーナーマップ、ADR 0010）は活性が追加されても
**決定的に収束したまま**である。
送信側のクロックスキューは `heartbeatTTL + promotionHoldDuration` で吸収される。
射影はローカルクロックに対して未来のタイムスタンプをクランプ**しない**（非決定的になるため）。
未来のスキューは status/`doctor` 経由で可視化する。
完全に停止したストリームはフェイルオーバーも停止するが、これは正しい（「観測なしに障害を宣言しない」）。
生存メンバーがいる接続コンポーネントはストリーム時刻を進め続ける。
**プロモーションホールド**は一時的なギャップを吸収しフラッピングを抑制する。
`maintenance.drain` は**即座の**除外のまま（協調的なのでホールド不要）。

### Phase 2 の実装判断（2026-06-01 確定）

- **ハートビートイベント**: タイプ `routerd.mobility.member.heartbeat`、group =
  `MobilityPool.groupRef`、payload `{pool, node, emittedAt, seq}`。
  **mobility コントローラー**が reconcile tick で発行する。**`autoFailover: true` の
  プールのみ**、かつ自ノード（クラウド `provider-secondary-ip` ロール）のみ。
  `heartbeatInterval` でレート制限する。staleness 判定にはイベントの `ObservedAt` を使用する。
  `lastHeartbeat` はリースと同じ射影済みイベントストリームから導出される
  （wall-clock の混入なし）。
- **ホールドフィールド**は `ipOwnershipPolicy` の下にフラットに配置する。
  `heartbeatInterval` / `heartbeatTTL` / `promotionHoldDuration`（duration 文字列）。
  リースのオーナー変更ホールドとは別。
  専用の状態テーブルは不要。適格性は純粋な
  `lastHeartbeat + ttl + hold <= streamMaxObservedAt` テストとなる。
  バリデーションは `autoFailover` が true のとき `heartbeatInterval`/`heartbeatTTL` を必須とし、
  `heartbeatTTL >= heartbeatInterval` を要求する。
- **Seize アクション**: 既存の `assign-secondary-ip` verb に `allowReassignment`
  パラメーターを追加する（新しい verb ではない）。stale/dead な前オーナーが自身で
  `unassign` できないとき、新オーナーがアドレスを取得するために設定する。
  AWS エグゼキューターはこれを `--allow-reassignment` にマップする。`ActionPlan` の
  description/risk は seize/reassign として読める。`ownershipEpoch` の
  スタンプ/フェンシングは ADR 0010 から変更なし。
- **`autoFailover` ゲート**: ハートビートの staleness は **`autoFailover: true`
  のときのみ**アービトレーション適格性に入る。未設定/false のプールは現行動作を維持する
  （ドレインのみがオーナー変更を駆動）。#76 Phase 1 / SAM / captureEpoch パスには
  影響なし。ハートビートは `autoFailover: true` のプールでのみ発行/消費される。
- **スコープ**: Phase 2 はクラウド `provider-secondary-ip` + **AWS** seize のみ。
  オンプレミス（proxy-ARP / VRRP マスター）と Azure/OCI reassign エグゼキューターは Phase 3。
- **既知のフォローアップ**: ハートビートイベントには TTL/expiry がないため、
  停止したメンバーの最後のハートビートは staleness 判定のために観測可能なまま残る。
  結果としてハートビート行は蓄積され prune されない
  （後の hygiene パスで追跡する。stale 判定が依存する最後のハートビートを prune してはならない）。

### 活性駆動 seize

適格オーナーが変わったとき（ドレイン、ハートビート期限切れ、ヘルス障害）、
`ownershipEpoch` がバンプし、**新オーナーが seize する**。
secondary IP の reassignment 付き acquire をプロバイダーに発行し、フォワーディングを有効にする。
旧オーナーのアクションは stale epoch を持ち、ゲートでフェンスされる。
`autoFailover`（ADR 0010 `ipOwnershipPolicy`）がこれを自動にするかをゲートする。

### プロバイダー非依存のアクション層

- **プランナーがプロバイダー非依存の所有権/アクション意図を発行する**（desired な
  `(owner, address, verb)` セット + `ownershipEpoch`）。**エグゼキューターがプロバイダーの
  差分を保持する**（AWS `--allow-reassignment`、OCI
  `--unassign-if-already-assigned`、Azure remove+add）。これは既に AWS で使われている
  共通の `ActionPlan` + エグゼキューター契約を汎用化したもの。
- **オンプレミスはクラウドプロバイダーではない**。その「アクション」はローカルデータプレーン
  （proxy-ARP/GARP/VIP）であり、プロバイダー API 呼び出しではなく、
  オンプレミスエグゼキューター / SAM-GARP ブリッジとして扱う。

## フェーズ分割（この ADR）

- **Phase 2**: クラウド活性フェイルオーバー。ハートビートイベント + TTL +
  プロモーションホールド + 統一適格性、`ownershipEpoch` バンプ、
  **クラウド secondary-IP seize**（AWS 先行、実証済みパス）、`autoFailover` ゲート。
  L3 が途切れないこと（プロモーション後にスタンバイがアドレスを提供する）の
  強制障害 CI/lab テスト。
- **Phase 3**: プロバイダーアクションパリティ。Azure（remove+add ipConfig）と
  OCI（`--unassign-if-already-assigned`）エグゼキューター。オンプレミスエグゼキューター /
  SAM ブリッジによる VRRP/GARP 統合で、VRRP/keepalived フェイルオーバーを同じポリシーでカバーする。
- **Phase 4**: クラウドインベントリ observe capability（`describe-secondary-ips`）→
  ドリフト/孤立/競合検出を status + `doctor` で可視化し、
  実験的 seize を reconcile 済み所有権に硬化する。所有権マップの管理 API。

## 結論

- 1 つのフェイルオーバーフレームワークがプロバイダーを横断する。活性/ヘルス/メンテナンス/VRRP が
  統一された適格性モデルに入力される。プランナーはプロバイダー非依存。
  プロバイダーごとの差異はエグゼキューターに閉じ込める。
- L3 の継続性はスタンバイのプロモーション + 捕捉済み IP の seize で達成され、
  `ownershipEpoch` でフェンスされる。既知の限界（コンセンサスなし、
  プロバイダー reassignment は普遍的にアトミックではない）はドキュメントされ、
  クラウドインベントリ（Phase 4）がドリフトギャップを埋める。
- オンプレミスはクラウドプロバイダーの型に押し込まれることなく統合される。
