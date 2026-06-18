---
title: 支持的平台
---

# 支持的平台

![Diagram showing supported platforms with Linux systemd primary integration, FreeBSD rc.d and pf groundwork, and pkg/platform feature-gated implementation rules](/img/diagrams/platforms.png)

routerd 以跨 OS 为前提设计。
各平台所使用的主机端机制因 OS 而异。
本页明确列出 routerd 在各平台使用的 OS 功能。
应用前，请先确认生成的文件与运行时的拥有范围。

## Linux (Ubuntu / Debian)

以使用 systemd 的 Linux 为主要目标。
发布安装程序的默认安装位置为 `/usr/local` 之下。
展开 Linux 用的发布归档文件后，执行 `sudo ./install.sh`。
安装程序可通过 `apt-get`、`dnf`、`pacman` 之一安装运行时软件包。

routerd 在 Linux 上使用的 OS 功能如下。

- systemd unit
- `/run/routerd` 与 `/var/lib/routerd`（运行时与持久状态）
- dnsmasq（DHCPv4 / DHCPv6 / DHCP relay / RA）
- nftables（包过滤 + NAT）
- conntrack（连接观测）
- iproute2（接口 + 路由）
- pppd / rp-pppoe（PPPoE）
- WireGuard、Tailscale、strongSwan、radvd

即使在 Ubuntu 上，也不预设软件包已预先安装。
初次准备时，`install.sh` 会安装实用的默认软件包组合。
持续的声明式管理请通过 `Package` 资源声明依赖关系。
参考软件包清单如下。

| 分类 | 软件包 |
| --- | --- |
| Runtime | `dnsmasq-base`, `nftables`, `conntrack`, `iproute2`, `ppp`, `wireguard-tools`, `tailscale`, `tailscale-archive-keyring`, `strongswan-swanctl`, `radvd` |
| Diagnostics | `dnsutils`, `iputils-ping`, `iputils-tracepath`, `tcpdump`, `traceroute`, `net-tools` |
| OS 控制 | `procps`, `systemd`, `kmod` |

`routerd-dhcpv6-client`、`routerd-dhcpv4-client`、`routerd-pppoe-client`、`routerd-healthcheck` 在 Linux 上以 systemd 服务运行。

Ubuntu 26.04 LTS（`resolute`）已针对受管理的 dnsmasq、nftables、DHCPv6-PD、
委派的 LAN IPv6 地址、控制 API，使用与 Ubuntu 24.04 相同的 Linux
data-plane renderer 完成实机验证。但在主机 bootstrap 方面，OS 的
网络配置有需注意之处。对于 routerd 所拥有的 DHCPv6-PD 或 LAN RA/DHCPv6
接口，请避免 OS 侧的 systemd-networkd 开启 DHCPv6 client socket。
否则 systemd-networkd 可能比 `routerd-dhcpv6-client` 更早 bind UDP port 546。

Ubuntu 26.04 的 lab 路由器做法是，OS 的 DHCP 仅保留于管理接口，
routerd 所拥有的 WAN/LAN 接口在 OS 层级只配置 link-local。

```yaml
network:
  version: 2
  renderer: networkd
  ethernets:
    ens18:
      dhcp4: false
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
    ens19:
      dhcp4: false
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
    ens20:
      dhcp4: true
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
```

若 WAN link 需要从 RA 获取 IPv6 默认路由，请声明 WAN 接口及 DHCPv6 / RA 资源。
routerd 会作为 systemd-networkd drop-in 推导出 `IPv6AcceptRA=yes` 与
`[IPv6AcceptRA] DHCPv6Client=no`，因此可在接受 RA 的同时停用 OS 侧的 DHCPv6 client。

## FreeBSD

FreeBSD 同样使用与 Ubuntu 相同的 routerd 资源模型。
应用目标为 FreeBSD 的主机机制。
DHCPv6-PD client 通过 `daemon(8)` 执行，稳定维持租约。
routerd 不使用 Linux 用的机制，而是将资源对应至 FreeBSD 的 `rc.conf`、`rc.d`、`pf`、`mpd5`、`ifconfig`、dnsmasq。
展开 FreeBSD 用的发布归档文件后，执行 `sudo ./install.sh`。
安装程序通过 `pkg` 安装 ports 软件包，并只对 base system 的命令进行确认，不另行安装。

已实现的项目如下。

- DHCPv6-PD 守护进程与租约持久化
- WireGuard 与 Linux 的互通
- VXLAN over WireGuard
- 通过 `mpd5.conf`、`mpd_enable`、`mpd5` 服务重启实现 PPPoE
- `Package` 通过 `pkg` 安装
- `gateway_enable`、`ipv6_gateway_enable`、`cloned_interfaces`、`ifconfig_*`、`static_routes`、`ipv6_static_routes`、`pf_enable`、`pflog_enable`、`mpd_enable` 的 FreeBSD 风格 `rc.conf.d` 输出
- `routerd render freebsd --out-dir` 生成 `dhclient.conf`、`mpd5.conf`、`pf.conf`、dnsmasq 配置、`rc.d` script
- 从 `FirewallZone` / `FirewallPolicy` / `FirewallRule` 生成（render）pf 规则
- 从 `NAT44Rule` 生成 pf NAT
- 对生成的 `pf.conf` 执行 `pfctl -nf` 验证与 `pfctl -f` 应用
- 将 `pfctl -ss -v` 输出转换为流量流（traffic flow）
- 通过 BPF 直接读取 `pflog0` 的防火墙日志，不依赖 tcpdump 文本格式的差异来解析数据包
- DHCPv4、DHCPv6、RA 用的受管理 dnsmasq
- 在 `/var/db/routerd/dnsmasq` 下持久化 dnsmasq 租约
- 服务重启前以 `dnsmasq --test` 确认配置
- 自动生成 DHCP、DNS、RA、DHCPv6-PD、DS-Lite、WireGuard、HealthCheck 所需的 pf 开口
- 从 `generated service artifacts` 生成 rc.d script
- `routerd-healthcheck` 的 rc.d script 生成
- `routerd-firewall-logger` 的 rc.d script 生成，并直接读取 `pflog0`

`ClientPolicy` 目前为 Linux 专用的防火墙功能。
使用 nftables 的 Ethernet 来源地址 set 隔离访客设备。
FreeBSD pf 无法在 routed filter 路径以相同模型处理，因此 routerd 明确将此资源标示为不支持。
- `TailscaleNode` 的 rc.d script 生成
- 静态 DS-Lite gif tunnel 的生成（render）
- 从静态 AFTR IPv6、AFTR FQDN、委派地址衍生的本地来源动态应用 DS-Lite
- 云端 VPN 用 `IPsecConnection` 的验证，以及 strongSwan `swanctl` 连接定义的生成。与云端网关的实际连通性确认依环境单独进行

FreeBSD 不使用 Linux 专用的 nftables / conntrack / iproute2。
`Package` 的示例声明 FreeBSD 侧的替代品。
pf 与 `pflog0` 使用 base system，PPPoE 使用 `mpd5`，DS-Lite 使用 `ifconfig gif`，
LAN 的 DHCP/RA 使用 dnsmasq，WireGuard、Tailscale、strongSwan 使用 ports 软件包。

| 分类 | 软件包 |
| --- | --- |
| Runtime | `dnsmasq`, `wireguard-tools`, `tailscale`, `strongswan`, `mpd5` |
| Diagnostics | `bind-tools`, `tcpdump` |
| Base system | `ifconfig`, `sysctl`, `service`, `sysrc`, `netstat`, `sockstat`, `tcpdump`, `ping`, `traceroute` |

`routerd render freebsd --out-dir <dir>` 输出以下内容。

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `dnsmasq.conf`
- `rc.d-*`

`routerctl apply` 会安装生成的 `pf.conf`，
并在应用前以 `pfctl -nf` 确认语法。
dnsmasq 也会以 `dnsmasq --test` 确认配置后重新启动。
应用后以 `pfctl -f` 反映，并以 `service <name> onestart` 启动生成的 rc.d script。
静态 `rc.conf` 生成不足以描述的 DS-Lite tunnel 以 `ifconfig gif` 动态应用。
正式投入生产前，请先以 `routerd render freebsd` 确认输出。

## Platform parity backlog

Ubuntu、FreeBSD 相互比较时的已知差异。

| 领域 | 当前差异 | 待办事项 |
| --- | --- | --- |
| CI / runtime coverage | CI 在 Ubuntu 上执行 unit test 与 Linux static check。FreeBSD 在发布时进行 cross build。 | 新增 FreeBSD VM 的 smoke 任务，涵盖 validate、plan、实际 package-manager 确认、服务启用、renderer 语法确认。 |
| FreeBSD 的功能例外 | `ClientPolicy` 依赖 nftables 的 Ethernet 来源地址 set，为 Linux 专用。 | 在找到可保留相同隔离语义的设计之前，明确拒绝。 |
| 软件包 bootstrap | Ubuntu、FreeBSD 可命令式安装软件包。 | 对 `apt`、`pkg` 的 schema、validation、安装程序软件包清单、示例、生成文档保持同步。 |

## OS 抽象化的实现方针

新增 OS 固有行为时，请勿在 business logic 层直接读取 `runtime.GOOS`。
使用 `pkg/platform` 层（`platform.Features`）或 Go 的 build tag 明确界定边界。
对不在支持范围的 OS，优先在 validation 或 planning 阶段明确报错，
而非等到执行时才发生预期外的失败。
