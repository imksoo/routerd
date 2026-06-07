# 日誌儲存

![routerd log writer、平台派生 SQLite store、retention 與唯讀運維 view 的關係圖](/img/diagrams/concept-log-storage.png)

routerd 將長期狀態與運作日誌分開儲存。

Linux 的預設配置如下：

| 檔案 | 用途 | 標準保存期限 |
| --- | --- | --- |
| `/var/lib/routerd/routerd.db` | 資源狀態與事件表 | 事件 30 天 |
| `/var/lib/routerd/dns-queries.db` | `routerd-dns-resolver` 的 DNS 查詢歷史 | 30 天 |
| `/var/lib/routerd/traffic-flows.db` | 從 conntrack 建立的通訊流量歷史 | 30 天 |
| `/var/lib/routerd/firewall-logs.db` | accept、drop、reject 的防火牆日誌 | 90 天 |

FreeBSD 上，相同的資料庫名稱存放於 `/var/db/routerd` 之下。

日誌表的欄位名稱以方便轉換為 OpenTelemetry 日誌屬性的方式命名。
`traffic-flows.db` 中為 nDPI 與 TLS SNI 預留了欄位，
但目前尚未實作向這些欄位寫入的處理，將在後續實作中新增。

`LogRetention` 依信號單位刪除舊有資料列，
也可執行 SQLite 的 incremental vacuum。DB 檔案路徑不出現在設定中，
routerd 從產生事件、DNS 查詢、通訊流量、防火牆事件的資源中導出。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: LogRetention
metadata:
  name: default
spec:
  retention: 30d
  schedule: daily
  vacuum: true
  signals:
    - events
    - dnsQueries
    - trafficFlows
  sinks:
    - LogSink/local-syslog
---
apiVersion: system.routerd.net/v1alpha1
kind: LogRetention
metadata:
  name: firewall-events
spec:
  retention: 90d
  schedule: daily
  vacuum: true
  signals:
    - firewallEvents
```

確認時使用以下指令：

```sh
routerctl dns-queries --since 1h
routerctl traffic-flows --since 1h
routerctl firewall-logs --since 24h --action drop
```
