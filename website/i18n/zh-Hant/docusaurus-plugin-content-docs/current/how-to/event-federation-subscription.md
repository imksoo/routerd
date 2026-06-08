---
title: 將聯邦事件轉換為 RemoteAddressClaim
---

# 將聯邦事件轉換為 RemoteAddressClaim

![聯邦用戶端觀測事件匹配 EventSubscription、執行外掛程式、產生帶有 RemoteAddressClaim provenance 的 DynamicConfigPart 的流程](/img/diagrams/how-to-event-federation-subscription.png)

CloudEdge Event Federation（ADR 0006）是一種機制，使一個 routerd 節點能夠對另一個節點觀測到的事實進行宣告式回應。Phase 3 閉合了接收端的迴圈：接收的事件匹配 `EventSubscription`，執行受信任的本機外掛程式，其輸出成為可透過 `routerctl dynamic render` 確認的 `DynamicConfigPart`。

本指南使用隨附的提供者無關範例外掛程式 `event-to-remote-claim`。

## 流程

```
on-prem routerd                         cloud routerd
---------------                         -------------
觀測 LAN 用戶端
  -> 發布聯邦事件 --push-->  接收事件 (EventGroup)
     routerd.client.ipv4.observed         |
                                          v
                                   EventSubscription 匹配
                                          |
                                          v
                                   Plugin 執行 (event-to-remote-claim)
                                          |
                                          v
                                   PluginResult -> DynamicConfigPart
                                          |
                                          v
                                   routerctl dynamic render
                                     顯示 RemoteAddressClaim
```

1. **發布** — on-prem 節點觀測到用戶端，向共享 `EventGroup` 發布 `routerd.client.ipv4.observed` 事件。
2. **傳輸（Phase 2）** — 事件透過 overlay 推送到 cloud 節點的 `EventGroup` 接收端。
3. **匹配** — cloud 節點的 `EventSubscription` 按類型（以及可選的 subject 前綴 / 來源節點）匹配事件。
4. **外掛程式** — subscription 的 `trigger.pluginRef` Plugin 接收匹配事件的 stdin 並執行，回傳 `PluginResult`。
5. **DynamicConfigPart** — routerctl 驗證結果，儲存為帶有 provenance 註解（`routerd.net/event-id`、`routerd.net/event-group`、`routerd.net/dynamic-source`）的動態組態部分。
6. **渲染** — `routerctl dynamic render` 顯示包含新 `RemoteAddressClaim` 的有效組態。

## 範例資源

- 接收端（cloud）的配線：[`examples/event-federation/receiver-cloud.yaml`](https://github.com/imksoo/routerd/blob/main/examples/event-federation/receiver-cloud.yaml) — `EventGroup`、`EventSubscription`、`Plugin`，以及解析結果 `RemoteAddressClaim` 的混合上下文（`OverlayPeer`、`AddressMobilityDomain`、`CloudProviderProfile`）。
- 傳送端（on-prem）的配線：[`examples/event-federation/sender-onprem.yaml`](https://github.com/imksoo/routerd/blob/main/examples/event-federation/sender-onprem.yaml) — `EventGroup` + `EventPeer` 推送目標。
- 範例外掛程式：[`examples/plugins/event-to-remote-claim/`](https://github.com/imksoo/routerd/tree/main/examples/plugins/event-to-remote-claim)。

## 試試看

建置並安裝範例外掛程式：

```sh
go build -o bin/event-to-remote-claim ./examples/plugins/event-to-remote-claim
install -D bin/event-to-remote-claim \
  /usr/local/libexec/routerd/plugins/event-to-remote-claim/bin/event-to-remote-claim
```

套用接收端組態，並發布測試事件（通常由 Phase 2 從 on-prem 節點交付事件）：

```sh
routerctl federation event emit \
  --state-file /var/lib/routerd/routerd.db \
  --group cloudedge --type routerd.client.ipv4.observed \
  --subject 10.88.60.9/32 --source-node onprem-router \
  --payload address=10.88.60.9/32 \
  --payload domain=cloudedge-same-subnet \
  --payload ownerSide=onprem \
  --payload peerRef=onprem-main \
  --payload providerRef=example-provider \
  --ttl 30m
```

EventSubscription controller reconcile 後，渲染有效組態：

```sh
routerctl dynamic render \
  --config /usr/local/etc/routerd/router.yaml \
  --state-file /var/lib/routerd/routerd.db
```

將顯示帶有事件 provenance 註解的 `10.88.60.9/32` 的 `RemoteAddressClaim`。

## 範圍與安全性

- 範例外掛程式是**提供者無關的**，**不執行任何雲端變更**。`capture` 區塊是 dry-run 意圖的佔位符（`configureOSAddress: false`）。
- 實際發起位址 claim 的提供者操作（`actionPlan`）執行屬於 **Phase 4/5**，MVP 中不執行操作計畫。
- routerd 不向外掛程式傳遞組態或 secret — 僅傳遞觀測到的事件。
- `EventSubscription.match.types` 是必要的，因此 subscription 不會在群組內所有事件上無差別地觸發外掛程式。為防止迴圈，請使用 `subjectPrefixes` 和 `sourceNodes` 進一步縮小範圍。
