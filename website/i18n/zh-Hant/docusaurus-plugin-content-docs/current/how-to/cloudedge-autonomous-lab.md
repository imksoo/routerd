---
title: CloudEdge 自主實驗室 (cloudedge-labctl)
---

# CloudEdge 自主實驗室 (`cloudedge-labctl`)

![cloudedge-labctl 的實驗室生命週期、冒煙與故障轉移操作、證據收集、dry-run 預設、TTL 標籤、清理守衛的流程](/img/diagrams/how-to-cloudedge-autonomous-lab.png)

> 實驗性功能（CloudEdge）。這是一個單命令線束，允許代理在無需人工審閱 Runbook 的情況下運行 CloudEdge **Selective Address Mobility (SAM)** 故障轉移實驗室。該線束固定介面，並實作所有非雲端邏輯（run-id/標籤規範、TTL + 清理成本守衛、故障原語、連線矩陣、證據組裝）。實際的逐提供者佈建要麼封裝現有的 [`examples/cloudedge-mobility-demo/`](https://github.com/imksoo/routerd/tree/main/examples/cloudedge-mobility-demo) 套件，要麼標記為 `TODO(lab-operator)` 以供 Terraform/CLI 整合。

線束為 `scripts/cloudedge-labctl.sh`，有 2 個輔助工具：

- `scripts/cloudedge-connectivity-matrix.sh` — 有向 ping+ssh 矩陣 + 斷言。
- `scripts/cloudedge-evidence-schema.json` — 運行證據的 JSON schema。

`--help`、dry 路徑、`down --expired` **不需要雲端憑證**。

## 生命週期

```sh
scripts/cloudedge-labctl.sh up        --profile full --provider aws,oci,azure,onprem --ttl 4h
scripts/cloudedge-labctl.sh deploy    --commit HEAD          # 或 --build <dist path>
scripts/cloudedge-labctl.sh smoke     --matrix d3 --out /tmp/matrix.json
scripts/cloudedge-labctl.sh failover  --provider aws --fault stop-active
scripts/cloudedge-labctl.sh smoke     --matrix d3 --out /tmp/matrix-after.json
scripts/cloudedge-labctl.sh evidence  collect --out evidence/<run-id> --matrix-json /tmp/matrix-after.json
scripts/cloudedge-labctl.sh down      --run-id <run-id> --force
```

`up` 將 **run-id** 輸出到 stdout。請擷取此值並傳遞給後續命令。雲端變更**預設為 DRY**（`CE_DRY_RUN=1`）。在憑證和預算獲核准後設定 `CE_DRY_RUN=0` 實際執行。

## 設定檔

| 設定檔 | 站點數 | 用途 |
| --- | --- | --- |
| `minimal` | on-prem + 1 個雲端 | 最低成本冒煙，介面/CI 驗證 |
| `provider` | 1 個提供者的 A/B 路由器 + 用戶端 | 提供者對等性（AWS/OCI/Azure seize） |
| `full` | on-prem + AWS + OCI + Azure | 4 站點 `/24` 12 流展示 |
| `soak` | 持續 TTL 全程的 `full` 運行 | 長時間再收斂檢查 |

`soak` 在運作上是一個使用較長 `--ttl` 維持的 `full` 運行（TTL 到期前不要執行 `down`）。用於 BFD/BGP 再收斂驗證。

## TTL 和成本策略

所有雲端資源**必須**附加以下標籤（輔助函式 `cloudedge_tags()` 輸出，`up` 蓋章）：

```text
routerd.cloudedge.run_id          <UTCdate>T<time>-cloudedge-<scenario>
routerd.cloudedge.owner
routerd.cloudedge.ttl_expires_at  絕對 UTC RFC3339
routerd.cloudedge.provider
routerd.cloudedge.purpose
```

成本守衛規則：

- `up --ttl <dur>` 蓋章 `ttl_expires_at`。請為運行選擇適當的最短 TTL。
- `down --run-id <id>` 清理一個運行。`down --expired` 清理所有超過 TTL 的運行（無實驗室時安全地 no-op — exit 0）。
- `up` 預先驗證 `--ttl`，對無效時長進行**硬性失敗**（非零結束碼）。不會在成本守衛損壞/已過期的情況下啟動實驗室。
- 線束的 **EXIT trap** 會在 `up` 被中斷或提供者**啟動途中**失敗時清理運行（僅在進行中的階段啟用，正常完成時解除。正常的 `up` 讓實驗室存續到顯式 `down` 或 TTL 到期）。指定 `up --keep` 可保留部分狀態供調查。
- 無論是否失敗，每次運行後都必須執行 `down`（或由 janitor 執行 `down --expired`）。超過 TTL 的實驗室可在無 run-id 的情況下清理。

## 故障原語 (`failover --fault`)

| 故障 | 含義 | 初始配線 |
| --- | --- | --- |
| `stop-active` | 停止活躍路由器 VM/執行個體 | 提供者 stop CLI（參見 `reset-lab.sh`） |
| `drain` | 在活躍的 MobilityPool 上設定 `maintenance.drain=true` | 複用 `run-demo.sh` 的 `*-drain.yaml` |
| `routerd-bgp-stop` | 停止 `routerd-bgp`（BGP 工作階段斷開） | ssh `systemctl stop routerd-bgp` |
| `executor-fail` | 提供者操作 executor 拒絕（ID 範圍縮小） | ID 策略 |
| `stale-replay` | 重放過時的 pathSig 操作。**必須**被 fence | `probe_stale_gate_on_aws_b` |

注入故障後重新運行 `smoke` 和 `evidence collect`，證明恢復情況。

## 證據 schema

`evidence collect --out <dir>` 輸出 `<dir>/result.json`。透過 `scripts/cloudedge-evidence-schema.json` 驗證，同時輸出 `summary.md` 和（如指定）連線矩陣 JSON。格式：

```json
{
  "runId": "20260601T031500Z-cloudedge-aws-failover",
  "commit": "<sha>",
  "scenario": "aws-active-stop-seize",
  "result": "pass",
  "providers": {
    "aws":    {"dataplane": "pass", "providerState": "pass"},
    "oci":    {"dataplane": "pass", "providerState": "pass"},
    "azure":  {"dataplane": "pass", "providerState": "pass"},
    "onprem": {"dataplane": "pass", "providerState": "pass"}
  },
  "assertions": [
    {"name": "ownership_epoch_bumped", "result": "pass"},
    {"name": "allow_reassignment_maintained_until_success", "result": "pass"},
    {"name": "source_ip_preserved", "result": "pass"},
    {"name": "default_gateway_unchanged", "result": "pass"},
    {"name": "old_holder_residue_absent", "result": "pass"},
    {"name": "stale_action_fenced", "result": "pass"}
  ],
  "costGuard": {"ttlHours": 4, "teardown": "completed"}
}
```

資料平面檢查和 `source_ip_preserved` / `default_gateway_unchanged` 從連線矩陣自動導出。seize/fencing 斷言（`ownership_epoch_bumped`、`allow_reassignment_maintained_until_success`、`old_holder_residue_absent`、`stale_action_fenced`）和 `providerState` 初始為 `na`，由實驗室操作員從提供者清單、BGP mobility 路徑、提供者 trap 操作計畫、操作日誌中匯入（參見 `collect-evidence.sh`）。運行判定為 **PASS** 的條件是 `result == pass` 且所有必要斷言通過。

## 連線矩陣

`cloudedge-connectivity-matrix.sh` 在共享 `/24` 上執行所有有向 `src -> dst` 流，並對每個流斷言：

- **source-IP-preserved** — 目的端將用戶端站點的實際來源 IP（mobility `/32`）辨識為對等位址（無 NAT）。
- **default-gw-unchanged** — 來源用戶端的預設閘道未變更。
- **no-NAT** — ping 到達目的端，SSH 的對等 IP 與來源 IP 一致。

執行透過 `MATRIX_RUNNER` 間接指定，因此矩陣可**離線獨立運行**（為 `MATRIX_RUNNER` 設定樁）。在實際實驗室中，預設運行器對展示環境使用 `ssh`/`ping`。輸出為逐流 JSON，可透過 `evidence collect --matrix-json` 匯入。

## 自主性章程（概要）

代理擁有**實驗室啟動 -> 部署 -> 故障注入 -> 資料平面驗證 -> 證據 -> 清理 -> issue/PR 更新**的完整迴圈，無需人工閱讀 Runbook 即可執行。雲端操作預設為 dry，由顯式的憑證/預算審核進行閘控。代理必須始終將實驗室保持在已清理或 TTL 成本守衛內的狀態，PASS 必須附帶 schema 有效的證據包。

## 人工閘控

以下情況僅需人工參與。其餘全部自動化：

1. **預算** — 核准支出 / 提升 TTL 或預算上限。
2. **憑證/權限** — 提供雲端憑證和 executor 使用的最小權限 ID/角色（secret 不提交，也不傳遞給外掛程式）。
3. **合併** — PR 的最終審核。
4. **生產** — 生產環境部署（實驗室線束絕不執行）。

## 注意事項

- 這是**實驗室線束**，不是生產就緒的交鑰匙方案。
- 初始實作：實際的逐提供者配置/清理/節點推送是 `TODO(lab-operator)` 的樁或展示套件的薄層封裝 — 請接入使用 run-id 標籤過濾的 Terraform/OpenTofu 或提供者 CLI。
- 絕不提交實際的帳戶 ID / 訂閱 ID / OCID / ENI/VNIC ID / secret / 私鑰。使用 `env.example` 中的佔位邏輯位址。
