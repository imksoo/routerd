---
title: routerd 是什么
slug: /concepts/what-is-routerd
sidebar_position: 1
---

# routerd 是什么

![routerd 将 YAML resource 转换为 local host networking、daemon、state、status 和 owned cleanup 的流程](/img/diagrams/concept-what-is-routerd.png)

routerd 是用来将 Linux 主机、FreeBSD 作为路由器运行的声明式控制平面。将路由器的配置以 YAML 资源的形式编写后，routerd 会将该意图反映至接口、地址、DHCP、DNS、NAT、路由、隧道、健康检查、软件包、sysctl、服务单元、日志等实际状态。

routerd 既不是发行版，也不是集中管理服务。它在各路由器主机上本地运行，并在必要范围内使用 systemd-networkd、dnsmasq、nftables、pppd、WireGuard、systemd 等主机端组件。

## 解决的问题

手动构建路由器时，状态会分散在许多地方。

- 接口地址分散在 netplan、systemd-networkd、rc.d 中。
- DHCP、DHCPv6、DHCP 中继、RA 分散在 dnsmasq 的配置中。
- DNS 转发和本地记录分散在各解析器的配置中。
- NAT、路由策略、conntrack、防火墙分散在 nftables 和 iproute2 中。
- DHCPv4、DHCPv6-PD、PPPoE、健康检查、日志各自成为独立的守护进程。
- 软件包、sysctl、服务单元容易残留在主机准备脚本中。

routerd 将这些全部统一作为资源来管理。通过 YAML 即可了解路由器的意图，变更可通过 git diff 追踪，实际观测的状态可通过 `routerctl` 和 Web 管理界面确认。

## 当前的架构

`routerd serve` 读取资源、解析依赖关系、启动子守护进程，并在订阅事件的同时，持续调和（reconcile）主机至期望状态。

长期运行的协议状态分由小型的受管理守护进程负责。

- `routerd-dhcpv6-client`：负责 DHCPv6 的前缀委派（PD）和信息请求。
- `routerd-dhcpv4-client`：负责 DHCPv4 的 WAN 租约。
- `routerd-pppoe-client`：负责 PPPoE 会话。
- `routerd-healthcheck`：负责 TCP、DNS、HTTP、ICMP 的连通确认。
- `routerd-dns-resolver`：负责 DNS 区域响应及 DoH、DoT、TCP、UDP 上游。
- `routerd-dhcp-event-relay`：将 dnsmasq 的租约变化转换为 routerd 事件。
- `routerd-firewall-logger`：将防火墙记录导入至 routerd 的记录存储位置。

各守护进程通过 Unix socket 上的本地 HTTP+JSON 公开状态，并将必要状态保存至文件。routerd 读取这些事件，并反映至 LAN 服务、DNS 记录、DS-Lite、NAT、路由策略、健康检查驱动的路由选择，以及观测用的存储位置。

## 可管理的项目

当前的实现可处理以下项目。

- DHCPv6-PD，以及从委派前缀衍生的 IPv6 LAN 地址
- DHCPv6 信息请求、AFTR 的 DNS 解析、DS-Lite
- DHCPv4 的 WAN 租约、DHCPv4 的 LAN 范围、固定分配
- DHCPv6 服务器模式、IPv6 RA 选项
- DNS 区域、DHCP 来源记录、条件式转发、DoH、DoT、TCP DNS、UDP 备援、多重监听、缓存
- NAT44、私有目标的 NAT 排除指定、IPv4 路由策略、reverse path filter、Path MTU 策略、TCP MSS 调整
- PPPoE、WireGuard、VXLAN、VRF、cloud VPN 用的 IPsec 连接定义、strongSwan `swanctl` 配置生成
- 软件包、sysctl 配置文件、网络接管、systemd 单元、NTP 客户端、日志转发、日志保留、Web 管理界面
- `EgressRoutePolicy`、`HealthCheck`、`EventRule`、`DerivedEvent` 的状态联动
- 状态、事件、DNS 查询、连接、连接流量、防火墙记录的确认

## 有意限缩的范围

routerd 当前为 v1alpha1 的预发布版本。为了使路由器更安全、配置更易读，在不保留兼容别名的情况下可能会变更名称或字段。

有状态防火墙过滤器也属于有意限缩的范围。routerd 生成的是 NAT44、区域策略、受管理服务的允许规则、拒绝记录，以及连接确认，而非通用的防火墙规则语言。

FreeBSD 使用相同的资源模型，只有反映目标会依各 OS 采用对应的机制。各平台的差异记载于对应表中。

## 延伸阅读

- [设计理念](./design-philosophy)
- [资源模型](./resource-model)
- [应用与生成](./apply-and-render)
- [状态与拥有权](./state-and-ownership)
- [安装](../tutorials/install)
