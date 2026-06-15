# CloudEdge Event Federation — チェックポイント (Phase 1 + 1.5 完了)

ステータス: **experimental** (開発中; 安定版として非推奨)
ブランチ: `event-federation` · チェックポイントコミット: `2bfd8b4d` · 日付: 2026-05-30

## 概要

CloudEdge Event Federation (ADR 0006) の Phase 1 と Phase 1.5 クリーンアップが `event-federation` 上で完了しました。これは routerd 間の typed イベントバスのローカル専用基盤です: observed-fact エンベロープ、`EventGroup` Kind、SQLite ローカルストア、イベントの emit/list を行う CLI。**クロスノード配信はまだありません** — それは Phase 2 です。

## このチェックポイントに含まれるもの

- `EventGroup` Kind (`federation.routerd.net/v1alpha1`) + バリデーション。
- `federation.Event` エンベロープ (observed fact; 設定でもコマンドでもない)、
  `Normalize`/`Validate`/`IsExpired` 付き。
- SQLite `federation_events` テーブル、冪等な `RecordFederationEvent`
  (`ON CONFLICT(id) DO NOTHING`)、フィルター付き `ListFederationEvents`
  (group フィルター + 読み取り時有効期限フィルター)。
- `routerctl federation event emit/list` (エイリアス `fed`)。
- ユニットテスト + CLI テスト; ADR 0006 を実装状態に合わせて更新。

ここで確定したセマンティクス (後続フェーズでリグレッションさせないこと): ストアの冪等性はイベント **`id`** でキー付け; **`dedupeKey`** は subscription 側のグルーピングキーであり、Phase 1 では DB のユニーク制約ではない。

## 次: Phase 2 — トランスポートのみ

`routerd-eventd` + `EventPeer` によるオーバーレイ経由の push 配信 + HMAC +
`event_deliveries` + retention prune。Phase 2 の **スコープ外** を明示:
`EventSubscription`、plugin トリガー、`DynamicConfigPart` 生成、
ARP/Clients observer、およびすべてのプロバイダーミューテーション (これらは Phase 3 以降)。

これはブランチチェックポイントノートであり、リリースタグ **ではありません**。
