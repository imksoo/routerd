---
title: Selective Address Mobility
---

# Selective Address Mobility

Selective Address Mobility (SAM) 不是 full L2 extension。routerd CloudEdge 不把
Ethernet segment 延伸到 public cloud，而是只移动选定的 IPv4 `/32`。source/destination
address 会保留；firewall 与 NAT 是单独的 routerd layer。

![SAM transport 图：MobilityPool 与 SAMTransportProfile 作为 authoring surface，生成 IPIP delivery、BGP peer、ECMP next hop，并由 secondary IP 或 proxy ARP capture](/img/diagrams/cloudedge-sam-ipip.png)

## primary resource model

当前 CloudEdge Mobility 的 operator-authored surface 是：

- `MobilityPool`: 声明 mobility prefix、EventGroup、member node/site、BGP delivery
  policy、capture policy、provider trap placement，以及本 node 的 capture/discovery 细节。
- `SAMTransportProfile`: 声明 router-to-router transport、`selfNodeRef`、共享
  `topologyNodeRefs`、`innerPrefix`、underlay interface、BGP router 与 peers。

`MobilityPool` 中 self site 应完整声明；remote site 通常保持 identity-only，仅包含
`nodeRef`、`site`、`role`，以及可选的 `placement` / `maintenance`。所有 node 应获得相同的
pool identity 与 placement set，以便 deterministic projection。

`SAMNodeSet.spec.nodes[].macAddresses` 可静态列出同一 fabric 中 member 的 MAC
地址。on-prem ARP observer 会把所有 member MAC 的并集作为 ignore set，避免 routerd
member 发出的 ARP frame 被当作 mobile `/32` 的 ownership signal。`macAddresses`
的编辑是声明式 intent：routerd 会导出 observer ignore set，并通过 observer socket
自动收敛，不需要重启 observer 或 routerd。observer status 会显示当前生效的 ignore set
和被忽略的 observation 计数，便于确认收敛状态。

`AddressMobilityDomain` 与 `RemoteAddressClaim` 是低层兼容 resource。pre-release 期间仍支持
hand-authored config，但新 CloudEdge Mobility config 应优先使用 `MobilityPool` 与
`SAMTransportProfile`。

## transport

当前 SAM transport 默认使用 IPIP delivery plane。WireGuard 如存在，只作为加密 underlay；
WireGuard peer 的 `AllowedIPs` 应只包含 transport endpoint prefix，不应包含 mobile `/32`。

`SAMTransportProfile` 会生成 per-peer `TunnelInterface`、endpoint `/32` `IPv4Route`
与 `BGPPeer`。多个 peer 的 profile 必须在所有 router 上使用相同的 `topologyNodeRefs` 与
`innerPrefix`，这样每条 node pair edge 才能导出相同的 `/31`。

## dynamic RR sync fail-static

RR 可以发布 `SAMPeerGroup` 和 `MobilityMemberSet`，leaf 通过 TCP 19652
获取缺失的 transport peer group 或 shared member set。获取成功后，leaf 会把它们保存为
带 TTL 的 dynamic config part：

- `peer-group-sync/<name>` 对应 `SAMPeerGroup`
- `member-set-sync/<name>` 对应 `MobilityMemberSet`

TTL 过期或 RR publisher 消失时，leaf 不会删除已经生成的 tunnel、BGP peer 或
MobilityPool planning artifact。routerd 会继续使用 last-known-good 记录，并把来源标记为
`Stale`，同时在 status 中输出 `warning`。只有从未获取过的必需 source 才保持
`Pending`。

## capture and delivery

`MobilityPool.spec.deliveryPolicy.mode` 默认为 `bgp`。owner advertise selected `/32`，
non-owner 将 BGP best path import 到 local FIB。旧的 route-lowered delivery 仅用于
`RemoteAddressClaim` 兼容 config。

支持的 capture type：

| Type | Meaning |
| --- | --- |
| `provider-secondary-ip` | cloud fabric 通过 provider secondary address object 或等价机制 capture `/32`。 |
| `proxy-arp` | site router 在本地对 selected address 回答 ARP。 |

cloud `provider-secondary-ip` capture 可选择 capture strategy。当前 release lab
认证仅覆盖 `secondary-ip` capture。`route-table` strategy 目前为 **uncertified**：
在 Azure 上它通过 UDR 指向 holder，并要求 routerd 等待 provider inventory 观测到
该 UDR 指向本地 router 后，才将已 capture 的 `/32` 广告到 BGP。这个 provider
观测 gate 是 `route-table` strategy 特有的；`secondary-ip` capture 不使用
route-table 观测来决定何时广告 overlay holder。由于该设计会把 ARM/provider API
延迟传递到 overlay 收敛，route-table strategy 需要在 release lab 中完成 provider
观测、BGP 广告耦合和 provider API 延迟行为验证后才能认证。

on-prem `proxy-arp` capture 可使用 `activeWhen.type: single-router` 作为单 router
always-active capture，也可使用 `vrrp-master` 由 HA pair 的 VRRP master gate 控制。

`on-demand-arp` source 会以低速 proactive sweep 探测 mobility prefix：每个
`scanInterval` 探测一个 target，使已启动但安静的 L2 client 也能被观测到。

## provider actions

provider capture planner 可输出 `assign-secondary-ip`、`ensure-forwarding-enabled` 等
provider `ActionPlan`。planner 本身不调用 provider API。action plan 只有在导入
provider-action journal 并通过 `ProviderActionPolicy`、approval、allowlist 与 executor
plugin gate 后才可能执行。

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

SAM 不包含 `nat`、`preserveSource`、firewall 或 zone 字段。若要 firewall/NAT mobile
address，请在现有 `FirewallZone`、`FirewallRule`、`NAT44Rule` 中引用 literal `/32`。
SAM forwarded traffic 仍会经过普通 forwarding/firewall/conntrack path。

### conntrack cleanup design note

routerd 曾短暂公开 `MobilityPool.spec.deliveryPolicy.conntrackCleanupOnSeize`，
作为 BGP mode SAM failover 的 opt-in scoped conntrack cleanup hook。该字段已经移除。
在参考 SAM leaf 构成中，routerd 不会绘制让 delivered overlay flow 进入 conntrack 的
dataplane rule，因此 leaf 侧 scoped cleanup 是 no-op，也不能解决 failover flow anomaly。

这个问题陈述对未来 stateful SAM leaf 设计仍然成立：如果某个 router 有意追踪 forwarded
mobile `/32` flow，它在成为 holder 时可能需要 scoped recovery hook。重新引入时应检测
routerd-managed ct-engage dataplane，并只在该场景自动启用 cleanup。不要以手动 opt-in flag
的形式重新引入。
