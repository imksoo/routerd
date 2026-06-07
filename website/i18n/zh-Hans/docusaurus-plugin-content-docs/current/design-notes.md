---
title: 设计备忘录
---

# 设计备忘录

![Diagram showing routerd design notes covering daemon contracts, DHCPv6-PD ownership, honest LAN advertisement, DS-Lite AFTR resolution, event coordination, reusable building blocks, and OpenRC rendering](/img/diagrams/design-notes.png)

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

## 6. Tier S 构成要素

WireGuard、Tailscale、IPsec、VRF、VXLAN 是 Tier S（SOHO / 分支机构）的构成要素。
WireGuard 与 VXLAN-over-WireGuard 已确认可在支持的 OS 之间互通。
`TailscaleNode` 负责处理 exit node 与 subnet router 的广播。
这样设计是为了避免将所有 VPN 强塞进同一种抽象形式。

不建立抽象的 `VPNTunnel` 资源。
WireGuard、Tailscale、IPsec，以及未来的 SoftEther 整合，分别以独立的 Kind 新增。
原因是各自的状态机差异甚大，若强行合并为多态的单一 Kind，将丧失语义清晰性。

## 7. OpenRC 服务生成（render）

Alpine 使用 OpenRC 而非 systemd。
OpenRC 支持先以 renderer 而非 applier 的形式开始实现。
`routerd render alpine --out-dir` 会输出可供审阅的 init script 及相关配置，让用户在 routerd 变更 OpenRC 状态之前，能先确认已部署主机的行为。

初期支持的 OpenRC 范围刻意保持精简：

- 从明确的 `generated service artifacts` 资源转换为 OpenRC script
- 自动生成 `routerd-healthcheck` script
- 当 DHCP 或 RA 资源需要 dnsmasq 时，自动生成受管理的 dnsmasq script
- 自动生成 DHCPv4 / DHCPv6 客户端、防火墙日志记录、PPPoE、Tailscale 的 script
- DNS 解析器 script，但在解析器的运行时配置能够在控制器循环之外实体化之前，不启用或启动

这样做是为了避免陷入兼容性死胡同。
API 形式暂时维持 `generated service artifacts`，但只有明确具备 init script 语义的字段才会转换为 OpenRC，具体包括 `ExecStart`、`ExecStartPre`、environment、working directory、user/group、runtime/state/log directory。
systemd sandboxing、networkd、resolved、timesyncd 的语义，不在 OpenRC 上模拟。

应用时的启动以 `HasOpenRC` 进行分支。
仅在内容或模式变更时才写入 script；通过 `rc-update show default` 确认注册状态后，再执行 add / del；通过 `rc-service <name> status` 确认后，再执行 start / restart / stop。
与 systemd 端相同，若期望状态与文件均未变更，不重复执行服务管理器命令。

下一个实现阶段，是将 Alpine 已部署主机的冒烟测试套件纳入一般 VM job。

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
