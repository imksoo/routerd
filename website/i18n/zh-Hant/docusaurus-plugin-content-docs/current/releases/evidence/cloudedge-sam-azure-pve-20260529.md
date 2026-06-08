# CloudEdge SAM Azure x PVE 冒煙測試證據

日期: 2026-05-29

分支/建置: `cloudedge-mvp`, `routerd v20260528.2308 (439ec316)`

Result: PASS

證據包:
`/home/imksoo/routerd-labs/cloudedge-sam/evidence/20260529T161157Z-439ec316-clean`

## 拓撲

- 雲端用戶端: `10.77.60.7/24`
- 本地用戶端: `10.77.60.9/24`
- 雲端路由器主: `10.77.60.4/24`
- 雲端路由器 Azure NIC 輔助捕獲位址: `10.77.60.9`
- 本地路由器: router06, `10.77.60.1/24` (`ens21`)
- Overlay: `wg-hybrid`, `169.254.110.1/32` <-> `169.254.110.2/32`

## Azure 捕獲

- `ce-router-nic` 已啟用 IP 轉發。
- 主私有 IP 為 `10.77.60.4`。
- 輔助私有 IP 為 `10.77.60.9`。
- routerd 的 reconciliation 後, `routerd-cloud` 的客體 OS 未將 `10.77.60.9` 保留為本地介面位址。
- `10.77.60.9/32` 透過 `wg-hybrid` 傳遞。

## 雲端側

- `RemoteAddressClaim/onprem-client-10-77-60-9` 為 `Ready`。
- 捕獲類型為 `provider-secondary-ip`。
- `captureDeassignedOSAddress.enforced=true`。
- 傳遞路由已安裝: `10.77.60.9 dev wg-hybrid scope link metric 120`。
- `ip route get 10.77.60.9` 選擇 `wg-hybrid`。
- `10.77.60.9/32` 在本地介面上不存在。
- `routerctl doctor hybrid` 為 `overall=pass`, `fail=0`。

## 本地側

- `RemoteAddressClaim/cloud-client-10-77-60-7` 為 `Ready`。
- 捕獲類型為 `ens21` 上的 `proxy-arp`。
- Proxy neighbor 存在: `10.77.60.7 proxy`。
- 傳遞路由已安裝: `10.77.60.7 dev wg-hybrid scope link metric 120`。
- `ens21.proxy_arp=1`。
- `routerctl doctor hybrid` 為 `overall=pass`, `fail=0`。

## 連線性

- 雲端用戶端到本地用戶端 ping: 3/3 收到, 0% loss。
- 本地用戶端到雲端用戶端 ping: 3/3 收到, 0% loss。
- 雲端用戶端到本地用戶端 SSH 來源位址保留成功:
  `SSH_CONNECTION=10.77.60.7 ... 10.77.60.9 22`。
- 本地用戶端到雲端用戶端 SSH 來源位址保留成功:
  `SSH_CONNECTION=10.77.60.9 ... 10.77.60.7 22`。
- 未觀察到 NAT。
- 用戶端預設閘道未變更。

## 乾淨執行加固檢查

- Azure Ubuntu 在 routerd 啟動前將 `10.77.60.9/24` 重新引入到 `eth0`。
- routerd `439ec316` 在無需手動 `ip addr del` 變通的情況下 de-assign 了該位址。
- routerd 在無需先前手動 `/dev/stdin` 變通的情況下套用了 WireGuard。
- 證據在 Azure VM deallocate 前捕獲。
- Azure VM 在證據捕獲後 deallocate; 資源群組未 tear down。

## 已知備註

- `routerd_filter` 表不可用時, FORWARD 策略的 doctor 檢查被跳過; 資料平面冒煙測試仍然通過。
- router06 的全域狀態保持 `Pending`, 但 `doctor hybrid` 通過且 SAM 資料平面路徑健康。
- 穩態下 `captureDeassignedOSAddress.deassigned=false` 表示在該 reconcile 中沒有需要刪除的位址; `enforced=true` + 本地位址的 doctor 通過是相關斷言。
