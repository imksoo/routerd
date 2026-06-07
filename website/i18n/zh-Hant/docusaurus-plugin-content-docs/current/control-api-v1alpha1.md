---
title: 控制 API v1alpha1
slug: /reference/control-api-v1alpha1
---

# 控制 API v1alpha1

![Diagram showing the Control API v1alpha1 local socket model from routerd main sockets and managed daemon sockets to status, events, commands, resource phases, and local-only client contracts](/img/diagrams/control-api-v1alpha1.png)

routerd 與受管理的常駐程式會在本機的 Unix domain socket 上公開 HTTP+JSON API。
此 API 並非用於遠端管理，而是供 `routerctl`、routerd 本體以及運維腳本在同一主機上讀取狀態所使用。

## routerd 本體

`routerd serve` 監聽以下 socket：

```text
/run/routerd/routerd.sock
/run/routerd/routerd-status.sock
```

主控制 socket 供具有權限的本機客戶端使用，並公開套用（apply）、刪除等變更類 endpoint。唯讀的 status socket 僅公開狀態查詢類 endpoint，供一般使用者確認系統狀態。

主控制 socket 的讀取 endpoint 可回傳狀態、事件及資源狀態，主要範例如下：

| Method + Path | 用途 |
| --- | --- |
| `GET /api/control.routerd.net/v1alpha1/status` | routerd 自身的狀態 |
| `GET /api/control.routerd.net/v1alpha1/connections` | 從 conntrack 或 pf state 取得的當前連線 |
| `GET /api/control.routerd.net/v1alpha1/dns-queries` | DNS 查詢歷程 |
| `GET /api/control.routerd.net/v1alpha1/traffic-flows` | 通訊流量歷程 |
| `GET /api/control.routerd.net/v1alpha1/firewall-logs` | 防火牆日誌 |

## Controller status

`Status.status.controllers` 與 `Controllers` endpoint 會回傳控制器在設定上的 mode，以及執行時期的調和（reconcile）狀態。runtime 欄位包含 `interval`、`lastTrigger`、`lastReconcileTime`、`nextReconcileTime`、`reconcileCount`、`reconcileErrorCount`、`consecutiveErrorCount`、`currentError`、`lastDuration`、`maxDuration`、`averageDuration`、`lastError`、`lastErrorTime`、`lastErrorClearedAt`。`reconcileErrorCount` 為累計值，如需判斷目前是否處於失敗狀態，請使用 `currentError` 與 `consecutiveErrorCount`。這些皆為觀測值，若控制器尚未執行過，請視為欄位不存在。

## 受管理的常駐程式

具有狀態的常駐程式各有其專屬的 socket：

```text
/run/routerd/dhcpv6-client/wan-pd.sock
/run/routerd/dhcpv4-client/wan.sock
/run/routerd/pppoe-client/wan-pppoe.sock
/run/routerd/healthcheck/internet.sock
```

在 FreeBSD 上，對應路徑為 `/var/run/routerd/...`。

## 常駐程式共通 endpoint

| Method + Path | 用途 |
| --- | --- |
| `GET /v1/healthz` | 存活確認（liveness check） |
| `GET /v1/status` | 常駐程式的狀態及相關資源的狀態 |
| `GET /v1/events` | 事件日誌。可透過 query 參數指定 `since`、`wait`、`topic` |
| `POST /v1/commands/reload` | 重新載入設定 |
| `POST /v1/commands/renew` | 各常駐程式的主動操作（DHCPv6 Renew、DHCPv4 更新租約、立即執行健康探測等） |
| `POST /v1/commands/stop` | 安全停止 |

`renew` 的意義因常駐程式而異。DHCPv6 為傳送 Renew、DHCPv4 為更新租約、healthcheck 為立即執行探測。

## Phase 詞彙

`ResourceStatus.phase` 跨資源使用共通詞彙：

| Phase | 說明 |
| --- | --- |
| `Pending` | 等待必要的輸入 |
| `Bound` | 持有 DHCP 等租約 |
| `Applied` | 已套用至主機端 |
| `Up` | tunnel 或 link 已啟動 |
| `Installed` | 路由或設定檔已就位 |
| `Healthy` | 健康檢查已達成功閾值 |
| `Unhealthy` | 健康檢查已達失敗閾值 |
| `Error` | 操作失敗 |

每個 phase 均附有 `conditions` 陣列。客戶端程式碼應使用 `phase` 與 `conditions` 進行判斷，而非解析日誌字串。

## 事件

事件具有 topic 與 attributes：

```json
{
  "topic": "routerd.dhcpv6.client.prefix.renewed",
  "attributes": {
    "resource.kind": "DHCPv6PrefixDelegation",
    "resource.name": "wan-pd"
  }
}
```

routerd 將事件持久化至 SQLite。
受管理的常駐程式另外也會記錄至各自的 `events.jsonl`。
`EventRule` 與 `DerivedEvent` 以此串流為輸入，發布虛擬事件。
