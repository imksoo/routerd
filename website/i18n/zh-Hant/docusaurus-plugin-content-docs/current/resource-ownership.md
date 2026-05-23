---
title: 資源擁有權
slug: /reference/resource-ownership
---

# 資源擁有權與反映模型

routerd 將主機上的構成物與資源對應管理。
透過記錄哪個資源建立了哪些構成物，使差異確認、刪除與障礙排查更加便利。

## 擁有權種類

| 種類 | 意義 |
| --- | --- |
| 建立 | routerd 新建構成物。 |
| 接管 | 將現有構成物納入 routerd 的管理範圍。 |
| 觀測 | routerd 僅觀測狀態，不做變更。 |

## 主要構成物

| 資源 | 主機端構成物 |
| --- | --- |
| `Interface` | OS 的介面名稱與管理狀態 |
| `DHCPv6PrefixDelegation` | `routerd-dhcpv6-client` 的 socket、租約、事件 |
| `DHCPv4Client` | `routerd-dhcpv4-client` 的 socket、租約、事件 |
| `PPPoESession` | `routerd-pppoe-client` 的 socket、狀態、pppd/ppp 設定 |
| `HealthCheck` | `routerd-healthcheck` 的 socket、狀態、事件 |
| `DHCPv4Server` / `DHCPv6Server` / `IPv6RouterAdvertisement` | 受管理的 dnsmasq 設定 |
| `DNSZone` | `routerd-dns-resolver` 的本機權威區域 |
| `DNSResolver` | `routerd-dns-resolver` 的 socket、狀態、事件、監聽設定 |
| `DNSForwarder` | `routerd-dns-resolver` 的轉發規則，以解析器設定的形式產生（render） |
| `DNSUpstream` | `routerd-dns-resolver` 的上游端點，以轉發規則的形式產生（render） |
| `DSLiteTunnel` | Linux 的 `ip6tnl` 介面 |
| `IPAddressSet` | Linux 產生器參照的 nftables IPv4/IPv6 named set |
| `IPv4Route` | 核心路由 |
| `ClusterNetworkRoute` | 將 Pod / Service CIDR 透過指定 next hop 路由的已產生 `IPv4StaticRoute` 意圖 |
| `NAT44Rule` | nftables 的 `routerd_nat` 資料表 |
| `PortForward` / `IngressService` | Linux 上為 nftables `routerd_nat` / `routerd_filter` 的 DNAT 及選用的 hairpin SNAT；FreeBSD 上為 `pf.conf` 的 `rdr pass` 及選用的 NAT reflection 規則 |
| `BGPRouter` / `BGPPeer` | 透過本機 GoBGP gRPC 控制的長壽命 `routerd-bgp` 常駐程式狀態。學習到的 IPv4 最佳路徑由 routerd 以其擁有的 protocol/metric 寫入核心 FIB |
| `BFD` | 僅保留 BFD 意圖。在不使用 FRR 的 BFD 實作加入之前，GoBGP 後端會回報 unsupported |
| `VirtualAddress` | 透過 `ip addr` / `ifconfig` 設定的靜態 VIP，或 Linux 的 keepalived / FreeBSD 的 CARP 所管理的 VRRP/VRRPv3 VIP 擁有權 |
| `ObservabilityPipeline` | 程序內 routerd 事件匯出器，以及受管理單元的 OpenTelemetry 環境變數 |
| `RouterdCluster` | `spec.leasePath` 的檔案租約。只有 leader 才執行 apply 與控制器變更 |
| `WireGuardInterface` / `WireGuardPeer` | WireGuard 設定 |
| `TailscaleNode` | `routerd-tailscale-<name>` 的服務單元 / script 與 `tailscale up` 參數 |
| `VRF` | Linux 的 VRF 裝置與路由表 |
| `VXLANTunnel` | VXLAN 裝置 |
| `Package` | 套件覆蓋設定。一般主機套件的意圖會從 router 資源自動推導 |
| `Sysctl` | sysctl 值 |
| `SysctlProfile` | 多個 sysctl 值 |
| 衍生主機執行期 | 從 router 資源推導的核心模組載入狀態，以及 systemd-networkd / resolved 的 drop-in |
| `generated service artifacts` | systemd 單元、FreeBSD rc.d script 或 OpenRC init script，及其啟用狀態 |
| `NTPClient` | NTP 用戶端設定 |

## 刪除時的思維

routerd 不會主動刪除未知的構成物。
即使 YAML 中的資源消失，也只能刪除 routerd 確認自己擁有的構成物。

目前不以完整回滾功能為目標。
特別是對正式網路有影響的變更，請遵循下列順序：

1. 驗證。
2. 確認計畫。
3. 試運行（dry-run）。
4. 確認管理連線不會中斷。
5. 套用（apply）。
6. 確認狀態與連通性。

## 舊設定的處理

Phase 4 中已移除舊 DHCPv6 實驗套件與舊產生器。
目前的 DHCPv6-PD 由 `routerd-dhcpv6-client` 擁有。
過去關於 `dhcpcd` 或 `dhcp6c` 路由的說明，不適用於目前的設定範例。
