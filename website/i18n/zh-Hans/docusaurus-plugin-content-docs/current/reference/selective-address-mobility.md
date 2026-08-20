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

- `SAMNodeSet`: 一次性声明完整共享的 node identity、topology、placement 与 SAM endpoint。
- `MobilityPool`: 声明 mobility prefix、EventGroup、BGP delivery、以及本 node 的
  capture/discovery/provider trap local overlay。
- `SAMTransportProfile`: 声明 router-to-router transport、`selfNodeRef`、`innerPrefix`、
  underlay interface、BGP router，以及从 `SAMNodeSet` 选择的 peer source。

`MobilityPool` 通过 `membersFrom` 导入共享 `SAMNodeSet`，并且只保留 self-member
overlay。不要在 Pool 内重复 remote identity、placement 或 maintenance。provider、capture
与 discovery detail 只能属于 self-member overlay。所有 node 应获得相同的 `SAMNodeSet`，以便
deterministic projection。

`SAMNodeSet.spec.nodes[].macAddresses` 可静态列出同一 fabric 中 member 的 MAC
地址。on-prem ARP observer 会把所有 member MAC 的并集作为 ignore set，避免 routerd
member 发出的 ARP frame 被当作 mobile `/32` 的 ownership signal。`macAddresses`
的编辑是声明式 intent：routerd 会导出 observer ignore set，并通过 observer socket
自动收敛，不需要重启 observer 或 routerd。observer status 会显示当前生效的 ignore set
和被忽略的 observation 计数，便于确认收敛状态。

## transport

当前 SAM transport 默认使用 IPIP delivery plane。WireGuard 如存在，只作为加密 underlay；
WireGuard peer 的 `AllowedIPs` 应只包含 transport endpoint prefix，不应包含 mobile `/32`。

`SAMTransportProfile` 会生成 per-peer `TunnelInterface`、endpoint `/32` `IPv4Route`
与 `BGPPeer`。所有 profile 通过 `peersFrom` 使用相同的 `SAMNodeSet`，因此每条 node pair
edge 都能导出相同的 `/31`。`nodeRefs` 可将 peer 限制为实际 adjacency；省略时会选择带
`samEndpoint` 的所有非 self node。直接 peer 或 topology list 不受支持。

## dynamic RR sync fail-static

RR 可以发布 `SAMPeerGroup`，leaf 通过 TCP 19652 获取缺失的 transport peer group。
`SAMPeerGroup` 仅是运行时同步 payload，不能作为 top-level `spec.resources` 声明。
获取成功后，leaf 会把它保存为带 TTL 的 dynamic config part：

- `peer-group-sync/<name>` 对应 `SAMPeerGroup`

TTL 过期或 RR publisher 消失时，leaf 不会删除已经生成的 tunnel 或 BGP peer。routerd
会继续使用 last-known-good 记录，并把来源标记为 `Stale`，同时在 status 中输出
`warning`。MobilityPool membership 直接从静态 `SAMNodeSet` 配置解析，不依赖成员资格
同步端点。

## capture and delivery

`MobilityPool` 始终使用 BGP delivery。owner advertise selected `/32`，non-owner 将
BGP best path import 到 local FIB。

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
`activeWhen` 仅支持此 on-prem `proxy-arp` capture；cloud
`provider-secondary-ip` capture 设置该字段会被拒绝，因此本地 VRRP 状态变化
不会在缺少对应 BGP 和本地数据平面计划时创建 provider assignment。

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

SAM 不包含 `nat`、`preserveSource`、firewall 或 zone 字段。若要 firewall/NAT mobile
address，请在现有 `FirewallZone`、`FirewallRule`、`NAT44Rule` 中引用 literal `/32`。
SAM forwarded traffic 仍会经过普通 forwarding/firewall/conntrack path。

### conntrack cleanup design note

routerd 曾短暂公开 BGP mode SAM failover 的手动 opt-in scoped conntrack cleanup hook。
该功能已经移除。
在参考 SAM leaf 构成中，routerd 不会绘制让 delivered overlay flow 进入 conntrack 的
dataplane rule，因此 leaf 侧 scoped cleanup 是 no-op，也不能解决 failover flow anomaly。

这个问题陈述对未来 stateful SAM leaf 设计仍然成立：如果某个 router 有意追踪 forwarded
mobile `/32` flow，它在成为 holder 时可能需要 scoped recovery hook。重新引入时应检测
routerd-managed ct-engage dataplane，并只在该场景自动启用 cleanup。不要以手动 opt-in flag
的形式重新引入。
