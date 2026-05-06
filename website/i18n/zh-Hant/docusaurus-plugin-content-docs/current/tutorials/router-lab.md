---
title: 建立評估用實驗環境
---

# 建立評估用實驗環境

在將 routerd 放入正式網路前，建議先在隔離的實驗環境中進行評估。
本頁示範如何在虛擬化平台上開設多台路由器 VM，跨多種作業系統驗證 routerd 的行為。

不限定特定硬體，Proxmox VE、KVM、VMware、Hyper-V 任一都可。

## 想定情境

正式部署前希望確認下列事項：

- DHCPv6-PD、DS-Lite、PPPoE、NAT44、firewall、DNS 各功能是否如預期動作
- Linux、FreeBSD、NixOS 何者較適合
- 將某項變更投入正式網路前，是否能在沙盒重現相同效果

實驗環境的目的有三：與正式環境隔離、能自由更換 OS 與架構、可直接套用未來要部署的 routerd YAML。

## 硬體需求

全部以 VM 執行，不需要實體路由器。

- 一台虛擬化主機（Proxmox VE、KVM、VMware、Hyper-V 任一）
- 4 GB 以上的閒置記憶體（每台 VM 約 512 MB）
- 上游網路（若要驗證 DHCPv6-PD，需要可派發 IPv6 prefix 的 HGW 或模擬器）

## 推薦的 VM 拓撲

最少需要 **2 台 VM**，建議 **4-5 台**。

| 角色 | OS 範例 | 可驗證內容 |
| --- | --- | --- |
| 量測主機 | Ubuntu / Debian | `iperf3`、`dig`、`curl`、`mtr` 等工具 |
| WAN 側路由器 A | Ubuntu | `routerd-dhcpv6-client`、PPPoE、DS-Lite |
| WAN 側路由器 B | NixOS | NixOS 模組路徑封裝 routerd |
| WAN 側路由器 C | FreeBSD | FreeBSD `pf`、DS-Lite、rc.d unit |
| LAN 側路由器 | Ubuntu | controller chain、DNS、firewall、NAT、`HealthCheck`、Web Console |

將 VM 接入共用 virtual switch（VLAN trunk 或 untagged bridge），並讓該 switch 與上游隔離。
每台 VM 一張 NIC 接至「上游 vSwitch」，另一張 NIC 接至「實驗 vSwitch」即可。

## 驗收清單

依下列項目確認實驗環境正常：

1. **DHCPv6-PD 取得**：每台路由器 VM 的 `routerctl status` 顯示 `DHCPv6PrefixDelegation` 為 `Bound`。
2. **PD 不重疊**：超過 5 台時，所派發的 prefix 不重複（HGW 需依 IA_PD 分割）。
3. **IPv6 連通性**：實驗 VM 之間以 link-local 與 GUA 雙向 ping 都通。
4. **DS-Lite**：`routerctl describe DSLiteTunnel/<name>` 為 `Up`，對應 `HealthCheck` 為 `Healthy`，`curl --interface ds-lite-X http://example.com/` 有回應。
5. **NixOS 路徑**：`nixos-rebuild test` 套用後重啟仍維持相同狀態。
6. **FreeBSD 路徑**：`pfctl -sr` 顯示 routerd 產生的 pf 規則，`service routerd onestatus` 為 active。

## 不需處理的事項

下列現象由 HGW、ISP 等外部裝置決定，routerd 本身無法控制。請勿在實驗環境中糾結重現：

- HGW 在 DHCPv6 information-request 不回 AFTR option：請以 `DSLiteTunnel.spec.aftrFQDN` 設定靜態備援。
- 部分 ISP 僅對特定 IA_ID 回應 PD：在 client 端固定 IA_ID。
- 實體交換器在某 VLAN 上丟棄 IPv6 RA：以 vSwitch 內路由繞過該交換器。

以上案例已整理於 `docs/knowledge-base/`。

## 接下來

實驗環境建立後，可進入下列章節：

- [啟動第一台路由器](./first-router.md)
- [配置 LAN 側服務](./lan-side-services.md)
- [建立基本 firewall](./basic-firewall.md)
- [建立多 WAN](../how-to/multi-wan.md)
