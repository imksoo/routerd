---
title: routerd 自行實作 DHCPv6-PD 用戶端的原因
---

# routerd 自行實作 DHCPv6-PD 用戶端的原因

![Diagram showing why routerd owns DHCPv6-PD from OS client variation and stale prefix risk through routerd-dhcpv6-client lease state, status, delegated LAN address inputs, and HA DUID operation](/img/diagrams/knowledge-base-dhcpv6-pd-clients.png)

routerd 目前的方針是由專屬常駐程式 `routerd-dhcpv6-client` 負責 DHCPv6-PD。
過去評估過的 OS 內建用戶端方案，並未納入現行設定範例。

## 改採專屬常駐程式的原因

DHCPv6-PD 不僅止於取得前綴，Renew、重新啟動後的還原，以及事件記錄同樣重要。
若只是為 OS 內建用戶端產生設定，難以將 routerd 的狀態模型與 LAN 側的套用過程整合得乾淨俐落。

改為專屬常駐程式後，具備以下能力：

- 將租約儲存至 `lease.json`
- 啟動時還原租約
- 將 Renew 結果記錄至事件日誌
- 透過 `/v1/status` 回傳 `Bound` / `Pending` 狀態
- 發出供其他控制器（LAN 位址派生、RA、DHCPv6 伺服器、DS-Lite、DNS）消費的事件

## 二進位與部署位置

```text
routerd-dhcpv6-client
```

| 路徑 | 用途 |
| --- | --- |
| `/run/routerd/dhcpv6-client/<name>.sock` | 各資源的控制插槽 |
| `/var/lib/routerd/dhcpv6-client/<name>/lease.json` | 租約持久化 |
| `/var/lib/routerd/dhcpv6-client/<name>/events.jsonl` | 僅追加的事件日誌 |

## 評估後未採用的替代方案

我們比較了 `systemd-networkd`、WIDE/KAME 系用戶端及其他 DHCP 用戶端，
最終採用由 routerd 自行擁有的常駐程式。
這些調查結果作為背景資料仍具參考價值，但不包含在目前的出貨設定中。

目前的 Kind 為 `DHCPv6PrefixDelegation`，並未提供用於選擇 OS 內建實作的 `client` 欄位，此為刻意設計。

## 操作注意事項

請勿在同一個 WAN 介面上同時執行多個 DHCPv6-PD 用戶端。
同時發出兩個用戶端會造成上游混亂，導致無法收到 Reply。

移轉至 routerd 管理時，請先停止舊有用戶端
（包含其租約檔案，以及啟動該用戶端的 systemd / rc.d 設定），
再啟動 `routerd-dhcpv6-client`。
