---
title: 以声明式方式进行路由器主机的启动配置
---

# 以声明式方式进行路由器主机的启动配置

![由 derived package、kernel module、sysctl、adoption drop-in 与 minimal installer networking 组成的 declarative host bootstrap](/img/diagrams/how-to-os-bootstrap.png)

routerd 能将初次构建时容易变成手动操作的主机准备工作，整合至 YAML 管理。
这不是安装程序的替代方案，而是将路由器特有的差异以配置文件保留，而非散落在 shell 历史记录中的功能。

## 软件包

routerd 会从配置内的资源自动推导一般 OS 软件包依赖。
`Package` 作为窄范围的覆盖，仅用于补充尚无法自动推导的依赖软件包。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Package
metadata:
  name: router-service-dependencies
spec:
  packages:
    - os: ubuntu
      manager: apt
      names:
        - dnsmasq
        - nftables
        - conntrack
        - kmod
        - wireguard-tools
        - tailscale
    - os: freebsd
      manager: pkg
      names:
        - dnsmasq
        - wireguard-tools
        - mpd5
```

## 内核模块

Linux 的内核模块会从 NAT、防火墙记录、流量记录、WireGuard 等资源自动推导。
`KernelModule` 不是用户直接编写的配置 Kind。

## Sysctl

routerd 会从路由器资源自动推导 forwarding、conntrack accounting、reverse path filter、redirect、TCP、RA 等 sysctl 设置。
通常不需要在配置中编写 `SysctlProfile`。

`SysctlProfile` 仅作为窄范围的逃生出口，用于补充 routerd 尚无法推导的平台特定内核设置。
请只在 `overrides` 中指定差异部分。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: SysctlProfile
metadata:
  name: router-runtime
spec:
  profile: router-linux
  runtime: true
  persistent: true
  overrides:
    net.netfilter.nf_conntrack_udp_timeout: "60"
```

## 接管现有主机配置

systemd-networkd 与 systemd-resolved 的接管用 drop-in 会从 `Interface`、DHCP、DNS、RA 等资源自动推导。
DHCP、DNS、PPPoE、healthcheck、Tailscale 等 routerd 受管 unit 也从各自的资源 Kind 生成，
请勿重复定义。

在 Ubuntu 26.04 LTS 上，依 RA 状态不同，即使安装程序写入的 netplan 配置了 `dhcp6: false`，
systemd-networkd 仍可能在接口上开启 DHCPv6 客户端 socket。
对于 routerd 所管理的 WAN/LAN 链路，请在 OS 启动配置（bootstrap）阶段明确加入 `accept-ra: false`，
并在安装程序的 netplan 层中仅保留 IPv6 link-local。
这样可确保 `routerd-dhcpv6-client` 能使用 UDP port 546，
避免 OS 初始网络配置与 routerd 的 DHCPv6-PD 及 RA/DHCPv6 产生竞争。
管理用 DHCP 请保留在独立的管理接口上。

```yaml
network:
  version: 2
  renderer: networkd
  ethernets:
    wan0:
      dhcp4: false
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
    lan0:
      dhcp4: false
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
    mgmt0:
      dhcp4: true
      dhcp6: false
      accept-ra: false
      link-local: [ipv6]
      optional: true
```

若需要 WAN 链路上来自 RA 的 IPv6 默认路由（例如用于解析 ISP DNS 或 AFTR），
请声明该 WAN 接口与 DHCPv6 / RA 的资源。
routerd 会推导所需的 systemd-networkd drop-in，并避免 systemd-networkd DHCP 客户端产生竞争。

