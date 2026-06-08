# CloudEdge Event Federation Phase 2 傳輸冒煙測試

Result: PASS

日期: 2026-05-30
分支/建置: event-federation / f951fd471a7e
建置命令: `make dist`

證據包:
`/home/imksoo/routerd-labs/event-federation/evidence/20260530T091652Z-phase2-transport-f951fd47`

## 拓撲

傳輸專用冒煙測試使用了 PVE 專用對。Azure、AWS、OCI 的 VM 均未啟動。

- 傳送側: router03 / 192.168.123.125 / `router03.lain.local`
- 接收側: router05 / 192.168.123.127 / `router05.lain.local`
- EventGroup: `cloudedge-event-smoke`
- 傳送側 EventGroup nodeName: `onprem-event-node`
- 接收側 EventGroup nodeName: `cloud-event-node`
- Overlay: `wg-hybrid`
- 傳送側 overlay 位址: `169.254.250.3/32`
- 接收側 overlay 位址: `169.254.250.5/32`
- 接收側 eventd listen: `169.254.250.5:9443`
- 傳送側 EventPeer endpoint: `http://169.254.250.5:9443`

emit 的事件使用 `--source-node onprem-event-node`, 與傳送側 EventGroup 的 `spec.nodeName` 匹配。

## 部署證據

- `make dist` 使用靜態 Linux 工件完成。
- 建置 `f951fd471a7e` 的 `routerd`、`routerctl`、`routerd-eventd` 已部署到兩個節點。
- 兩個產生的設定均通過 `routerd check`。
- 接收側的 `routerd-eventd@cloudedge-event-smoke.service` 在 `169.254.250.5:9443` 上監聽。
- 傳送側的 `routerd-eventd@cloudedge-event-smoke.service` 按預期僅執行 push/prune。
- overlay 雙向可達性通過:
  - router03 -> `169.254.250.5`: 3/3 ping, 0% loss
  - router05 -> `169.254.250.3`: 3/3 ping, 0% loss
  - router03 到 `http://169.254.250.5:9443/` 的 curl: eventd 回傳 HTTP 404, 證明監聽器可達

## 斷言

### A. 傳送側本地儲存

PASS。傳送側儲存了事件:

- ID: `evt-phase2-smoke-20260530T092231Z`
- Group: `cloudedge-event-smoke`
- SourceNode: `onprem-event-node`
- Type: `routerd.client.ipv4.observed`
- Subject: `10.88.60.9/32`

### B. 傳送側傳遞

PASS。傳送側的傳遞到達接收側 peer:

- EventID: `evt-phase2-smoke-20260530T092231Z`
- Peer: `cloud-event-node`
- Status: `delivered`
- Attempts: `1`
- DeliveredAt: `2026-05-30T09:22:41Z`

### C. 接收側儲存

PASS。接收側儲存了相同事件 ID:

- EventID: `evt-phase2-smoke-20260530T092231Z`
- 接收側 RecordedAt: `2026-05-30T09:22:43Z`
- 首次傳遞後的接收側狀態: `received=1 duplicate=0 rejected=0 storedEvents=1`

### D. 冪等重複

PASS。重新 emit 相同事件 ID 未在接收側建立第二個事件。

- 傳送側傳遞保持 `attempts=1`
- 接收側僅有 1 個 ID 為 `evt-phase2-smoke-20260530T092231Z` 的事件
- 接收側狀態保持 `received=1 duplicate=0 storedEvents=1`

### E. 非法 HMAC

PASS。攜帶非法 `X-Routerd-Signature` 的合成 POST 回傳:

- HTTP 狀態: `401 Unauthorized`
- Body: `bad signature`
- 接收側儲存無變更
- 接收側狀態 `rejected=1` 遞增

### F. 過期事件

PASS。過期事件在傳送側本地儲存但未被推送。

- 過期 EventID: `evt-expired-20260530T092347Z`
- ObservedAt: `2026-05-30T09:14:02Z`
- ExpiresAt: `2026-05-30T09:14:03Z`
- 傳送側傳遞查詢: `null`
- 接收側未收到過期事件

### G. 重啟後恢復

PASS。傳送側 eventd 停止期間 emit 的新事件在傳送側 eventd 服務重啟後被傳遞。

- 恢復 EventID: `evt-resume-20260530T092347Z`
- emit 前的傳送側 eventd: `inactive`
- 傳送側重啟前的接收側: 僅原始主事件
- 重啟後的傳遞: `delivered`, `attempts=1`, `deliveredAt=2026-05-30T09:24:18Z`
- 恢復後的接收側狀態: `received=2 duplicate=0 rejected=1 storedEvents=2`

## 已知實驗室備註

- PVE 接收側已有防火牆的 default-drop 策略。為使 eventd 能在新 overlay 介面上接收流量, 此次冒煙測試將 `WireGuardInterface/wg-hybrid` 新增到 router05 的既有管理防火牆 zone。
- 新增了 overlay peer 位址的顯式 `/32` 路由資源:
  router03 上 `169.254.250.5 dev wg-hybrid metric 120`,
  router05 上 `169.254.250.3 dev wg-hybrid metric 120`。
- 執行手冊使用了 `routerctl federation event deliveries --group ...`,
  但當前 CLI 支援透過 `--event-id` 查找傳遞。斷言中使用了
  `--event-id`。
- `make dist` 最初未將 `routerd-eventd` 包含在發佈載荷中。
  在工作樹中的追加使其包含在 Makefile 的發佈建置/安裝列表中。

## 判定

CloudEdge Event Federation Phase 2 傳輸專用冒煙測試通過:

- 本地 emit 持久化到傳送側 SQLite
- outbox 迴圈向 EventPeer 推送
- 接收側 HMAC 驗證接受有效事件
- 接收側持久化相同事件 ID
- 傳送側傳遞變為 `delivered`
- 重複 ID 被冪等處理
- 非法 HMAC 被 401 拒絕
- 過期事件未被傳遞
- 重啟後恢復證明了基於 SQLite 的 outbox 傳遞
- 未使用 EventSubscription、plugin 觸發、DynamicConfigPart、ARP observer、provider action、雲端 mutation
