# CloudEdge + Event Federation マージ前棚卸し (event-federation → main)

ステータス: **experimental** (ラボ検証済みの基盤; 安定版として非推奨)
ブランチ: `event-federation` · Head: `8c4821c8` · 日付: 2026-05-30
作成者: review subagent (事実のみ記載; マージ判断は orchestrator が最終決定)

`event-federation` ブランチを `main` への experimental-MVP 候補として読み取り専用で棚卸しした文書です。
コード変更やマージは行いません。

## スコープ

`event-federation` は **`main` の 36 コミット先、0 コミット遅れ**です（クリーンな fast-forward が可能）。
`cloudedge-mvp` の厳密なスーパーセットであり、`cloudedge-mvp` の head `713233b0` は `event-federation` の祖先です（`git merge-base --is-ancestor` で確認済み）。
このブランチは **CloudEdge/SAM**（`cloudedge-mvp` の全内容）**+ Event Federation Phase 1 / 1.5 / 2 / 3** です。

`main` に対して追加される内容:

- **CloudEdge / SAM**（`cloudedge-mvp` 由来）: dynamic-config 基盤
  （`DynamicConfigPart` / masks / `DynamicOverridePolicy`）、plugin runner
  （observe-only, dry-run; actionPlans は表示専用）、L3 hybrid
  （`OverlayPeer` / `HybridRoute`）、Selective Address Mobility
  （`AddressMobilityDomain` / `RemoteAddressClaim` / `CloudProviderProfile`、
  Linux データプレーン）、zone 非依存の PMTU/MSS clamp (#53)、nft 所有権
  診断、`routerctl doctor hybrid`。
- **Event Federation**（ADR 0006）: typed observed-event エンベロープ + SQLite ローカル
  ストア + `routerctl federation event` CLI（Phase 1/1.5）; `routerd-eventd`
  トランスポートデーモン + `EventGroup` / `EventPeer` Kind + HMAC push 配信 +
  `event_deliveries` + retention prune（Phase 2）; `EventSubscription` Kind +
  subscription トリガーの plugin → `DynamicConfigPart`（`RemoteAddressClaim`）
  （Phase 3）。新規 Kind は計 3 つ: `EventGroup`、`EventPeer`、`EventSubscription`
  （apiVersion `federation.routerd.net/v1alpha1`）。

## エビデンス一覧

リポジトリ内のエビデンス文書（すべて存在確認済み）:

| 文書 | 結果 / 判定 |
|---|---|
| `docs/releases/cloudedge-sam-mvp-milestone.md` | Azure/AWS/OCI × PVE すべて **PASS / clean**; 3 クラウドパリティ; experimental |
| `docs/releases/cloudedge-sam-stocktake-20260529.md` | マージ前棚卸し; 粗削りな部分 = experimental フォローアップであり、ブロッカーではない |
| `docs/releases/evidence/cloudedge-sam-azure-pve-20260529.md` | Azure × PVE **PASS / clean** |
| `docs/releases/evidence/cloudedge-sam-aws-pve-20260529.md` | AWS × PVE **PASS / clean**（Azure パリティ、初回実行） |
| `docs/releases/event-federation-checkpoint.md` | Phase 1 + 1.5 チェックポイント; experimental; リリースタグではない |
| `docs/releases/evidence/cloudedge-event-federation-transport-20260530.md` | Phase 2 トランスポートスモーク **Result: PASS**（アサーション A-G の 7 項目） |
| `docs/releases/evidence/cloudedge-event-federation-subscription-20260530.md` | Phase 3 subscription スモーク **Result: PASS**（メインパス + 4 つのネガティブチェック） |

完全なエビデンスバンドル（および OCI サマリー）は、隣接するラボリポジトリ `/home/imksoo/routerd-labs/...` に存在します（このリポジトリにはコミットされていません）。
参照されているトランスポートおよび subscription エビデンスバンドル（`routerd-labs/event-federation/evidence/20260530T091652Z-...` および `...20260530T111612Z-...`）はディスク上に存在します。

### リンク整合性（軽微、修正推奨）

`cloudedge-sam-mvp-milestone.md:24` が OCI エビデンスを
`routerd-labs/cloudedge-sam/evidence/20260530T031247Z-oci-pve-hardening/summary.md`
としてリンクしていますが、ディスク上の実際のディレクトリは
`20260530T031247Z-oci-pve-hardening-43a64c55/` です（コミットサフィックス `-43a64c55` が欠落）。
**参照パスが解決できません**（リンク切れ）。
このパスは外部のラボリポジトリ内のため、Web サイトのビルドには影響しませんが、参照として不正確です。
リポジトリ内の `docs/releases/evidence/*.md` における 4 つの参照はすべて正しく解決されます。

## 整合性の指摘

### ADR 0006 のステータスが古い（マージ前に修正必須）

`docs/adr/0006-event-federation.md` の Status セクションにはまだ以下の記述があります:

> Phase 1 (...) is implemented on `event-federation`. **Phase 2 (peer delivery
> over the overlay) is pending.**

また Context には **"OCI×PVE in progress"** とあります。
どちらも現在は正しくありません。Phase 2 と Phase 3 が実装済み（PASS スモーク付き）で、OCI×PVE もパスしています。
ADR の Status ブロックを Phase 1-3 実装済み + OCI クリーンに更新する必要があります。

### ドキュメントサイトのナビゲーション（新規ドキュメントが孤立、マージ前に修正必須）

`website/sidebars.ts` はドキュメントのサイドバー（`docs/` 配下のデフォルト英語版）です。
SAM リファレンス（`reference/selective-address-mobility`）はサイドバーに登録済みです（sidebars.ts:150）。

しかし以下の問題があります:

- **`docs/how-to/event-federation-subscription.md` は `website/sidebars.ts` に登録されていません**
  （`grep` 結果 = 0）。孤立しており、サイトの How-to guides カテゴリに表示されません。
- **`docs/reference/` 内に専用の federation リファレンスドキュメントがありません**
  （`docs/reference/` 配下には `dynamic-config.md` と `selective-address-mobility.md` のみ）。
  federation リファレンスページが計画されているなら未作成であり、how-to だけが唯一の
  federation ドキュメントであるなら、サイドバーエントリは依然として必要です。

プロジェクトポリシー（正本 = 日本語 `website/i18n/ja`、Web デフォルト = 英語 `docs/`）に従い、
i18n/ja のサイドバーおよび翻訳にもエントリが必要です。
ただしサイドバー構造は共有（`sidebars.ts`）なので、how-to を `sidebars.ts` に追加するだけで単一の必須配線変更となります。
ja 翻訳コンテンツは別途の（低優先度、experimental）フォローアップです。

## API スキーマ生成（マージ前に修正必須）

ジェネレーター: `make generate-schema` → `cmd/routerd-schema` →
`schemas/routerd-config-v1alpha1.schema.json`（+ control + control-openapi）。
3 つのスキーマファイルはすべて git で追跡されています。

- `make generate-schema`（および `make check-schema`）を実行しても **差分なし**です。
  `git status --short schemas/` はクリーンであり、コミット済みのスキーマはジェネレーターと内部整合性が取れています。
- **ただしスキーマは不完全です。**
  `cmd/routerd-schema/main.go` は `resourceSchema(apiVersion, "Kind", Spec{})` で各 Kind を手動列挙しています。
  SAM の Kind は登録済みです（327-331 行目: OverlayPeer, HybridRoute, AddressMobilityDomain, CloudProviderProfile, RemoteAddressClaim）。
  **新規の federation 3 Kind（`EventGroup`、`EventPeer`、`EventSubscription`）はジェネレーターのリストに登録されていません。**
  そのため生成される JSON スキーマに含まれず、再生成しても差分は出ません（ジェネレーターがそれらの存在を認識していないため）。
- 修正 = `cmd/routerd-schema/main.go` に `resourceSchema(api.FederationAPIVersion, "EventGroup"/"EventPeer"/"EventSubscription", api.…Spec{})` の 3 行を追加し、`make generate-schema` を実行して結果の `schemas/` 差分をコミット。
  （ここでは修正せず、orchestrator への報告のみ。）

検証メモ: `make check-schema` は現在 **パスします**。
これはジェネレーター出力をコミット済みファイルと照合するだけであり、欠落した Kind は検出しません。
CI のグリーンはこのギャップを捕捉できません。

## make dist とパッケージングの完全性

- `routerd-eventd` は `make dist` に **含まれています**: Makefile の `ROUTERD_RELEASE_BINS` は
  `$(ROUTERD_EVENTD_BIN)` を含み（Makefile:33-34）、`build-daemons` がビルドし（Makefile:74）、
  dist がインストールします（Makefile:199）。`make -n dist | grep eventd` でビルド + インストール行を確認済み。
- **サンプルプラグイン（`examples/plugins/event-to-remote-claim`）は `make dist` に含まれていません**
  （Makefile に参照なし; `make -n dist` に `examples/plugins` なし）。
  これは **ドキュメントに記載済み**です:
  `examples/plugins/event-to-remote-claim/README.md`（"## Build and install" →
  `go build -o bin/event-to-remote-claim ./examples/plugins/event-to-remote-claim`）と
  `docs/how-to/event-federation-subscription.md:61-64` の両方で、オペレーターに別途ビルドするよう案内しています。
- **パッケージングに eventd 固有の変更は不要です。**
  `packaging/install.sh` は汎用 glob（`for binary in bin/*`、1873 行目）で全バイナリをインストールするため、`routerd-eventd` は自動的にインストールされます。
  グループ別の systemd ユニット `routerd-eventd@<group>.service` は **routerd 自身が生成** します（controller chain / `pkg/render/eventd_systemd.go` + `pkg/controller/eventfederation` の `EventGroup` supervision 経由）。
  静的ユニットとして同梱されるわけではないため、`install.sh` の `systemd/*.service` ループには不要です。
  `contrib/systemd/` に静的な `routerd-eventd.service` は存在しません（設計上テンプレート化された `@.service` です）。

## プロバイダーミューテーションなし（セキュリティ / スコープゲート、確認済み）

ツリー全体を grep（Go ソース: `pkg/`、`cmd/`、`examples/`）:

- **クラウド SDK のインポートなし**（`aws-sdk` / `azure-sdk` / `oci-go-sdk` /
  `cloud.google.com` / `github.com/{aws,Azure,oracle}/`）。マッチゼロ。
- **クラウド CLI の exec なし。**
  外部ツールを呼び出す `exec.Command*` は `pkg/controller/dhcpv4client/controller.go` と
  `cmd/routerd-pppoe-client/main.go`（ローカル DHCP/PPPoE）のみで、クラウド関連ではありません。
- `ActionPlan` は **表示専用として宣言**: `pkg/plugin/types.go:85-86`
  （"MVP routerd never executes ActionPlans"）; テスト
  `TestRunRemoteAddressClaimActionPlanIsDisplayOnly` が強制。
- サンプルプラグインは `os.Stdin` JSON を読み取り `os.Stdout` JSON のみを書き込みます
  （`examples/plugins/event-to-remote-claim/main.go`）。
  exec、http、net、クラウドコールなし。
  自身のヘッダーコメントでプロバイダーアクション実行は MVP スコープ外（Phase 4/5）と記載。

結論: **このブランチに実行可能なプロバイダーミューテーションパスは存在しません。**
プロバイダーに関連するサーフェスは、宣言的 spec（`CloudProviderProfile`、capture type `provider-secondary-ip`）、表示専用の actionPlans、およびクラウドコールなしのサンプルプラグインのみです。

## Experimental ラベリングの確認

- `cloudedge-sam-mvp-milestone.md`: "Status: **experimental** (lab-validated; NOT
  recommended-stable)"; 安定昇格やリリースタグの付与を明示的に保留。
- `event-federation-checkpoint.md`: "Status: **experimental** (in development;
  NOT recommended-stable)"; "**not** a release tag."
- ADR 0006: "Accepted for **experimental implementation**."
- Phase 2/3 エビデンスの判定は結果をコントロールプレーンのみにスコープし、
  プロバイダーやクラウドミューテーションが発生していないことをアサート。

レビューしたドキュメントで安定版や推奨を示唆するものはありません。
リリースタグや安定昇格の主張はありません。

## 既知のギャップ

予想される 4 つのギャップのうち、2 つは正確にギャップとして認識されており、1 つは
誤認されており、1 つは文書化されていません:

1. **FreeBSD rc.d による `routerd-eventd` の supervision（未実装、systemd のみ、未文書化）。**
   `pkg/render/eventd_systemd.go` は systemd ユニットのみをレンダリングしており、eventd 用の
   rc.d 相当はありません。ADR にも eventd の rc.d や FreeBSD に関する記述はありません。
   experimental のプラットフォーム制限として記録すべきです。
2. **`EventSubscription` の `batchWindow` / `debounce` は受け入れられるが精密なタイマーでは実行されない。**
   Spec フィールドは存在（`pkg/api/specs.go:1298-1303`）しますが、
   `pkg/controller/eventsubscription/controller.go` は **poll-tick バッチ**
   （"poll + dedup … each tick"、4-8 行目）であり、精密な batch/debounce タイマーはありません。
   フィールドは受け入れられる設定ですが、現時点では情報提供のみです。
   制限事項として文書化すべきです。
3. **セルフプッシュおよびループ防止（実装済み、ギャップではない）。**
   ADR のループ防止不変条件は強制されています:
   `pkg/eventd/outbox.go:78` はローカル発信のイベント（`SourceNode == nodeName`）のみをプッシュし、受信イベントは再プッシュしません。
   `TestOutboxLoopPrevention`（`pkg/eventd/outbox_test.go`）でカバー済み。
   もう一つの observer 側の不変条件（「ノード自身が捕捉したアドレスを observed event として再 emit しない」）は
   ARP/Clients observer に属し、**Phase 4 でありこのブランチにはありません**。
   スキップすべきものは現時点では存在しません。
4. **ラボノードが `515fe7e8` ビルドのまま残置。**
   Phase 3 エビデンス（`...subscription-20260530.md`）は `515fe7e8` を router03 + router05 にデプロイしたと記録していますが、**teardown および revert の記載がなく**、ラボ記録上ではこれらのノードは Phase 3 ビルドで稼働中と推定されます。
   明示的なラボノート（クリーンアップまたは意図的残置）が望ましいです。

## ビルドおよびテスト正常性（最終ゲート）

すべて `event-federation` head `8c4821c8` で実行:

- `gofmt -l pkg cmd examples` → **クリーン**（出力ファイルなし）。
- `go build ./...` → **成功**。
- `go test ./...` → **1880 テスト通過、95 パッケージ**（exit 0）。失敗なし。
- `make check-schema` → **パス**（差分なし）。ただし上記のスキーマ不完全性の指摘を参照。check-schema は欠落した Kind を検出しません。
- この実行では `cmd/routerd` の networkd-env テスト失敗は観測されませんでした。

## PR #49 との関係（選択肢の列挙、事実のみ）

PR #49（`gh pr view 49`）: OPEN、**draft**、`cloudedge-mvp → main`、タイトル
"CloudEdge MVP: hybrid routing and selective address mobility"。
内容は `event-federation` の **厳密なサブセット**（head `713233b0` が祖先）。
`event-federation` は main の 36 先 / 0 遅れ → **クリーンな fast-forward が可能**。

- **(a) #49 を `event-federation → main` PR にリターゲットまたは置換。**
  CloudEdge/SAM + EF Phase 1-3 を運ぶ単一 PR。#49 はクローズまたは superseded。1 回のレビュー、1 回のマージ。
- **(b) まず `cloudedge-mvp` を #49 でマージし、次に `event-federation`。**
  2 段階: CloudEdge/SAM が独立マージとして着地し、EF が続く。より細かい履歴。2 回のレビューおよびマージサイクル。#49 が意味を持つ。
- **(c) `event-federation` の単一 experimental マージ。**
  (a) と同じ最終状態だが、1 回の experimental マージとしてフレーミング。#49 は superseded としてクローズ。

いずれの場合も #49 の差分は `event-federation` に完全に含まれ、FF はクリーンです。

## 推奨（最終）

**判定: experimental 機能として `main` へのマージ準備完了。**
ビルドクリーン、gofmt クリーン、1880 テストグリーン、golden 変更なし、プロバイダーミューテーションパスなし、一貫した experimental ラベリング、`make dist` は `routerd-eventd` を同梱、パッケージング変更不要。
CloudEdge/SAM は 3 クラウドでラボ検証済み（PASS/clean）、EF Phase 1-3 それぞれに PASS ラボスモークあり（transport + subscription）。

棚卸しで指摘されたマージ前の整備項目は、同一パス（同一ブランチ、このドキュメントと並行してコミット）で解決済み:

1. **スキーマ（修正必須、解決済み）。**
   `EventGroup` / `EventPeer` / `EventSubscription` を `cmd/routerd-schema/main.go` に登録。
   `schemas/routerd-config-v1alpha1.schema.json` を再生成して含めた。
   `make check-schema` パス。
2. **ADR 0006 ステータス（修正必須、解決済み）。**
   Status/Context を Phase 1-3 実装済み + OCI×PVE クリーン（3 クラウドパリティ）に更新。
   フェーズごとのマーカー設定。
   `## Known limitations (experimental)` サブセクションを追加。
3. **ドキュメントナビ（修正必須、解決済み）。**
   `how-to/event-federation-subscription` を `website/sidebars.ts` に追加（英語デフォルトサイドバー）。
   ja 翻訳は遅延フォローアップ（非ブロッキング。Docusaurus はソースドキュメントにフォールバック）。
4. **OCI エビデンスリンク（修正推奨、解決済み）。**
   `cloudedge-sam-mvp-milestone.md` 内の `-43a64c55` ディレクトリサフィックスを修正。
5. **Experimental ギャップ（修正推奨、解決済み）。**
   ADR 0006 "Known limitations" に文書化:
   systemd のみの `routerd-eventd`（FreeBSD rc.d は未対応）;
   `batchWindow`/`debounce` は受け入れるが poll-tick バッチ（精密タイマーなし）。

残り（非ブロッキング、ここで追跡）:

6. **ラボ teardown ノート。**
   router03/router05 は Phase 3 スモーク後に `515fe7e8` ビルドのまま残置（設定はベースラインに復元済み; バイナリのみ未リバート）。
   `main` マージのブロッカーではなく、ラボ管理上のノート。
   次回のラボ作業時にリバートまたは再ピンする。
7. **i18n。**
   `event-federation-subscription.md` の ja/zh 翻訳および専用の federation リファレンスページは遅延フォローアップ。

**推奨マージ形態:** 単一の `event-federation → main` PR、**PR #49 は superseded としてクローズ**
（`cloudedge-mvp` の内容は `event-federation` の厳密な祖先かつサブセット。クリーン fast-forward、0 遅れ）。
これが最小オーバーヘッドのパスであり、1 回の experimental ランディングを維持します。
選択肢 (b)（まず `cloudedge-mvp` を #49 でランドし、次に `event-federation`）は、別途の CloudEdge/SAM 履歴チェックポイントが望まれる場合にのみ意味がありますが、必須ではありません。

**マージ自体と PR #49 の処分はメンテナーの判断です**（main へのリリースおよびマージはオーナーゲート）。
タグなし。experimental。
