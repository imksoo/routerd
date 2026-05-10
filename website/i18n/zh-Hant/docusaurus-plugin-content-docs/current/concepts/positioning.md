---
title: 定位
slug: /concepts/positioning
---

# 定位

routerd 是一個本地控制平面，用來建構可以從設定理解、也可以從執行狀態解釋的路由器。

routerd 不是完整網路作業系統的替代品。它也不是從外部管理大量路由器的雲端控制器。routerd 在每台路由器主機本地執行，把帶型別的 YAML 資源轉換為主機網路、服務、路由、隧道、防火牆、日誌和狀態。

## routerd 重視什麼

routerd 重視以下運維方式。

- 可以放入 git 管理的宣告式路由器設定
- 不依賴託管控制器的本地執行
- 對產生的主機檔案和服務有明確所有權
- 透過事件說明狀態，而不是隱藏在守護程式內部
- 在危險變更前檢查管理路徑
- 能解釋路由、隧道和防火牆判斷存在的原因

典型使用者包括家庭實驗室、小型辦公室、使用 Proxmox VE 或 KVM 的開發者，以及想把手寫 Linux 路由腳本替換為可重複機制的人。

## 覆蓋範圍

| 領域 | 範例 |
| --- | --- |
| WAN 接入 | DHCPv4、DHCPv6-PD、DHCPv6 資訊請求、PPPoE |
| IPv4 過渡 | DS-Lite、NAT44、多階段 WAN fallback |
| LAN 服務 | DHCPv4、DHCPv6、RA、DNS、NTP |
| 路由 | 靜態路由、策略路由、EgressRoutePolicy、健康檢查 |
| 安全 | 三角色防火牆模型、訪客模式、拒絕日誌 |
| Overlay | WireGuard、Tailscale 整合、VXLAN 基礎、VRF |
| 運維 | Web Console、`routerctl`、OpenTelemetry、日誌儲存 |
| 初始建置 | 軟體套件、sysctl profile、systemd unit、live ISO |

這個範圍的兩端相距很遠。

- **虛擬 SDN/VNET 間路由：** 連接 Proxmox VE SDN、WireGuard overlay、VRF、VXLAN 實驗和實驗室策略路由的路由器 VM。
- **無碟 PC 路由器：** 小型 x86 mini PC 從 live ISO 啟動，從 USB 還原 `router.yaml`，把日誌保存在 RAM 中，並提供實體 LAN。

很少有路由器專案把這兩端當成同一個設定問題。routerd 會這樣做。差異主要在產生的主機成果物，而不是意圖模型。

範圍廣是有意義的。路由器故障常常發生在邊界處。DNS 選擇可能依賴 DHCPv6 information option。DS-Lite 隧道可能依賴只能透過特定 upstream 解析的 AFTR 記錄。路由應該在健康檢查確認以後才成為 primary。routerd 把這些關係放在同一個資源圖中。

## 與 shell 腳本的差異

shell 腳本容易開始，但後續很難稽核。它們常常能回答「執行了什麼命令」，卻不能回答「現在應該存在什麼狀態」。

routerd 把期望狀態保存在 YAML 中，保存觀測狀態，發出事件，並透過 API、CLI 和 Web Console 暴露結果。這樣更容易檢查漂移、比較世代，以及除錯真實流量。

## 與設備韌體的差異

當用途符合 UI 設計時，設備韌體很方便。可是如果需要精確組合 DS-Lite、PPPoE fallback、本地 DNS、自訂防火牆、OpenTelemetry 或實驗室 overlay 網路，操作會變得困難。

routerd 把這些功能作為資源處理。UI 用於讀取和排查。設定變更仍然以 CLI 和 YAML 為準。

## 與 Kubernetes 風格控制器的差異

routerd 借用了資源和控制器的思想，但不需要叢集。邊界是主機。被調整的是核心、本地守護程式和本地檔案。

這種形態足夠小，可以作為家庭路由器執行，同時仍然允許 DHCP、DNS、隧道、健康檢查、路由、防火牆日誌和 telemetry 透過事件協同。

## 非目標

routerd 目前不以以下目標為主。

- 託管型 SDN 控制器
- 遠端外掛市場
- 通用防火牆語言
- 替代所有企業路由器功能
- GUI 優先的設定系統

routerd 重視明確的 YAML、本地控制和高品質運維資訊，而不是廣泛的點擊式管理介面。
