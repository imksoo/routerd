# CloudEdge Phase 5.1 AWS Provider Executor 冒煙測試

Result: PASS

日期: 2026-05-31 UTC  
分支/建置: `main` / `routerd v20260528.2308 (92f4cc94)` (附帶 `execute.providerAction` 的本地驗證器修復)  
證據包: `/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260530T235341Z-phase5-aws-rebaseline-92f4cc94`

## 範圍

- Provider mutation 對象: 僅 AWS。
- 帳戶/區域: `350538780953` / `ap-northeast-1`。
- 複用的 routerd 專用 SAM 實驗室: `SourceLab=routerd-cloudedge-sam-aws-pve`。
- 目標路由器執行個體: `routerd-cloud-aws` / `i-05b6cfd2b3e4e0da6`。
- 目標用戶端執行個體: `aws-cloud-client` / `i-0ae791389518353d6`。
- 目標 ENI: `eni-0904ccbed8d383f65`。
- 捕獲位址: `10.88.60.9`。

## 重設基線

mutation 前, 將現有 SAM 實驗室重設為全新的 provider 基線:

- 從 `eni-0904ccbed8d383f65` 刪除 `10.88.60.9` 輔助私有 IP。
- ENI 恢復 `SourceDestCheck=true`。
- 重設後證據: `aws-router-eni-post-reset.json`, `aws-router-eni-post-reset-confirm.json`。

## IAM 門控

`routerd-cloud-aws` 接收了 executor 用的 EC2 執行個體設定檔。

內聯策略僅允許:

- `ec2:DescribeNetworkInterfaces`
- `ec2:AssignPrivateIpAddresses`
- `ec2:UnassignPrivateIpAddresses`
- `ec2:ModifyNetworkInterfaceAttribute`

mutation 權限範圍:

- 區域: `ap-northeast-1`
- ENI ARN: `arn:aws:ec2:ap-northeast-1:350538780953:network-interface/eni-0904ccbed8d383f65`
- 資源標籤: `Project=routerd-cloudedge-phase5`

路由器上的執行個體角色 preflight 通過:

- `aws sts get-caller-identity` 回傳 `arn:aws:sts::350538780953:assumed-role/routerd-phase5-aws-executor-role/i-05b6cfd2b3e4e0da6`。
- `aws ec2 describe-network-interfaces` 可讀取目標 ENI。

## Executor 執行

在 `routerd-cloud-aws` 上建置並安裝了 `aws-provider-executor`。

匯入、核准、dry-run 並執行了 2 個 action journal 條目:

- `assign-secondary-ip`
  - Result: `succeeded`
  - Message: `assigned 10.88.60.9 to eni-0904ccbed8d383f65`
- `ensure-forwarding-enabled`
  - Result: `succeeded`
  - Message: `disabled SourceDestCheck on eni-0904ccbed8d383f65 (prior=true)`
  - 觀察到的 journal fact: `priorSourceDestCheck=true`

mutation 後的 AWS 驗證:

- ENI 主: `10.88.60.4`
- ENI 輔助: `10.88.60.9`
- `SourceDestCheck=false`

## 資料平面驗證

雲端側:

- `routerctl doctor hybrid`: `overall=pass`, `pass=12`, `warn=0`, `fail=0`, `skip=1`。
- 傳遞路由: `10.88.60.9 dev wg-hybrid metric 120`。
- 本地 OS 位址不存在: `10.88.60.9/32 absent from local interfaces`。
- MSS clamp: `routerd_mss covers ens5 -> wg-hybrid`。

本地側:

- router07 `routerctl doctor hybrid`: `overall=pass`, `pass=13`, `warn=0`, `fail=0`, `skip=1`。
- 雲端用戶端 `10.88.60.7` 的 Proxy ARP claim 保持健康。

用戶端連線性:

- cloud-client `10.88.60.7` -> onprem-client `10.88.60.9` ping: `3/3`, `0% packet loss`。
- onprem-client `10.88.60.9` -> cloud-client `10.88.60.7` ping: `3/3`, `0% packet loss`。
- cloud -> onprem SSH 來源位址保留:
  - `SSH_CONNECTION=10.88.60.7 ... 10.88.60.9 22`
- onprem -> cloud SSH 來源位址保留:
  - `SSH_CONNECTION=10.88.60.9 ... 10.88.60.7 22`
- 預設閘道未變更:
  - cloud-client: `default via 10.88.60.1 dev ens5`
  - onprem-client: `default via 10.88.60.1 dev eth0`
- NAT: 透過 SSH 來源位址保留確認不存在。

## 回滾與恢復

透過 `routerctl action rollback` 執行了回滾:

- `ensure-forwarding-enabled` 回滾 dry-run: 預計重新啟用 `SourceDestCheck`。
- `assign-secondary-ip` 回滾 dry-run: 預計取消分配 `10.88.60.9`。
- 實際回滾結果:
  - action 2: `rolledBack`, 恢復 `SourceDestCheck=true`。
  - action 1: `rolledBack`, 取消分配 `10.88.60.9`。

最終 teardown 使用選項 B: 恢復現有的 SAM 實驗室狀態。

- `10.88.60.9` 輔助私有 IP 重新存在。
- `SourceDestCheck=false`。
- `routerd-cloud-aws`: `stopped`。
- `aws-cloud-client`: `stopped`。

成本狀態:

- EC2 運算已停止。
- 現有的 EIP/磁碟/NIC/VPC 實驗室資源作為可複用的 SAM 實驗室狀態保留。

## 備註

- 執行期間發現程式碼 bug 並在本地修復: `PluginSpec` schema 和 executor resolver 支援 `execute.providerAction`, 但 `pkg/config/validate_plugin.go` 仍在拒絕。
- forwarding action 也需要 `target.address=10.88.60.9`。這使得 `ProviderActionPolicy.allowedCIDRs` 可以在不削弱策略的情況下對 action 進行門控。
