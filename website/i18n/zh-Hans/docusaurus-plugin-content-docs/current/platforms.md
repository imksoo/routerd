---
title: 支持的平台
---

# 支持的平台

routerd 按跨 OS 设计,但每个平台使用的主机集成方式不同。本页列出 routerd 在各平台会使用的具体 OS 接口,方便你在应用 router 配置前检查生成文件和运行时所有权。

## Linux (Ubuntu / Debian)

Linux 是目前验证最多的部署目标。源码安装默认放在 `/usr/local`。

routerd 在 Linux 上使用下列 OS 接口:

- systemd unit 文件
- `/run/routerd` 与 `/var/lib/routerd`,分别保存运行时数据和持久状态
- dnsmasq,负责 DHCPv4、DHCPv6、DHCP relay 与 Router Advertisement
- nftables,负责过滤和 NAT
- conntrack,负责连接观测
- iproute2,负责 interface 和路由
- pppd / rp-pppoe,负责 PPPoE
- WireGuard、Tailscale、strongSwan、radvd

即使在 Ubuntu 上,routerd 也不假设包已预先安装。请用 `Package` resource 声明依赖包。参考清单如下:

| 分类 | 包 |
| --- | --- |
| Runtime | `dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `tailscale-archive-keyring`, `strongswan-swanctl`, `radvd` |
| Diagnostics | `dnsutils`, `iputils-ping`, `iputils-tracepath`, `tcpdump`, `traceroute`, `net-tools` |
| OS control | `procps`, `systemd`, `kmod` |

`routerd-dhcpv6-client`、`routerd-dhcpv4-client`、`routerd-pppoe-client` 与 `routerd-healthcheck` 在 Linux 上以 systemd service 运行。

## NixOS

NixOS 使用与 Ubuntu 相同的 routerd resource model,但启用流程走 NixOS module。routerd 不写 transient systemd unit,而是生成 `/etc/nixos/routerd-generated.nix`,再由 `nixos-rebuild test` / `nixos-rebuild switch` 启用。

已实现:

- `routerd-dhcpv6-client` 的 systemd unit 生成
- `routerd-dhcpv4-client` 的 systemd unit 生成
- `routerd-pppoe-client` 的 systemd unit 生成
- `Package`、`SysctlProfile`、`NetworkAdoption`、`SystemdUnit` 的 NixOS module 生成
- `routerd apply --dry-run` 触发 `nixos-rebuild test`
- `routerd apply` 触发 `nixos-rebuild switch`
- `nixos-rebuild switch` 失败时尝试 `nixos-rebuild switch --rollback`
- `nixos-rebuild` 前后的 generation 记录
- DHCPv6-PD 到达 `Bound`
- DHCP 或 RA resource 需要 dnsmasq 时生成 `routerd-dnsmasq` service
- DNS resolver、HealthCheck、firewall logger、Tailscale、DHCPv4 client、DHCPv6 client、PPPoE client service 生成
- NAT、firewall、policy routing、Path MTU resource 需要 nftables 时生成 `networking.nftables.enable = true`
- WireGuard、Tailscale、VXLAN,以及 native systemd-networkd VRF 生成
- NixOS native network 声明无法表达的 Linux runtime resource,由 NixOS 启用后的 `routerd.service` 调整

在 NixOS 上,请把 routerd 需要的命令放进 `systemd.services.routerd.path`。当 `Package` resource 写了 `os: nixos` 时,routerd 不会在运行时以命令方式安装包。它会把包写进 `/etc/nixos/routerd-generated.nix` 的 `environment.systemPackages`,再交给 `nixos-rebuild` 启用系统 profile。

| 分类 | 包 |
| --- | --- |
| Runtime | `dnsmasq`, `nftables`, `conntrack-tools`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `strongswan`, `radvd` |
| Diagnostics | `bind`, `iputils`, `tcpdump`, `traceroute`, `nettools` |
| OS control | `procps`, `systemd`, `kmod` |

## FreeBSD

FreeBSD 使用与 Ubuntu 相同的 routerd resource model,但映射到 FreeBSD 主机机制。DHCPv6-PD client 通过 `daemon(8)` 运行,并维持持久 lease。routerd 不使用 Linux 专用机制,而是把 resource 对应到 FreeBSD native 的 `rc.conf`、`rc.d`、`pf`、`mpd5`、`ifconfig` 与 dnsmasq。

已实现:

- DHCPv6-PD daemon 与持久 lease
- WireGuard 与 Linux / NixOS 互通
- VXLAN over WireGuard
- 通过生成的 `mpd5.conf`、`mpd_enable` 与 `mpd5` service restart 提供 PPPoE
- 通过 `pkg` 安装 `Package`
- `render freebsd --out-dir` 生成可审阅的 `install-packages.sh`
- FreeBSD 风格的 `rc.conf.d` 输出,包含 `gateway_enable`、`ipv6_gateway_enable`、`cloned_interfaces`、`ifconfig_*`、`static_routes`、`ipv6_static_routes`、`pf_enable`、`pflog_enable`、`mpd_enable`
- `routerd render freebsd --out-dir` 生成 `dhclient.conf`、`mpd5.conf`、`pf.conf`、dnsmasq 配置与 `rc.d` scripts
- 从 `FirewallZone`、`FirewallPolicy`、`FirewallRule` 生成 pf 规则
- 从 `IPv4SourceNAT` 与 `NAT44Rule` 生成 pf NAT
- 对生成的 `pf.conf` 自动执行 `pfctl -nf` 验证与 `pfctl -f` 应用
- 从 `pfctl -ss -v` 获取相当于 conntrack 的 traffic flow
- 通过 BPF 直接读取 `pflog0` 获取 firewall log,避免依赖各版本 tcpdump 文字格式
- 以管理中的 dnsmasq 提供 DHCPv4、DHCPv6 与 Router Advertisement
- 在 `/var/db/routerd/dnsmasq` 保存 dnsmasq lease
- service restart 前以 `dnsmasq --test` 验证 dnsmasq 配置
- 针对 routerd 拥有的 DHCP、DNS、RA、DHCPv6-PD、DS-Lite、WireGuard、healthcheck traffic 自动开 pf 洞
- DNS resolver daemon 可在 FreeBSD 上构建;`viaInterface` 可指定 `fib:<n>` 以绑定 FIB 的 upstream routing
- cloud VPN `IPsecConnection` 会验证并生成 strongSwan `swanctl` 连接定义;云端 gateway 实通验证依部署环境进行
- 从 `SystemdUnit` 生成、安装 rc.d script,并以 `service <name> onestart` 启用
- `routerd-healthcheck` 的 rc.d script 生成
- `routerd-firewall-logger` 的 rc.d script 生成,并直接读取 `pflog0`
- `TailscaleNode` 的 rc.d script 生成
- PPPoE 共存时,dnsmasq 的 rc.d ordering 会排在 `mpd5` 之后
- Static DS-Lite gif tunnel 生成
- 从 static AFTR IPv6、AFTR FQDN、或 delegated-address local source 动态应用 DS-Lite

FreeBSD 不使用 Linux 专用的 nftables、conntrack、iproute2。`Package` 示例声明 FreeBSD native 替代项:`pf` 与 `pflog0` 来自 base system,PPPoE 使用 `mpd5`,DS-Lite 使用 `ifconfig gif`,LAN DHCP/RA 使用 dnsmasq,WireGuard、Tailscale、strongSwan 使用 ports 包。

| 分类 | 包 |
| --- | --- |
| Runtime | `dnsmasq`, `wireguard-tools`, `tailscale`, `strongswan`, `mpd5` |
| Diagnostics | `bind-tools`, `tcpdump` |
| Base system | `ifconfig`, `sysctl`, `service`, `sysrc`, `netstat`, `sockstat`, `ping`, `traceroute` |

`routerd render freebsd --out-dir <dir>` 会生成:

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `dnsmasq.conf`
- `rc.d-*`

`routerd apply` 会安装生成的 `pf.conf`,用 `pfctl -nf` 验证,再用 `pfctl -f` 应用。dnsmasq 会先以 `dnsmasq --test` 验证再启动。生成的 rc.d scripts 会以 `service <name> onestart` 启动。静态 `rc.conf` 生成不足以描述的动态 DS-Lite tunnel,会用 `ifconfig gif` 应用。正式导入流量前,请先用 `routerd render freebsd` 审阅与离线验证。

## OS 抽象化实现方针

加入新的 OS 固有行为时,不要在 business logic 直接读 `runtime.GOOS`。请使用 `pkg/platform` 层 (`platform.Features`) 或 Go build tags 明确边界。对不在支持范围的 OS,优先在 validation 或 planning 阶段明确报错,不要等到运行时才出现意外失败。
