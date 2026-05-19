---
title: 架构概览
---

# routerd 架构概览

本文档面向运维者与贡献者，介绍 routerd 的设计意图与内部结构。
日常使用请从 [教程](/docs/tutorials/getting-started) 与 [How-to](/docs/how-to/multi-wan) 开始。
资源定义请参照 [API 参考](/docs/reference/api-v1alpha1)。

---

## 1. routerd 的定位

routerd 是声明式的路由器框架。
目标是用同一组 primitive 构建家用路由器、SOHO 路由器，以及小型数据中心边界路由器。

具体锁定的三个替换对象：

| 目标 | 范围 | 所需阶段 |
| --- | --- | --- |
| 替换家用路由器 | 1 台主机、1-2 条上行、1-3 个 LAN VLAN | H |
| 虚拟化 SDN 路由器 | 集群内的 VXLAN / EVPN / underlay routing | C |
| Kubernetes 边界 | 用 BGP 公告 Pod CIDR / LoadBalancer IP，终结 ingress | S → C |

三者皆以同一组声明式 primitive 表达，可按用途逐步启用功能。

### 1.1 功能阶段（capability tier）

| tier | 用途 | 主要功能 |
| --- | --- | --- |
| **H**（Home） | 家用、小型办公室 | WAN acquire（PD/RA/PPPoE/DHCPv4/DS-Lite）、LAN service（RA/DHCPv6/dnsmasq）、NAT44、firewall、`EgressRoutePolicy` |
| **S**（SOHO/分支） | 多个站点，VPN 为主 | + WireGuard / IPsec、VRF、VPN 上的 dynamic routing、commit-confirmed |
| **C**（Campus / 小型 DC） | 数十节点 | + EVPN-VXLAN、iBGP RR、BFD、RouteMap DSL、FRR 包装 |
| **E**（Enterprise / SP） | 数百节点以上 | + 完整 BGP、MP-BGP L3VPN、segment routing、HA leader election |

primitive 从 H 到 E 共用，阶段提升只是增加包装对象（如 FRR）。

---

## 2. 运行环境

### 2.1 部署形式

routerd 锁定虚拟机环境运行；嵌入式 appliance 为未来工作。

对虚拟化平台的需求：

- virtio NIC（vmxnet、ne2k 等不在范围）
- 不依赖特权 kernel 模块（DPDK / XDP 为可选，不需要 host passthrough）
- 用 console 与 SSH 运维
- 实验时建议善用快照与克隆

### 2.2 OS 策略

routerd 设计为 cross-OS：同一份 binary 与配置可对应多种 OS。

| OS | 强项 | 用途 |
| --- | --- | --- |
| **Linux（Ubuntu / Debian）** | systemd 标准、易获取、kernel 较新 | 开发与生产环境的主流 |
| **NixOS** | 声明式 OS 与 routerd 高度契合，可重现 | 声明式运维的主力 |
| **FreeBSD** | base 稳定、占资源小、jail 隔离 | 长期运行与低资源环境 |
| **Alpine** | 最小体积、musl、apk | 未来的最小配置 |

OS 差异由 `pkg/platform` 层吸收。
nftables ↔ pf、systemd-networkd ↔ rc.conf、systemd unit ↔ rc.d 之类的对应，由各 OS 的渲染器负责。

版本策略：routerd 从 `20260509` 开始使用 `yyyymmdd` 格式的日期型版本号。旧的 `0.x.y` 预发布编号停止使用。

---

## 3. 整体架构图

```
┌─────────────────────────────────────────────────────────────────┐
│ 用户                                                              │
│   /etc/routerd/*.yaml  +  routerctl CLI                          │
└─────────┬─────────────────────────────────────────┬───────────────┘
          │ inotify                          HTTP+JSON
          │ (仅通知)                          (显式 apply)
          ▼                                         ▼
┌─────────────────────────────────────────────────────────────────┐
│ routerd（单一 binary、跨 OS）                                       │
│                                                                   │
│   ConfigWatcher ──notify only──▶ Bus                              │
│   ConfigLoader ◀──explicit trigger───── routerctl apply           │
│                                                                   │
│   ┌──────────────────────────────────────────────────────────┐   │
│   │ Bus（in-process channel + SQLite event 持久层）            │   │
│   │  topics: routerd.<area>.<subject>.<verb>                  │   │
│   │  cursor: events.id (autoincrement)                        │   │
│   │  fanout: subscribe pattern match → controller channel     │   │
│   └─────┬─────────────────────────────────────────────────────┘   │
│         │                                                         │
│         ▼ Controllers（in-process reactor 群）                     │
│   PrefixDelegationCtrl / LANAddressCtrl / RAAnnouncerCtrl         │
│   DNSAnswerCtrl / DNSResolverCtrl / FirewallCtrl / RouteCtrl      │
│   EgressRouteCtrl / ServiceLifecycleCtrl / ConfigLoaderCtrl       │
│   EventRuleEngine / DerivedEventEngine                            │
│         │                                                         │
│         ▼ SQLite state DB（objects/events/artifacts/generations） │
└─────────┬─────────────────────────────────────────────────────────┘
          │ Unix socket HTTP+JSON                fsnotify (lease/snapshot)
          ▼                                            ▲
┌─────────────────────────────────────────────────────────────────┐
│ Layer 1 source daemons（各为一个 process）                         │
│   routerd-dhcpv6-client / routerd-dhcpv4-client                   │
│   routerd-pppoe-client / routerd-dns-resolver                     │
│   routerd-healthcheck@<resource> / routerd-firewall-logger        │
└─────────────────────────────────────────────────────────────────┘
```

---

## 4. 资源模型

routerd 的配置以资源集合表达。形式类似 Kubernetes，但 apiVersion 层级与 controller 结构更简洁。

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
| `net.routerd.net/v1alpha1` | 网络功能（Link、IPv4Static、DSLite、PPPoE、EgressRoute、HealthCheck 等） |
| `dns.routerd.net/v1alpha1` | DNS（DNSZone、DNSResolver、DHCPv4Reservation 等） |
| `firewall.routerd.net/v1alpha1` | Firewall（FirewallZone、FirewallPolicy、FirewallRule、NAT44Rule 等） |
| `system.routerd.net/v1alpha1` | OS bootstrap（Package、SysctlProfile、generated service artifacts、NetworkAdoption、WebConsole 等） |
| `control.routerd.net/v1alpha1` | controller chain 与 routerctl 控制 API |

完整清单见 [API 参考](/docs/reference/api-v1alpha1)。

### 4.2 资源间引用

当某资源要引用另一资源的 status 时，请使用类型化的 `*From` 字段，避免字面值。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WebConsole
  spec:
    listenAddressFrom:
      resource: Interface/mgmt
      field: ipv4Addresses
    port: 8080
```

`addressFrom`、`ipv4From`、`ipv6From`、`upstreamFrom`、`prefixFrom`、`rdnssFrom`、`gatewayFrom` 皆采同样形式。
依赖关系（`dependsOn`）也以同一机制声明。

详见 [资源模型](/docs/concepts/resource-model) 与 [状态与所有权](/docs/concepts/state-and-ownership)。

---

## 5. Event bus 与 controller chain

routerd 在 in-process event bus 与多个 controller 之间配合，将配置收敛为声明的目标状态。

### 5.1 Event bus

- in-process channel + SQLite 事件记录做持久化
- topics 采 `routerd.<area>.<subject>.<verb>` 格式（例如 `routerd.dhcpv6.bind.changed`）
- subscribers 以 pattern match 接收
- 所有事件都有 `events.id` cursor，重启后可重新评估

### 5.2 Controller chain

每个 controller 共用 `framework.FuncController` 结构：

- `Subscriptions`：关注的 topic
- `Bootstrap`：启动时一次性初始化
- `PeriodicFunc`：幂等的定期再评估
- `ReconcileFunc`：事件到达时的状态收敛

`eventedStore` 包装确保状态保存时必发 `routerd.resource.status.changed`，
下游 controller 因此能完成跨资源的依赖解析。

### 5.3 Daemon contract

长时间运行的 OS 进程（DHCPv6 client、DNS resolver、healthcheck 等）以 **daemon** 而非 controller 形式运行。
daemon 与 controller chain 通过 Unix domain socket + JSON 通信，并将自身状态落入 `lease.json` 等文件。

详见 [reconcile loop 行为](/docs/operations/reconcile)。

---

## 6. 配置文件运维

routerd 配置文件（默认 `/usr/local/etc/routerd/router.yaml`）按以下流程套用：

```
编辑 → routerctl validate → routerctl apply（或自动重新加载）
                              │
                              └─ controller chain 更新状态 DB
                                 → daemon 重启 / reload
                                 → OS 状态（nftables / netlink / systemd）反映
```

强烈建议将配置文件纳入 git 管理。
变更请一律通过 routerd 套用，勿在主机直接执行 `nft add rule`、`ip route add`、`sysctl -w` 等临时命令。
临时变更会被下次 reconcile 还原，或更糟地造成 routerd 状态 DB 与 OS 实状态的偏移（drift）。

发现 drift 时，正确做法是于配置文件中表达后再 apply。
这样才能让配置文件 ↔ 状态 DB ↔ OS 实状态三者保持一致。

---

## 7. 可观测性与调试

routerd 通过以下接口提供运行状态：

- `routerctl status`：各资源的 phase 一览
- `routerctl describe <kind>/<name>`：单一资源的 spec、status、近期事件
- `routerctl events --topic <pattern> --resource <kind>/<name>`：tail bus event
- `routerctl plan --diff`：apply 前差异预览
- Web Console（默认 `http://<mgmt-ip>:8080/`）：summary、events、connections、clients、firewall、config 之浏览
- `journalctl -u routerd.service -f | grep "routerd event"`：以 systemd journal 追踪 bus event

记录按用途分为四个 SQLite 文件：`events.db`（controller）、`dns-queries.db`（DNS resolver）、`traffic-flows.db`（conntrack/pf）、`firewall-logs.db`（NFLOG/pflog）。
详见 [日志存储](/docs/concepts/log-storage)。

---

## 8. 相关文档

- [何为 routerd](/docs/concepts/what-is-routerd)
- [资源模型](/docs/concepts/resource-model)
- [设计理念](/docs/concepts/design-philosophy)
- [apply 与 render](/docs/concepts/apply-and-render)
- [状态与所有权](/docs/concepts/state-and-ownership)
- [reconcile loop](/docs/operations/reconcile)
- [状态 DB 运维](/docs/operations/state-database)
- [API 参考 v1alpha1](/docs/reference/api-v1alpha1)
- [Plugin 协议](/docs/reference/plugin-protocol)
- [支持平台](/docs/platforms)
