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
`Stale`。只有從未取得過的必需 source 才保持 `Pending`。

## capture and delivery

`MobilityPool.spec.deliveryPolicy.mode` 預設為 `bgp`。owner advertise selected `/32`，
non-owner 將 BGP best path import 到 local FIB。舊的 route-lowered delivery 僅用於
`RemoteAddressClaim` 相容 config。

支援的 capture type：

| Type | Meaning |
| --- | --- |
| `provider-secondary-ip` | cloud fabric 透過 provider secondary address object 或等價機制 capture `/32`。 |
| `proxy-arp` | site router 在本地對 selected address 回答 ARP。 |

on-prem `proxy-arp` capture 可使用 `activeWhen.type: single-router` 作為單 router
always-active capture，也可使用 `vrrp-master` 由 HA pair 的 VRRP master gate 控制。

`on-demand-arp` source 會以低速 proactive sweep 探測 mobility prefix：每個
`scanInterval` 探測一個 target，使已啟動但安靜的 L2 client 也能被觀測到。

## provider actions

provider capture planner 可輸出 `assign-secondary-ip`、`ensure-forwarding-enabled` 等
provider `ActionPlan`。planner 本身不呼叫 provider API。action plan 只有在匯入
provider-action journal 並通過 `ProviderActionPolicy`、approval、allowlist 與 executor
plugin gate 後才可能執行。

## firewall and NAT

SAM 不包含 `nat`、`preserveSource`、firewall 或 zone 欄位。若要 firewall/NAT mobile
address，請在現有 `FirewallZone`、`FirewallRule`、`NAT44Rule` 中參照 literal `/32`。
SAM forwarded traffic 仍會經過普通 forwarding/firewall/conntrack path。
