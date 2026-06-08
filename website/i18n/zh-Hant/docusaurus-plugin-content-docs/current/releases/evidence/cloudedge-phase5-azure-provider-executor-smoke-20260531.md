# CloudEdge Phase 5.1 Azure Provider Executor 冒煙測試

Result: PASS

日期: 2026-05-31 UTC  
分支/建置: `phase5-oci-azure-executors` / `routerd v20260528.2308 (c51ba0ca)`  
證據包: `/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260531T013055Z-phase5-azure-live-c51ba0ca`

## 範圍

- Provider mutation 對象: 僅 Azure。
- 租用戶/訂閱/區域: `53a7de65-6b1f-4878-a424-acad5e25db4b` / `26412fa4-cd3a-4128-9794-72ee01876d84` / `japaneast`。
- 複用的 routerd 專用 SAM 實驗室: 資源群組 `cloudedge-lab`。
- 目標路由器 VM: `routerd-cloud`, 私有 `10.77.60.4`, 公用 `20.46.113.237`。
- 目標用戶端 VM: `cloud-client`, 私有 `10.77.60.7`。
- 目標 NIC: `ce-router-nic`。
- 捕獲位址: `10.77.60.9`。

## 重設基線

mutation 前, 將現有 Azure SAM 實驗室重設為全新的 provider 基線:

- 從 `ce-router-nic` 刪除輔助 ipconfig `ipconfig-onprem-capture` / `10.77.60.9`。
- `ce-router-nic` 恢復 `enableIPForwarding=false`。
- 重設後證據: `azure-router-nic-post-reset.json`, `post-reset-nic-summary.tsv`。

重設後狀態:

- `ce-router-nic`: `ipForwarding=false`。
- IP configs: 僅主 `10.77.60.4`。

## 受管理身分識別門控

`routerd-cloud` 接收了系統指派的受管理身分識別:

- 主體 ID: `4b9423bc-01e3-4244-a898-b911f140cb6f`。
- 為 executor 在 `routerd-cloud` 上安裝了 Azure CLI。
- 路由器上的受管理身分識別 preflight 通過:
  - `az login --identity --allow-no-subscriptions`
  - `az network nic show --ids <ce-router-nic>`

初始的 NIC 範圍 Network Contributor 角色對 `ip-config create` 不夠。Azure 還要求關聯 NSG 的 `join/action` 權限。作為推進優先的修復, 在實驗室資源群組和 NSG 範圍新增了 Network Contributor。之後 executor 的 mutation 成功。

## Executor 執行

在 `routerd-cloud` 上建置並安裝了 `azure-provider-executor`。

路由器設定包含:

- `ProviderActionPolicy/azure-live-mutation`
- `Plugin/azure-executor`
- Plugin timeout `120s`
- `AZURE_CONFIG_DIR=/var/lib/routerd/azure`

Action 執行:

- `ensure-forwarding-enabled`
  - Action ID: `4`
  - Result: `succeeded`
  - 觀察到的 journal fact: `priorIpForwarding=false`
  - 結果訊息: `set ipForwarding=true`
- `assign-secondary-ip`
  - Action ID: `7`
  - Result: `succeeded`
  - 結果訊息: `assigned 10.77.60.9 to ce-router-nic (ip-config ipconfig-onprem-capture)`

mutation 後的 Azure 驗證:

- `ce-router-nic`: `ipForwarding=true`。
- IP configs: `10.77.60.4`, `10.77.60.9`。
- 證據: `azure-router-nic-after-mutation.json`, `azure-router-nic-after-mutation-summary.tsv`。

## 資料平面驗證

雲端側:

- `routerctl doctor hybrid`: `overall=pass`, `warn=0`, `fail=0`, `skip=1`。
- 傳遞路由: `10.77.60.9 dev wg-hybrid metric 120`。
- 本地 OS 位址不存在: `10.77.60.9/32 absent from local interfaces`。
- MSS clamp: `routerd_mss covers eth0 -> wg-hybrid`。

本地側:

- router06 `routerctl doctor hybrid`: `overall=pass`, `warn=0`, `fail=0`, `skip=1`。
- 雲端用戶端 `10.77.60.7` 的 Proxy ARP claim 保持健康。
- MSS clamp: `routerd_mss covers ens21 -> wg-hybrid`。

用戶端連線性:

- cloud-client `10.77.60.7` -> onprem-client `10.77.60.9` ping: `3/3`, `0% packet loss`。
- onprem-client `10.77.60.9` -> cloud-client `10.77.60.7` ping: `3/3`, `0% packet loss`。
- cloud -> onprem SSH 來源位址保留:
  - `SSH_CONNECTION=10.77.60.7 ... 10.77.60.9 22`
- onprem -> cloud SSH 來源位址保留:
  - `SSH_CONNECTION=10.77.60.9 ... 10.77.60.7 22`
- 預設閘道未變更:
  - cloud-client: `default via 10.77.60.1 dev eth0`
  - onprem-client: `default via 10.77.60.1 dev eth0`
- NAT: 透過 SSH 來源位址保留確認不存在。

## 回滾與恢復

透過 `routerctl action rollback` 執行了回滾:

- action 7 `assign-secondary-ip`: `rolledBack`, 取消分配 `ipconfig-onprem-capture`。
- action 4 `ensure-forwarding-enabled`: `rolledBack`, 恢復 `ipForwarding=false`。

回滾期間發現 1 個可修復的實驗室問題: 路由器設定重新套用後, Plugin 環境不再暴露 `AZURE_CONFIG_DIR`, Azure CLI 報告 `Please run 'az login'`。修正設定並在 `/var/lib/routerd/azure` 下重新建立受管理身分識別登入後, 回滾通過。

最終 teardown 使用選項 B: 恢復現有的 Azure SAM 實驗室狀態。

- `10.77.60.9` 輔助 ipconfig 重新存在。
- `ipForwarding=true`。
- `routerd-cloud`: `VM deallocated`。
- `cloud-client`: `VM deallocated`。

成本狀態:

- Azure 運算已 deallocate。
- 現有的公用 IP、NIC、磁碟、VNet、NSG、受管理身分識別/角色指派作為可複用的 SAM 實驗室狀態保留。

## 備註

- 在雲端 `RemoteAddressClaim` 實驗室設定中新增了 `capture.interface: eth0`。使新的 MSS/PMTU doctor 檢查能夠證明 `eth0 -> wg-hybrid` 的覆蓋。
- 首次 action 嘗試因受管理身分識別的角色範圍過窄而失敗。最終成功的 action 為 ID 4 和 7。
- `rtk` 包裝器會截斷較長的 Azure 資源 ID。需要精確資源 ID 的命令在 `rtk bash -lc` 內使用原始 `az`。
