---
title: 資源擁有權
slug: /reference/resource-ownership
---

# 資源擁有權與反映模型

routerd 將主機上的構成物與資源對應管理。
透過記錄哪個資源建立了哪些構成物，使差異確認、刪除與障礙排查更加便利。

![owner-reference lifecycle GC 圖：desired resource、ledger row、object status、host inventory、teardown contract、skip protection、backup 與 audit event](/img/diagrams/lifecycle-gc.png)

## 擁有權種類

| 種類 | 意義 |
| --- | --- |
| 建立 | routerd 新建構成物。 |
| 接管 | 將現有構成物納入 routerd 的管理範圍。 |
| 觀測 | routerd 僅觀測狀態，不做變更。 |

穩定的 owner identity 是 `apiVersion/kind/name`。apply generation 不屬於 owner key：
同一個資源即使跨越不同世代，也以同一擁有者身分替換或刪除其產生的 artifact。
object status 也記錄 owner metadata 與 lifecycle class，使 stale status cleanup 能與
apply path 作出相同判斷。

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
| `TunnelInterface` | Linux `ipip` / `gre` tunnel device；FOU/GUE mode 也會確保對應的 `ip fou` listener port |
| `SAMTransportProfile` | 包含產生的 `TunnelInterface`、endpoint `/32` `IPv4Route` 與 `BGPPeer` 的 `DynamicConfigPart` |
| `MobilityPool` | 動態 SAM capture/control-plane resource、BGP `/32` advertisement、provider action plan 與 ownership observation |
| `RemoteAddressClaim` | 低層 SAM capture state、proxy-ARP sysctl/neighbor state、provider-secondary capture status 與 resource-specific teardown |
| `IPAddressSet` | Linux 產生器參照的 nftables IPv4/IPv6 named set |
| `IPv4Route` | 核心路由 |
| `ClusterNetworkRoute` | 將 Pod / Service CIDR 透過指定 next hop 路由的已產生 `IPv4StaticRoute` 意圖 |
| `NAT44Rule` | nftables 的 `routerd_nat` 資料表 |
| `PortForward` / `IngressService` | Linux 上為 nftables `routerd_nat` / `routerd_filter` 的 DNAT 及選用的 hairpin SNAT；FreeBSD 上為 `pf.conf` 的 `rdr pass` 及選用的 NAT reflection 規則 |
| `BGPRouter` / `BGPPeer` | 透過本機 GoBGP gRPC 控制的長壽命 `routerd-bgp` 常駐程式狀態。學習到的 IPv4 最佳路徑由 routerd 以其擁有的 protocol/metric 寫入核心 FIB |
| `BFD` | Linux FRR `bfdd` session 設定，以及所參照 GoBGP peer 的 BFD 觀測狀態 |
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

## lifecycle contract

所有 config resource kind 都在 lifecycle registry 中宣告。宣告記錄 resource class，
並且必須具有下列 teardown contract 之一：

- `ArtifactKinds`: resource 將具體 host artifact 記錄到 ownership ledger，而 generic
  artifact teardown registry 知道如何刪除這些 artifact kind。
- `TeardownLifecycle: resource`: 從 object status 執行 resource 專用 teardown，例如
  kernel route 刪除、WireGuard adopted/external 保護、SAM proxy-ARP cleanup。
- `NoHostTeardownReason`: renderer input、external policy、dynamic source 等不擁有
  單獨 host artifact 的資源，需要明確說明原因。

CI 會檢查所有 config kind 都有明確 contract，並檢查 `ArtifactKinds` 中寫入的
artifact kind 存在於 teardown registry。這用於防止新資源靜默繞過 cleanup。

## 刪除時的思維

routerd 不會主動刪除未知的構成物。
即使 YAML 中的資源消失，也只能刪除 routerd 建立或明確接管的構成物。

GC planner 會比較 current effective resource set、ownership ledger、object status
與 host inventory，並產生可 dry-run 的 plan。plan 可包含 artifact removal、
resource-specific teardown、ledger forget、stale status deletion、state backup 與
audit event。

desired set 使用與 apply/serve 相同的 effective view：`FilterRouterByWhen`、dynamic SAM
resource 以及 `DynamicConfigPart` merge 之後的結果。因此 `when: false` 的資源不會被當成
active cleanup target，仍由 profile 產生的 SAM tunnel/BGP/route resource 也不會被誤判為 orphan。

刪除 `SAMTransportProfile` 時，profile 的 dynamic part 會變成空的 active part。
產生的 `TunnelInterface` / `BGPPeer` / endpoint route 隨即從 effective config 消失，
並由各產生資源的 owner 觸發 cleanup。

破壞性 cleanup 需要 state backup 並記錄 event。不支援的 OS integration 會被跳過而不執行破壞操作。
adopted 或 externally managed object status 不會由 resource lifecycle GC teardown。

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
