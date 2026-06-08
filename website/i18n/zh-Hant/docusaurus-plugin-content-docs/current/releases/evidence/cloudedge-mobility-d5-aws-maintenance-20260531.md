# CloudEdge Mobility D5 AWS 維護冒煙測試

Result: PASS

日期: 2026-05-31
建置: main 99eb1d45
證據包: `/home/imksoo/routerd-labs/cloudedge-mobility/evidence/20260531T215831Z-d5-aws-rerun-99eb1d45`

## 場景

- 僅 AWS 的 D5 線上維護 / 捕獲遷移。
- 複用現有活動路由器 A: `i-001f62ac01d66e782`, ENI-A `eni-0d17f203a6717e4d9`, 主 `10.77.60.4`。
- 為此次執行重新建立備用路由器 B: `i-045382a4f5bbf6fc0`, ENI-B `eni-017dd140722f5d819`, 主 `10.77.60.14`, `t3.small`。
- 複用 AWS 雲端用戶端: `i-0c5d4e3578e7669a9`, `10.77.60.11`。
- 捕獲位址: 本地用戶端 `10.77.60.10/32`。

## 初始捕獲

- A 匯入並執行:
  - `assign-secondary-ip` epoch 1 (`10.77.60.10/32` 到 ENI-A)。
  - `ensure-forwarding-enabled` epoch 1 (針對 ENI-A)。
- 初始 execute 後的 AWS provider 狀態:
  - ENI-A: `10.77.60.4,10.77.60.10`, `SourceDestCheck=false`。
  - ENI-B: `10.77.60.14`, `SourceDestCheck=true`。
- 遷移前資料平面:
  - cloud-client `10.77.60.11 -> 10.77.60.10` ping: `3/3`, `0% loss`。
  - 透過 SSH 以來源位址保留到達本地用戶端: `SSH_CONNECTION=10.77.60.11 ... 10.77.60.10 22`。

## Drain 和遷移

- 對路由器 A 宣告式地套用 `maintenance.drain=true`。
- A 匯入 epoch 2 的 de-provision action:
  - `unassign-secondary-ip` (從 ENI-A 移除 `10.77.60.10/32`)。
  - `ensure-forwarding-disabled` (針對 ENI-A)。
- B 匯入 epoch 2 的 active-capture action:
  - `assign-secondary-ip` (`10.77.60.10/32` 到 ENI-B)。
  - `ensure-forwarding-enabled` (針對 ENI-B)。
- A 的 unassign 成功執行, 從 ENI-A 刪除 `.10`。
- B 的 assign 成功執行, 向 ENI-B 新增 `.10`。
- 遷移後的 AWS provider 狀態:
  - ENI-A: `10.77.60.4`, `SourceDestCheck=true`。
  - ENI-B: `10.77.60.14,10.77.60.10`, `SourceDestCheck=false`。
- 捕獲 epoch 收斂至持有者 `aws-router-b`, epoch `2`。

## Epoch 圍欄

- A 的 epoch 1 action 在 drain 前成功。
- A 的 epoch 2 unassign 和 forwarding-disable 在執行前保留在日誌中。
- B 的 epoch 2 assign 和 forwarding-enable 成功執行。
- 透過非 provider 的日誌探針驗證 stale 門控:
  - 將同一捕獲鍵的 epoch 1 pending action 作為 `d5-rerun-stale-probe-epoch1` 插入;
  - `routerctl action import` 將其變更為 `status=skipped`;
  - 結果訊息: `stale mobility capture epoch`。

## 遷移後資料平面

- B 側 `doctor hybrid`: PASS。
- B 側 `routerd_mss`: `ens5 -> wg-hybrid` 存在。
- 本地 `routerd_mss`: `ens21 -> wg-hybrid` 存在。
- neighbor 重新整理後, cloud-client `10.77.60.11 -> 10.77.60.10` ping 在 3 輪連續中 `3/3` 通過。
- 透過 B 以 SSH 來源位址保留到達本地用戶端:
  - `SSH_CONNECTION=10.77.60.11 ... 10.77.60.10 22`。
- 用戶端預設閘道未變更: `default via 10.77.60.1`。

## Teardown

- 從 ENI-B 刪除 `10.77.60.10`。
- ENI-A 和 ENI-B 恢復 `SourceDestCheck=true`。
- IAM 內聯策略恢復到 B 範圍之前的文件。
- 終止 B。
- 停止 A 和 cloud-client。
- 最終成本狀態:
  - A: `stopped`。
  - cloud-client: `stopped`。
  - B: `terminated`。
  - ENI-A 基線: 僅 `10.77.60.4`, `SourceDestCheck=true`。
