# Event Federation 參考

![展示從觀測到的本機事實到 EventGroup、routerd-eventd push 分發、EventSubscription 比對、plugin 衍生的 DynamicConfigPart 輸出的 Event Federation 示意圖](/img/diagrams/reference-event-federation.png)

> 實驗性（CloudEdge）。關於設計和不變條件，請參見 [ADR 0006: CloudEdge Event Federation](../adr/0006-event-federation.md)；
> 關於實踐範例，請參見 how-to 的
> [Event Federation subscription](../how-to/event-federation-subscription.md)。

Event Federation 是一種機制，透過 overlay 在 routerd 節點之間交換**類型化的觀測事實**
（例如：「此用戶端 IPv4 已被觀測到」、「此位址已過期」），訂閱者將比對的事件透過 plugin
轉換為衍生設定。它是[選擇性位址移動性](./selective-address-mobility)下的控制平面基礎設施，
一個節點上觀測到的位址會成為另一個節點的 `RemoteAddressClaim`（capture）。

模型是**冪等觀測事實事件的 at-least-once 分發**。事件是關於世界的不可變描述
（"observed"），而非命令式指令。對於從同一事件重新導出相同狀態的接收者來說是 no-op。

## Kind

### `EventGroup`

節點參加的匯流排。每個節點在每個群組中擁有一個 identity。

| 欄位 | 含義 |
|---|---|
| `nodeName` | 此節點在群組內的 identity。作為 `sourceNode` 印刻在發佈的事件上。 |
| `retention` | 本機儲存保留事件的數量/時長上限。空/零 = 無限制。 |
| `auth` | 對等分發（push）用的 HMAC 金鑰材料。 |
| `listen` | 入站對等 push 的接收器繫結（`address`）。空 = 僅 push（無接收器）。 |
| `replayWindow` | 用於重放保護而接受的訊息時間戳偏差上限的 Go duration（預設 `5m`）。 |

### `EventPeer`

此節點向其 push 事件的遠端節點。

| 欄位 | 含義 |
|---|---|
| `groupRef` | 此對等方所屬的 `EventGroup`（必要）。 |
| `nodeName` | 遠端對等方的節點 identity（必要）。 |
| `endpoint` | push 目標的基礎 URL。例：`http://10.99.0.7:8787`（push 時必要）。 |
| `direction` | 分發方向。僅支援 `push`。為空時預設 `push`。 |
| `types` | 選用的事件類型允許清單。為空時全部分發。 |
| `subjectPrefixes` | 選用的主題前綴允許清單。為空時全部分發。 |

### `EventSubscription`

將比對的事件轉換為發出 `DynamicConfigPart` 的 plugin 呼叫。

| 欄位 | 含義 |
|---|---|
| `groupRef` | 消費的 `EventGroup`。 |
| `match` | 按類型/主題比對事件的條件。 |
| `trigger.pluginRef` | 比對事件時呼叫的 `Plugin`。 |
| `trigger.batchWindow` | 將比對事件彙總為一次呼叫的 Go duration。 |
| `trigger.debounce` | 在最後一個比對事件之後延遲呼叫的 Go duration。 |

## `routerctl federation` CLI

```
routerctl federation event emit  --group <g> --type <topic> --subject <entity> [--source-node <n>] [--ttl <dur>] [--payload k=v ...]
routerctl federation event list  --group <g>
routerctl federation event deliveries --group <g>
```

`emit` 將觀測事實記錄到本機儲存（例如：
`--type routerd.client.ipv4.observed --subject 10.88.60.9/32`）。`list` 顯示已記錄的
事件，`deliveries` 顯示每個對等方的 push 分發狀態。

> 自我擷取保護（ADR 0006 的 no-feedback-loop 不變條件）：節點不得為自身透過本機
> `RemoteAddressClaim` capture 的位址發出 `routerd.client.ipv4.observed`。否則，
> 已分發的 capture 位址將作為新觀測回圈。

## 傳輸 — `routerd-eventd`

`routerd-eventd@<group>` 是每組一個的長壽命常駐程式（Linux 上由產生的 systemd unit
管理，FreeBSD 上由 rc.d 管理），執行以下操作：

- 將本機記錄的事件透過 HTTP **push** 到每個 `EventPeer`，並使用群組 HMAC 簽章。
  接收方驗證簽章並拒絕 `replayWindow` 之外的訊息。
- 按對等方/事件記錄**分發**狀態，限制 at-least-once 重試的範圍並使其可觀測。
- 根據群組的 `retention` **修剪**本機事件儲存。

outbox 具有 `sourceNode` 保護，接收到的事件不會被轉發回發起方（無分發迴圈）。

## Subscription -> plugin -> DynamicConfigPart 流程

1. 節點發出觀測事實（`routerctl federation event emit`，或未來的 observer）。
2. `routerd-eventd` 向對等方分發，各對等方記錄到自身的事件儲存。
3. 對等方的 `EventSubscription` 比對事件並呼叫 `trigger.pluginRef`
   （透過 `batchWindow` / `debounce` 彙總）。
4. plugin 回傳 `DynamicConfigPart`（例如：`RemoteAddressClaim`），
   [dynamic-config](./dynamic-config.md) 鏈將其整合到 effective config 並
   reconcile 到資料平面。

這樣維運人員編寫的 intent 保持宣告式。維運人員宣告 group/peers/subscription，
claim、capture、action plan 均為**衍生**產物。
