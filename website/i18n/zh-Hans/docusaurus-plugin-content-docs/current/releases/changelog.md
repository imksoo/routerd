---
title: 变更记录
---

# 变更记录

routerd 的版本历程。格式遵循 [Keep a Changelog](https://keepachangelog.com/)。
routerd 从 `20260509` 开始使用 `yyyymmdd` 格式的日期型版本号。
本软件仍处于 v1alpha1 阶段，版本之间可能含有破坏性改动。

## 20260509.10

### 新增

- Web Console Overview 会显示 generation、resource phase、HealthCheck 状态的简易趋势图。
- Config 页可比较当前 YAML 文件与最新已应用 generation，便于执行 `routerd apply` 前确认差异。
- Resource 表格支持 kind、name、phase、详细内容搜索、phase 筛选与结果标记。
- VPN 页面新增 Tailscale 与 WireGuard peer 状态的视觉摘要。

## 20260509.9

### 新增

- release archive 现在包含 `share/doc/TARGET`，`install.sh` 会检查 archive 的 OS / CPU 架构是否匹配主机。
- GitHub Actions 会生成 Linux 与 FreeBSD 的 `amd64` / `arm64` archive。
- release CI 会对 `install.sh` 与 `uninstall.sh` 运行 `shellcheck`。

### 变更

- `install.sh --list-deps` 改为结构化输出，列出 OS、CPU 架构、包管理器、包与检查命令。
- 依赖清单加入 PPPoE、RA、IPsec、包捕获、路由与 firewall 工具。

## 20260509.8

### 修复

- 修复 zh-Hant 与 zh-Hans 文档链接，翻译页不再指向尚未翻译的同语言页面。
- 在完整翻译完成前，概览页会链接到英文正准参考页。

## 20260509

### 新增

- `EgressRoutePolicy` 现在可以表达 DS-Lite 主路径、RA 来源 DS-Lite、PPPoE 和 WAN 直连的多级回退。
- 通过声明式 `Telemetry` 资源和 OTLP 环境变量传播，将 OpenTelemetry 配置扩展到路由器群。
- DS-Lite 示例改用 RFC 6333 的 B4-AFTR link prefix `192.0.0.0/29` 作为隧道内侧 IPv4 源地址。
- `PPPoEInterface.disabled` 和禁用的路径候选允许在 YAML 中保留 PPPoE 回退定义，同时避免生产 PPPoE 会话泄漏。

### 变更

- 版本号从 `0.x.y` 改为 `20260509` 这样的日期字符串。
- Linux nftables 与 FreeBSD pf 的 NAT44 生成收敛到按接口生成规则。
- 在 Linux 与 FreeBSD 上验证了 3-role firewall；service hole 绑定到拥有它的接收入接口。
- FreeBSD pf 支持为 `PathMTUPolicy` 生成 TCP MSS clamp；dnsmasq RA 也会发布 MTU option。

### 修复

- FreeBSD pf 不再把 DHCPv6、WireGuard、VXLAN 的 service hole 扩展到 `wan` zone 的所有接口。
- FreeBSD NAT artifact 现在报告为 `pf.anchor/routerd_nat`。
- NAT 生成前会把 PPPoE 资源名解析为实际 OS 接口名。

## 0.4.0

### 新增

- nftables 的隐式拒绝包记录由 `routerd-firewall-logger` 接收并写入 `firewall-logs.db`。Linux 直接读取 `nfnetlink`，FreeBSD 通过 `tcpdump` 读取 `pflog`。
- Web Console 新增「Connections」选项卡（实时 conntrack / pf state）、「Clients」选项卡（DHCP 租约与流量整合）以及「Firewall」选项卡（拒绝排行 + 时间序列）。
- `WebConsole.spec.listenAddressFrom` 与 `DNSResolver` 系列的监听地址，可由 `Interface/<name>.status.ipv4Addresses` 推导。允许用引用代替字面值。
- 默认启用 conntrack 计数（`net.netfilter.nf_conntrack_acct=1`），`SysctlProfile/router-linux` 已纳入；`TrafficFlowLog` 因此能聚合 `bytesOut` / `bytesIn`。

### 变更

- 实时连接视图的 API / CLI 统一命名为 `connections`（旧称 `conntrack-snapshot`）。请使用 `/api/v1/connections`、`routerctl connections`。IPv6 也并入同一张表。
- 扩展了 NixOS 的声明式渲染。`Package`（NixOS 包声明）、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit` 都会输出到 `routerd render nixos`。NixOS 上的 `Package` 不再于运行时安装，由生成的 NixOS 配置接管。
- `SystemdUnit` 可生成 FreeBSD `rc.d` 脚本（`routerd render freebsd --out-dir`）。

### 修复

- 当 `Link/<name>` 状态为空时，`IPv6DelegatedAddress` 不再跳过将 PD 派生地址绑定到主机接口的步骤。
- `SystemdUnit` 不再对未变动的 active unit 进行不必要的重启。

## 0.3.0

### 新增

- 声明式 OS bootstrap 资源 `Package` 与 `SysctlProfile`。覆盖 apt、dnf、nix、pkg 的包声明，以及面向路由器场景的 sysctl 推荐值（`nf_conntrack_max`、socket buffer、TCP/UDP timeout、`ip_forward` 等）。
- `NetworkAdoption` 可由 YAML 关闭 systemd-networkd 的 DHCP / RA。`SystemdUnit` 由 routerd 自身渲染、安装、启用 unit 文件。
- `routerctl events --limit N --topic X --resource K/N -o json` 不再依赖 `sqlite3` 即可查看 bus event。
- `routerd plan --diff` 提供 apply 前的差异预览。
- `DNSResolver` 支持 bootstrap forwarder（内部 DNS 优先，公共 DNS 作为兜底）。

### 变更

- 配置文件中的 `${...status.field}` 字符串引用改为类型化 `*From` 字段（`addressFrom`、`ipv4From`、`ipv6From`、`upstreamFrom`、`prefixFrom`、`rdnssFrom`、`dependsOn`）。没有兼容别名。
- controller chain 重构为纯 event-loop。共用 `framework.FuncController`（Subscriptions + Bootstrap + PeriodicFunc）与 `eventedStore`，状态保存时必发 `routerd.resource.status.changed`，由下游 controller 触发再评估。
- bus event 通过 `slog` 输出到 systemd journal（`journalctl -u routerd.service -f | grep "routerd event"` 即可追踪 controller 行为）。高频事件为 debug 级别。
- 全部 binary 改为静态链接（`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`）。OS 别包清单（`dnsmasq-base`、`nftables`、`conntrack`、`iproute2`、`ppp`、`wireguard-tools`、`strongswan-swanctl`、`radvd`、`tcpdump` 等）按 Ubuntu / NixOS / FreeBSD 整理。
- `HealthCheck.sourceInterface` 在 YAML 上以资源名书写，运行时解析为 OS 接口名。

### 修复

- `SystemdUnit` 之间的 `RuntimeDirectory` 冲突会在重启时删除 socket，已通过 `runtimeDirectoryPreserve` 声明式解决。
- `state: absent` 的 `SystemdUnit` 现可正确判定为 Drifted，并加入 plan 中删除。
- `SysctlProfile` 观测时的类型漂移误判已抑制。

## 0.2.0

### 新增

- 状态化 firewall：`FirewallZone`、`FirewallPolicy`、`FirewallRule` 生成 nftables 的 `inet routerd_filter` table。
- `EgressRoutePolicy`（原名 `WANEgressPolicy`）新增 `destinationCIDRs`、`gateway`、`gatewaySource`。`HealthCheck` 可通过 `via`、`sourceInterface`、`sourceAddress` 指定 probe 路径。
- DNS 子系统重构：`DNSZone`（权威区）与 `DNSResolver`（转发 / 缓存）分离。覆盖本地区、条件转发、DoH / DoT / DoQ、明文 UDP DNS。dnsmasq 仅限 DHCPv4 / DHCPv6 / RA / 中继。
- DS-Lite（`DSLiteTunnel`）、PPPoE（`PPPoESession`、`routerd-pppoe-client`）、DHCPv4 client（`routerd-dhcpv4-client`、`DHCPv4Lease`）。
- NAT44（`NAT44Rule`）与 conntrack 观测。在无 `/proc/net/nf_conntrack` 环境中回退到 sysctl 统计。

### 变更

- `WANEgressPolicy` 改名为 `EgressRoutePolicy`。没有兼容别名。
- DHCP 相关 Kind 与 binary 名称对齐 RFC 表记法（`routerd-dhcpv4-client`、`routerd-dhcpv6-client`）。没有兼容别名。

## 0.1.0

最初的 v1alpha1 实现。

- 引入 DHCPv6-PD client、daemon contract、event bus、controller framework。
- 实现从 DHCPv6-PD 到 LAN 地址推导再到 DNS 响应的 controller chain。
- 新增 DHCPv6 information-request、DS-Lite（试做）、IPv4 路由、RA、DHCPv6 server、`HealthCheck`、`EventRule`、`DerivedEvent`。

之后出货前整理过程中，API 名称与实现策略做了大幅调整。请参考上方 `Unreleased` 与 `examples/` 获取最新使用方式。
