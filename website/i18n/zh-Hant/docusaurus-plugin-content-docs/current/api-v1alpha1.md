---
title: 資源 API v1alpha1
slug: /reference/api-v1alpha1
---

# 資源 API v1alpha1

routerd 的設定由最頂層的 `Router` 以及型別化資源清單組成。
本頁依照目前實作列出各資源。
Phase 1.6 起，DHCP 相關的 Kind 依 RFC 表記改為 `DHCPv4*` 與 `DHCPv6*`。
舊名稱無相容別名。

## 共通格式

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: Interface
metadata:
  name: wan
spec:
  ifname: ens18
  adminUp: true
```

| 欄位 | 說明 |
| --- | --- |
| `apiVersion` | API 群組與版本。 |
| `kind` | 資源種類。 |
| `metadata.name` | 同種類內的名稱。 |
| `spec` | 使用者宣告的意圖。 |
| `status` | routerd 或專用常駐程式觀測到的狀態。 |

## API 群組

| API 群組 | 主要 Kind |
| --- | --- |
| `routerd.net/v1alpha1` | `Router` |
| `net.routerd.net/v1alpha1` | 介面、可重複使用的 `IPAddressSet`、DHCP、DNS、路由、隧道、VIP、BGP、事件、通訊流量紀錄 |
| `firewall.routerd.net/v1alpha1` | `FirewallZone`, `FirewallPolicy`, `FirewallRule`, `FirewallEventLog`, `ClientPolicy`, `PortForward`, `IngressService`, `LocalServiceRedirect` |
| `system.routerd.net/v1alpha1` | `Hostname`, `Sysctl`, `SysctlProfile`, `Package`, `NTPClient`, `NTPServer`, `LogSink`, `ObservabilityPipeline`, `RouterdCluster`, `LogRetention`, `WebConsole` |
| `plugin.routerd.net/v1alpha1` | 外掛程式清單 |

## 系統準備

| Kind | 用途 |
| --- | --- |
| `Package` | 補充無法從其他資源導出的 OS 套件，是限定範圍的 override。一般的執行時期相依套件會自動導出。 |
| `Sysctl` | 補充目前尚無法從路由器資源導出的 sysctl 值，是限定範圍的 escape hatch。可透過 `compare: exact` 與 `compare: atLeast` 選擇回讀的判斷方式。 |
| `SysctlProfile` | 補充路由器建議的 sysctl 值，是限定範圍的 escape hatch。一般的路由器 sysctl 會自動導出。 |
| `Hostname` | 設定主機名稱。 |
| `NTPClient` | 啟用 OS 的 NTP 用戶端。從 DHCPv4 / DHCPv6 的狀態導出時間伺服器，若為空則回退至公共 NTP 伺服器。 |
| `NTPServer` | 執行面向 LAN 的本地 NTP 伺服器。除靜態的 `allowCIDRs` 外，也可透過 `allowCIDRFrom` 從 `IPv6DelegatedAddress/<name>.address` 或 `DHCPv6PrefixDelegation/<name>.currentPrefix` 等 status 欄位導出允許範圍。 |
| `LogSink` | 將日誌事件轉發至 syslog、OTLP、webhook、file、journald。 |
| `ObservabilityPipeline` | 設定 OTLP 的環境，以及將 routerd 事件轉發至 stdout / syslog / Loki。 |
| `RouterdCluster` | 透過 file 租約讓 leader 負責變更主機設定，standby 則僅進行狀態觀測。 |
| `LogRetention` | 管理事件、DNS、通訊流量、防火牆事件紀錄的保存期限。 |
| `WebConsole` | 在管理網路上提供唯讀的 Web 管理介面。 |

## 介面

| Kind | 用途 |
| --- | --- |
| `Interface` | 將 routerd 使用的穩定名稱與 OS 介面名稱對應，並提供下游資源所需的 link/address status。 |
| `PPPoESession` | 表示 PPPoE 的底層介面設定。 |
| `PPPoESession` | 由 `routerd-pppoe-client` 管理的 PPPoE 工作階段。 |
| `WireGuardInterface` | 表示 WireGuard 介面。 |
| `WireGuardPeer` | 表示 WireGuard 對端。 |
| `TailscaleNode` | 設定 Tailscale 節點。透過受管理的 systemd 單元管理 Exit node 與 subnet router 的廣播。 |
| `IPsecConnection` | 表示 strongSwan 的 cloud VPN 連線定義。 |
| `VRF` | 表示 Linux VRF 裝置與路由表。 |
| `VXLANTunnel` | 表示 VXLAN 隧道。 |

將 `PPPoESession.spec.enabled` 設為 `false`，可在保留 PPPoE 定義的同時，停止並停用受管理的 pppd 單元。
這樣在正常運作時不使用 PPPoE 工作階段，僅在需要時手動用作備援路徑。

`TailscaleNode` 可在初次註冊時使用 `authKey`。
正式環境建議使用 `authKeyEnv` 或 `authKeyFile`，
以避免將機密值寫入 YAML 與 Git 歷史記錄。
兩者皆未指定時，`tailscaled` 視為已登入狀態。
routerd 僅重新套用所廣播的節點設定。
Tailscale 預設的 UDP/41641 視為保留用途。
WireGuard 的監聽埠請使用其他號碼。
詳細設定步驟請參閱 Tailscale 設定指南。

`WireGuardInterface` 可接受 `privateKeyFile`，以便將私鑰存放在路由器 YAML 之外。
`WireGuardPeer` 也可接受選用的 `presharedKeyFile` 作為 PSK。
內嵌的金鑰欄位主要用於範例與測試。
在 FreeBSD 上，routerd 會產生 rc.d 服務，
該服務負責建立 `wg` 介面、從檔案讀取私鑰，
並套用所宣告的 peer 與靜態位址。

核心模組，以及 systemd-networkd/resolved 的 adoption drop-in 均從路由器資源自動導出。若已刪除的 `KernelModule`、`NetworkAdoption`、`Link`、`NixOSHost` 仍殘留在設定中，routerd 不會靜默忽略，而是回傳錯誤。

## WAN 位址與委派

| Kind | 用途 |
| --- | --- |
| `IPv4StaticAddress` | 指派靜態 IPv4 位址。 |
| `VirtualAddress` | 宣告 IPv4 `/32` 或 IPv6 `/128` VIP。`spec.family` 為 `ipv4` 或 `ipv6`。`mode: vrrp` 在 Linux 使用 keepalived，在 FreeBSD 使用 CARP。 |
| `DHCPv4Client` | 由 `routerd-dhcpv4-client` 管理 DHCPv4 租約、IPv4 位址及可選的預設路由。 |
| `DHCPv6Address` | 表示 DHCPv6 IA_NA 的意圖。 |
| `DHCPv6PrefixDelegation` | 由 `routerd-dhcpv6-client` 管理的 DHCPv6-PD 租約。 |
| `DHCPv6Information` | DHCPv6 資訊要求的結果。觀測 DNS、SNTP、網域搜尋、AFTR 資訊。 |
| `IPv6DelegatedAddress` | 從委派前綴導出 LAN 側位址。 |
| `IPv6RAAddress` | 表示透過 RA/SLAAC 取得的 IPv6 位址。 |

`DHCPv6PrefixDelegation` 不具備舊式的 OS 用戶端選擇欄位。
DHCPv6-PD 由 `routerd-dhcpv6-client` 負責。

## LAN 側服務

| Kind | 用途 |
| --- | --- |
| `DHCPv4Server` | 提供 dnsmasq 的 DHCPv4 服務與可選的位址池。 |
| `DHCPv4Reservation` | 表示依 MAC 位址的固定指派。 |
| `DHCPv4Relay` | 表示 dnsmasq 的 DHCPv4 中繼。 |
| `IPv6RouterAdvertisement` | 產生 RA、PIO、RDNSS、DNSSL、M/O 旗標、MTU、優先度、存活時間。 |
| `RogueRADetector` | 自動導出的資源，以 status 顯示在送出 RA 的介面上觀測到的、非自身發出的 IPv6 Router Advertisement。 |
| `DHCPv6Server` | dnsmasq 的 DHCPv6/RA 服務。支援 `stateless`、`stateful`、`both`、`ra-only`。 |
| `DNSZone` | 表示本地權威區域。處理手動輸入的記錄與 DHCP 租約衍生的記錄。 |
| `DNSResolver` | 表示 `routerd-dns-resolver` 的常駐程式實例、監聽、快取、metrics、查詢紀錄。 |
| `DNSForwarder` | 隸屬於某個解析器的 DNS match 規則。可從 `DNSZone` 回應，或轉發至指定的 `DNSUpstream`。 |
| `DNSUpstream` | 表示一個上游端點，支援 `udp`、`tcp`、`dot`、`doh`。亦可指定狀態衍生位址、bootstrap 解析器、TLS 名稱及來源介面。 |

由於 Android 僅靠 DHCPv6 的 DNS 無法完成名稱解析，在 IPv6 LAN 環境中需設定 `IPv6RouterAdvertisement.spec.rdnss`。

dnsmasq 僅負責 DHCPv4、DHCPv6、中繼、RA。
DNS 的監聽與回應由 `DNSResolver` 負責。
LAN 的 DNS suffix 可透過 `DHCPv4Server.spec.domainFrom`、
`IPv6RouterAdvertisement.spec.dnsslFrom`、`DHCPv6Server.spec.domainSearchFrom`
參照 `DNSZone/<name>.zone`，與本地區域保持一致。
`DNSResolver.spec.listen[].sources` 中列出該 listener 使用的 `DNSForwarder` 名稱。
省略 listener 時，會使用參照該解析器的所有 `DNSForwarder`。
使用者 YAML 的 `DNSResolver.spec.sources` 不予接受。請將舊式的內嵌 source
拆分為 `DNSForwarder` 與 `DNSUpstream`。

`DNSForwarder.spec.match` 可指定 `home.example` 或表示預設上游的 `.`。
`spec.zoneRefs` 從本地 `DNSZone` 回應，`spec.upstreams` 則轉發至 `DNSUpstream`。
DNSSEC 驗證寫在 `DNSForwarder.spec.dnssecValidate`。

`DNSUpstream.spec.protocol` 為 `udp`、`tcp`、`dot`、`doh` 之一。
`addressFrom` 可從 `DHCPv6Information/<name>.dnsServers` 等來源導出 UDP 上游位址。
`sourceInterface` 在 Linux 上綁定送出介面，`bootstrap` 用於解析 DoH/DoT 端點名稱的輔助解析器。

## DS-Lite、路由、NAT

| Kind | 用途 |
| --- | --- |
| `DSLiteTunnel` | 向 AFTR 建立 `ip6tnl` 隧道。AFTR 可直接指定 IPv6、FQDN 或從 DHCPv6 資訊取得。 |
| `IPAddressSet` | 從直接指定的位址或 FQDN 定義可重複使用的 IP 位址集。Linux nftables 的產生器將其輸出為 named set，可從 redirect、NAT、policy routing 參照。 |
| `IPv4Route` | 新增 IPv4 路由。也可用於 DS-Lite 的預設路由或明確的捨棄路由。 |
| `ClusterNetworkRoute` | 將 Kubernetes 的 Pod / Service CIDR 展開為經由 worker next hop 的靜態 IPv4 路由。 |
| `BGPRouter` | 宣告本地 BGP 路由器。目前的後端是長壽命的 `routerd-bgp` GoBGP 常駐程式，匯入策略預設為 deny。 |
| `BGPPeer` | 宣告隸屬於 `BGPRouter` 的 GoBGP 管理 BGP peer。適用於 Kubernetes BGP speaker 等情境。 |
| `BFD` | 宣告 BFD session 的意圖。GoBGP 後端在尚未實作不依賴 FRR 的 BFD 之前，回報為 unsupported。 |
| `NAT44Rule` | 在 nftables 的 `routerd_nat` 表中執行 IPv4 NAPT。 |
| `PortForward` | 將 WAN 側的 IPv4 TCP/UDP 連接埠 DNAT 至單一內部 IPv4 目的地。 |
| `IngressService` | 公開 WAN 側的 IPv4 TCP/UDP 服務。支援多個 backend、TCP/HTTP 健康檢查，以及 `failover` / `sourceHash` / `random` 選擇策略。 |
| `LocalServiceRedirect` | 將 LAN 側用戶端向 `IPAddressSet` 發出的 IPv4/IPv6 通訊 redirect 至路由器本地連接埠。適用於集中純文字 DNS/NTP，不影響 DoH 或 DoT 連接埠。 |
| `EgressRoutePolicy` | 表示預設路由選擇、基於 mark 的 IPv4 policy routing，以及向多個 target 的 hash 分散。 |

`EgressRoutePolicy` 除 CIDR 指定外，還具有 `destinationSetRefs` 與
`excludeDestinationSetRefs`。這讓以 FQDN 為後端的目的地集合無需在 policy
資源中展開位址，即可用於路由控制與排除條件。
`mode: priority` 用於預設路由 failover，`mode: mark` 用於單一帶 mark 的路由
表，`mode: hash` 或 `candidates[].targets` 用於向多個路由表進行
source/destination 的 hash 分散。

routerd 從路由器角色、隧道、防火牆區域、RA/DHCPv6 資源自動導出 reverse path filter sysctl、隧道 MTU、RA MTU、TCP MSS clamp。
設定中只需宣告 LAN/WAN 與隧道的意圖，無需撰寫 `IPv4ReversePathFilter` 或
`PathMTUPolicy`。
若外部管理的來源介面（如 `tailscale0`）具有較低的 MTU，可設定 `Interface.spec.mtu`。routerd 僅將其用於該來源路徑，不會將較低的 MTU 套用至無關的 LAN 路徑。

`EgressRoutePolicy` 具有 `excludeDestinationCIDRs`，可將 LAN 內部、管理網路、HGW LAN、RFC 1918 私有網路等排除在 policy routing 對象之外。

`ClusterNetworkRoute` 是面向 Kubernetes 節點的輔助資源。
在 `spec.pods.cidrs` 與 `spec.services.cidrs` 中列出 Pod / Service CIDR，
並在 `spec.via[]` 中指定 worker 或 VIP 的 next hop，routerd 即會產生
對應的 `IPv4StaticRoute` 意圖。相同 weight 視為相同 metric，可用於多 next hop 的 ECMP；不同 weight 會轉換為 metric 差值，表示優先路由與備援路由。

`FirewallRule` 除宛先 CIDR 外，還具有 `destinationSetRefs` 與
`excludeDestinationSetRefs`，讓可重複使用的 FQDN 後端集合無需在各規則中展開位址，即可作為允許、拒絕、reject 的條件。
Stateful rule expression 亦支援 `sourcePorts`、`destinationPorts`、ICMP / ICMPv6 的
type matching、`rateLimit`、`connLimit`。`port` 作為單一目的地連接埠的簡寫仍可使用，但新範例建議改用 `destinationPorts`。

`NAT44Rule` 支援透過 `outboundInterface`、`sourceCIDRs`、`translation` 進行簡單的
source NAT，以及透過 `type`、`egressInterface` 或 `egressPolicyRef`、`sourceRanges`
進行具有 policy 感知的 NAT。此外還具有 `destinationCIDRs`、`destinationSetRefs`、
`excludeDestinationCIDRs`、`excludeDestinationSetRefs`，可將僅網際網路流量進行 masquerade，而有靜態路由的私有目的地或可重複使用的位址集合則不進行 NAT。

`PortForward` 與 `IngressService` 在 Linux nftables 與 FreeBSD pf 上產生 DNAT。
指定 `spec.hairpin.enabled: true` 與 `spec.hairpin.interfaces` 後，也會產生讓 LAN
用戶端透過 WAN 位址連到同一服務的 hairpin NAT。
hairpin 需要 `listen.address` 或 `listen.addressFrom`，routerd 會產生 LAN 側的
DNAT 與返回路徑的 masquerade/NAT reflection。
`listen.addressFrom` 與 backend 的 `addressFrom` 可參照 `IPv4StaticAddress/<name>.address`
或 `VirtualAddress/<name>.address` 等可靜態描繪的位址資源。
`IngressService` 中，未指定 `spec.hairpin.mode` 視為 `auto`。
當 listen 位址與所選 backend 位於 listen 介面宣告的同一前綴上時，routerd 會自動產生
LAN 用戶端使用 VIP 所需的同一介面返回 SNAT。即使 YAML 未宣告 listen 介面的前綴，
只要私有 IPv4 的 listen/backend 位址位於同一 `/24`，也會判斷需要 hairpin。
這是為了涵蓋如 Live ISO 等從啟動環境繼承介面位址的情境。
若要停用，請使用 `spec.hairpin.mode: off`；明確指定時使用 `manual` 與 `interfaces`。
`VirtualAddress.spec.vrrp.authentication` 在 keepalived 中產生為 `auth_pass`，
在 FreeBSD CARP 中產生為 `pass`。正式環境不建議將共用機密留在 routerd YAML 中，
請優先使用 `VirtualAddress.spec.vrrp.authenticationFrom`。
`authenticationFrom.file` 讀取本地機密檔案，
`authenticationFrom.env` 讀取環境變數，`base64: true` 可解碼 base64 值。
已產生的 keepalived/CARP 設定與主機介面狀態請視為機密。
VRRP authentication 在 VRRPv3（RFC 5798）中已 deprecated。routerd 以 L2 隔離為前提，
authentication 僅在周邊網路有要求或作為簡單的設定錯誤防護時使用。
`IngressService` 支援多個 backend、TCP health check、failover policy。
runtime 控制器解析 backend 的 FQDN，DNS 暫時失敗時以上次解析的 IPv4 作為 fallback。當健康的 backend 有多個時，Linux nftables 以 `sourceHash` 使用 `jhash ip saddr`、`random` 使用 `numgen random` 進行分配；健康的 backend 只剩一個時則降格為 failover。
validator 會拒絕 `IngressService`、`LocalServiceRedirect`、routerd 管理的常駐程式在同一介面/協定上發生衝突的監聽埠設定。

`IPAddressSet` 在套用時將直接指定的 IPv4/IPv6 位址輸出至 nftables 的 named set。
FQDN 的 `A`/`AAAA` 記錄由 runtime 控制器解析，並在不重新載入整個防火牆、NAT、policy 表的情況下即時更新所參照的 set。下次更新以觀測到的最小 DNS TTL 的一半為基準，最短不低於 60 秒。`refreshInterval` 可用作希望更積極更新時的上限。

`IPAddressSet.spec.names` 僅處理完全比對的 DNS 名稱。`microsoft.com` 僅表示
`microsoft.com` 本身的 `A`/`AAAA` 記錄，不包含 `www.microsoft.com`、
`login.microsoft.com`、`*.microsoft.com` 及更深層的子網域。
萬用字元或以 suffix 形式判斷服務的情境，需要能處理 DNS 查詢觀測或 provider 端點 feed 的其他資源，而非簡單的 FQDN 解析。

`BGPRouter` 與 `BGPPeer` 使用長壽命的 `routerd-bgp` 常駐程式。
routerd 透過本地 gRPC Unix socket 將資源 spec 直接映射為型別化的 GoBGP API 物件，
並以 `ListPeer` 與 `ListPath` 觀測狀態。不使用 FRR 的文字設定、
`frr-reload.py`、`vtysh` 解析、GoBGP 的檔案設定。
`apply --once` 僅產生主機 artifact，
BGP 作為 `routerd serve` 的管理對象顯示於 status。`routerctl show bgp` 顯示從儲存的
GoBGP 觀測資料中，路由器、peer、訊息計數器、路由選擇狀態及最近的錯誤。
前綴 status 包含 `best`、`valid`、`installed`、`stale`、`nextHop`、
observed community。符合 `spec.importPolicy.allowedPrefixes` 的已學習 IPv4 best path，
以 routerd 所有的 protocol/metric 寫入核心 FIB。
預設情況下，GoBGP import policy 接受的 eBGP next-hop 會改寫為學習來源的 peer 位址
（`spec.importPolicy.nextHopRewrite: peer-address`）。這與舊版 FRR 的
`set ip next-hop peer-address` 含義相同，即使廣播的 next-hop 指向 downstream speaker，
也可以 peer 位址的 ECMP 形式寫入。僅在希望將廣播的 next-hop 原樣寫入核心時，才指定 `nextHopRewrite: unchanged`。
相同前綴的 equal best path 作為 ECMP 的 next-hop 寫入。

`BGPRouter.spec.convergenceProfile: fast` 適用於 Kubernetes/edge 路由器，優先快速收斂而非 graceful restart 的 stale-path 保留。fast profile 縮短 peer timer，並在未明確設定 `spec.gracefulRestart.enabled` 時停用 graceful restart。匯入策略預設為 deny。請在 `spec.importPolicy.allowedPrefixes` 中列出希望接受的前綴，例如 Kubernetes LoadBalancer pool。
`BGPPeer.spec.ebgpMultihop` 適用於 loopback peering 或 lab 至正式路由器驗證等非直連的 eBGP session。未指定、`0`、`1` 為直連 eBGP 的預設行為；指定 `2` 至 `255` 時，作為該 peer group 的 GoBGP multihop TTL。
router ID 不必與 TCP 的來源位址相同，但 peer 側需設定主機實際使用的 BGP 來源位址。LAN 有多個位址時，在 Linux 可用 `ip route get <peer-address>` 確認來源位址，除非有明確理由，建議將 router ID 也與該運作中的來源位址對齊，以避免混亂。

`BGPRouter` 可廣播 connected/static IPv4 路由，並附上個別的 `allowedPrefixes`。
僅在 `BGPRouter.spec.exportPolicy.allowedPrefixes` 或 redistribute 的 allow-list 中明確列出的前綴，才會作為 GoBGP 的本地路徑新增。BGP community policy 可在路由器或
peer 上以 `communities.send`、`communities.accept`、`communities.set.in/out` 宣告，
GoBGP 回報的 observed route community 儲存於 status。watcher 預設每 15 秒輪詢，前綴 status 上限為 4096 個條目。可透過 `BGPRouter.spec.watcher` 調整
`pollInterval`、`maxPrefixes`、`peerStateChangeThrottle`。驗證會拒絕小於 3 秒的 interval 與 1,000,000 以上的前綴上限。GoBGP MVP 每個路由器支援一個 `BGPRouter`，`spec.vrf` 尚未支援。
multi-router、VRF、BFD 資源不會靜默忽略，而是回報為 Pending。
`spec.listen.address` 與 `spec.listen.port` 用於綁定 `routerd-bgp` 的 GoBGP listener。

`VirtualAddress` 的 `mode: vrrp` 在 Linux 使用 keepalived，在 FreeBSD 使用 CARP。
`spec.family: ipv4` 需要 IPv4 `/32`，`spec.family: ipv6` 需要 IPv6 `/128`。
IPv6 VIP 在 keepalived 中產生為 VRRPv3 的 `family inet6`，在 FreeBSD 中成為 `inet6` 的 CARP alias。
Linux VRRP 使用明確的 unicast peer，預設為 `nopreempt`。
FreeBSD CARP 使用父介面上的 multicast advertisement，因此 `spec.vrrp.peers` 在 FreeBSD 上會被忽略。`preempt: true` 僅在需要自動 failback 時使用。advertisement 與
failback 的低階 timing 不透過個別資源欄位，而是透過 routerd 的 profile 預設值處理。可透過 `track` 根據 `BGPRouter`、`BGPPeer`、`IngressService` 等的狀態降低優先度。預設在連續 3 次 unhealthy 時套用懲罰，連續 2 次 healthy 時解除。`spec.hostname` 可讓 DNSResolver 自動將 VIP 發佈至對應的 `DNSZone`。IPv4 VIP 成為 A 記錄，IPv6 VIP 成為 AAAA
記錄。若由外部 AD DNS 等管理名稱，請設定 `spec.externalDNS: true`。routerd 僅驗證 hostname 語法，不發出 DNSZone coverage 警告也不自動發佈。`routerctl show vrrp` 顯示角色、
優先度、peer，以及自上次轉換以來的經過時間。

### VRRP 正式環境調整

僅在需要自動 failback 的控制平面 VIP 等情境下使用 `preempt: true`。
家庭 LAN 或 DS-Lite 周邊的 VIP，穩定性優先於回復至優先 owner，建議使用預設的非搶佔行為。backup 取得 VIP 後，在該節點停機或明確移動之前會持續持有。完整的資源片段請參閱
`examples/vrrp-tuning-presets.yaml`。

`BGPPeer.spec.password` 作為 GoBGP peer 的 TCP MD5 authentication 密碼傳遞。
正式環境不建議將共用機密留在 routerd YAML 中，請優先使用 `BGPPeer.spec.passwordFrom`。
`passwordFrom.file` 讀取本地 root 擁有的機密檔案，
`passwordFrom.env` 讀取環境變數，`base64: true` 可解碼 base64 值。


`IngressService` 支援多個 backend、TCP health check、failover policy。
runtime 控制器解析 backend 的 FQDN，DNS 失敗時以上次解析的 IPv4 作為 fallback。Linux nftables 在下次 NAT 調和（reconcile）時以 status 中的 active backend 作為轉發目的地。不清除現有的 conntrack，因此現有流量保留在舊 backend，新流量則導向所選的 backend。`spec.hostname` 可自動反映至 DNSResolver 作為 listen 位址的 A 記錄。若由外部 DNS 管理名稱，請設定 `spec.externalDNS: true`。
`routerctl show ingress` 顯示 active backend 及各 backend 的健康狀態。
`routerctl show ingress --verbose` 亦顯示 live dataplane 的 `ip_forward`、nftables 的
DNAT/SNAT 規則數、對應的 conntrack 流量數。`DETAIL` 欄顯示
`hairpinMode`、是否需要 hairpin，以及預期的 nftables SNAT 規則是 present 還是 missing。從 Ingress、NAT 系、DS-Lite、IPv6 PD/RA、路由資源導出轉發、redirect 抑制、reverse path filter 例外、各介面的 RA 接收等所需的 runtime sysctl。`routerd apply --once` 會 plan / render 衍生設定，但主機變更僅限於明確的 `Sysctl` / `SysctlProfile` escape hatch。
衍生 runtime 設定的套用由 `routerd serve` 的控制器調和（reconcile）負責。
維護期間可用 `routerctl drain
ingress/<service> backend=<name> --duration 10m` 將 backend 設為 drain 狀態。控制器在 duration 結束或執行 `routerctl undrain
ingress/<service> backend=<name>` 解除前，將該 backend 視為以 `Drained` 為原因的 unhealthy。

`LocalServiceRedirect` 在 Linux nftables 的 `prerouting` 產生 `redirect` 規則。
僅針對從指定介面進入的封包，以及指向 `IPAddressSet` 目的地的流量。
路由器自身發出的通訊與健康檢查不經過此 hook。

範例：

```yaml
apiVersion: firewall.routerd.net/v1alpha1
kind: PortForward
metadata:
  name: web-admin
spec:
  listen:
    interface: wan
    addressFrom:
      resource: IPv4StaticAddress/wan-ip
      field: address
    protocol: tcp
    port: 8443
  target:
    address: 172.18.1.88
    port: 443
  hairpin:
    enabled: true
    mode: manual
    interfaces:
      - lan
```

DS-Lite、IPv4 預設路由、NAT44 均已在實際 lab 中驗證。

## 狀態聯動

| Kind | 用途 |
| --- | --- |
| `HealthCheck` | 從 target、protocol、cadence、threshold 宣告連線探測的意圖。被 `EgressRoutePolicy` 的 candidate/target 參照時，routerd 自動導出 health-check 常駐程式、來源綁定及 socket mark。 |
| `EgressRoutePolicy` | 從準備就緒的候選中選出 weight 最高的 egress 路由。具有 `destinationCIDRs` 以及 candidate 的 `gatewaySource`、`gateway`。 |
| `EventRule` | 對事件序列評估 all_of、any_of、sequence、window、absence、throttle、debounce、count。 |
| `DerivedEvent` | 從多個資源狀態發出虛擬事件。 |
| `SelfAddressPolicy` | 表示自身主機位址的選擇策略。 |

將 `HealthCheck.spec.enabled` 設為 `false` 時，常駐程式單元仍會產生，但會停止並停用。
`EgressRoutePolicy` 的候選也可指定 `enabled: false`。
停用的候選即使最後觀測狀態為 Healthy，也不會被選中。
`mode: priority` 中，candidate 的 `weight` 仍是選擇的第一排序鍵，`priority` 用於平局決勝與 policy 規則的優先度。刪除候選時，ledger 所擁有的 policy-route 規則/路由表也會一併刪除。

## `spec.when`

具有 `spec.when` 的資源，只在對 routerd 本地狀態儲存的 predicate 相符時才生效。傳統的單一 predicate 語法仍可使用。

```yaml
when:
  state:
    wan.ipv6.mode:
      equals: pd-ready
```

AND 以 `all`、OR 以 `any` 表示，可任意深度巢狀。

```yaml
when:
  any:
    - all:
        - state:
            dslite.a.health:
              status: set
        - state:
            wan.ipv6.mode:
              in: [pd-ready, address-only]
    - state:
        pppoe.health:
          equals: healthy
```

每個 `when` 節點只能包含 `state`、`all`、`any` 之一。
`state` 以狀態變數名稱為鍵，透過 `exists`、`equals`、`in`、`contains`、
`status`、`for` 進行比對。只有一個元素的 `all` 等同於單一 predicate 語法。
不公開專門用於狀態管理的資源 Kind。條件式 activation 直接寫在相依資源的 `spec.when` 中。

`HealthCheck.spec.sourceInterface` 在執行時解析為 OS 的介面名稱。
Linux 使用 `SO_BINDTODEVICE`。若指定 `fwmark`，也會設定 `SO_MARK`。`HealthCheck` 被 `EgressRoutePolicy` 的 candidate 或 target 參照時，routerd 自動從該路由 target 的 mark 導出 `SO_MARK`。
直接指定 `fwmark` 適用於不與路由 target 綁定的低階探測。
FreeBSD 因沒有與 Linux 相同的 socket option，改從指定介面選擇來源位址。

## 系統

| Kind | 用途 |
| --- | --- |
| `Hostname` | 管理主機名稱。 |
| `Sysctl` | 管理 sysctl 值。 |
| `NTPClient` | 管理 NTP 用戶端設定。可透過 `serverFrom` 參照 `DHCPv4Client.status.ntpServers` 或 `DHCPv6Information.status.sntpServers`。 |
| `LogSink` | 表示日誌的傳送目的地。 |
| `WebConsole` | 顯示狀態、事件、IPv4/IPv6 連線觀測的唯讀畫面。 |

`Telemetry` 是將 routerd 自身及受管理常駐程式的 metrics / traces / logs 送至
OpenTelemetry 端點的資源。`LogSink` 表示運作事件與觀測日誌的轉發路徑。若要將日誌轉發至 OTLP，請勿重複填寫 collector 端點，而是透過 `LogSink.spec.otlp.telemetryRef` 參照 `Telemetry`。

`WebConsole.spec.listenAddressFrom` 從其他資源的狀態導出 HTTP 的監聽位址。
例如可參照 `Interface/mgmt.status.ipv4Addresses`。
若管理位址由 DHCP、IPAM 或其他宣告資源提供，請使用此方式而非固定的 `listenAddress`。

## Status Provides Contract

`addressFrom`、`gatewayFrom`、`dnsServerFrom`、`dependsOn[].field`
等參照欄位，只能參照來源 Kind 在此 contract 中宣告的欄位。參照不存在的資源，或 `provides` 中未宣告的欄位，驗證器會回傳錯誤。

| Kind | Provides |
| --- | --- |
| `BFD` | `peer` (string), `phase` (string) |
| `BGPPeer` | `acceptedPrefixes` (int), `address` (string), `observedAt` (timestamp), `phase` (string), `state` (string) |
| `BGPRouter` | `acceptedPrefixes` (int), `establishedPeers` (int), `observedAt` (timestamp), `peers` (objectList), `phase` (string), `prefixes` (int) |
| `Bridge` | `ifname` (string), `members` (stringList), `phase` (string) |
| `ClientPolicy` | `phase` (string) |
| `ClusterNetworkRoute` | `phase` (string), `pods` (stringList), `services` (stringList) |
| `DHCPv4Client` | `currentAddress` (string), `defaultGateway` (string), `device` (string), `dnsServers` (stringList), `domain` (string), `expiresAt` (timestamp), `gateway` (string), `interface` (string), `leaseTime` (int), `ntpServers` (stringList), `phase` (string), `rebindAt` (timestamp), `renewAt` (timestamp) |
| `DHCPv4Relay` | `phase` (string) |
| `DHCPv4Reservation` | `address` (string), `hostname` (string), `phase` (string) |
| `DHCPv4Server` | `configPath` (string), `dnsServers` (stringList), `domain` (string), `dryRun` (bool), `interface` (string), `ntpServers` (stringList), `phase` (string) |
| `DHCPv6Address` | `address` (string), `interface` (string), `phase` (string) |
| `DHCPv6Information` | `aftrName` (string), `dnsServers` (stringList), `domainSearch` (stringList), `phase` (string), `sntpServers` (stringList), `source` (string) |
| `DHCPv6PrefixDelegation` | `aftrName` (string), `currentPrefix` (string), `dnsServers` (stringList), `domainSearch` (stringList), `interface` (string), `phase` (string), `sntpServers` (stringList) |
| `DHCPv6Server` | `configPath` (string), `dnsServers` (stringList), `dryRun` (bool), `interface` (string), `phase` (string), `sntpServers` (stringList) |
| `DNSForwarder` | `phase` (string), `resolver` (string), `upstreams` (stringList) |
| `DNSResolver` | `listenAddresses` (stringList), `listeners` (int), `phase` (string), `sources` (int), `updatedAt` (timestamp) |
| `DNSUpstream` | `address` (string), `phase` (string), `url` (string) |
| `DNSZone` | `pendingRecords` (objectList), `phase` (string), `records` (int), `updatedAt` (timestamp), `zone` (string) |
| `DSLiteTunnel` | `aftrIPv6` (string), `aftrName` (string), `device` (string), `dryRun` (bool), `innerLocalIPv4` (string), `innerRemoteIPv4` (string), `interface` (string), `localIPv6` (string), `localInterface` (string), `mtu` (int), `phase` (string), `tunnelName` (string) |
| `DerivedEvent` | `phase` (string), `topic` (string) |
| `EgressRoutePolicy` | `advisory` (bool), `candidates` (objectList), `dryRun` (bool), `family` (string), `lastTransitionAt` (timestamp), `phase` (string), `role` (string), `selectedCandidate` (string), `selectedDevice` (string), `selectedGateway` (string), `selectedGatewaySource` (string), `selectedInterface` (string), `selectedMetric` (int), `selectedRouteTable` (int), `selectedSource` (string), `selectedTargets` (int), `selectedWeight` (int), `updatedAt` (timestamp) |
| `EventRule` | `phase` (string), `topic` (string) |
| `FirewallEventLog` | `path` (string), `phase` (string), `sinks` (stringList) |
| `FirewallPolicy` | `phase` (string) |
| `FirewallRule` | `action` (string), `phase` (string) |
| `FirewallZone` | `interfaces` (stringList), `phase` (string) |
| `HealthCheck` | `consecutiveFailed` (int), `lastCheckedAt` (timestamp), `phase` (string), `protocol` (string), `role` (string), `sourceAddress` (string), `sourceInterface` (string), `target` (string) |
| `Hostname` | `hostname` (string), `phase` (string) |
| `IPAddressSet` | `addresses` (stringList), `ipv4Addresses` (stringList), `ipv6Addresses` (stringList), `phase` (string), `updatedAt` (timestamp) |
| `IPsecConnection` | `phase` (string) |
| `IPv4Route` | `destination` (string), `device` (string), `dryRun` (bool), `gateway` (string), `metric` (int), `phase` (string), `type` (string) |
| `IPv4StaticAddress` | `address` (string), `dryRun` (bool), `ifname` (string), `interface` (string), `phase` (string) |
| `IPv4StaticRoute` | `destination` (string), `gateway` (string), `interface` (string), `phase` (string) |
| `IPv6DelegatedAddress` | `address` (string), `dryRun` (bool), `interface` (string), `phase` (string), `prefixSource` (string) |
| `IPv6RAAddress` | `address` (string), `interface` (string), `phase` (string) |
| `IPv6RouterAdvertisement` | `configPath` (string), `dryRun` (bool), `interface` (string), `phase` (string), `prefix` (string), `rdnss` (stringList) |
| `RogueRADetector` | `interface` (string), `observedRouters` (string), `packetsSeen` (string), `phase` (string), `rogueCount` (string), `selfMAC` (string) |
| `IPv6StaticRoute` | `destination` (string), `gateway` (string), `interface` (string), `phase` (string) |
| `IngressService` | `activeBackend` (object), `activeBackends` (objectList), `backends` (objectList), `dryRun` (bool), `healthyBackends` (int), `hostname` (string), `listenAddress` (string), `observedAt` (timestamp), `phase` (string), `totalBackends` (int) |
| `Interface` | `addresses` (stringList), `ifname` (string), `ipv4Addresses` (stringList), `ipv6Addresses` (stringList), `macAddress` (string), `phase` (string) |
| `Inventory` | `host` (object), `phase` (string) |
| `LocalServiceRedirect` | `phase` (string) |
| `LogRetention` | `phase` (string), `targets` (objectList), `updatedAt` (timestamp) |
| `LogSink` | `phase` (string), `type` (string) |
| `NAT44Rule` | `dryRun` (bool), `egressInterface` (string), `phase` (string), `snatAddress` (string) |
| `NTPClient` | `phase` (string), `servers` (stringList), `source` (string), `updatedAt` (timestamp) |
| `NTPServer` | `allowCIDRs` (stringList), `listenAddresses` (stringList), `phase` (string), `servers` (stringList), `source` (string), `updatedAt` (timestamp) |
| `ObservabilityPipeline` | `phase` (string), `signals` (stringList) |
| `PPPoESession` | `connectedAt` (timestamp), `currentAddress` (string), `device` (string), `dnsServers` (stringList), `dryRun` (bool), `gateway` (string), `interface` (string), `peerAddress` (string), `phase` (string) |
| `Package` | `dryRun` (bool), `packages` (stringList), `phase` (string) |
| `PortForward` | `dryRun` (bool), `listenAddress` (string), `phase` (string), `target` (object) |
| `RouterdCluster` | `leader` (string), `leaseExpiresAt` (timestamp), `phase` (string) |
| `SelfAddressPolicy` | `address` (string), `phase` (string), `source` (string) |
| `Sysctl` | `dryRun` (bool), `key` (string), `phase` (string), `value` (string) |
| `SysctlProfile` | `dryRun` (bool), `phase` (string), `profile` (string) |
| `TailscaleNode` | `advertiseRoutes` (stringList), `peerCount` (int), `phase` (string), `tailnetName` (string) |
| `Telemetry` | `phase` (string), `signals` (stringList) |
| `TrafficFlowLog` | `path` (string), `phase` (string), `sinks` (stringList) |
| `VRF` | `ifname` (string), `members` (stringList), `phase` (string), `routeTable` (int) |
| `VXLANSegment` | `ifname` (string), `phase` (string), `vni` (int) |
| `VXLANTunnel` | `ifname` (string), `phase` (string), `vni` (int) |
| `VirtualAddress` | `address` (string), `dryRun` (bool), `hostname` (string), `ifname` (string), `phase` (string), `priority` (int), `role` (string), `virtualRouterID` (int) |
| `WebConsole` | `listenAddress` (string), `phase` (string), `port` (int) |
| `WireGuardInterface` | `fwmark` (int), `listenPort` (int), `peerCount` (int), `phase` (string), `publicKey` (string) |
| `WireGuardPeer` | `handshakeAgeSeconds` (int), `latestEndpoint` (string), `latestHandshake` (timestamp), `phase` (string), `transferRxBytes` (int), `transferTxBytes` (int) |

## 防火牆

| Kind | 用途 |
| --- | --- |
| `FirewallZone` | 將介面指派至區域，設定 `untrust`、`trust`、`mgmt` 角色。 |
| `FirewallPolicy` | 表示拒絕日誌等全域設定。 |
| `FirewallRule` | 表示無法以角色組合表達的例外。可透過來源 CIDR、目的地 CIDR、`IPAddressSet` 目的地參照縮小範圍。 |
| `ClientPolicy` | 依 MAC 位址分類用戶端，透過 Linux nftables 實現訪客隔離。 |
| `PortForward` | 新增單一目的地的 ingress DNAT 規則。routerd 同時管理防火牆表時，也會產生內部的 forward accept。選用的 hairpin 模式下，也會產生 LAN 側的 DNAT 與返回路徑的 SNAT。 |
| `IngressService` | 新增與 `PortForward` 相同的 ingress DNAT。接受多個 backend、選擇策略及健康檢查的意圖，runtime 的 failover 狀態由控制器路徑處理。選用的 hairpin 模式與 `PortForward` 相同。 |
| `LocalServiceRedirect` | 將明確指向 `IPAddressSet` 的通訊 redirect 至本地服務。防火牆的產生器也會產生從來源區域到對應本地輸入埠的開口。 |

有狀態的過濾條件產生於 nftables 的 `inet routerd_filter` 表。
已建立的通訊、loopback、必要的 ICMPv6 始終允許。
DHCP、DNS、DS-Lite 等所需的開口由 routerd 內部產生。

`ClientPolicy` 在 `mode: include` 下的行為是「將清單中的 MAC 位址視為訪客」。
`mode: exclude` 下的行為是「將清單中的 MAC 位址視為 trusted，對象介面上的其餘裝置視為訪客」。
`spec.macs` 是簡寫形式。`classification[]` 是結構化形式，每個條目具有
`mode: trusted|guest|isolated` 以及 `match.macs`、`match.ouiPrefixes`、
`match.hostnamePatterns`、`match.dhcpFingerprints` 選擇器。
match 欄位以 OR 評估。`ipv4Reservation` 亦可用於在無法直接比對 Ethernet 來源位址的平台上，穩定以位址為基礎的產生。
`spec.isolation` 可表達典型訪客的意圖，例如允許網際網路、拒絕 LAN/mgmt、拒絕 mDNS/SSDP/NetBIOS discovery。
FreeBSD pf 在 routed filter path 上不具備相同的 MAC 比對模型，因此此資源在 FreeBSD 上視為不支援。

## 名稱變更重點

Phase 1.6 中進行了以下名稱整理：

| 舊名稱 | 現在的名稱 |
| --- | --- |
| `IPv4DHCPServer` | `DHCPv4Server` |
| `IPv4DHCPReservation` | `DHCPv4Reservation` |
| `IPv4DHCPScope` | `DHCPv4Server` |
| `IPv6DHCPAddress` | `DHCPv6Address` |
| `IPv6PrefixDelegation` | `DHCPv6PrefixDelegation` |
| `IPv6DHCPServer` / `IPv6DHCPv6Server` | `DHCPv6Server` |
| `IPv6DHCPScope` | `DHCPv6Server` |
| `DHCPRelay` | `DHCPv4Relay` |

二進位檔名稱也已更新為 `routerd-dhcpv4-client`、`routerd-dhcpv6-client`。
