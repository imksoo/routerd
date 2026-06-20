# CloudEdge Event Federation チェックポイント（Phase 1 + 1.5 完了）

ステータス: **experimental**（開発中、安定版として非推奨）
ブランチ: `event-federation` · チェックポイントコミット: `2bfd8b4d` · 日付: 2026-05-30

## 概要

CloudEdge Event Federation（ADR 0006）の Phase 1 と Phase 1.5 クリーンアップが `event-federation` 上で完了しました。
routerd 間の typed イベントバスのローカル専用基盤として、observed-fact エンベロープ、`EventGroup` Kind、SQLite ローカルストア、イベントの emit/list を行う CLI を実装しています。
**クロスノード配信はまだ含まれていません**。クロスノード配信は Phase 2 で実装します。

## このチェックポイントに含まれるもの

- `EventGroup` Kind（`federation.routerd.net/v1alpha1`）+ バリデーション。
- `federation.Event` エンベロープ（observed fact であり、設定でもコマンドでもない）、`Normalize`/`Validate`/`IsExpired` 付き。
- SQLite `federation_events` テーブル、冪等な `RecordFederationEvent`（`ON CONFLICT(id) DO NOTHING`）、フィルター付き `ListFederationEvents`（group フィルター + 読み取り時有効期限フィルター）。
- `routerctl federation event emit/list`（エイリアス `fed`）。
- ユニットテスト + CLI テスト。ADR 0006 を実装状態に合わせて更新。

ここで確定したセマンティクス（後続フェーズでリグレッションさせないこと）: ストアの冪等性はイベント **`id`** でキー付けします。
**`dedupeKey`** は subscription 側のグルーピングキーであり、Phase 1 では DB のユニーク制約ではありません。

## 次のステップ: Phase 2（トランスポート）

`routerd-eventd` + `EventPeer` によるオーバーレイ経由の push 配信 + HMAC + `event_deliveries` + retention prune を実装します。
Phase 2 の**スコープ外**: `EventSubscription`、plugin トリガー、`DynamicConfigPart` 生成、ARP/Clients observer、およびすべてのプロバイダーミューテーション（これらは Phase 3 以降）。

これはブランチチェックポイントノートであり、リリースタグ**ではありません**。
