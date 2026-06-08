---
title: 文件說明
slug: /
sidebar_position: 0
sidebar_label: 概覽
---

# routerd 文件

![Diagram showing the routerd documentation map from install and first router goals through concepts, examples, tutorials, how-to guides, operations, API references, platforms, plugins, and schemas](/img/diagrams/intro.png)

routerd 是一個宣告式路由器，透過以型別化 YAML 描述的期望狀態，在 Linux / NixOS / FreeBSD 上建構可運作的路由器。無需以程序步驟堆疊設定，只需宣告您想要的狀態，routerd 便會將實際系統收斂至該狀態。

請從符合您目的的章節開始閱讀。

:::tip 建議的穩定版本
若是全新導入，請從建議的穩定版里程碑 **v20260608.2325** 開始。詳情請參閱[穩定版里程碑](./releases/stable.md)。
:::

## 依目的查找

| 想做的事 | 起點 |
| --- | --- |
| 導入或更新 routerd | [導入 → 安裝與升級](./install-and-upgrade.md) |
| 了解 routerd 是什麼、為何存在 | [入門 → routerd 是什麼](./concepts/what-is-routerd.md) |
| 了解與其他產品和方式的定位差異 | [入門 → 定位](./concepts/positioning.md) |
| 第一次建立路由器 | [導入 → 快速入門](./tutorials/getting-started.md) |
| 在瀏覽器中產生初始設定 | [routerd config wizard](https://routerd.net/wizard) |
| 啟用 editor 補全與驗證 | [How-to → VS Code YAML schema](./how-to/vscode-yaml-schema.md) |
| 將無磁碟 mini PC 作為路由器 | [導入 → 無磁碟 mini PC](./tutorials/diskless-minipc-walkthrough.md) |
| 理解宣告式模型（資源、套用、調和） | [功能說明 → 資源模型](./concepts/resource-model.md) |
| 從已驗證的設定範例組建設定 | [設定範例集](./config-examples/index.md) |
| 解決特定部署問題 | [How-to 指南](./how-to/multi-wan.md) |
| 查詢資源種類或欄位 | [參考文件 → 資源 API](./api-v1alpha1.md) |
| 運維運行中的路由器 | [功能說明 → 調和（reconcile）](/docs/operations/reconcile) |
| 追蹤變更內容 | [發行版與穩定版 → 變更記錄](./releases/changelog.md) |
| 了解複雜案例的背景 | [知識庫](./knowledge-base/dhcpv6-pd-clients.md) |

## 章節一覽

- **入門** — routerd 是什麼、定位、設計理念
- **導入（快速入門）** — 安裝與升級、第一台路由器、各 OS 入門（NixOS / FreeBSD）、無磁碟 mini PC
- **功能說明（宣告式模型）** — 詞彙表、資源模型、套用與產生、狀態與擁有權、調和（reconcile）、Web 管理介面
- **設定參考文件（依功能）** — DNS 解析器、防火牆、Egress・多 WAN、BGP、Tailscale、OpenTelemetry 等各功能的設定方式
- **設定範例集（依情境）** — NAT、LAN 的 DHCP/DNS、DS-Lite、PPPoE、連接埠轉發、訪客隔離、多 WAN 故障切換等已驗證的設定範例
- **How-to 指南** — Flets 初始設定、IPv6 雙協定疊加、訪客模式、OS 啟動設定（bootstrap）、VS Code YAML schema、PVE overlay、疑難排解
- **知識庫（實際環境知識）** — 從實際環境獲得的現場筆記（DHCPv6-PD 客戶端、NTT NGN 的前綴委派取得）
- **運維** — 狀態資料庫、設備清單、USB 持久化、Alpine 部署、密鑰、可觀測性、備援等
- **參考文件（API・通訊協定・支援環境）** — 資源 API、控制 API、外掛程式通訊協定、支援平台、硬體
- **發行版與穩定版** — 穩定版里程碑、變更記錄、發行程序
- **設計筆記** — 架構上的討論點與設計依據
- **專案** — 貢獻方式、授權與法律事務
