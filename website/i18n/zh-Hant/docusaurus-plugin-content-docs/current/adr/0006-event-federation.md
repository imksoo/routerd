# ADR 0006: CloudEdge Event Federation（routerd 間的型別化事件）

![ADR 0006 Event Federation 示意圖。從手動描述 claim 的問題出發，到 EventGroup、EventPeer、EventSubscription 的設計決策，以及 observed-fact 不變量](/img/diagrams/adr-0006-event-federation.png)

## 狀態

已核准。實驗性實作進行中 — 2026-05-30。
Phase 1、1.5、2、3 已在 **`event-federation` 分支實作**：

- **Phase 1**（事件信封 + `EventGroup` Kind + SQLite 本機儲存 + `routerctl
  federation event emit/list`）— 完成。
- **Phase 1.5**（`EventPeer`/`EventSubscription` Kind + 驗證）— 完成。
- **Phase 2**（經由 overlay 的 peer 投遞、`routerd-eventd`、HMAC、重試、
  retention 清理）— 完成。**lab-smoke PASS**
  （[傳輸證據](../releases/evidence/cloudedge-event-federation-transport-20260530.md)）。
- **Phase 3**（subscription → plugin → `RemoteAddressClaim` `DynamicConfigPart`）—
  完成。**lab-smoke PASS**
  （[subscription 證據](../releases/evidence/cloudedge-event-federation-subscription-20260530.md)、
  [how-to](../how-to/event-federation-subscription.md)）。

Phase 4（provider `actionPlan` 外掛、dry-run）**下一階段尚未開始**。
Phase 5（provider action 執行）**不在 MVP 範圍內**。

## 背景

SAM（[參考](../reference/selective-address-mobility)、
[里程碑](../releases/cloudedge-sam-mvp-milestone.md)）已在
Azure×PVE、AWS×PVE、OCI×PVE 上完成乾淨驗證（3 雲對等）。SAM 證明了
**capture（provider 特定）/ delivery+claim（routerd 通用）** 的分離。然而，
驅動它的 `RemoteAddressClaim` **目前仍是手動描述的**。下一步是透過
**事件驅動**來發現、傳播和實體化 claim：

> on-prem 的 routerctl 偵測到用戶端 IPv4（ARP/Clients/DHCP）→ 發出型別化事件 →
> federation 匯流排將其投遞到雲端 routerd → subscription 啟動 provider 外掛 →
> 外掛以 `DynamicConfigPart` 形式傳回 `RemoteAddressClaim`
> （+ provider secondary-IP `actionPlan`）→ **無需人工編輯雲端組態**，
> 雲端即準備好執行 `provider-secondary-ip` capture。

### 現有資產（MVP 並非從零開始）

設計基於目前的程式碼樹。大多數建構區塊已經存在，
真正全新的工作是**節點間 federation 傳輸**和
**事件→外掛 subscription 觸發器**：

- **型別化事件信封**: `pkg/daemonapi` 的 `DaemonEvent{Type,Time,Daemon,Resource,
  Severity,Reason,Message,Attributes}` + `NewEvent(...)`。目前是 daemon→main 流程，
  但已經是帶型別和 topic 的信封。
- **daemon→routerd 傳輸模式**: daemon 透過 UNIX 套接字上的
  HTTP POST 到控制套接字（`cmd/routerd-dhcp-event-relay` → `controlapi.Prefix +
  /dhcp-lease-event` via `unix:/run/routerd/routerd.sock`）。已有*事件中繼 daemon 的先例*。
- **分離的長生命週期 daemon 先例**: 13 個 `cmd/routerd-*` daemon
  （`routerd-bgp`、`routerd-ra-observer`、`routerd-dhcp-event-relay` 等）。
  gobgp pivot（ADR 0004）確立了「為避免重啟導致的中斷，使用分離行程而非行程內嵌入」。
- **Plugin → DynamicConfigPart 管線**: `pkg/plugin/runner.go`、
  `pkg/plugin/dynamic_config.go`、`pkg/dynamicconfig/{types,merge}.go`、
  `PluginRequest`/`PluginResult`。effective = startup + active dynamic − masks。
- **狀態**: SQLite（`pkg/state/sqlite.go`）。
- **Provider profile + 外部驗證**: `CloudProviderProfile`、
  `auth.mode=external-command`（specs.go:1193）— provider 特定外掛的鉤子。
  `provider: oci|aws|azure|gcp` 已通過驗證。

## 決策

將 **CloudEdge Event Federation** 作為下一個實驗性 MVP，建構在已合併的實驗性 SAM 之上的新分支中。**不縮減範圍，而是分解為有序的、可獨立驗收的階段，每個階段作為一個工作流驅動。** 每個階段交付一個可運作、可展示的切片，並作為下一階段的準入閘門。

### 設計原則

1. **事件是觀測事實，不是組態。** 節點傳送
   `routerd.client.ipv4.observed`，而不傳送原始的 `RemoteAddressClaim`。接收端的
   *受信任本機外掛*決定是否以及如何將其轉換為型別化 claim + actionPlan。線路上不傳輸命令。
2. **at-least-once + 冪等**，而非 exactly-once。儲存的冪等性以事件 `id` 為鍵
   （重複 `id` 為 no-op insert）。`dedupeKey` 是 subscription 端的分組鍵，
   用於彙總同一事實的重複觀測，在 Phase 1 中**不是** DB 的唯一約束。動態資源名稱是確定性的
   （`onprem-10-88-60-9`）。provider action 如已滿足則為 no-op。無共識、無 gossip、無全序。
3. **複用，不重新發明。** 複用 `DaemonEvent` 信封、控制套接字 HTTP 傳輸慣例、
   Plugin→DynamicConfigPart 管線、SQLite 狀態、`CloudProviderProfile`/`Plugin`
   （不需要新的 `CloudProviderPlugin` Kind）。
4. **新增 Kind 最少化。** MVP 引入 **3 個**：`EventGroup`（匯流排識別符 + 驗證 + retention）、
   `EventPeer`（投遞目標 + 內聯的 push/receive 篩選器）、
   `EventSubscription`（接收事件 → 本機外掛觸發器）。原提案中獨立的
   `EventFilter` 已合併到 `EventPeer`，僅在需要跨 peer 共享篩選器時才提升為獨立 Kind。
5. **分離的 daemon。** Federation 的收發放在新的
   `cmd/routerd-eventd` 長生命週期 daemon 中（遵循 ADR 0004 先例）。不在 reconcile 迴圈內。
   僅繫結到 overlay（`wg-hybrid`）。
6. **MVP 中 provider mutation 保持 dry-run。** 外掛發出 `actionPlan`。
   執行放在後續階段，位於明確的 approval/auto-apply 策略之後。

### 傳輸與安全（MVP）

- 接收端 = **僅繫結到 WireGuard overlay 介面/位址**的 HTTP 監聽器
  （例：`169.254.x.y:9443`）。WG 隧道是機密性邊界。為完整性和防止誤投遞新增
  **訊息層級 HMAC**（來自檔案的共享密鑰）。
  **TLS 延後** — TLS 監聽器需要憑證佈建，會重新引入
  SAM stocktake 指出的引導摩擦。（未來：mTLS / 每 peer 的 Ed25519 / 雲 KMS 簽章。）
- MVP 中僅 push（`onprem→cloud` 的觀測，`cloud→onprem` 的 claim/result ack）。
- 帶退避的重試。每 (event, peer) 的投遞狀態保存在 SQLite。

### 應在狀態機層面審查的關鍵不變量（而非僅審查差分）

遵循專案對行程外有狀態 daemon 的規則，
將正確性條件描述為不變量：

- **禁止回饋迴圈。** 節點不得對自身正在 *capture* 的位址（provider-secondary-ip
  或 proxy-arp）重新發出 `*.observed`。觀測以 `ownerSide` + `domain` 為範圍，
  capture 的/secondary 位址從觀察者的來源集合中排除。
  否則，雲端自身的 secondary `.9` 會被重新觀測 → 重新傳播 → 震盪。
- **provision 與 de-provision 的不對稱性。** provisioning（claim 出現）可以即時。
  **de-provisioning（TTL 過期 / `*.expired`）必須具有遲滯性** —
  遠大於 300 秒 observe TTL 的寬限期 + 去抖。震盪的用戶端不得反覆驅動
  雲端 secondary-IP 的 assign/unassign（API 速率限制 + 成本 +
  資料平面擾動）。TTL→teardown 策略應明確且保守。
- **(domain, address) 單寫入者。** 擁有方具有權威。接收方僅為
  `ownerSide` 是*傳送方*的位址提議 claim。
- **冪等的 provider action。** "already assigned" ⇒ 跨 aws/azure/oci 均為 success/no-op。

### Provider 外掛框架

呼叫 OS CLI 的本機可執行檔。**不是**將 SDK 靜態連結到 routerd
（將 SDK 的變更/驗證排除在核心之外，啟用雲原生身分，便於除錯）：

- **AWS**: `aws ec2 assign-private-ip-addresses` — 驗證：優先 **IAM 執行個體設定檔**，
  `AWS_PROFILE`/env 回退。
- **Azure**: `az network nic ip-config …` — 驗證：優先**受控識別**，
  `az login`/SP env 回退。
- **OCI**: `oci network private-ip create` / `vnic` — 驗證：優先**執行個體主體**，
  OCI config profile 回退。

`Plugin.capabilities` 對外掛權限進行閘門控制
（`observe.events`/`propose.dynamicConfig`/`propose.providerAction`）。

## 階段分解（每階段 1 個工作流，依序執行）

每個階段 = 可獨立驗收的切片。後續階段以先行階段的驗收為閘門。
實作委託給 codex，claude 負責編排 + 審查。

- **已完成 — Phase 1 — 事件模型 + 本機儲存。** `EventGroup` Kind。將 `DaemonEvent`
  複用/擴充為外部 `Event` 信封（id, group, sourceNode, type, subject, ttl, dedupeKey, payload）。
  SQLite `federation_events` 資料表。`routerctl federation event emit/list`。
  *驗收條件:* emit→stored（帶 TTL）。重複 id 冪等。過期被忽略。
- **已完成（lab-smoke PASS）— Phase 1.5 — `EventPeer`/`EventSubscription` Kind + 驗證。**
- **已完成（lab-smoke PASS）— Phase 2 — 經由 overlay 的 peer 投遞。** `EventPeer` Kind。
  `routerd-eventd` 接收端繫結到 `wg-hybrid`。HMAC。push + 退避。`event_deliveries`。
  *驗收條件:* on-prem 經由 `wg-hybrid` push 到雲端。重複 push 冪等。錯誤 HMAC 被拒絕。
  `routerctl federation event deliveries`。`routerd-eventd` 按 `EventGroup` retention（`maxAge`/`maxEvents`）
  定期清理 `federation_events`。
- **已完成（lab-smoke PASS）— Phase 3 — subscription 觸發的外掛 → DynamicConfigPart。**
  `EventSubscription` Kind。事件批次 → `PluginRequest`。`PluginResult` →
  `DynamicConfigPart`（帶 `routerd.net/dynamic-source`、`event-id`、`event-group`
  註解）。去抖/batchWindow。`event_subscription_runs`。
  *驗收條件:* 雲端收到 `10.88.60.9/32` 的 `client.ipv4.observed` → 外掛 →
  `RemoteAddressClaim` DynamicConfigPart 可透過 `routerctl dynamic render` 確認。
  actionPlan 僅顯示，不執行。
- **下一步（未開始）— Phase 4 — provider actionPlan 外掛（dry-run）。** `aws/azure/oci-address-claim`
  範例外掛。標準化 `actionPlan` 格式。執行個體 ID 驗證。
  *驗收條件:* 外掛提議 assign-secondary-IP。無 mutation。計畫可透過
  `routerctl plugin`/`dynamic` 確認。
- **Phase 5 —（MVP 後）provider action 執行。** approval/auto-apply 策略、
  action 日誌、盡力 undo、身分文件。不在 MVP 範圍內。

首個端對端冒煙測試為 **手動 `routerctl federation event emit` →
federation → DynamicConfigPart**（Phase 1-3）。ARP/Clients 觀察者外掛在
該冒煙測試*之後*引入（以 `routerd-ra-observer` 為模型），以便隔離故障。

### MVP 事件類型

`routerd.client.ipv4.observed`、`…ipv4.expired`、`…dynamic.part.accepted/rejected`、
`…provider.action.planned/succeeded/failed`。首次冒煙測試僅需 `observed`+`expired`。

## 結論

- **正面影響:** 將 SAM 從手動描述轉變為事件驅動。小而可展示的階段。
  複用現有信封/傳輸/外掛/狀態。新增 Kind 不膨脹（3 個）。
  provider mutation 有閘門控制。從第一天起支援雲原生身分。
- **負面影響 / 風險:** 新增網路監聽器（透過 overlay 繫結 + HMAC 緩解）。
  迴圈/震盪和 provision/de-provision 的不對稱性需作為不變量強制執行（見上文）。
  at-least-once 將冪等性推到了外掛和命名上。TLS/mTLS 延後。
  de-provisioning 的自動化被有意*最後*啟用。
- **不在 MVP 範圍內:** 共識、exactly-once、gossip mesh、任意遠端命令執行、
  provider mutation 自動化、完整的 IP 生命週期自動化、遠端外掛登錄檔、
  跨節點組態重寫。

## 已知限制（實驗性）

- **`routerd-eventd` 的 supervision 為 systemd 和 FreeBSD `rc.d` 產生。**
  其他服務管理員需要渲染器的顯式支援才能自動管理 eventd。
- **`EventSubscription` 的 `batchWindow`/`debounce` 被接受但是粗粒度的。**
  欄位經過驗證，以輪詢粒度生效 — 控制器在
  **每次輪詢 tick** 時批次處理事件，而不是以精確的亞 tick 計時器運作。短的去抖視窗
  實際上會被向上取整到 tick 間隔。

## 範圍外 / 未來的開放問題

- 是否需要從 `cloud→onprem` 傳遞 ack 以外的內容（例如：雲端 secondary 存在後
  切換 on-prem proxy-arp 的 capture-ready 訊號）。
- 跨 peer 共享篩選器的功能（將 `EventFilter` 提升為獨立 Kind）。
- 多 peer / 3 節點以上的 group（MVP 針對已驗證的成對拓撲）。
