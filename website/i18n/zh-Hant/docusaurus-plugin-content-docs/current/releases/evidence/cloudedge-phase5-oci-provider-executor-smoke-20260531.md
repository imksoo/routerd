# CloudEdge Phase 5.1 OCI Provider Executor 冒煙測試

Result: PASS

日期: 2026-05-31 UTC  
分支/建置: `phase5-oci-azure-executors` / `routerd v20260528.2308 (67d96103)`  
證據包: `/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260531T005414Z-phase5-oci-live-67d96103`

## 範圍

- Provider mutation 對象: 僅 OCI。
- 租用戶/區域: `ocid1.tenancy.oc1..aaaaaaaaby2raoa2kzgywrsz6ofjk4eks6uwtpczgtqxulach3xgksfx52qq` / `ap-tokyo-1`。
- 複用的 routerd 專用 SAM 實驗室: `Project=routerd-cloudedge-sam-oci-pve`。
- 目標路由器執行個體: `routerd-cloud-oci` / `ocid1.instance.oc1.ap-tokyo-1.anxhiljr6yebb3qc2sucs3kor7u77ki2cg7zf3xlgmubj5utwfqeejmm7crq`。
- 目標用戶端執行個體: `oci-cloud-client` / `ocid1.instance.oc1.ap-tokyo-1.anxhiljr6yebb3qc2biuwl7yyjglwn6aompawzlfmkohpbrqceuijiuf7dva`。
- 目標 VNIC: `ocid1.vnic.oc1.ap-tokyo-1.abxhiljrzn6c2b4hs2jljbs4cmbshywzr7ldugepftjdrvm77nlvcvbdzzkq`。
- 捕獲位址: `10.77.60.9`。

## 重設基線

mutation 前, 將現有 SAM 實驗室重設為全新的 provider 基線:

- 從路由器 VNIC 刪除 `10.77.60.9` 輔助私有 IP。
- VNIC 恢復 `skipSourceDestCheck=false`。
- 重設後證據: `oci-router-vnic-post-reset.json`, `oci-router-private-ips-post-reset.json`, `retry-reset-summary.tsv`。

## 執行個體主體門控

`routerd-cloud-oci` 接收了 executor 用的 OCI 動態群組和策略。

- 動態群組: `routerd_phase5_oci_executor`。
- 初始的最小權限策略對 `private-ip create` 不夠, 回傳 `NotAuthorizedOrNotFound`。
- 推進優先的修復: 將此實驗室動態群組的策略擴大為 `manage virtual-network-family in tenancy`。

路由器上的執行個體主體 preflight 通過:

- `oci network vnic get` 可讀取目標 VNIC。
- `oci network private-ip list` 可讀取目標 VNIC 的私有 IP。

## Executor 執行

在 `routerd-cloud-oci` 上建置並安裝了 `oci-provider-executor`。

匯入、核准、dry-run 並執行了 2 個 retry2 action journal 條目:

- `assign-secondary-ip`
  - Result: `succeeded`
  - Message: `assigned 10.77.60.9 to <target VNIC>`
- `ensure-forwarding-enabled`
  - Result: `succeeded`
  - Message: `set skipSourceDestCheck=true on <target VNIC> (prior=false)`
  - 觀察到的 journal fact: `priorSkipSourceDestCheck=false`

mutation 後的 OCI 驗證:

- VNIC 主: `10.77.60.4`
- VNIC 輔助: `10.77.60.9`
- `skipSourceDestCheck=true`

## 資料平面驗證

雲端側:

- `routerctl doctor hybrid`: `overall=pass`, `pass=12`, `warn=0`, `fail=0`, `skip=1`。
- 傳遞路由: `10.77.60.9 dev wg-hybrid metric 120`。
- 本地 OS 位址不存在: `10.77.60.9/32 absent from local interfaces`。
- MSS clamp: `routerd_mss covers ens3 -> wg-hybrid`。

本地側:

- router06 `routerctl doctor hybrid`: `overall=pass`, `pass=15`, `warn=0`, `fail=0`, `skip=1`。
- 雲端用戶端 `10.77.60.7` 的 Proxy ARP claim 保持健康。

用戶端連線性:

- cloud-client `10.77.60.7` -> onprem-client `10.77.60.9` ping: `3/3`, `0% packet loss`。
- onprem-client `10.77.60.9` -> cloud-client `10.77.60.7` ping: `3/3`, `0% packet loss`。
- cloud -> onprem SSH 來源位址保留:
  - `SSH_CONNECTION=10.77.60.7 ... 10.77.60.9 22`
- onprem -> cloud SSH 來源位址保留:
  - `SSH_CONNECTION=10.77.60.9 ... 10.77.60.7 22`
- 預設閘道未變更:
  - cloud-client: `default via 10.77.60.1 dev ens3`
  - onprem-client: `default via 10.77.60.1 dev eth0`
- NAT: 透過 SSH 來源位址保留確認不存在。

## 回滾與恢復

透過 `routerctl action rollback` 執行了回滾:

- action 4 `ensure-forwarding-enabled`: `rolledBack`, 恢復 `skipSourceDestCheck=false`。
- action 3 `assign-secondary-ip`: `rolledBack`, 取消分配 `10.77.60.9`。

回滾期間發現 1 個可修復的實驗室問題: OCI 的 `private-ip delete` 可能超過 Plugin 原始的 `30s` 逾時。將實驗室的 Plugin 逾時擴大到 `120s` 後, action 3 的回滾完成, journal 中記錄了 `rolledBack`。

最終 teardown 使用選項 B: 恢復現有的 SAM 實驗室狀態。

- `10.77.60.9` 輔助私有 IP 重新存在。
- `skipSourceDestCheck=true`。
- `routerd-cloud-oci`: `STOPPED`。
- `oci-cloud-client`: `STOPPED`。

成本狀態:

- OCI 運算已停止。
- 現有的公用 IP、開機磁碟區、VNIC、子網、VCN、策略作為可複用的 SAM 實驗室狀態保留。

## 備註

- OCI Ubuntu 映像帶有終端的 iptables reject 規則。在資料平面驗證前套用了與 OCI SAM 冒煙測試相同的實驗室防火牆引導。
- 首次 executor 嘗試發現執行個體主體策略對私有 IP 建立過窄。擴大實驗室動態群組策略後, retry2 的 action 對通過。
- 首次以一般使用者執行回滾的嘗試因 action DB 檔案權限被拒絕。回滾使用 `sudo routerctl` 執行, 與 action DB 的擁有權一致。
