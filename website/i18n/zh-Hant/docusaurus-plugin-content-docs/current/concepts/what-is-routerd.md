---
title: routerd 是什麼
slug: /concepts/what-is-routerd
sidebar_position: 1
---

# routerd 是什麼

routerd 是用來將 Linux 主機、NixOS、FreeBSD 作為路由器運行的宣告式控制平面。將路由器的設定以 YAML 資源的形式撰寫後，routerd 會將該意圖反映至介面、位址、DHCP、DNS、NAT、路由、通道、健康檢查、套件、sysctl、服務單元、記錄等實際狀態。

routerd 既不是發行版，也不是集中管理服務。它在各路由器主機上本地運行，並在必要範圍內使用 systemd-networkd、dnsmasq、nftables、pppd、WireGuard、systemd 等主機端元件。

## 解決的問題

手動建置路由器時，狀態會分散在許多地方。

- 介面位址分散在 netplan、systemd-networkd、rc.d、NixOS 設定中。
- DHCP、DHCPv6、DHCP 中繼、RA 分散在 dnsmasq 的設定中。
- DNS 轉送和本地記錄分散在各解析器的設定中。
- NAT、路由政策、conntrack、防火牆分散在 nftables 和 iproute2 中。
- DHCPv4、DHCPv6-PD、PPPoE、健康檢查、記錄各自成為獨立的常駐程式。
- 套件、sysctl、服務單元容易殘留在主機準備腳本中。

routerd 將這些全部統一作為資源來管理。透過 YAML 即可了解路由器的意圖，變更可透過 git diff 追蹤，實際觀測的狀態可透過 `routerctl` 和 Web 管理介面確認。

## 目前的架構

`routerd serve` 讀取資源、解析相依關係、啟動子常駐程式，並在訂閱事件的同時，持續調和（reconcile）主機至期望狀態。

長期運行的協定狀態分由小型的受管理常駐程式負責。

- `routerd-dhcpv6-client`：負責 DHCPv6 的前綴委派（PD）和資訊要求。
- `routerd-dhcpv4-client`：負責 DHCPv4 的 WAN 租約。
- `routerd-pppoe-client`：負責 PPPoE 工作階段。
- `routerd-healthcheck`：負責 TCP、DNS、HTTP、ICMP 的連通確認。
- `routerd-dns-resolver`：負責 DNS 區域回應及 DoH、DoT、TCP、UDP 上游。
- `routerd-dhcp-event-relay`：將 dnsmasq 的租約變化轉換為 routerd 事件。
- `routerd-firewall-logger`：將防火牆記錄匯入至 routerd 的記錄儲存位置。

各常駐程式透過 Unix socket 上的本地 HTTP+JSON 公開狀態，並將必要狀態儲存至檔案。routerd 讀取這些事件，並反映至 LAN 服務、DNS 記錄、DS-Lite、NAT、路由政策、健康檢查驅動的路由選擇，以及觀測用的儲存位置。

## 可管理的項目

目前的實作可處理以下項目。

- DHCPv6-PD，以及從委派前綴衍生的 IPv6 LAN 位址
- DHCPv6 資訊要求、AFTR 的 DNS 解析、DS-Lite
- DHCPv4 的 WAN 租約、DHCPv4 的 LAN 範圍、固定分配
- DHCPv6 伺服器模式、IPv6 RA 選項
- DNS 區域、DHCP 來源記錄、條件式轉送、DoH、DoT、TCP DNS、UDP 備援、多重監聽、快取
- NAT44、私有目的地的 NAT 排除指定、IPv4 路由政策、reverse path filter、Path MTU 政策、TCP MSS 調整
- PPPoE、WireGuard、VXLAN、VRF、cloud VPN 用的 IPsec 連線定義、strongSwan `swanctl` 設定產生
- 套件、sysctl 設定檔、網路接管、systemd 單元、NTP 用戶端、記錄轉送、記錄保存、Web 管理介面
- `EgressRoutePolicy`、`HealthCheck`、`EventRule`、`DerivedEvent` 的狀態聯動
- 狀態、事件、DNS 查詢、連線、連線流量、防火牆記錄的確認

## 有意限縮的範圍

routerd 目前為 v1alpha1 的預發布版本。為了使路由器更安全、設定更易讀，在不保留相容別名的情況下可能會變更名稱或欄位。

有狀態防火牆過濾器也屬於有意限縮的範圍。routerd 產生的是 NAT44、區域政策、受管理服務的允許規則、拒絕記錄，以及連線確認，而非通用的防火牆規則語言。

NixOS 和 FreeBSD 使用相同的資源模型，只有反映目標會依各 OS 採用對應的機制。各平台的差異記載於對應表中。

## 延伸閱讀

- [設計理念](./design-philosophy)
- [資源模型](./resource-model)
- [套用與產生](./apply-and-render)
- [狀態與擁有權](./state-and-ownership)
- [安裝](../tutorials/install)
