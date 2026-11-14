---
title: 詞彙表
sidebar_label: 詞彙表
sidebar_position: 1
---

# 詞彙表

routerd 文件中使用的主要術語與譯詞。

## 網路術語

| 英文 | 譯詞（本文件） | 備註 |
| --- | --- | --- |
| interface | 介面 | 主機上的網路介面 |
| route / routing | 路由 | 轉送條目與其選擇 |
| gateway | 閘道 | 離開網路時使用的下一跳路由器 |
| NAT | NAT | |
| NAPT | NAPT | 動態的多對一轉換 |
| firewall | 防火牆 | routerd 的區域式有狀態過濾功能 |
| filter / rule | 過濾條件 / 規則 | 個別的允許或拒絕規則 |
| prefix delegation (PD) | 前綴委派（PD） | DHCPv6 前綴委派 |
| upstream | 上游 | DNS 或路由的上游側 |
| egress / ingress | egress / ingress | 送出側 / 接收側，保留英文 |

## 宣告式模型術語

| 英文 | 譯詞（本文件） | 備註 |
| --- | --- | --- |
| declarative | 宣告式 | 描述期望狀態而非步驟 |
| resource | 資源 | |
| Kind | Kind（種類） | 保留大寫 Kind |
| spec | spec | 期望狀態 |
| status | status | 實際觀測狀態 |
| apply | 套用 | `routerctl apply` 的動作 |
| reconcile | 調和（reconcile） | 使實際狀態趨近期望狀態的處理 |
| controller | 控制器 | |
| render | 產生（render） | 由資源組出設定檔等成品 |
| daemon | 常駐程式 | |
| generation | 世代（generation） | SQLite 的世代編號 |
| ownership | 擁有 / 擁有權 | |
| bootstrap | 啟動設定（bootstrap） | |
| Tier (H/S/C/E) | Tier H / Tier S … | 功能階段的專有名詞 |

## 其他標記

- **Web Console**（routerd 的網頁 UI）寫作「Web 管理介面」。但 `WebConsole`（啟用該 UI 的 Kind 名稱）是程式識別字，保留原文。
- **HGW** 保留為「HGW」，不展開。
