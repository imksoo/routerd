# ADR 0007: プロバイダーアクション実行（ゲート付き、executor 分離）

![ADR 0007 の図。不活性な planner の actionPlan から、ProviderActionPolicy によるゲーティングと approval を経て、分離された executor プラグインのジャーナリングまで](/img/diagrams/adr-0007-provider-action-execution.png)

## ステータス

提案済み。実験的実装として承認 — 2026-05-30。

この ADR は [ADR 0006: CloudEdge Event Federation](../adr/0006-event-federation.md) と
[Selective Address Mobility](../reference/selective-address-mobility) データプレーンを
直接の土台とする。**実験的**である。

Phase 5.0（このチャンク）は、**設計、`ProviderActionPolicy` Kind、
`action_executions` ジャーナルのみ**を導入する。Phase 5.0 には**実行ステートマシン、
`routerctl action` コマンド、executor の呼び出し、実際のプロバイダー CLI/SDK 呼び出しは
含まれない** — フェイク executor と実行パスは後続チャンクで到着する。

## 背景

- **Phase 4.1 で dry-run `actionPlans` を導入済み。** planner プラグイン（capability
  `propose.providerAction`）は表示専用のプロバイダー操作を `DynamicConfigPart` に記録する。
  routerd は `actionPlan` を**決して実行せず**、そこからプロバイダー CLI/SDK を呼び出すこともない。
  `pkg/plugin.ValidateActionPlan` が `mode=execute` を拒否する。これらは
  EventSubscription 駆動の実行をレビュー可能に保つためだけに存在する。
- **SAM データプレーンは実クラウドで検証済み。** Selective Address Mobility は
  AWS、Azure、OCI でクリーンスモークを通過済み（3 クラウドパリティ）。オンプレミス側は
  claim されたアドレスをオーバーレイ経由で配送する。クラウド側は依然として、プロバイダーが
  NIC に secondary IP を実際に attach/detach する必要がある。現在、その attach/detach は
  オペレーターの手動操作。
- **不足しているのはゲート付き実行。** routerd が承認済みのプロバイダー mutation を
  駆動できるようにしたいが、プロバイダー資格情報は routerd コアに入ってはならず、
  実行はデフォルトオフ、明示的に承認され、完全にジャーナルされなければならない。

## 決定

### 2 つのプラグインロール

- **Planner**（Phase 4.1、capability `propose.providerAction`）: dry-run の
  `actionPlans` を発行する。資格情報を**持たない**。
- **Executor**（Phase 5、capability `execute.providerAction` — `PluginSpec.Capabilities`
  の新しい列挙値）: **自身のプロセスで自身の資格情報を使って**アクションを実行する。
  クラウドネイティブ ID（AWS インスタンスプロファイル、Azure マネージド ID、
  OCI インスタンスプリンシパル）または自身の環境を使用する。

### 資格情報モデル（ハードな不変条件）

**routerd コアはプロバイダー資格情報を保持・読み取り・受け渡しすることは決してない。**
routerd は executor に承認済みの `actionPlan`（秘密なし）と Phase 4.0 の
allowlist 済み/リダクト済みコンテキストのみを渡す。executor 自身がクラウドに対して
認証する。資格情報は routerd コアや `action_executions` ジャーナルを通過しない。

### フロー

1. planner が `DynamicConfigPart` 上に `actionPlan` を発行する（dry-run、現行どおり）。
2. プランが `action_executions` ジャーナルに `status=pending` として**インポート**される。
   `idempotencyKey` でキーイング。
3. **Approval**: オペレーターが承認する、またはポリシーが自動承認する
   （`requireApproval=false` かつ `enabled=true` かつ `dryRunOnly` でない、かつ
   allowlist が一致する場合のみ）。
4. **Execute**: routerd が一致する executor プラグインを呼び出し、
   承認済みプランを渡す（秘密なし）。
5. **結果がジャーナルされる**: `succeeded` / `failed` / `skipped` / `rolledBack`。

### `ProviderActionPolicy` Kind

新しい Kind（`apiVersion: hybrid.routerd.net/v1alpha1`）が実行をゲートする。
`RemoteAddressClaim` と `CloudProviderProfile` と同じ `hybrid` グループに定義し、
それらを管理する。ゼロ値は安全なロックダウン状態：

- `enabled`（bool、デフォルト false）— true でない限り実行は無効。
- `dryRunOnly`（`*bool`、nil のとき デフォルト true）— dry-run のみ許可。
- `requireApproval`（`*bool`、nil のとき デフォルト true）。
- `allowedProviders` / `allowedProviderRefs` / `allowedActions` — 空は none
  （デフォルト拒否）。
- `allowedCIDRs` — アクション対象アドレスがいずれかに含まれる必要がある。
- `maxActionsPerRun`（int、デフォルト 0 = アクションなし。オペレーターが
  正の上限を設定する必要がある）。
- `allowUndo`（bool、デフォルト false）。
- `executionWindow`（string、緩やかにバリデーション）。

### `routerctl action` UX サーフェス（後続チャンク、ここに文書化）

`routerctl action list`、`show`、`approve`、`execute --dry-run|--approved`、
`journal`、`rollback --dry-run`。これらはオペレーターサーフェス。Phase 5.0 では
**いずれも出荷しない**。

### フェーズ分割

- **Phase 5.0** — フレームワーク + データモデル: `ProviderActionPolicy` Kind、
  `action_executions` ジャーナル、スキーマ/バリデーション。**フェイク executor**
  （実クラウドなし）が Phase 5.0 の後半チャンクでパスをエンドツーエンドで検証する。
  **Phase 5.0 は実際のプロバイダー CLI/SDK を呼び出さない。**
- **ライブ mutation スモーク** — ゲート付き、1 プロバイダーずつ、
  SAM 検証済みクラウドに対して実行。
- **Phase 5.x** — ハードニング（ウィンドウ、レート制限、より豊富なロールバック、監査）。

## ハードセーフティストップ

1. **実行はデフォルト無効。** `ProviderActionPolicy.enabled` のデフォルトは false。
   `dryRunOnly` のデフォルトは true。
2. **明示的な承認が必要。** アクションは承認された場合のみ実行される（オペレーター承認、
   またはポリシーの `requireApproval=false` で `enabled` かつ `dryRunOnly` でなく
   allowlist が一致する場合）。
3. **`mode=execute` は拒否される** — ポリシーが許可する承認済み
   `action_execution` がない限り。
4. **`idempotencyKey` 必須**。既に succeeded のキーは再実行されない（skipped / duplicate）。
   インポートは `ON CONFLICT DO NOTHING` で、繰り返しキーは 2 つ目の実行行を作らない。
5. **すべての実行結果がジャーナルされる** — `succeeded` / `failed` / `skipped` /
   `rolledBack`、および `pending` / `approved` のライフサイクル状態。
6. **Undo/ロールバックはベストエフォート** — executor がサポートしない場合がある。
   ロールバックは `allowUndo` でゲートされる。
7. **プロバイダー資格情報は routerd コアを通過しない** — executor が自身の
   クラウドネイティブ ID を保持・使用する。
8. **Phase 5.0 は実際のプロバイダー CLI/SDK を呼び出さない** — フェイク executor のみ。

## 結論

- routerd は、クラウド資格情報を保持することなく、クラウド側 SAM の attach/detach を
  駆動するための、レビュー可能でデフォルトオフのパスを獲得する。
- ジャーナルが監査証跡であり冪等性のガードである。何が実行されたかの単一の正本となる。
- provision と de-provision の非対称性（ADR 0006 に従う TTL teardown のヒステリシス）は、
  すべてのイベントにリアクティブにではなく、実行をゲート付き・ジャーナル付きに保つことで遵守される。
