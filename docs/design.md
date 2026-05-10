---
title: Architecture overview
---

# routerd architecture overview

This document is an introduction to routerd for operators and contributors. It covers the design intent and the major moving parts.
For day-to-day usage, start with the [tutorials](./tutorials/getting-started.md) and the [how-to guides](./how-to/multi-wan.md).
For resource definitions, see the [API reference](./api-v1alpha1.md).

---

## 1. Where routerd fits

routerd is a declarative router framework. Its goal is to let you build a home router, a SOHO router, or a small data-center edge router from the same set of primitives.

The three deployment targets we design for:

| Target | Scope | Required tier |
| --- | --- | --- |
| Home-router replacement | One host, one or two uplinks, one to three LAN VLANs | H |
| Hypervisor SDN router | VXLAN / EVPN / underlay routing inside a cluster | C |
| Kubernetes cluster edge | Advertise Pod CIDR / LoadBalancer IP via BGP, terminate ingress | S → C |

All three are expressible with the same declarative primitives. The applicable feature set scales with the deployment.

### 1.1 Capability tiers

| Tier | Use case | Headline features |
| --- | --- | --- |
| **H** (Home) | Home or small office | WAN acquire (PD/RA/PPPoE/DHCPv4/DS-Lite), LAN service (RA/DHCPv6/dnsmasq), NAT44, firewall, `EgressRoutePolicy` |
| **S** (SOHO/branch) | Several sites with VPN | + WireGuard / IPsec, VRF, dynamic routing across VPN, commit-confirmed |
| **C** (Campus / small DC) | Tens of nodes | + EVPN-VXLAN, iBGP RR, BFD, RouteMap DSL, FRR wrapper |
| **E** (Enterprise / SP) | Hundreds of nodes | + Full BGP, MP-BGP L3VPN, segment routing, HA leader election |

The primitives are the same from H to E. Higher tiers add wrappers (FRR, etc.) on top of the same model.

---

## 2. Runtime environment

### 2.1 Deployment shape

routerd targets virtual machines. Embedded appliances are out of scope for now.

Requirements for the host environment:

- virtio NICs (vmxnet, ne2k, etc. are out of scope)
- No dependency on privileged kernel modules (DPDK / XDP optional, host passthrough not required)
- Console + SSH for operations
- For lab work, snapshots and clones are encouraged

### 2.2 OS strategy

routerd is designed to be cross-OS. The same binary and the same configuration target multiple operating systems.

| OS | Strengths | Role |
| --- | --- | --- |
| **Linux (Ubuntu / Debian)** | systemd standard, easy to obtain, recent kernels | Primary platform for development and production |
| **NixOS** | Declarative OS aligns with declarative routerd configuration; reproducible | Primary platform for declarative operations |
| **FreeBSD** | Stable base, small footprint, jail isolation | Long-running and low-resource deployments |
| **Alpine** | Minimal footprint, musl, apk | Minimal profile (future) |

OS-specific differences are absorbed in the `pkg/platform` layer.
Mappings such as nftables ↔ pf, systemd-networkd ↔ rc.conf, and systemd unit ↔ rc.d script are owned by per-OS renderers.

Versioning policy: routerd uses date-and-time-based release versions in `vYYYYMMDD.HHmm` format; the previous `0.x.y` and `yyyymmdd.N` pre-release numbering is discontinued.

---

## 3. End-to-end picture

```
┌─────────────────────────────────────────────────────────────────┐
│ User                                                              │
│   /etc/routerd/*.yaml  +  routerctl CLI                          │
└─────────┬─────────────────────────────────────────┬───────────────┘
          │ inotify                          HTTP+JSON
          │ (notify only)                    (explicit apply)
          ▼                                         ▼
┌─────────────────────────────────────────────────────────────────┐
│ routerd (1 binary, multi-OS)                                      │
│                                                                   │
│   ConfigWatcher ──notify only──▶ Bus                              │
│   ConfigLoader ◀──explicit trigger───── routerctl apply           │
│                                                                   │
│   ┌──────────────────────────────────────────────────────────┐   │
│   │ Bus (in-process channel + persistent SQLite event log)    │   │
│   │  topics: routerd.<area>.<subject>.<verb>                  │   │
│   │  cursor: events.id (autoincrement)                        │   │
│   │  fanout: subscribe pattern match → controller channel     │   │
│   └─────┬─────────────────────────────────────────────────────┘   │
│         │                                                         │
│         ▼ Controllers (in-process reactors)                       │
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
│ Layer 1 source daemons (one process each)                         │
│   routerd-dhcpv6-client / routerd-dhcpv4-client                   │
│   routerd-pppoe-client / routerd-dns-resolver                     │
│   routerd-healthcheck@<resource> / routerd-firewall-logger        │
└─────────────────────────────────────────────────────────────────┘
```

---

## 4. Resource model

routerd configuration is a set of resources. The shape is similar to Kubernetes but the apiVersion hierarchy and the controller plumbing are simpler.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: ds-lite-primary
  spec:
    aftrFQDN: gw.transix.jp
```

### 4.1 Major apiVersions

| apiVersion | Responsibility |
| --- | --- |
| `net.routerd.net/v1alpha1` | Networking (Link, IPv4Static, DSLite, PPPoE, EgressRoute, HealthCheck, etc.) |
| `dns.routerd.net/v1alpha1` | DNS (DNSZone, DNSResolver, DHCPv4Reservation, etc.) |
| `firewall.routerd.net/v1alpha1` | Firewall (FirewallZone, FirewallPolicy, FirewallRule, NAT44Rule, etc.) |
| `system.routerd.net/v1alpha1` | OS bootstrap (Package, SysctlProfile, SystemdUnit, NetworkAdoption, WebConsole, etc.) |
| `control.routerd.net/v1alpha1` | controller chain and routerctl control surface |

The full list is in the [API reference](./api-v1alpha1.md).

### 4.2 Cross-resource references

When one resource refers to the status of another, use a typed `*From` field instead of a literal value.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WebConsole
  spec:
    listenAddressFrom:
      resource: Interface/mgmt
      field: ipv4Addresses
    port: 8080
```

`addressFrom`, `ipv4From`, `ipv6From`, `upstreamFrom`, `prefixFrom`, `rdnssFrom`, and `gatewayFrom` follow the same shape. Dependencies (`dependsOn`) use the same mechanism.

For details, see [resource model](./concepts/resource-model.md) and [state and ownership](./concepts/state-and-ownership.md).

---

## 5. Event bus and controller chain

routerd combines an in-process event bus with a set of controllers to converge to the desired state declared in configuration.

### 5.1 Event bus

- in-process channels backed by a SQLite event log for persistence
- topics use the pattern `routerd.<area>.<subject>.<verb>` (for example `routerd.dhcpv6.bind.changed`)
- subscribers receive events via pattern match
- every event has an `events.id` cursor so re-evaluation is possible after a restart

### 5.2 Controller chain

Every controller follows the common `framework.FuncController` shape:

- `Subscriptions`: topics this controller cares about
- `Bootstrap`: one-shot initialisation at startup
- `PeriodicFunc`: idempotent periodic re-evaluation
- `ReconcileFunc`: state convergence on event arrival

The `eventedStore` wrapper guarantees that every persisted state change emits `routerd.resource.status.changed`, which downstream controllers consume to resolve cross-resource dependencies.

### 5.3 Daemon contract

Long-running OS processes (DHCPv6 client, DNS resolver, healthcheck, etc.) live as **daemons** rather than as controllers.
Each daemon talks to the controller chain over a Unix domain socket using JSON, and persists its own state under files such as `lease.json`.

For details, see [reconcile loop behaviour](./operations/reconcile.md).

---

## 6. Operating the configuration file

The routerd configuration file (default `/usr/local/etc/routerd/router.yaml`) is rolled out as follows.

```
edit → routerctl validate → routerctl apply (or auto reload)
                              │
                              └─ controller chain updates the state DB
                                 → daemons restart / reload
                                 → OS state (nftables / netlink / systemd) is updated
```

We strongly recommend keeping the configuration in git.
Apply changes to production via routerd; do not run ad hoc commands such as `nft add rule`, `ip route add`, or `sysctl -w` directly on the host.
Ad hoc changes are either reverted by the next reconcile or, worse, create drift between the routerd state DB and what the kernel actually has.

The right response to drift is to express the new desired state in configuration and apply it again. That keeps the configuration ↔ state DB ↔ OS state triangle aligned.

---

## 7. Observability and debugging

routerd exposes its operating state through several surfaces.

- `routerctl status` — phase per resource
- `routerctl describe <kind>/<name>` — spec, status, and recent events for one resource
- `routerctl events --topic <pattern> --resource <kind>/<name>` — tail the bus
- `routerctl plan --diff` — preview the diff a future apply would produce
- Web Console (default `http://<mgmt-ip>:8080/`) — summary, events, connections, clients, firewall, configuration in a browser
- `journalctl -u routerd.service -f | grep "routerd event"` — bus events through the systemd journal

Logs are persisted across four databases by purpose: `events.db` (controller-driven), `dns-queries.db` (DNS resolver), `traffic-flows.db` (conntrack/pf), and `firewall-logs.db` (NFLOG/pflog).
For details, see [log storage](./concepts/log-storage.md).

OpenTelemetry export is configured by the `Telemetry` resource in
`observability.routerd.net/v1alpha1`. routerd does not bundle an OTLP
collector. When an endpoint is declared, generated systemd, NixOS, and FreeBSD
rc.d units receive the matching `OTEL_*` environment variables and the existing
SDK path sends logs, metrics, and traces to that endpoint.

---

## 8. Related documents

- [What is routerd](./concepts/what-is-routerd.md)
- [Resource model](./concepts/resource-model.md)
- [Design philosophy](./concepts/design-philosophy.md)
- [Apply and render](./concepts/apply-and-render.md)
- [State and ownership](./concepts/state-and-ownership.md)
- [Reconcile loop](./operations/reconcile.md)
- [State database operations](./operations/state-database.md)
- [API reference v1alpha1](./api-v1alpha1.md)
- [Plugin protocol](./plugin-protocol.md)
- [Supported platforms](./platforms.md)
