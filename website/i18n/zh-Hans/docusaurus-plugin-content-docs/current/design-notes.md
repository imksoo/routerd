---
title: 设计备忘录
---

# 设计备忘录

![Diagram showing routerd design notes covering daemon contracts, DHCPv6-PD ownership, honest LAN advertisement, DS-Lite AFTR resolution, event coordination, and reusable building blocks](/img/diagrams/design-notes.png)

本文件记录 routerd 中值得保留的设计决策。
内容仅保留现行代码所遵循的原则，以及未来变更时应恪守的方针，而非过往试错的时序日志。

## 1. 守护进程契约（Daemon contract）

具有状态的处理由专用守护进程负责。
为使工具端能够统一处理，所有守护进程均公开相同的接口：

- Unix domain socket 上的 HTTP+JSON API
- `/v1/status`
- `/v1/healthz`
- `/v1/events`
- `/v1/commands/reload`
- `/v1/commands/renew`
- `/v1/commands/stop`
- 状态或租约文件
- `events.jsonl`（仅追加）

此契约适用于 `routerd-dhcpv6-client`、`routerd-dhcpv4-client`、`routerd-pppoe-client`、`routerd-healthcheck`。

## 2. DHCPv6-PD

DHCPv6-PD 由 `routerd-dhcpv6-client` 负责拥有。不再有为 OS 内置客户端生成配置的路径。

在一般的 residential gateway 环境中，标准的 solicit / advertise / request / renew 加上租约持久化与 T1 renew 即已足够。
依照现行方针，不使用为规避损坏环境而设计的过度重传机制。

## 3. 诚实的 LAN 广播

DHCPv6-PD 若未处于 `Bound` 状态，routerd 不会向 LAN 输出过时的 IPv6 信息。
这适用于 RA、DHCPv6 server、AAAA 记录，以及从前缀衍生的 LAN 地址。
原则是「损坏的状态，就如实呈现损坏」。不会持续散布无法到达的前缀。

## 4. DS-Lite

部分接入网络的 DHCPv6 information-request 不会返回 AFTR 选项。
因此 `DSLiteTunnel` 将 `aftrFQDN` 或 `aftrIPv6` 的静态指定视为正规路径，而非回退选项。

AFTR 的 FQDN 在公众 DNS 上往往无法解析。请使用 AFTR domain 专用的 `DNSForwarder`，并搭配从 DHCPv6 information status 读取运营商内部解析器的 `DNSUpstream.addressFrom` 进行转发。

## 5. 事件整合

routerd 具有进程内事件总线。控制器收到事件后，仅重新评估受影响的资源。

高层次的整合使用以下 Kind：

- `EgressRoutePolicy`
- `EventRule`
- `DerivedEvent`
- `HealthCheck`

`EventRule` 以事件流为输入，生成另一个事件流。
`DerivedEvent` 从观测到的状态合成 asserted / retracted 的虚拟事件。

## 6. CloudEdge SAM capture completion

Provider-secondary-IP capture 的 completion 来自 provider action journal 的事实，
而不是 provider-capture BGP 广播。#707/#740 只建立了 source-capture path 的观测侧脚手架；
生产广告路径从未存在。

仅当 fabric 内部确有 capture `/32` 路由需求时，才重新引入 fabric 广播的 capture `/32`。
届时应从可达性契约重新设计：谁发出路由、哪个 RIB 观测它，以及什么条件算作 complete。

## 7. Tier S 构成要素

WireGuard、Tailscale、IPsec、VRF、VXLAN 是 Tier S（SOHO / 分支机构）的构成要素。
WireGuard 与 VXLAN-over-WireGuard 已确认可在支持的 OS 之间互通。
`TailscaleNode` 负责处理 exit node 与 subnet router 的广播。
这样设计是为了避免将所有 VPN 强塞进同一种抽象形式。

不建立抽象的 `VPNTunnel` 资源。
WireGuard、Tailscale、IPsec，以及未来的 SoftEther 整合，分别以独立的 Kind 新增。
原因是各自的状态机差异甚大，若强行合并为多态的单一 Kind，将丧失语义清晰性。

## 8. 待解事项

- 有状态防火墙的正式生产运用。`FirewallRule` 支持 ICMP type 匹配、
  多个端口、nftables rate limit、每个来源的连接数限制。
  今后将着重改善规则组与上层策略的易用性，
  而非追求基本表达式的全面覆盖。
- LAN 端的 DoH 代理。
- 面向 Tier C 的 OSPF 等动态路由整合。
- 高可用性（leader 选举、容错控制平面）。
- 生产环境的可观测性（OpenTelemetry 收集器与远程日志接收端）。
- 在家用线路上以 routerd 作为唯一 WAN 路由器长期运行的验证。
