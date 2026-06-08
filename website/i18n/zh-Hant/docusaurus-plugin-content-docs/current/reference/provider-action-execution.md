# Provider Action Execution（實驗性）

> **實驗性。** 此功能帶有閘控，預設停用，正在積極開發中。
> 關於設計和安全性依據，請參見 [ADR 0007](../adr/0007-provider-action-execution.md)。

routerd 可以透過 **executor plugin** 執行經核准的雲端供應商變更（例如：為
[選擇性位址移動性](./selective-address-mobility)指派 secondary IP）。routerd
不持有雲端憑證。

![展示 actionPlans 作為不活躍提案儲存、匯入到 action journal、透過策略閘控核准、由持有憑證的 executor plugin 執行的 Provider action execution 示意圖](/img/diagrams/dynamic-config-provider-actions.png)

## 憑證模型

- **持有憑證的是 executor plugin，而非 routerd。**
  executor（capability `execute.providerAction`）在自己的行程中執行，透過雲端原生
  identity（AWS instance profile、Azure managed identity、OCI instance principal）
  或自身環境進行驗證。
- **routerd 核心不持有、讀取或傳遞供應商憑證。**
  routerd 僅向 executor 傳遞已核准的 action plan（不含金鑰）和經過允許清單化、
  脫敏處理的 plugin 脈絡。
- `action_executions` journal 僅記錄計畫及其結果，不包含任何金鑰。

## `ProviderActionPolicy`

`apiVersion: hybrid.routerd.net/v1alpha1`，`kind: ProviderActionPolicy`。零值為
安全的鎖定狀態。執行停用，僅 dry-run，需要核准，無允許清單。

| 欄位 | 型別 | 預設值 | 含義 |
| --- | --- | --- | --- |
| `enabled` | bool | `false` | 非 `true` 時執行被停用。 |
| `dryRunOnly` | bool (pointer) | 省略時 `true` | 僅允許 dry-run。拒絕實際變更。 |
| `requireApproval` | bool (pointer) | 省略時 `true` | 執行前需要維運人員核准。 |
| `allowedProviders` | list | 空 = 無 | 允許的供應商：`aws`、`azure`、`oci`、`gcp`。 |
| `allowedProviderRefs` | list | 空 = 不限制 | 限制為指定的 `CloudProviderProfile` ref。 |
| `allowedActions` | list | 空 = 無 | 規範動詞：`assign-secondary-ip`、`unassign-secondary-ip`、`ensure-forwarding-enabled`、`ensure-forwarding-disabled`。 |
| `allowedCIDRs` | list | 空 = 不限制 | action 的目標位址必須在某個 CIDR 範圍內。 |
| `maxActionsPerRun` | int | `0` = 無 action | 每次執行的 action 上限。設定正值以允許。 |
| `allowUndo` | bool | `false` | 允許盡力回復。 |
| `executionWindow` | string | 空 = 無視窗 | 選用的時間視窗。寬鬆驗證。 |

範例（除單一允許清單化的 action 外全部鎖定）：

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

## action 生命週期

planner plugin 提議的 action plan 被匯入到 journal 中，經歷以下狀態轉換。

```text
pending  ->  approved  ->  succeeded
                       ->  failed
                       ->  skipped
                            (succeeded) -> rolledBack
```

- **pending** — 從 `actionPlan` 匯入，以 `idempotencyKey` 為鍵，等待核准。
- **approved** — 維運人員已核准，或策略自動核准
  （當 `requireApproval: false` 且 `enabled` 且非 `dryRunOnly` 時）。
- **succeeded / failed / skipped** — executor 回報的結果。`skipped` 表示已
  succeeded 的 `idempotencyKey` 的重複，或策略拒絕的 action。
- **rolledBack** — 對先前 succeeded 的 action 套用盡力 undo
  （僅在 `allowUndo` 為 true 時）。

匯入是冪等的。重新匯入相同的 `idempotencyKey` 不會建立第二列，因此已 succeeded
的 action 不會被執行兩次。

## `routerctl action` 命令

目前面向維運人員的介面有意採用 journal 導向。先匯入或核准 action，僅執行通過策略
的已核准項目。

| 命令 | 用途 |
| --- | --- |
| `routerctl action list` | 列出 journal 項目（按狀態/供應商篩選）。 |
| `routerctl action show ID` | 顯示單一 journal 項目。 |
| `routerctl action approve ID` | 維運人員核准：從 `pending` 變為 `approved`。 |
| `routerctl action execute --dry-run` | 驗證和預覽。無變更。 |
| `routerctl action execute --approved` | 執行策略允許的已核准 action。 |
| `routerctl action journal` | 輸出執行 journal / 稽核軌跡。 |
| `routerctl action rollback ID --dry-run` | 盡力 undo 的預覽（無變更）。 |

## dry-run vs 執行

- **dry-run** 是預設行為，是 `dryRunOnly` 為 true（或 `enabled` 為 false）期間
  唯一允許的路徑。它驗證計畫、檢查策略並預覽效果，但**不進行**任何供應商變更。
- **執行** 透過 executor 進行實際變更，僅在所有硬安全停止條件滿足時才執行：
  `enabled`、非 `dryRunOnly`、已核准（或策略自動核准）、允許清單比對、
  在 `maxActionsPerRun` 以內。

關於硬安全停止條件的完整清單，請參見
[ADR 0007](../adr/0007-provider-action-execution.md)。
