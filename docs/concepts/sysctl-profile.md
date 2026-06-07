---
title: Sysctl profile
slug: /concepts/sysctl-profile
---

# Sysctl profile

![Diagram showing derived router sysctls, explicit Sysctl and SysctlProfile overrides, platform gates, and runtime or persistent writes](/img/diagrams/concept-sysctl-profile.png)

routerd derives the Linux router sysctl set from router resources.
Normal home-router configs should not list `SysctlProfile` or many individual
`Sysctl` entries. NAT, DS-Lite, BGP, IPv6 prefix delegation, RA, and LAN service
resources imply the forwarding, redirect, reverse-path-filter, conntrack, TCP,
and per-interface RA settings they need.

`Sysctl` and `SysctlProfile` remain as narrow escape hatches for hardware,
kernel, or distribution-specific settings that routerd cannot yet derive. Treat
such entries as implementation overrides, not as the main way to describe router
intent.

`runtime: true` writes immediately to the running kernel when the controller
chain is serving. `persistent: true` writes a persistent file under
`/etc/sysctl.d/`. `routerd apply --once` mutates only explicit `Sysctl` and
`SysctlProfile` resources; derived sysctls are rendered and planned there, then
applied by `routerd serve`.

Use `overrides` only when you intentionally keep an explicit profile escape
hatch.

```yaml
spec:
  profile: router-linux
  overrides:
    net.netfilter.nf_conntrack_max: "524288"
```

routerd reads the current value before writing.
If the current value already satisfies the expectation, routerd does not write — and does not emit an apply event for that key.

Some sysctl values are rounded up by the kernel.
For those, use `compare: atLeast`.
`value` is what routerd writes; `expectedValue` is what routerd accepts on read-back.
If `expectedValue` is omitted, `value` is used.

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

## Values in the `router-linux` profile

| Key | Value | Reason |
| --- | --- | --- |
| `net.ipv4.ip_forward` | `1` | Enable IPv4 forwarding. |
| `net.ipv4.conf.all.forwarding` | `1` | Enable per-interface IPv4 forwarding. |
| `net.ipv4.conf.all.rp_filter` | `0` | Prevent reverse-path filtering from dropping return traffic for policy routing or DS-Lite tunnels. |
| `net.ipv4.conf.default.rp_filter` | `0` | Same, applied to interfaces created later (e.g. tunnels). |
| `net.ipv4.conf.all.send_redirects` | `0` | Do not emit ICMP redirects from the router. |
| `net.ipv4.conf.default.send_redirects` | `0` | Same, applied to interfaces created later. |
| `net.ipv4.conf.all.src_valid_mark` | `1` | Allow `fwmark`-based routing decisions to participate in reverse-path validation. |
| `net.ipv6.conf.all.forwarding` | `1` | Enable IPv6 forwarding. |
| `net.ipv6.conf.default.forwarding` | `1` | Same, applied to interfaces created later. |
| `net.netfilter.nf_conntrack_acct` | `1` | Enable per-flow packet/byte accounting; used by the Web Console client traffic view. Optional when conntrack is not loaded. |
| `net.netfilter.nf_conntrack_max` | `262144` | Avoid table exhaustion under many devices and apps. Optional when conntrack is not loaded. |
| `net.netfilter.nf_conntrack_buckets` | `65536` | Roughly `nf_conntrack_max / 4`. Optional — not all environments allow writing this. |
| `net.netfilter.nf_conntrack_tcp_timeout_established` | `86400` | Reduce the default 5-day timeout to 24 hours (more appropriate for home routers). Optional when conntrack is not loaded. |
| `net.netfilter.nf_conntrack_udp_timeout` | `30` | Shorten one-shot UDP retention. Optional when conntrack is not loaded. |
| `net.netfilter.nf_conntrack_udp_timeout_stream` | `180` | Set sustained UDP retention to 3 minutes. Optional when conntrack is not loaded. |
| `net.core.rmem_max` | `16777216` | Cap socket receive buffers at 16 MiB. |
| `net.core.wmem_max` | `16777216` | Cap socket send buffers at 16 MiB. |
| `net.ipv4.tcp_rmem` | `4096 87380 16777216` | Widen the TCP receive autotuning range. |
| `net.ipv4.tcp_wmem` | `4096 65536 16777216` | Widen the TCP send autotuning range. |
| `net.core.netdev_max_backlog` | `5000` | Reduce drops on short receive bursts. |
| `net.core.somaxconn` | `4096` | Set an explicit listen backlog ceiling. |
| `net.ipv4.ip_local_port_range` | `1024 65535` | Widen the ephemeral port range used by the router itself. |
| `net.ipv4.tcp_fin_timeout` | `30` | Shorten FIN-WAIT-2 retention. |
| `net.ipv4.tcp_mtu_probing` | `1` | Let TCP fall back to smaller segments when path-MTU notifications are blackholed. |
| `net.ipv4.tcp_tw_reuse` | `1` | Allow reuse of TIME-WAIT sockets. |
| `net.ipv6.route.max_size` | `16384` | Raise the IPv6 route cache ceiling. |

`net.ipv4.route.max_size` is no longer effective on some recent Linux kernels and is *not* set by the default profile.
If you need it, add it as a discrete `Sysctl` (not via `overrides`) and verify on the target host first.

`30` seconds is the Linux conntrack default for unreplied UDP flows and is also
routerd's profile default. On busy home routers, `60` seconds can be a better
operational override when short UDP flows are being correlated with firewall
denies or DPI observations:

```yaml
spec:
  profile: router-linux
  overrides:
    net.netfilter.nf_conntrack_udp_timeout: "60"
```

## Kernel Modules

`SysctlProfile` expects the relevant kernel subsystems to exist. routerd derives
kernel module loading from resources such as NAT, firewall logging, traffic flow
logging, and WireGuard. `KernelModule` is no longer a user-facing config kind;
if a module is missing from the derived set, fix the derivation instead of
adding implementation-specific YAML.

## When to use a discrete `Sysctl`

Use a discrete `Sysctl` only for a setting that is truly outside routerd's
derived model. Per-interface router needs such as DS-Lite tunnel
`rp_filter=0`, WAN/LAN `accept_ra=2`, and LAN `send_redirects=0` are derived
from resources and should not appear in normal configs.

Example escape hatch for a lab kernel that needs a temporary socket-buffer
override:

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
