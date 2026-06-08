# CloudEdge Event Federation Phase 3 subscription 冒煙測試

Result: PASS

日期: 2026-05-30
分支/建置: event-federation / 515fe7e8d086
建置命令: `make dist`

證據包:
`/home/imksoo/routerd-labs/event-federation/evidence/20260530T111612Z-phase3-subscription-515fe7e8`

## 拓撲

冒煙測試使用了與 Phase 2 相同的 PVE 專用對。未啟動雲端 VM。

- 傳送側: router03 / 192.168.123.125 / `router03.lain.local`
- 接收側: router05 / 192.168.123.127 / `router05.lain.local`
- EventGroup: `cloudedge-event-smoke`
- 傳送側 EventGroup nodeName: `onprem-event-node`
- 接收側 EventGroup nodeName: `cloud-event-node`
- 接收側 listen: `169.254.250.5:9443`
- 傳送側 EventPeer endpoint: `http://169.254.250.5:9443`
- AddressMobilityDomain: `cloudedge-same-subnet`
- Plugin 可執行檔路徑: `/usr/local/libexec/routerd/plugins/event-to-remote-claim/bin/event-to-remote-claim`

傳送側和接收側的設定分別從以下檔案套用:

- `examples/event-federation/sender-onprem.yaml`
- `examples/event-federation/receiver-cloud.yaml`

在接收側的 plugin 路徑上設置了一個將 stdin 記錄到日誌的包裝器, 然後將建置好的範例外掛二進位檔案作為 `event-to-remote-claim.real` 執行。這樣可以在不修改 plugin 輸出的情況下取得 `PluginRequest.spec.events` 的證據。

## 部署

- `515fe7e8` 的 `make dist` 已完成。
- `routerd`、`routerctl`、`routerd-eventd` 已部署到兩個節點。
- 範例外掛使用 `CGO_ENABLED=0 GOOS=linux` 個別建置並安裝到 router05。
- 兩個產生的設定均通過 `routerd check`。
- 兩個節點上 `routerd-eventd@cloudedge-event-smoke.service` 均處於活動狀態。
- 接收側的 `ss` 確認 `169.254.250.5:9443` 上的監聽器。
- overlay 雙向可達性通過:
  - router03 -> `169.254.250.5`: 3/3 ping, 0% loss
  - router05 -> `169.254.250.3`: 3/3 ping, 0% loss

## 主斷言

事件:

- ID: `evt-phase3-smoke-20260530T112250Z`
- Type: `routerd.client.ipv4.observed`
- Subject: `10.88.60.9/32`
- SourceNode: `onprem-event-node`
- Payload domain: `cloudedge-same-subnet`
- Payload ownerSide: `onprem`

結果:

- 從傳送側到 `cloud-event-node` 的傳遞: `delivered`, attempts `1`
- 接收側 federation 儲存中存在相同事件 ID
- EventSubscription 執行:
  - subscription: `EventSubscription/cloud-claims`
  - plugin: `event-to-remote-claim`
  - status: `succeeded`
  - attempts: `1`
  - dynamic source: `EventSubscription/cloud-claims/07634fdff9b3235c`
- Plugin 執行:
  - trigger type: `federation-subscription`
  - trigger topic: `cloud-claims`
  - exit code: `0`
  - status: `succeeded`
- `routerctl dynamic list -o json` 顯示 1 個活動的 DynamicConfigPart。
- `routerctl dynamic render -o yaml` 的顯示:
  - kind: `RemoteAddressClaim`
  - name: `onprem-10-88-60-9`
  - address: `10.88.60.9/32`
  - domainRef: `cloudedge-same-subnet`
  - ownerSide: `onprem`
  - capture.type: `provider-secondary-ip`
  - capture.providerRef: `example-provider`
  - capture.nicRef: `example-nic-ref`
  - delivery.peerRef: `onprem-main`
  - delivery.tunnelInterface: `wg-hybrid`

渲染的 claim 附帶了來源註解:

- `routerd.net/dynamic-source: EventSubscription/cloud-claims`
- `routerd.net/event-group: cloudedge-event-smoke`
- `routerd.net/event-id: evt-phase3-smoke-20260530T112250Z`
- `routerd.net/event-subject: 10.88.60.9/32`

捕獲的 PluginRequest 在 `spec.events` 下包含具有相同 ID、subject、source node、payload 的主事件。

## 否定檢查

重複冪等性: PASS

- 重新 emit `evt-phase3-smoke-20260530T112250Z` 不會產生新的 subscription 執行。
- 主事件的傳遞保持 attempts `1`。
- DynamicConfigPart 數量保持 `1`。
- 渲染的 `RemoteAddressClaim/onprem-10-88-60-9` 數量保持 `1`。
- Plugin 請求日誌保持 1 個成功請求。

非匹配事件: PASS

- 事件 ID: `evt-phase3-nonmatch-20260530T112250Z`
- ownerSide: `cloud`
- 傳輸傳遞: `delivered`
- 接收側儲存了事件。
- 未建立 subscription 執行。
- 無 `10.88.60.10/32` 的 DynamicConfigPart 或渲染的 claim。

過期事件: PASS

- 事件 ID: `evt-phase3-expired-20260530T112250Z`
- ObservedAt: `2026-05-30T11:14:07Z`
- ExpiresAt: `2026-05-30T11:14:08Z`
- 傳送側傳遞查詢: `null`
- 接收側未收到過期事件。
- 無 `10.88.60.11/32` 的 subscription 執行或渲染的 claim。

Plugin 失敗重試上限: PASS

- 事件 ID: `evt-phase3-pluginfail-20260530T112250Z`
- 匹配實驗室專用的 `EventSubscription/cloud-claims-fail`。
- 失敗 plugin 以 exit code `42` 退出。
- `event_subscription_runs` 以 `status=failed`, `attempts=3` 結束。
- 記錄了 3 行失敗 plugin 執行。
- 未建立 `10.88.60.66/32` 的 DynamicConfigPart。

## 範圍檢查

- 未執行 provider action。
- 未建立、啟動、停止或修改雲端資源。
- 未嘗試 Phase 4 的 actionPlan 執行。
- 未執行 SAM 資料平面 apply; RemoteAddressClaim 僅存在於 `routerctl dynamic render` 中。
- 未使用 ARP observer、provider 特定 plugin、DynamicConfigPart consumer 路徑。

## 判定

Phase 3 控制平面自動化通過:

manual emit -> transport -> EventSubscription match -> plugin.Run ->
DynamicConfigPart -> `routerctl dynamic render` RemoteAddressClaim。

Phase 4 尚未開始。

## Pre-flight 備註

Pre-flight 在冒煙測試進入執行後才被要求。主路徑通過, 產生的 PluginResult/DynamicConfigPart 追溯確認了以下內容:

- payload domain 與 `AddressMobilityDomain.metadata.name` (`cloudedge-same-subnet`) 匹配
- plugin 可執行檔存在並被呼叫 (`event-to-remote-claim`, exit 0)
- 接收側的 hybrid 上下文完整 (渲染的 `RemoteAddressClaim` 針對接收側設定解析了
  `domainRef` / `delivery.peerRef` / `capture.providerRef`, 並通過 `dynamic render` 驗證)
- 未嘗試 provider mutation

即 pre-flight 未被跳過 — 主路徑 PASS 證明了設定/上下文的正確性。
