---
title: 從這裡開始
slug: /
sidebar_position: 0
sidebar_label: 開始
---

# 歡迎使用 routerd

![routerd 文件地圖：從安裝與第一台路由器，延伸到概念、設定範例、教學、維運與 API 參考](/img/diagrams/intro.png)

routerd 讓你以 YAML 寫下「這台電腦要成為怎樣的路由器」，再把這個目標轉成主機上的網路設定和服務。可以把 YAML 想成一張可檢查的清單：哪個介面接上游、哪個介面服務本地裝置，以及要開哪些功能。

第一次嘗試請使用**隔離的 Ubuntu Server VM 或備用電腦**。Ubuntu Server + systemd 是目前最適合依照本入門文件操作的環境。NixOS 與 FreeBSD 有整合基礎，但功能和操作流程不與 Ubuntu 完全對等；上線前請先看[支援的平台](./platforms.md)。

## 先認識兩個程式

- `routerd` 是主要程式。它能直接檢查本機的檔案，例如 `routerd validate --config …`；也能進行一次不提交網路變更的試跑：`routerd apply --once --dry-run …`。
- `routerctl` 是連到**正在執行的** `routerd serve` 的客戶端。它經由本機 Unix socket 看狀態、送出候選設定或要求計畫；它不是離線 YAML 檢查工具。

所以在服務尚未啟動時，應先使用 `routerd validate`，而不是 `routerctl validate` 或 `routerctl plan`。

## 建議的安全起點

1. 準備 VM 主控台、序列主控台，或完全獨立的管理網卡。
2. 依照[安裝與升級](./install-and-upgrade.md)在 Ubuntu Server 安裝 routerd。
3. 先閱讀[網路基礎](./tutorials/network-basics.md)，再完成[安全起步](./tutorials/getting-started.md)：驗證檔案，接著以暫存路徑做 dry-run。
4. 確認不會中斷管理連線後，才讓 `routerd serve` 或 systemd 服務套用真實設定。
5. 服務運行後，使用 `routerctl get status` 和 `routerctl describe …` 觀察。

:::caution 不要把第一次實驗放在生產網路
`routerd apply --once`（未加 `--dry-run`）和 `routerd serve` 都可能改變主機網路。不要透過同一條即將被改動的 SSH 連線，去實驗唯一承載家中、學校或工作網路的路由器。請保留主控台或另一條管理路徑。
:::

:::caution 防火牆不是安全認證
routerd 仍在預發布階段。防火牆資源仍在基礎實作階段，不是通用防火牆語言或安全認證。不要把網站上的單一範例當成面向網際網路設備的唯一防線。
:::

## 依目的選頁面

| 你想做什麼 | 建議從哪裡開始 |
| --- | --- |
| 認識 WAN、LAN、DHCP、DNS 與 NAT | [網路基礎](./tutorials/network-basics.md) |
| 在 Ubuntu Server 安裝或升級 routerd | [安裝與升級](./install-and-upgrade.md) |
| 不變更網路地檢查第一份 YAML | [安全起步](./tutorials/getting-started.md) |
| 讓 WAN 以 DHCP 取得 IPv4，並設定 LAN 閘道 | [第一台實驗路由器](./tutorials/first-router.md) |
| 看完整 IPv4 NAT 的結構 | [基本 IPv4 NAT 閘道](./config-examples/basic-ipv4-nat.md) |
| 搞清楚「套用」「產生」「調和」 | [套用、產生與調和](./concepts/apply-and-render.md) |
| 檢查作業系統支援範圍 | [支援的平台](./platforms.md) |
| 在服務運行後檢查健康狀態 | [routerctl doctor](./operations/routerctl-doctor.md) |

## 接下來

- [安裝 routerd](./tutorials/install.md)
- [安全起步：驗證和 dry-run](./tutorials/getting-started.md)
- [設定範例集](./config-examples/index.md)
