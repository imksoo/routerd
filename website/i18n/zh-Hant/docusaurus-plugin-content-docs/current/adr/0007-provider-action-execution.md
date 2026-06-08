# ADR 0007: Provider Action 執行（閘門式、executor 分離）

![ADR 0007 的示意圖。從非活性 planner 的 actionPlan 出發，經過 ProviderActionPolicy 的閘門控制和核准，到分離的 executor 外掛的日誌記錄](/img/diagrams/adr-0007-provider-action-execution.png)

## 狀態

已提議。核准為實驗性實作 — 2026-05-30。

此 ADR 直接以 [ADR 0006: CloudEdge Event Federation](../adr/0006-event-federation.md) 和
[Selective Address Mobility](../reference/selective-address-mobility) 資料平面為基礎。
**屬實驗性質**。

Phase 5.0（此區塊）僅引入**設計、`ProviderActionPolicy` Kind 和
`action_executions` 日誌**。Phase 5.0 **不包含執行狀態機、
`routerctl action` 指令、executor 呼叫或實際的 provider CLI/SDK 呼叫** —
假 executor 和執行路徑將在後續區塊到來。

## 背景

- **Phase 4.1 已引入 dry-run 的 `actionPlans`。** planner 外掛（capability
  `propose.providerAction`）將僅供顯示的 provider 操作記錄到 `DynamicConfigPart`。
  routerd **從不執行** `actionPlan`，也不從中呼叫 provider CLI/SDK。
  `pkg/plugin.ValidateActionPlan` 拒絕 `mode=execute`。這些僅用於
  讓 EventSubscription 驅動的執行可審查。
- **SAM 資料平面已在真實雲端驗證。** Selective Address Mobility 已在
  AWS、Azure、OCI 上通過乾淨冒煙測試（3 雲對等）。on-prem 端透過 overlay
  投遞 claim 的位址。雲端仍然需要 provider 實際將 secondary IP attach/detach 到 NIC。
  目前該 attach/detach 是操作員手動完成的。
- **缺少的是閘門式執行。** 我們希望 routerd 能驅動已核准的 provider mutation，
  但 provider 憑證不得進入 routerd 核心，執行必須預設關閉、明確核准且完全記錄日誌。

## 決策

### 兩個外掛角色

- **Planner**（Phase 4.1、capability `propose.providerAction`）：發出 dry-run 的
  `actionPlans`。**不持有**憑證。
- **Executor**（Phase 5、capability `execute.providerAction` — `PluginSpec.Capabilities`
  的新列舉值）：**在自身行程中使用自身憑證**執行 action。
  使用雲原生身分（AWS 執行個體設定檔、Azure 受控識別、
  OCI 執行個體主體）或自身環境。

### 憑證模型（硬性不變量）

**routerd 核心絕不持有、讀取或傳遞 provider 憑證。**
routerd 僅向 executor 傳遞已核准的 `actionPlan`（不含秘密）和 Phase 4.0 的
allowlist/脫敏上下文。executor 自行向雲端驗證。憑證不經過 routerd 核心或
`action_executions` 日誌。

### 流程

1. planner 在 `DynamicConfigPart` 上發出 `actionPlan`（dry-run，與目前相同）。
2. 計畫以 `status=pending` **匯入**到 `action_executions` 日誌。
   以 `idempotencyKey` 為鍵。
3. **核准**: 操作員核准，或策略自動核准
   （僅當 `requireApproval=false` 且 `enabled=true` 且非 `dryRunOnly`，且
   allowlist 匹配時）。
4. **執行**: routerd 呼叫匹配的 executor 外掛，
   傳遞已核准的計畫（不含秘密）。
5. **結果記入日誌**: `succeeded` / `failed` / `skipped` / `rolledBack`。

### `ProviderActionPolicy` Kind

新 Kind（`apiVersion: hybrid.routerd.net/v1alpha1`）對執行進行閘門控制。
與 `RemoteAddressClaim` 和 `CloudProviderProfile` 定義在同一 `hybrid` 組中，
由它們管理。零值為安全的鎖定狀態：

- `enabled`（bool，預設 false）— 除非為 true，否則執行被停用。
- `dryRunOnly`（`*bool`，nil 時預設 true）— 僅允許 dry-run。
- `requireApproval`（`*bool`，nil 時預設 true）。
- `allowedProviders` / `allowedProviderRefs` / `allowedActions` — 空表示 none
  （預設拒絕）。
- `allowedCIDRs` — action 目標位址必須包含在其中。
- `maxActionsPerRun`（int，預設 0 = 無 action。操作員需設定
  正數上限）。
- `allowUndo`（bool，預設 false）。
- `executionWindow`（string，寬鬆驗證）。

### `routerctl action` UX 介面（後續區塊，此處記錄）

`routerctl action list`、`show`、`approve`、`execute --dry-run|--approved`、
`journal`、`rollback --dry-run`。這些是操作員介面。Phase 5.0
**均不交付**。

### 階段劃分

- **Phase 5.0** — 框架 + 資料模型: `ProviderActionPolicy` Kind、
  `action_executions` 日誌、schema/驗證。**假 executor**
  （無真實雲端）在 Phase 5.0 後半區塊端對端驗證路徑。
  **Phase 5.0 不呼叫實際的 provider CLI/SDK。**
- **實際 mutation 冒煙測試** — 閘門式，逐 provider，
  針對 SAM 驗證過的雲端執行。
- **Phase 5.x** — 強化（視窗、速率限制、更豐富的回滾、稽核）。

## 硬性安全停止措施

1. **執行預設停用。** `ProviderActionPolicy.enabled` 預設為 false。
   `dryRunOnly` 預設為 true。
2. **需要明確核准。** action 僅在核准後執行（操作員核准，
   或策略的 `requireApproval=false` 且 `enabled` 且非 `dryRunOnly` 且
   allowlist 匹配）。
3. **`mode=execute` 被拒絕** — 除非存在策略許可的已核准
   `action_execution`。
4. **`idempotencyKey` 必需**。已 succeeded 的 key 不會重新執行（skipped / duplicate）。
   匯入時 `ON CONFLICT DO NOTHING`，重複 key 不會建立第二個執行列。
5. **所有執行結果均記入日誌** — `succeeded` / `failed` / `skipped` /
   `rolledBack`，以及 `pending` / `approved` 的生命週期狀態。
6. **Undo/回滾是盡力而為** — executor 可能不支援。
   回滾受 `allowUndo` 閘門控制。
7. **Provider 憑證不經過 routerd 核心** — executor 持有並使用自身的
   雲原生身分。
8. **Phase 5.0 不呼叫實際的 provider CLI/SDK** — 僅假 executor。

## 結論

- routerd 取得了一條可審查的、預設關閉的路徑，用於驅動雲端 SAM 的 attach/detach，
  而無需持有雲端憑證。
- 日誌既是稽核記錄也是冪等性保障。它是已執行內容的唯一正本。
- provision 與 de-provision 的不對稱性（遵循 ADR 0006 的 TTL teardown 遲滯）
  透過保持執行的閘門控制和日誌記錄來遵守，而非對每個事件做出反應式執行。
