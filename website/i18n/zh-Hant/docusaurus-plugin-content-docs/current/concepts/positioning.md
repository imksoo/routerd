---
title: 定位
slug: /concepts/positioning
---

# 定位

![routerd 作為 local declarative router control plane 的適用範圍及其邊界](/img/diagrams/concept-positioning.png)

routerd 是一個本地控制平面，用來建構可以從設定理解、也可以從執行狀態說明的路由器。

routerd 不是完整網路作業系統的替代品，也不是從外部統一管理大量路由器的雲端控制器。routerd 在每台路由器主機本地執行，將型別化的 YAML 資源轉換為主機的網路、服務、路由、隧道、防火牆、日誌與狀態。

## 重視的事

routerd 重視以下幾點運維方式。

- 可以納入 git 管理的宣告式路由器設定
- 不依賴託管控制器、可本地自主運作
- 對產生的主機成果物有明確的擁有關係
- 以事件而非隱藏在常駐程式內部的狀態來說明現況
- 在危險變更前確認管理路徑
- 能追溯路由、隧道與防火牆判斷依據的可觀測性

主要對象包含：家庭實驗室、小型辦公室、使用 Proxmox VE 或 KVM 的開發者，以及想把手寫 Linux 路由腳本替換為可重現機制的人。

## 覆蓋範圍

| 領域 | 範例 |
| --- | --- |
| WAN 接入 | DHCPv4、DHCPv6-PD、DHCPv6 資訊請求、PPPoE |
| IPv4 過渡 | DS-Lite、NAT44、多段 WAN 故障切換 |
| LAN 服務 | DHCPv4、DHCPv6、RA、DNS、NTP |
| 路由 | 靜態路由、策略路由、EgressRoutePolicy、健康檢查 |
| 安全 | 三角色防火牆、訪客模式、拒絕日誌 |
| Overlay | WireGuard、Tailscale 整合、VXLAN 基礎、VRF |
| 運維 | Web 管理介面、`routerctl`、OpenTelemetry、日誌儲存 |
| 初始建置 | 套件管理、sysctl profile、systemd unit、live ISO |

routerd 所覆蓋的範圍，兩端相距頗遠。

- **虛擬 SDN / VNET 間的路由：** 連接 Proxmox VE SDN、WireGuard overlay、VRF、VXLAN 實驗及實驗室策略路由的路由器 VM。
- **無磁碟 PC 路由器：** 小型 x86 mini PC 從 live ISO 啟動，從 USB 還原 `router.yaml`，將日誌保存在 RAM 中，並提供實體 LAN。

很少有路由器專案把這兩種場景視為同一個設定問題。routerd 選擇這樣做。差異主要在於產生的主機成果物，而非意圖模型本身。

覆蓋範圍廣是有意義的。路由器故障往往發生在功能邊界處。DNS 的選擇可能依賴 DHCPv6 資訊選項；DS-Lite 隧道可能依賴只能透過特定上游解析的 AFTR 記錄；路由應在健康檢查確認後才晉升為主要路由。routerd 將這些關係統一置於同一份資源圖中。

## 與 shell 腳本的差異

shell 腳本容易上手，但事後很難稽核。它們只能回答「執行了什麼指令」，卻無法保留「現在應該存在什麼狀態」。

routerd 將期望狀態保存在 YAML 中，記錄觀測狀態，發出事件，並透過 API、CLI 與 Web 管理介面呈現結果。這使得差異比較、世代回溯及實際流量的除錯都更加容易。

## 與設備韌體的差異

設備韌體在用途符合 UI 設計時相當方便。然而，當需要精確組合 DS-Lite、PPPoE 故障切換、本地 DNS、自訂防火牆、OpenTelemetry 或實驗室 overlay 網路時，操作往往變得困難。

routerd 將這些功能視為資源來處理。Web 管理介面用於查閱與排查，設定變更則以 CLI 和 YAML 為準。

## 與 Kubernetes 式控制器的差異

routerd 借鑑了資源與控制器的概念，但不需要叢集。邊界是主機本身，調和（reconcile）的對象是核心、本地常駐程式與本地檔案。

這種形式讓 routerd 足夠精簡，可作為家庭路由器執行，同時仍能讓 DHCP、DNS、隧道、健康檢查、路由、防火牆日誌與遙測以事件驅動方式協同運作。

## 非目標

routerd 目前不以下列為目標。

- 託管型 SDN 控制器
- 遠端外掛程式市集
- 通用防火牆語言
- 取代所有企業路由器功能
- GUI 優先的設定系統

routerd 重視明確的 YAML、本地控制與高品質的運維資訊，而非功能繁多的點擊式管理介面。
