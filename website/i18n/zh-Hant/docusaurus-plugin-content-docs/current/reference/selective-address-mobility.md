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

- `MobilityPool`: 宣告 mobility prefix、EventGroup、member node/site、BGP delivery
  policy、capture policy、provider trap placement，以及本 node 的 capture/discovery 細節。
- `SAMTransportProfile`: 宣告 router-to-router transport、`selfNodeRef`、共享
  `topologyNodeRefs`、`innerPrefix`、underlay interface、BGP router 與 peers。

`MobilityPool` 中 self site 應完整宣告；remote site 通常保持 identity-only，僅包含
`nodeRef`、`site`、`role`，以及可選的 `placement` / `maintenance`。所有 node 應取得相同的
pool identity 與 placement set，以便 deterministic projection。

`SAMNodeSet.spec.nodes[].macAddresses` 可靜態列出同一 fabric 中 member 的 MAC
地址。on-prem ARP observer 會把所有 member MAC 的聯集作為 ignore set，避免 routerd
member 發出的 ARP frame 被當作 mobile `/32` 的 ownership signal。`macAddresses`
的編輯是宣告式 intent：routerd 會導出 observer ignore set，並透過 observer socket
自動收斂，不需要重啟 observer 或 routerd。observer status 會顯示目前生效的 ignore set
和被忽略的 observation 計數，便於確認收斂狀態。

`AddressMobilityDomain` 與 `RemoteAddressClaim` 是低層相容 resource。pre-release 期間仍支援
hand-authored config，但新 CloudEdge Mobility config 應優先使用 `MobilityPool` 與
`SAMTransportProfile`。

## transport

目前 SAM transport 預設使用 IPIP delivery plane。WireGuard 如存在，只作為加密 underlay；
WireGuard peer 的 `AllowedIPs` 應只包含 transport endpoint prefix，不應包含 mobile `/32`。

`SAMTransportProfile` 會產生 per-peer `TunnelInterface`、endpoint `/32` `IPv4Route`
與 `BGPPeer`。多個 peer 的 profile 必須在所有 router 上使用相同的 `topologyNodeRefs` 與
`innerPrefix`，這樣每條 node pair edge 才能導出相同的 `/31`。

## dynamic RR sync fail-static

RR 可以發布 `SAMPeerGroup` 和 `MobilityMemberSet`，leaf 透過 TCP 19652
取得缺失的 transport peer group 或 shared member set。取得成功後，leaf 會把它們保存為
帶 TTL 的 dynamic config part：

- `peer-group-sync/<name>` 對應 `SAMPeerGroup`
- `member-set-sync/<name>` 對應 `MobilityMemberSet`

TTL 過期或 RR publisher 消失時，leaf 不會刪除已經產生的 tunnel、BGP peer 或
MobilityPool planning artifact。routerd 會繼續使用 last-known-good record，並把來源標記為
`Stale`，同時在 status 中輸出 `warning`。只有從未取得過的必需 source 才保持
`Pending`。

## capture and delivery

`MobilityPool.spec.deliveryPolicy.mode` 預設為 `bgp`。owner advertise selected `/32`，
non-owner 將 BGP best path import 到 local FIB。舊的 route-lowered delivery 僅用於
`RemoteAddressClaim` 相容 config。

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

`MobilityPool` status exposes `ownershipResolverOwnerTable` for local
`doctor sam` / FIB checks and `ownershipResolverControlPlaneOwnerTable` for
operators. The control-plane table keeps one deterministic row per observed
mobility address and includes owner provider/NIC/subnet/resource, local
evidence, capture state, advertise/suppression state, and conflict details.

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

routerd 曾短暫公開 `MobilityPool.spec.deliveryPolicy.conntrackCleanupOnSeize`，
作為 BGP mode SAM failover 的 opt-in scoped conntrack cleanup hook。該欄位已經移除。
在參考 SAM leaf 構成中，routerd 不會繪製讓 delivered overlay flow 進入 conntrack 的
dataplane rule，因此 leaf 側 scoped cleanup 是 no-op，也不能解決 failover flow anomaly。

這個問題陳述對未來 stateful SAM leaf 設計仍然成立：如果某個 router 有意追蹤 forwarded
mobile `/32` flow，它在成為 holder 時可能需要 scoped recovery hook。重新引入時應偵測
routerd-managed ct-engage dataplane，並只在該場景自動啟用 cleanup。不要以手動 opt-in flag
的形式重新引入。
