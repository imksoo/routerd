# ADR 0010: Capture 所有権アービトレーション（マルチインスタンス所有権マップ + ownershipEpoch フェンシング）

![ADR 0010 の図。重複ホルダーのハザードから、ownershipEpoch と所有権マップの設計判断、VRRP または単一ルーターの capture ガードレールまで](/img/diagrams/adr-0010-capture-ownership-arbitration.png)

## ステータス

提案済み。実験的実装として承認 — 2026-06-01。

[ADR 0008: フェンシングトークンによる Capture 協調](../adr/0008-capture-coordination-fencing.md)
と [Selective Address Mobility](../reference/selective-address-mobility) データプレーンを
土台とする。issue #76 に対応。消費者は
[ADR 0011: 汎用フェイルオーバー](../adr/0011-generalized-failover.md)（#74）。
実験的。

## 背景

スケール時には単一のクラウドルーターが全ての capture 済み secondary IP を保持できない
（ENI/NIC/VNIC スロット制限）ため、`N+1` 構成の同一プロバイダールーターが capture された
アドレスを**分散**させる必要がある。現在の routerd には**クロスノード所有権マップも
排他制御もない**：

- 協調は**単一ノードのローカル射影**：各ノードが同じフェデレーションイベントストリームから
  独立に同じ `AddressLease` 状態に射影する
  （`pkg/controller/mobility/controller.go`）。**分散ロック、クォーラム、コンセンサスはない**。
- 「単一所有者」は*暗黙的*（capturePolicy `all-non-owner-sites` + 決定的
  `evaluatePlacement`）であり、`captureEpoch`
  （`pkg/state/mobility_capture_epoch.go`）は**ノードごと、(pool, address,
  captureDomain) ごと**の単調増加トークンで、インポート/実行ゲートで stale な
  プロバイダーアクションをフェンスする（ADR 0008）。
- 予約フィールド `MobilityPoolSpec.Authority` は未使用。

#76 は集中型の所有権マップ、競合排除、スプリットブレイン防止を求めている。
ADR 0008 は意図的に**コンセンサスを回避**し（Paxos/Raft/etcd）、
単調増加フェンシングトークン + プロバイダーの構造的な単一割り当て + 冪等な収束から
安全性を構築した。この ADR はその哲学を維持する。

### 「所有権」がコンセンサスなしで保証できること・できないこと（正直なスコープ）

これは **linearizable な分散ロックではない**。イベント順序アービトレーション +
フェンシング + クラウドの単一割り当てセマンティクスにより、以下を保証する：

1. 同じイベントストリームを見た全ノードが**同一のオーナーマップに収束する**；
2. ownershipEpoch *N+1* を見たノードは epoch-*N* のアクションを実行しない
   （ゲートでフェンスされる）；
3. クラウドの secondary IP は正確に 1 つの NIC に属するため、プロバイダー状態は
   **単一割り当てに収束する**。

**保証できないこと**：フェデレーションからパーティションされた（*N+1* を見ていない）
旧オーナーが、まだ生きている場合にプロバイダー API でアドレスを再取得すること —
これを排除するにはコンセンサス / STONITH / プロバイダーの条件付きフェンシングが必要だが、
追加しない。したがってプロパティは**「フェンス付き eventual 所有権 +
プロバイダー強制の単一割り当て」**であり、「スプリットブレイン防止」ではない。
オンプレミスの **proxy-ARP** はさらに弱い（プロバイダーの単一割り当てがない）：
上限は VRRP マスター権限 + fail-closed（ADR 0008 に従う）。

## 決定

### `ownershipEpoch` — (pool, address) ごとのクラスターフェンストークン

**`ownershipEpoch`** を導入する。`captureEpoch` よりも上位の概念：
(pool, address) ごとの単調増加トークンで、**確認されたオーナー変更時にのみ**インクリメントする
（リースが candidate/holding の間はインクリメントしない）。クラウド / オンプレミス /
プロバイダー / アクションを横断するフェンストークン。`captureEpoch` は
互換性/派生アノテーションとして保持する。正本は `ownershipEpoch` に移行する。

### 所有権マップ — リーダー不要の決定的収束

**選出されたリーダーはない**（リーダー選出にはコンセンサスが必要）。所有権マップは
各ノードがフェデレーションイベントストリームから決定的に構築する**収束したビュー**：

- 各 `(pool, address)` について、オーナーは決定的アービトレーションで選択される：
  **preferNodes → プレースメント優先度 → 安定タイブレーク**を*適格な*メンバーに対して適用
  （適格性は ADR 0011 で定義：ドレインされていない、健全、生存、該当する場合 VRRP マスター）。
- マルチインスタンス分散：プレースメントグループ内で各アドレスが 1 つのオーナーに
  アービトレーションされる。アドレスのセットは適格なメンバーに分散される
  （将来：最小負荷）。1 IP → 同時に 1 オーナー。
- マップは**可視化される**（status DB + メトリクス + control/`routerctl`）ので、
  オペレーターは「どの IP がどのノードに所有されているか」を確認できる —
  #76 が求める「集中型所有権マップ」を、単一ライターストアではなく
  収束したビューとして実現。

### `MobilityPool` の `ipOwnershipPolicy`

```yaml
spec:
  ipOwnershipPolicy:
    type: centralized          # 収束した決定的マップ（唯一のモード）
    epochLocking: true         # ownershipEpoch でアクションをスタンプ+フェンス
    preferNodes: [aws-router-a, aws-router-b]
    autoFailover: true         # ADR 0011（活性駆動 seize）が消費
```

`preferNodes` がアービトレーションにバイアスをかける。`epochLocking` が
ownershipEpoch フェンシングを有効にする。`autoFailover` は ADR 0011 が使うフック。
`type` は現在 1 つのモード（`centralized` = 収束した決定的）。

### アクション冪等性キー

プロバイダーアクションの冪等性キーは、少なくとも `pool / address / ownerNode /
ownershipEpoch / actionVerb / provider / nicRef` を含む。stale epoch または
間違ったオーナーのアクションが決定的にフェンスされる。

## フェーズ分割（この ADR）

- **Phase 1（この ADR の最小スコープ）**: `ownershipEpoch` トークン、
  決定的所有権レコード + アービトレーション（preferNodes/priority/tie-break）、
  `ipOwnershipPolicy` spec + バリデーション、**所有権マップの可視化**（status +
  メトリクス + `routerctl`）。**自動 seizure なし** — Phase 1 は desired 所有権を
  *計算・公開*し、ownershipEpoch でアクションをフェンスするのみ。
  既存の静的プレースメントが引き続き誰が行動するかを駆動する。
- 活性駆動のフェイルオーバー/seize は **ADR 0011**。

## 結論

- routerd は、コンセンサスストアを追加することなく、N+1 の同一プロバイダールーター間で
  capture 済み IP を分散するための、クラスター収束型でフェンス付きの所有権モデルを獲得する。
- 安全性のスコープは正直に述べられている（「フェンス付き eventual 所有権」であり、
  分散ロックではない）。クラウドは構造的に強く、オンプレミスは VRRP 権限のベストエフォート。
- `ownershipEpoch` は、ADR 0011 の seize と Phase 4 のクラウドインベントリ/
  ドリフト検出が構築する単一のクロスカッティングフェンストークン。
