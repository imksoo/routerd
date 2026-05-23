---
title: 狀態資料庫
slug: /operations/state-database
---

# 狀態資料庫

routerd 將狀態與事件持久化至 SQLite。每個常駐程式除此之外還各自擁有自身的租約或狀態檔案與事件日誌。

## 主要路徑

| 種類 | 路徑 |
| --- | --- |
| routerd 狀態 DB | `/var/lib/routerd/routerd.db` |
| DHCPv6-PD 租約 | `/var/lib/routerd/dhcpv6-client/<name>/lease.json` |
| DHCPv4 租約 | `/var/lib/routerd/dhcpv4-client/<name>/lease.json` |
| PPPoE 狀態 | `/var/lib/routerd/pppoe-client/<name>/state.json` |
| HealthCheck 狀態 | `/var/lib/routerd/healthcheck/<name>/state.json` |
| 常駐程式別事件 | `/var/lib/routerd/<daemon>/<name>/events.jsonl` |

## events 資料表

事件匯流排會將事件持久化至 SQLite。`EventRule` 與 `DerivedEvent` 以此串流作為輸入。
日常運維請使用 `routerctl events`，而非直接操作 `sqlite3`：

```sh
routerctl events --limit 20
routerctl events --topic routerd.resource.status.changed
routerctl events --resource DNSResolver/lan-resolver -o json
```

## 備份思維

狀態 DB 保存的是**已觀測到**的狀態，無法取代設定。
意圖的正本是 YAML 設定檔，請以 git 管理。
重建主機時，比起還原 SQLite，套用設定檔並讓 routerd 進行調和（reconcile）更為可靠。

若出於鑑識目的需要保留操作事件歷史，請定期為 `events.db`、`dns-queries.db`、`traffic-flows.db`、`firewall-logs.db` 建立快照。這些檔案為僅附加模式，不需要像 `routerd.db` 那樣進行特定時間點的備份。

## 相關說明

- [日誌儲存](../concepts/log-storage.md)
- [調和（Reconcile）與刪除](./reconcile)
