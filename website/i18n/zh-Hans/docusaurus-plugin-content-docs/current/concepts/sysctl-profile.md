---
title: sysctl 配置文件
slug: /concepts/sysctl-profile
---

# sysctl 配置文件

routerd 会从路由器的资源中自动推导出适用于 Linux 路由器的 sysctl 配置。
在一般的家用路由器配置中，不需要列举大量的 `SysctlProfile` 或 `Sysctl`。
routerd 会从 NAT、DS-Lite、BGP、IPv6 前缀委派（PD）、RA、LAN 服务等资源，
自动推导出 forwarding、redirect、reverse path filter、conntrack、TCP，以及各接口的 RA 配置。

`Sysctl` 和 `SysctlProfile` 仅作为有限的逃生出口，用来补充 routerd 尚无法自动推导的
硬件、内核或发行版特有配置。它们不是表达路由器需求的主要手段，
而是作为实现层面的覆盖选项。

`runtime: true` 会在控制器链 serve 执行期间，立即将配置反映至运行中的内核。
`persistent: true` 会将持久配置写入 `/etc/sysctl.d/`。
`routerd apply --once` 只会将明确指定的 `Sysctl` / `SysctlProfile` 应用至主机。
推导生成的 sysctl 属于 plan / render 的对象，实际应用由 `routerd serve` 负责。

仅在需要使用明确配置文件作为逃生出口时，才通过 `overrides` 覆盖差异。

```yaml
spec:
  profile: router-linux
  overrides:
    net.netfilter.nf_conntrack_max: "524288"
```

routerd 在写入前会先读回确认现有值。
若当前的值已符合预期，则不执行写入。
此情况下也不会发出应用事件。

部分 sysctl 的值会被内核向上取整。
对于这类值，请使用 `compare: atLeast`。
`value` 是写入的值，`expectedValue` 是读回时预期的值。
省略 `expectedValue` 时，以 `value` 作为预期值。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Sysctl
metadata:
  name: socket-buffer
spec:
  key: net.core.rmem_max
  value: "16777216"
  expectedValue: "16777216"
  compare: atLeast
  runtime: true
```

## router-linux 的配置值

| 键 | 值 | 说明 |
| --- | --- | --- |
| `net.ipv4.ip_forward` | `1` | 启用 IPv4 数据包转发。 |
| `net.ipv4.conf.all.forwarding` | `1` | 启用各接口的 IPv4 转发。 |
| `net.ipv4.conf.all.rp_filter` | `0` | 避免 reverse path filter 丢弃策略路由或 DS-Lite 隧道的回程数据包。 |
| `net.ipv4.conf.default.rp_filter` | `0` | 对之后创建的隧道接口也禁用 reverse path filter。 |
| `net.ipv4.conf.all.send_redirects` | `0` | 不从路由器发送 ICMP redirect。 |
| `net.ipv4.conf.default.send_redirects` | `0` | 对之后创建的接口应用相同配置。 |
| `net.ipv4.conf.all.src_valid_mark` | `1` | 让使用 fwmark 的路由选择在 reverse path 判断时能够考虑 mark 值。 |
| `net.ipv6.conf.all.forwarding` | `1` | 启用 IPv6 数据包转发。 |
| `net.ipv6.conf.default.forwarding` | `1` | 对之后创建的接口也启用 IPv6 转发。 |
| `net.netfilter.nf_conntrack_acct` | `1` | 启用 conntrack 的数据包与字节统计，用于 Web 管理界面的客户端流量汇总。在未载入 conntrack 的环境中为可选。 |
| `net.netfilter.nf_conntrack_max` | `262144` | 避免大量设备和应用程序同时连接时 conntrack 满载。在未载入 conntrack 的环境中为可选。 |
| `net.netfilter.nf_conntrack_buckets` | `65536` | 建议设为 `nf_conntrack_max / 4`。因环境而异可能无法写入，故为可选。 |
| `net.netfilter.nf_conntrack_tcp_timeout_established` | `86400` | 默认的 5 天对家用路由器而言过长，缩短为 24 小时。在未载入 conntrack 的环境中为可选。 |
| `net.netfilter.nf_conntrack_udp_timeout` | `30` | 缩短单次 UDP 的保留时间。在未载入 conntrack 的环境中为可选。 |
| `net.netfilter.nf_conntrack_udp_timeout_stream` | `180` | 将持续 UDP 的保留时间设为 3 分钟。在未载入 conntrack 的环境中为可选。 |
| `net.core.rmem_max` | `16777216` | 将接收缓冲区上限设为 16 MiB。 |
| `net.core.wmem_max` | `16777216` | 将发送缓冲区上限设为 16 MiB。 |
| `net.ipv4.tcp_rmem` | `4096 87380 16777216` | 扩大 TCP 接收缓冲区的自动调整范围。 |
| `net.ipv4.tcp_wmem` | `4096 65536 16777216` | 扩大 TCP 发送缓冲区的自动调整范围。 |
| `net.core.netdev_max_backlog` | `5000` | 降低瞬间接收突发流量时发生丢包的概率。 |
| `net.core.somaxconn` | `4096` | 明确指定 listen backlog 的上限。 |
| `net.ipv4.ip_local_port_range` | `1024 65535` | 扩大路由器本身使用的临时端口范围。 |
| `net.ipv4.tcp_fin_timeout` | `30` | 缩短 FIN-WAIT-2 的保留时间。 |
| `net.ipv4.tcp_mtu_probing` | `1` | 让 TCP 在无法收到 Path MTU notification 的路径上也能退回较小的 segment。 |
| `net.ipv4.tcp_tw_reuse` | `1` | 允许重复使用 TIME-WAIT socket。 |
| `net.ipv6.route.max_size` | `16384` | 提高 IPv6 路由缓存的上限。 |

`net.ipv4.route.max_size` 在现行 Linux 的部分环境中效果有限，
routerd 的默认配置文件不予设置。
若有需要，请以单独 `Sysctl` 的形式（而非 `overrides`）添加，并在实机上确认该键是否存在。

`net.netfilter.nf_conntrack_udp_timeout` 的默认值为 `30` 秒，
与 Linux conntrack 对无响应 UDP 的默认值一致。
若需要稍长时间以便与防火墙拒绝或 DPI 观测记录相关联，可覆盖为 `60` 秒。

```yaml
spec:
  profile: router-linux
  overrides:
    net.netfilter.nf_conntrack_udp_timeout: "60"
```

conntrack、NFLOG、WireGuard 等模块的载入，routerd 会从 NAT、防火墙记录、
连接流量记录、WireGuard 等资源自动推导。
`KernelModule` 不是用户编写的配置 Kind。若有推导遗漏，
应视为实现端推导逻辑的错误加以修正。

## 与单独 Sysctl 的使用区别

单独 `Sysctl` 仅用于真正偏离 routerd 推导模型的值。
DS-Lite 隧道的 `rp_filter=0`、WAN/LAN 的 `accept_ra=2`、LAN 的
`send_redirects=0` 等 routerd 能够理解的接口配置，会从资源自动推导，
通常不需要在配置中手动编写。

示例：在验证用内核上临时提高 socket 缓冲区大小

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Sysctl
metadata:
  name: lab-rmem-max
spec:
  key: net.core.rmem_max
  value: "33554432"
  compare: atLeast
  runtime: true
  persistent: true
```
