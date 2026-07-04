---
title: 架构概览
---

# routerd 架构概览

本文档针对运维者与贡献者，概览 routerd 的设计理念与内部结构。
个别功能的使用方式请参阅[教程](./tutorials/getting-started.md)与 [How-to](./how-to/multi-wan.md)，
资源定义请参阅 [API 参考文档](./api-v1alpha1.md)。

![routerd 架构图：router YAML 与 routerctl 经过验证、effective config、controller、SQLite state、renderer，最终形成拥有的 host artifact](/img/diagrams/routerd-architecture.png)

---

## 1. routerd 的定位

routerd 是声明式的路由器框架。
目标是以相同的 primitive，构建家庭路由器、SOHO 路由器，以及小型数据中心的边界路由器。

具体的替代对象有以下三种。

| 目标 | 覆盖范围 | 所需功能阶段 |
| --- | --- | --- |
| 家庭路由器替换 | 1 台主机、1-2 条上行链路、1-3 个 LAN VLAN | H |
| 虚拟化环境的 SDN 路由器 | 集群内的 VXLAN / EVPN / underlay routing | C |
| Kubernetes 集群的边界 | 以 BGP 公告 Pod CIDR / LoadBalancer IP，终结 ingress | S → C |

三者皆以相同的声明式 primitive 表达，可依用途逐步启用功能。

### 1.1 功能阶段（capability tier）

| tier | 用途 | 主要功能 |
| --- | --- | --- |
| **H**（Home） | 家庭、小型办公室 | WAN acquire（PD/RA/PPPoE/DHCPv4/DS-Lite）、LAN service（RA/DHCPv6/dnsmasq）、NAT44、防火墙、`EgressRoutePolicy` |
| **S**（SOHO/分支） | 多站点、以 VPN 为主 | + WireGuard / IPsec、VRF、VPN 上的动态路由、commit-confirmed |
| **C**（Campus / 小型 DC） | 数十节点 | + EVPN-VXLAN、iBGP RR、BFD、RouteMap DSL、更进阶的路由策略 |
| **E**（Enterprise / SP） | 数百节点以上 | + 完整 BGP、MP-BGP L3VPN、segment routing、HA leader election |

primitive 从 H 到 E 共用，功能阶段提升只是增加路由与策略的控制器。

---

## 2. 运行环境

### 2.1 部署形式

routerd 以虚拟机运行为主要设计对象，嵌入式实体设备的支持留待日后。

对虚拟化环境的需求如下。

- virtio NIC（vmxnet、ne2k 等不在支持范围）
- 不依赖特权内核模块（DPDK / XDP 为可选，不需 host passthrough）
- 以 console 与 SSH 进行运维
- 验证时建议善用快照与复制功能

### 2.2 OS 策略

routerd 以跨 OS 为前提设计，同一份 binary 与相同配置可支持多种 OS。

| OS | 强项 | 用途 |
| --- | --- | --- |
| **Linux（Ubuntu / Debian）** | systemd 标准、易获取、内核版本较新 | 开发与生产环境的主流 |
| **FreeBSD** | base 稳定、资源占用小、jail 隔离 | 长期运转与低资源环境 |

OS 之间的差异由 `pkg/platform` 层吸收。
nftables ↔ pf、systemd-networkd ↔ rc.conf、systemd unit ↔ rc.d 脚本等对应，由各 OS 的生成器（renderer）负责。

版本策略方面，routerd 采用 `vYYYYMMDD.HHmm` 格式的日期时刻型版号。旧有的 `0.x.y` 格式与 `yyyymmdd.N` 格式的预发行版号已停止使用。

---

## 3. 整体架构图

```
┌─────────────────────────────────────────────────────────────────┐
│ 用户                                                            │
│   /etc/routerd/*.yaml  +  routerctl CLI                          │
└─────────┬─────────────────────────────────────────┬───────────────┘
          │ inotify                          HTTP+JSON
          │ (仅检测)                         (显式 apply)
          ▼                                         ▼
┌─────────────────────────────────────────────────────────────────┐
│ routerd (1 binary, multi-OS)                                      │
│                                                                   │
│   ConfigWatcher ──notify only──▶ Bus                              │
│   ConfigLoader ◀──explicit trigger───── routerctl apply           │
│                                                                   │
│   ┌──────────────────────────────────────────────────────────┐   │
│   │ Bus (in-process channel + SQLite events 持久层)           │   │
│   │  topics: routerd.<area>.<subject>.<verb>                  │   │
│   │  cursor: events.id (autoincrement)                        │   │
│   │  fanout: subscribe pattern match → controller channel     │   │
│   └─────┬─────────────────────────────────────────────────────┘   │
│         │                                                         │
│         ▼ 控制器（in-process reactor 群）                         │
│   PrefixDelegationCtrl / LANAddressCtrl / RAAnnouncerCtrl         │
│   DNSAnswerCtrl / DNSResolverCtrl / FirewallCtrl / RouteCtrl      │
│   EgressRouteCtrl / ServiceLifecycleCtrl / ConfigLoaderCtrl       │
│   EventRuleEngine / DerivedEventEngine                            │
│         │                                                         │
│         ▼ SQLite state DB (objects/events/artifacts/generations)  │
└─────────┬─────────────────────────────────────────────────────────┘
          │ Unix socket HTTP+JSON                fsnotify (lease/snapshot)
          ▼                                            ▲
┌─────────────────────────────────────────────────────────────────┐
│ Layer 1 source 守护进程（各自为一个 process）                     │
│   routerd-dhcpv6-client / routerd-dhcpv4-client                   │
│   routerd-pppoe-client / routerd-dns-resolver                     │
│   routerd-healthcheck@<resource> / routerd-firewall-logger        │
└─────────────────────────────────────────────────────────────────┘
```

---

## 4. 资源模型

routerd 的配置以资源集合来描述。概念上类似 Kubernetes，但 apiVersion 的层次与控制器结构更为简洁。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: ds-lite-primary
  spec:
    aftrFQDN: gw.transix.jp
```

### 4.1 主要 apiVersion

| apiVersion | 职责 |
| --- | --- |
| `net.routerd.net/v1alpha1` | 网络功能（Interface、IPv4Static、DSLite、PPPoE、EgressRoute、HealthCheck 等） |
| `dns.routerd.net/v1alpha1` | DNS（DNSZone、DNSResolver、DHCPv4Reservation 等） |
| `firewall.routerd.net/v1alpha1` | 防火墙（FirewallZone、FirewallPolicy、FirewallRule、NAT44Rule 等） |
| `system.routerd.net/v1alpha1` | OS 启动配置意图与覆盖（Package、SysctlProfile、WebConsole 等）。主机运行时产物由资源自动推导。 |
| `control.routerd.net/v1alpha1` | 控制器链与 routerctl 的控制 API |

完整清单请参阅 [API 参考文档](./api-v1alpha1.md)。

### 4.2 资源间的引用

当某资源需要引用另一资源的 status 时，请使用类型化的 `*From` 字段，而非直接写入字面值。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WebConsole
  spec:
    listenAddressFrom:
      resource: Interface/mgmt
      field: ipv4Addresses
    port: 8080
```

常见的引用格式包括：`addressFrom`、`ipv4From`、`ipv6From`、`prefixFrom`、`rdnssFrom`、`gatewayFrom` 等。
依赖关系（`dependsOn`）也以相同机制声明。

详情请参阅[资源模型](./concepts/resource-model.md)与[状态与拥有权](./concepts/state-and-ownership.md)。

---

## 5. Event bus 与控制器链

routerd 通过 in-process event bus 与多个控制器的组合，将系统收敛至声明的期望状态。

### 5.1 Event bus

- 以 in-process channel 加上 SQLite 事件日志实现持久化
- topics 采 `routerd.<area>.<subject>.<verb>` 格式（例如：`routerd.dhcpv6.bind.changed`）
- 订阅者以 pattern match 接收事件
- 所有事件均以 `events.id` 作为 cursor，重启后仍可重新评估

### 5.2 控制器链

所有控制器共用 `framework.FuncController` 模式。

- `Subscriptions`：关注的 topic
- `Bootstrap`：启动时执行一次的初始化
- `PeriodicFunc`：定期的幂等再评估
- `ReconcileFunc`：收到事件时的状态收敛

`eventedStore` 包装确保状态保存时必然发出 `routerd.resource.status.changed`。
下游控制器因此能连锁地再评估，完成跨资源的依赖解析。

### 5.3 守护进程契约

长时间运行的 OS 进程（DHCPv6 客户端、DNS 解析器、健康检查等）以**守护进程**形式运行，而非控制器。
守护进程通过 Unix domain socket 上的 JSON 与控制器链通信，并将自身状态持久化至 `lease.json` 等文件。

详情请参阅 [reconcile loop 的行为](./operations/reconcile)。

---

## 6. 配置文件运维

routerd 的配置文件（默认为 `/usr/local/etc/routerd/router.yaml`）以下列流程应用。

```
编辑 → routerctl validate → routerctl apply（或自动重新加载）
                              │
                              └─ 控制器链更新状态 DB
                                 → 守护进程重启 / reload
                                 → OS 状态（nftables / netlink / systemd）反映
```

强烈建议将配置文件纳入 git 管理。
对生产主机的变更请一律通过 routerd 以声明方式进行，勿直接在主机上执行 `nft add rule`、`ip route add`、`sysctl -w` 等临时命令。
临时变更会在下次 reconcile 时被还原，或更糟地在 routerd 状态 DB 与 OS 实际状态之间造成偏移（drift）。

发现偏移时，正确做法是在配置文件中表达后再 apply。
如此才能保持配置文件 ↔ 状态 DB ↔ OS 实际状态三者始终一致。

---

## 7. 可观测性与调试

routerd 提供以下方式观测运转状态。

- `routerctl get status`：所有资源的 phase 一览
- `routerctl describe <kind>/<name>`：单一资源的 spec、status 及近期事件
- `routerctl get events --topic <pattern> --resource <kind>/<name>`：tail bus event
- `routerctl plan --diff`：apply 前的差异预览
- Web 管理界面（默认为 `http://<mgmt-ip>:8080/`）：在浏览器中查看 summary、events、connections、clients、firewall、config
- `journalctl -u routerd.service -f | grep "routerd event"`：以 systemd journal 追踪 bus event

日志依用途分为四个 SQLite 文件持久保存：`events.db`（控制器生成）、`dns-queries.db`（DNS 解析器生成）、`traffic-flows.db`（conntrack/pf 生成）、`firewall-logs.db`（NFLOG/pflog 生成）。
详情请参阅[日志存储](./concepts/log-storage.md)。

---

## 8. 相关文档

- [routerd 是什么](./concepts/what-is-routerd.md)
- [资源模型](./concepts/resource-model.md)
- [设计理念](./concepts/design-philosophy.md)
- [应用与生成](./concepts/apply-and-render.md)
- [状态与拥有权](./concepts/state-and-ownership.md)
- [reconcile loop](./operations/reconcile)
- [状态 DB 运维](./operations/state-database.md)
- [API 参考文档 v1alpha1](./api-v1alpha1.md)
- [插件协议](./plugin-protocol.md)
- [支持平台](./platforms.md)
