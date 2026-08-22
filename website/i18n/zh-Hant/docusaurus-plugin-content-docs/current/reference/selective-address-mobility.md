---
title: Selective Address Mobility
---

# Selective Address Mobility

Selective Address Mobility (SAM) 不是 full L2 extension。routerd CloudEdge 不把
Ethernet segment 延伸到 public cloud，而是只移動選定的 IPv4 `/32`。source/destination
address 會保留；firewall 與 NAT 是單獨的 routerd layer。

![SAM transport 圖：MobilityPool 與 SAMTransportProfile 作為 authoring surface，產生 IPIP delivery、BGP peer、ECMP next hop，並由 secondary IP 或 proxy ARP capture](/img/diagrams/cloudedge-sam-ipip.png)

## primary resource model

目前 CloudEdge Mobility 的 operator-authored surface 是：

- `SAMNodeSet`: 一次性宣告完整共享的 node identity、topology、placement 與 SAM endpoint。
- `MobilityPool`: 宣告 mobility prefix、EventGroup、BGP delivery，以及本 node 的
  capture/discovery/provider trap local overlay。
- `SAMTransportProfile`: 宣告 router-to-router transport、`selfNodeRef`、`innerPrefix`、
  underlay interface、BGP router，以及從 `SAMNodeSet` 選擇的 peer source。

`MobilityPool` 透過 `membersFrom` 匯入共享 `SAMNodeSet`，並且只保留 self-member
overlay。不要在 Pool 內重複 remote identity、placement 或 maintenance。provider、capture
與 discovery detail 只能屬於 self-member overlay。所有 node 應取得相同的 `SAMNodeSet`，以便
deterministic projection。

`SAMNodeSet.spec.nodes[].macAddresses` 可靜態列出同一 fabric 中 member 的 MAC
地址。on-prem ARP observer 會把所有 member MAC 的聯集作為 ignore set，避免 routerd
member 發出的 ARP frame 被當作 mobile `/32` 的 ownership signal。`macAddresses`
的編輯是宣告式 intent：routerd 會導出 observer ignore set，並透過 observer socket
自動收斂，不需要重啟 observer 或 routerd。observer status 會顯示目前生效的 ignore set
和被忽略的 observation 計數，便於確認收斂狀態。

## transport

目前 SAM transport 預設使用 IPIP delivery plane。WireGuard 如存在，只作為加密 underlay；
WireGuard peer 的 `AllowedIPs` 應只包含 transport endpoint prefix，不應包含 mobile `/32`。

`SAMTransportProfile` 會產生 per-peer `TunnelInterface`、endpoint `/32` `IPv4Route`
與 `BGPPeer`。所有 profile 透過 `peersFrom` 使用相同的 `SAMNodeSet`，因此每條 node pair
edge 都能導出相同的 `/31`。`nodeRefs` 可將 peer 限制為實際 adjacency；省略時會選擇帶
`samEndpoint` 的所有非 self node。

已簽署的 enrollment 可以在同一個 runtime snapshot 中額外回傳一個
`SAMPeerGroup`，並在 `peersFrom` 中標記為 `direct: true`。它只是在可達 leaf 之間嘗試
一條選用的 L3 直連路徑；前面的非 optional `SAMRRSet` 仍是啟動與故障時的備援路徑。
direct source 必須使用 `addressingMode: pair-stable`，並且不能把 peer 設為 route
reflector client。

當 direct BGP session 建立後，routerd 會給它匯入的路由設定比 RR 更高的 BGP
`LOCAL_PREF`。這不是根據 AS_PATH 長度或 administrative distance 選擇：iBGP 的 RR
反射不會可靠地把額外的轉送 hop 寫入 AS_PATH。profile 必須明確設定 RR 的
`bgp.importPolicy.localPreference`，`bgp.directLocalPreference` 必須更高。每個 direct
leaf 還只能通告其簽署 claim 中列出的 IPv4 `/32`；沒有、過期、不相容或不可達的 direct
group 會被忽略，RR 路徑繼續工作。

RR import 的 `nextHopRewrite` 請維持預設的 `peer-address`，或明確寫出該值。direct
profile 不能使用 `unchanged`：RR 反射的路由可能來自另一台 leaf，但真正可達的下一跳是
直接相連的 RR tunnel。驗證會拒絕 `unchanged`，以免 direct mesh 依賴無關的 transport
prefix route；它只決定學到 route 後的轉送下一跳，不會改變是否接納 direct peer。

## dynamic RR sync fail-static

RR 可以發布 `SAMPeerGroup`，leaf 透過 TCP 19652 取得缺失的 transport peer group。
`SAMPeerGroup` 僅是執行期同步 payload，不能作為 top-level `spec.resources` 宣告。
取得成功後，leaf 會把它保存為帶 TTL 的 dynamic config part：

- `peer-group-sync/<name>` 對應 `SAMPeerGroup`

TTL 過期或 RR publisher 消失時，leaf 不會刪除已經產生的 tunnel 或 BGP peer。routerd
會繼續使用 last-known-good record，並把來源標記為 `Stale`，同時在 status 中輸出
`warning`。MobilityPool membership 直接從靜態 `SAMNodeSet` configuration 解析，不依賴
成員資格同步端點。

## capture and delivery

`MobilityPool` 一律使用 BGP delivery。owner advertise selected `/32`，non-owner 將
BGP best path import 到 local FIB。

支援的 capture type：

| Type | Meaning |
| --- | --- |
| `provider-secondary-ip` | cloud fabric 透過 provider secondary address object 或等價機制 capture `/32`。 |
| `proxy-arp` | site router 在本地對 selected address 回答 ARP。 |

cloud `provider-secondary-ip` capture 可選擇 capture strategy。當前 release lab
認證僅涵蓋 `secondary-ip` capture。`route-table` strategy 目前為 **uncertified**：
在 Azure 上它透過 UDR 指向 holder，並要求 routerd 等待 provider inventory 觀測到
該 UDR 指向本地 router 後，才將已 capture 的 `/32` 廣告到 BGP。這個 provider
觀測 gate 是 `route-table` strategy 特有的；`secondary-ip` capture 不使用
route-table 觀測來決定何時廣告 overlay holder。由於該設計會把 ARM/provider API
延遲傳遞到 overlay 收斂，route-table strategy 需要在 release lab 中完成 provider
觀測、BGP 廣告耦合和 provider API 延遲行為驗證後才能認證。

on-prem `proxy-arp` capture 可使用 `activeWhen.type: single-router` 作為單 router
always-active capture，也可使用 `vrrp-master` 由 HA pair 的 VRRP master gate 控制。
`activeWhen` 僅支援此 on-prem `proxy-arp` capture；cloud
`provider-secondary-ip` capture 設定該欄位會被拒絕，因此本機 VRRP 狀態變化
不會在缺少對應 BGP 與本機資料平面計畫時建立 provider assignment。

`on-demand-arp` source 會以低速 proactive sweep 探測 mobility prefix：每個
`scanInterval` 探測一個 target，使已啟動但安靜的 L2 client 也能被觀測到。

## provider actions

provider capture planner 可輸出 `assign-secondary-ip`、`ensure-forwarding-enabled` 等
provider `ActionPlan`。planner 本身不呼叫 provider API。action plan 只有在匯入
provider-action journal 並通過 `ProviderActionPolicy`、approval、allowlist 與 executor
plugin gate 後才可能執行。

## RR admission filters

Generated route-reflector client BGP peers derive an import admission policy from
the SAM topology and `importPolicy.allowedPrefixes`; if that prefix list is
omitted, routerd defaults it from declared `MobilityPool` prefixes. Imported
routes must be `/32`s under the permitted prefixes, must carry the advertising
leaf's own node-identity community, and must not carry another topology node's
identity. This prevents a leaf from claiming another node identity or advertising
a broad mobility prefix through the generated RR session. A compromised leaf can
still advertise a pool-local `/32` with its own identity; constraining per-node
ownership requires a separate authorization signal beyond this route filter.

## ownership inspection

`MobilityPool` status exposes one per-address operational view:
`ownershipResolverControlPlaneOwnerTable`. `doctor sam`, FIB checks, and
operators use this control-plane table. It keeps one deterministic row per
observed mobility address and includes owner provider/NIC/subnet/resource,
local evidence, capture state and final `captureDisposition`/`captureReason`,
advertise/suppression state, and conflict details.

Use `routerctl mobility explain --pool <pool> --address <ipv4/32>` to render an
owner-table row together with pool-level provider status. `OwnershipResolved`
comes from the row. `providerActionFailedAddresses` makes
`ProviderActionApplied=False` only for a named address, and
`providerObservationPendingAddresses` makes `ProviderObserved=False` only for
a named address. Those conditions remain `Unknown` for all other addresses;
the pool-level provider phase is not projected as a failure for every address.

When two fresh provider owners claim the same `/32`, the row state is
`Conflict` with `conflictReason=duplicate-provider-home-owners`. The row also
includes `conflictWinnerNode` and `conflictResolution`: the healed BGP owner
wins when present; otherwise the lowest stable owner key wins (`nodeRef`,
provider ref, resource ref, NIC ref, subnet ref, then address), independent of
provider scan recency. A losing node that still observes a local
provider-secondary capture reports `loser-release-local-capture` and releases
only that local capture after the stale-capture hold-down.

## firewall and NAT

SAM 不包含 `nat`、`preserveSource`、firewall 或 zone 欄位。若要 firewall/NAT mobile
address，請在現有 `FirewallZone`、`FirewallRule`、`NAT44Rule` 中參照 literal `/32`。
SAM forwarded traffic 仍會經過普通 forwarding/firewall/conntrack path。

### conntrack cleanup design note

routerd 曾短暫公開 BGP mode SAM failover 的手動 opt-in scoped conntrack cleanup hook。
該功能已經移除。
在參考 SAM leaf 構成中，routerd 不會繪製讓 delivered overlay flow 進入 conntrack 的
dataplane rule，因此 leaf 側 scoped cleanup 是 no-op，也不能解決 failover flow anomaly。

這個問題陳述對未來 stateful SAM leaf 設計仍然成立：如果某個 router 有意追蹤 forwarded
mobile `/32` flow，它在成為 holder 時可能需要 scoped recovery hook。重新引入時應偵測
routerd-managed ct-engage dataplane，並只在該場景自動啟用 cleanup。不要以手動 opt-in flag
的形式重新引入。
