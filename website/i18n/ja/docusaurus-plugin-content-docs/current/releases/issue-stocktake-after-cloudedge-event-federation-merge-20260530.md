# Issue 棚卸し: CloudEdge SAM + Event Federation マージ後 (2026-05-30)

ステータス: 読み取り専用の棚卸し。issue のクローズ、コメント、ラベル付け、作成は行っていません。
以下のすべてのアクションは orchestrator またはユーザーが適用するための**提案**です。

## 概要

PR #54 (`event-federation → main`) は**マージ済み**（マージコミット `baeaff16`）。
`cloudedge-mvp` の厳密なスーパーセットであり、CloudEdge/SAM + Event Federation Phase 1 / 1.5 / 2 / 3 を **experimental** としてランディングしています（リリースタグなし、安定版への昇格なし）。
PR #49 (`cloudedge-mvp → main`) は #54 により **superseded としてクローズ**されています。

4 つの issue がオープンのまま残っています（#50、#51、#52、#53）。すべて CloudEdge SAM であり、いずれも現在は古くなった `branch cloudedge-mvp` ラベルを持っています。

- **#53 と #50 は事実上解決済み**。マージされた zone 非依存 PMTU/MSS clamp（コミット `3c540656`）による。両方の codex OCI×PVE 再テストコメントが PASS を確認（routerd_mss 存在、MSS 1300、doctor hybrid PASS、ping/SSH/scp すべてパス）。クローズ可能。
- **#52 は部分的に解決**。マージされた `doctor hybrid` が reject-all FORWARD/INPUT ホストファイアウォールを検出して警告するようになったが、ドキュメント成果物（OCI イメージファイアウォールブートストラップを SAM how-to に記載）が残りのオープン部分。ドキュメントフォローアップとしてオープン維持するか、doctor 警告で十分と判断すればノート付きクローズ。
- **#51（wizard OCI プロバイダー）はマージの影響を受けない**。wizard はラボプロトタイプでありコアではなく、OCI プロバイダー生成は追加されていない。Phase 4.1 の自然な候補としてオープン維持。

オープンまたはクローズ済みの issue のいずれも Phase 4.0 最小権限 plugin コンテキストフレームワーク（Plugin コンテキスト allowlist + シークレットリダクション）をブロックしません。この作業はグリーンフィールドであり、**新規** issue として起票が必要です。

## マージ済みベースライン

- main = `baeaff16`（PR #54 マージコミット）。
- PR #54 マージ 2026-05-30T12:20。"Experimental: CloudEdge SAM + Event Federation Phase 1-3"。
- PR #49 クローズ（#54 により superseded）。
- Experimental: **リリースタグなし**、安定版への昇格なし。
- 関連するマージ済みコミット:
  - `3c540656`: SAM 転送パス用の zone 非依存 PMTU/MSS clamp（#53）+ doctor hybrid PMTU/ファイアウォールチェック（#52）。`pkg/render/mtu.go`、`cmd/routerctl/doctor.go`、golden SAM fixture、`docs/adr/0006-event-federation.md` に影響。
  - `713233b0`: OCI×PVE SAM クリーンスモーク + 3 クラウドパリティ記録。
  - Event Federation Phase 1→3 チャンク（`9c785db8` ... `515fe7e8`）、Phase 2/3 スモークエビデンス（`docs/releases/evidence/cloudedge-event-federation-{transport,subscription}-20260530.md`）。
- クローズ済みコンテキスト issue: #41-#48（および以前の #2-#40）は SAM や以前の作業中にクローズ。特に #12（"MSS clamp can raise lower MSS / ignores source iface MTU"）と #9（"routerd_mss reported as orphan"）は #50/#53 の背景にある歴史的 MSS 系譜。#42（"forwarded /32 dropped by FORWARD policy -- doctor visualize"）と #48（"doctor hybrid classify FORWARD skip reasons"）は #52 の doctor 作業のクローズ済み前身。

## Issue 分類テーブル

| # | タイトル | 現在のステータス | #54 マージの影響 | 推奨アクション | クローズ可能? |
|---|---------|-----------------|-----------------|---------------|--------------|
| 53 | SAM OCI: TCP/SSH stalls after ping without MSS handling | OPEN、ラベルなし。codex 再テストコメント = PASS | `3c540656` により**解決**（zone 非依存 clamp → MSS 1300。OCI 再テスト PASS） | **クローズ**（done-by-main-merge） | **はい** |
| 50 | SAM: surface/derive PMTU/MSS for wg-hybrid delivery paths | OPEN、`enhancement` + `branch cloudedge-mvp`。codex 再テスト = PASS | `3c540656` により**解決**（clamp 導出 + `doctor hybrid` PMTU/MSS 警告）。OCI 再テスト PASS | **クローズ**（done-by-main-merge）。クローズ前/時にラベル変更 | **はい** |
| 52 | SAM OCI: Ubuntu image iptables rejects WireGuard/FORWARD | OPEN、`documentation` + `branch cloudedge-mvp`。codex 再テスト = doctor 警告 + ラボブートストラップ | **部分的**: doctor が警告（`3c540656`）。**ドキュメント how-to** 部分はまだオープン | **維持**（ドキュメントフォローアップ）またはノート付きクローズ。ラベル変更 | いいえ（ドキュメント部分） |
| 51 | cloudedge-sam wizard: add OCI provider support | OPEN、`enhancement` + `branch cloudedge-mvp` | **影響なし**: wizard はラボプロトタイプでコア変更なし。OCI プロバイダー生成未追加 | **維持**（引き続き関連、Phase 4.1 候補）。ラベル変更 | いいえ |

分類マッピング:
- **done-by-main-merge**: #53、#50
- **docs-i18n / ドキュメントフォローアップ**: #52（残りのドキュメント部分）
- **still-relevant / phase4.1-follow-up**: #51、および #52 の doctor-FORWARD-pattern 拡張
- **phase4.0-blocker**: なし
- **superseded-by-#54**: PR #49（すでにクローズ）。issue はなし
- **obsolete-duplicate**: なし

## 推奨クローズ (ドラフトコメント)

### #53: done-by-main-merge としてクローズ
> `3c540656` により解決（PR #54 → main `baeaff16` でマージ）。PMTU/MSS clamp が FirewallZone にゲートされていた。SAM は zone なしの転送プレーンであるため clamp が導出されず、OCI の低 PMTU アンダーレイで ICMP はパスしつつ TCP がブラックホール化していた。修正により FirewallZone 非依存かつインターフェースタイプ非依存の MSS clamp を RemoteAddressClaim 配送パスに対して有効オーバーレイ MTU（約 1392 inner → MSS 1300）を使用して導出。OCI×PVE 再テスト PASS: `routerd_mss` 両側に存在（MSS 1300）、`doctor hybrid` PASS、双方向 ping/SSH（ソース保持）および 100MiB scp x3 すべてパス、3 クラウドクリーンパリティ。クローズ。（Experimental、リリースタグなし。）

### #50: done-by-main-merge としてクローズ
> `3c540656` により解決（PR #54 → main `baeaff16`）。SAM 配送パスがスコープ付き TCP MSS clamp を導出するようになり、`doctor hybrid` が PMTU/MSS ポスチャーを表示（SAM 配送パスに clamp がない場合に警告）。ここで要求された 2 つの動作そのもの。以前 `routerd_mss` が不在だった OCI 再テストが MSS 1300 を出力し、双方向 ping/SSH/scp がパス。クローズ。（ラベル変更ノート: `branch cloudedge-mvp` は古い。ブランチはマージ済みで PR #49 はクローズ。）

### #52: ドキュメントフォローアップとしてオープン維持（またはノート付きクローズ）
> `3c540656`（PR #54 → main `baeaff16`）により部分的に対処。`doctor hybrid` が wg/オーバーレイ転送をブロックする reject-all FORWARD/INPUT ホストファイアウォールを検出して警告するようになり、自動ミューテーションせずに必要なホスト設定を表示。再テストで確認: ブートストラップ前に doctor が警告、スコープ付きラボ allow ルール後に PASS。**まだオープン**: OCI Ubuntu イメージのファイアウォールブートストラップ前提条件（UDP/51820 INPUT、FORWARD `<vnic> ↔ wg-hybrid`）を CloudEdge SAM how-to に文書化。ドキュメントタスクとしてオープン維持。ラベルを `documentation` のみに変更。

### #51: オープン維持（Phase 4.1 候補）
> PR #54 では対処されていない。cloudedge-sam wizard はラボプロトタイプであり、マージ中にコアへの OCI プロバイダー生成は追加されていない。オープン維持。Phase 4.1 プロバイダー actionPlan plugin 作業（aws/azure/oci のプロバイダープロファイル生成）に自然に組み込まれる。ラベル変更: `branch cloudedge-mvp` を削除。

## 推奨ラベル変更: `branch cloudedge-mvp` は古い

4 つのオープン issue (#50、#51、#52、#53) すべてが `branch cloudedge-mvp` を持っています。そのブランチは PR #54 により main にマージ済みで、対応する PR #49 はクローズされているため、ラベルはもはや実在するブランチを指していません。

提案（ここでは適用**しない**）:
- #50、#53: クローズ時に `branch cloudedge-mvp` を削除。
- #51: `branch cloudedge-mvp` を削除、`enhancement` を維持（将来的に Phase 4.1 / cloudedge 追跡ラベルが導入されれば追加）。
- #52: `branch cloudedge-mvp` を削除、`documentation` を維持。

将来の追跡用に、ブランチスコープのラベルに代わる安定的な `cloudedge` または `event-federation` ラベルの導入を検討。

## 推奨される新規フォローアップ issue（ドラフト、未作成）

1. **i18n: Event Federation how-to + リファレンスの ja/zh 翻訳**
   event-federation-subscription how-to と federation リファレンスページをドキュメントロケールポリシーに従い ja（正本）および zh-Hans/zh-Hant に翻訳。Phase 3 マージ後は現在英語のみ。（新規 issue。既存一致なし。）

2. **FreeBSD rc.d による `routerd-eventd` の supervision**
   Phase 2 で controller/systemd 経由の EventGroup 自動 supervision を追加（`1791cd5a`）。FreeBSD ルーター（router04 パリティ）で `routerd-eventd` が supervised されるよう FreeBSD rc.d 相当を追加。（新規 issue。既存一致なし。）

3. **EventSubscription batchWindow / debounce 精密タイマー**
   Phase 3 の EventSubscriptionController は poll + dedup で動作する。plugin 呼び出し前にバースト的なイベントが決定的に合体するよう精密な debounce/batchWindow タイマーを追加（ADR 0006 のヒステリシスおよびアンチフラップ不変条件に関連）。（新規 issue。）

4. **Observer セルフ捕捉不変条件（Phase 4 ループ防止）**
   ルーターが自身で捕捉したアドレスのイベントを再 emit しないという ADR 0006 不変条件を強制。プロバイダー plugin がクラウド状態をミューテーションし始める前に observe→federate パスにリグレッションテストおよびガードを追加。（新規 issue。Phase 4 前提条件。）

5. **ラボクリーンアップ: router03 / router05 が `515fe7e8` バイナリのまま残置**
   Phase 3 ラボスモークのバイナリが router03/router05 にデプロイされたまま。マージ済み main のアーティファクト（または推奨安定ビルド）へ再デプロイし、ラボルーターが experimental コミットに取り残されないよう追跡。（新規 issue。ラボクリーンアップ。）

6. **Phase 4.1: プロバイダー actionPlan plugin（aws/azure/oci）dry-run**
   RemoteAddressClaim をプロバイダー API コール（AWS/Azure/OCI セカンダリ IP 割り当て）に変換するプロバイダー actionPlan plugin を実装し、dry-run/observe-only から開始。**#51 を包含**（wizard OCI プロバイダー生成）するため重複起票せず、#51 を OCI スライスとしてリンク。（新規 issue。Phase 4.1。）

7. **Phase 4.0: Plugin コンテキスト allowlist + シークレットリダクション**
   最小権限 plugin コンテキストフレームワーク。リダクションポリシー A: インラインシークレットをリダクト、シークレットファイルパスを省略、`SecretValueSourceSpec` を省略、完全な `router.yaml` を公開しない、プロバイダー認証情報を公開しない、コンテキスト層からのプロバイダーミューテーションなし。すべてのプロバイダーミューテーション plugin の **Phase 4.0 ブロッカー**。（新規 issue。既存一致なし。グリーンフィールド。）

## Phase 4.0 ブロッカー（明示）

**4 つのオープン issue（#50-#53）のいずれも Phase 4.0 をブロックしません**（最小権限 plugin コンテキスト allowlist + シークレットリダクション、偶発的なプロバイダーミューテーションおよび認証情報漏洩の防止）。クローズ済み issue（#2-#48）のスキャンでも plugin コンテキスト、シークレット漏洩、認証情報リダクションに関する issue は**見つかりませんでした**。Phase 4.0 フレームワークはグリーンフィールド作業であり、プロバイダー actionPlan plugin（Phase 4.1）がミューテーションを許可される前に上記ドラフト #7 として起票が必要です。

## Phase 4.1 候補

- **#51**: wizard OCI プロバイダーサポート → プロバイダープロファイル生成にフィード。Phase 4.1 プロバイダー actionPlan plugin issue（ドラフト #6）に包含。
- **#50 / #52 / #53**: すでに解決済みだが、PMTU/ファイアウォールのプロバイダーコンテキスト知識（有効オーバーレイ MTU、ホスト FORWARD ポスチャー）は Phase 4.1 プロバイダー plugin が Phase 4.0 コンテキスト allowlist を通じて提示する必要があるデータに示唆を与える。再オープン不要。設計入力として参照。

## 安定昇格なし、リリースタグなし

CloudEdge SAM + Event Federation 作業は PR #54 により **experimental のみ**として main にランディングしています。**リリースタグなし**であり、安定版への**昇格なし**。リリースタグ付与はユーザーの判断であり、この棚卸しのスコープ外です。
