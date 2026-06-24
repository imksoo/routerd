# Provider Action Execution（実験的）

> **実験的。** この機能はゲート付きで、デフォルト無効であり、活発に開発中です。
> 設計と安全性の根拠については [ADR 0007](../adr/0007-provider-action-execution.md)
> を参照してください。

routerd は、承認されたクラウドプロバイダー変更（例:
[選択的アドレス移動性](./selective-address-mobility)のための secondary IP 付与）を
**エグゼキュータープラグイン**経由で実行できます。クラウドの認証情報を保持することはありません。

![actionPlans が不活性提案として保存され、action journal にインポートされ、ポリシーゲートで承認され、認証情報を保持するエグゼキュータープラグインで実行されることを示す Provider action execution の図](/img/diagrams/dynamic-config-provider-actions.png)

## 認証情報モデル

- **認証情報を保持するのはエグゼキュータープラグインであり、routerd ではありません。**
  エグゼキューター（capability `execute.providerAction`）は自身のプロセスで動作し、
  クラウドネイティブな識別情報（AWS instance profile、Azure managed identity、
  OCI instance principal）または自身の環境で認証します。
- **routerd コアはプロバイダーの認証情報を保持、読み取り、受け渡しすることはありません。**
  routerd はエグゼキューターに承認済みの action plan（秘密なし）と許可リスト化・
  墨消し済みのプラグインコンテキストのみを渡します。
- `action_executions` journal にはプランとその結果のみを記録し、秘密は
  一切含まれません。

## `ProviderActionPolicy`

`apiVersion: hybrid.routerd.net/v1alpha1`、`kind: ProviderActionPolicy`。ゼロ値は
安全なロックダウン状態です。実行無効、ドライランのみ、承認必須、許可リスト空です。

| フィールド | 型 | デフォルト | 意味 |
| --- | --- | --- | --- |
| `enabled` | bool | `false` | `true` でなければ実行は無効。 |
| `dryRunOnly` | bool (pointer) | 省略時 `true` | ドライランのみ許可。実変更は拒否。 |
| `requireApproval` | bool (pointer) | 省略時 `true` | 実行前に運用者の承認が必要。 |
| `allowedProviders` | list | 空 = なし | 許可するプロバイダー: `aws`、`azure`、`oci`、`gcp`。 |
| `allowedProviderRefs` | list | 空 = 制限なし | 指定した `CloudProviderProfile` ref に制限。 |
| `allowedActions` | list | 空 = なし | 正規動詞: `assign-secondary-ip`、`unassign-secondary-ip`、`assign-route-table-route`、`unassign-route-table-route`、`ensure-forwarding-enabled`、`ensure-forwarding-disabled`。 |
| `allowedCIDRs` | list | 空 = 制限なし | action のターゲットアドレスはいずれかの CIDR 内でなければならない。 |
| `maxActionsPerRun` | int | `0` = action なし | 1 回の実行あたりの action 上限。正の値を設定して許可する。 |
| `allowUndo` | bool | `false` | ベストエフォートのロールバックを許可。 |
| `executionWindow` | string | 空 = ウィンドウなし | オプションの時間ウィンドウ。寛容にバリデーション。 |

例（単一の許可リスト化された action 以外はロックダウン）:

```yaml
apiVersion: hybrid.routerd.net/v1alpha1
kind: ProviderActionPolicy
metadata:
  name: sam-execution
spec:
  enabled: true
  dryRunOnly: false
  requireApproval: true
  allowedProviders: [aws]
  allowedActions: [assign-secondary-ip, unassign-secondary-ip]
  allowedCIDRs: [10.88.60.0/24]
  maxActionsPerRun: 4
  allowUndo: true
```

## action ライフサイクル

プランナープラグインが提案した action plan は journal にインポートされ、以下の状態を
遷移します。

```text
pending  ->  approved  ->  succeeded
                       ->  failed
                       ->  skipped
                            (succeeded) -> rolledBack
```

- **pending** — `actionPlan` からインポートされ、`idempotencyKey` で一意に識別され、
  承認待ち。
- **approved** — 運用者が承認、またはポリシーによる自動承認
  （`requireApproval: false` かつ `enabled` かつ `dryRunOnly` でない場合）。
- **succeeded / failed / skipped** — エグゼキューターが報告した結果。`skipped` は既に
  succeeded した `idempotencyKey` の重複、またはポリシーが拒否した action を表す。
- **rolledBack** — 以前に succeeded した action にベストエフォートの取り消しを適用
  （`allowUndo` が true の場合のみ）。

インポートは冪等です。同じ `idempotencyKey` を再インポートしても 2 行目は作成されない
ため、既に succeeded した action が 2 回実行されることはありません。

## `routerctl action` コマンド

現在の運用者向けインターフェースは意図的に journal 指向です。まず action をインポート
または承認し、ポリシーを通過した承認済みエントリのみを実行します。

| コマンド | 目的 |
| --- | --- |
| `routerctl action list` | journal エントリを一覧表示（ステータス/プロバイダーでフィルター）。 |
| `routerctl action show ID` | 1 つの journal エントリを表示。 |
| `routerctl action approve ID` | 運用者承認: `pending` から `approved` へ。 |
| `routerctl action execute --dry-run` | バリデーションとプレビュー。変更なし。 |
| `routerctl action execute --approved` | ポリシーで許可された承認済み action を実行。 |
| `routerctl action journal` | 実行 journal / 監査証跡を出力。 |
| `routerctl action rollback ID --dry-run` | ベストエフォート undo のプレビュー（変更なし）。 |

## ドライラン vs 実行

- **ドライラン**はデフォルトであり、`dryRunOnly` が true（または `enabled` が false）
  の間に許可される唯一のパスです。プランをバリデートし、ポリシーを確認し、効果を
  プレビューしますが、プロバイダー変更は**一切**行いません。
- **実行**はエグゼキューターを通じて実際の変更を行い、すべての安全停止条件が
  満たされている場合のみ実行されます: `enabled`、`dryRunOnly` でない、承認済み
  （またはポリシー自動承認）、許可リスト一致、`maxActionsPerRun` 以内。

安全停止条件の完全なリストについては
[ADR 0007](../adr/0007-provider-action-execution.md) を参照してください。
