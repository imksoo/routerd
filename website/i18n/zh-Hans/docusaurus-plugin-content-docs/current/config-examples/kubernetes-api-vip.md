---
title: 带有 BGP 的 Kubernetes API VIP
---

# 带有 BGP 的 Kubernetes API VIP

本示例说明如何不将 Kubernetes API 的 endpoint 放在 cluster 内部，
而是通过 routerd 的 edge pair 进行引导配置（bootstrap）。
路由器持有 VRRP VIP，将 `k8s-api.cluster.example:6443` 转发至 3 台 control-plane backend，
确认 HTTPS 的 `/readyz`，并与 Kubernetes 的 BGP speaker 建立 peer 连接以接收 Service 前缀。

作为出发点，依下列顺序确认：

```bash
routerd validate --config examples/kubernetes-api-vip.yaml
routerd plan --config examples/kubernetes-api-vip.yaml
routerd apply --config examples/kubernetes-api-vip.yaml --once --dry-run
```

构成：

```text
routerd-01/02  VRRP VIP 192.168.70.10
       |
       +-- k8s-cp-01..03 :6443  HTTPS /readyz
       |
       +-- k8s-wk-01..04  BGP ASN 64513
```

重要配置：

| 资源 | 配置 |
| --- | --- |
| `VirtualAddress/k8s-api-vip` | VRRP 的 preempt 设置、API 健康状态与 BGP 健康状态的追踪。 |
| `IngressService/kubernetes-api` | 对 `/readyz` 的 HTTPS 健康检查、用于 kubeadm 引导配置（bootstrap）时自签证书的 `tlsSkipVerify: true`、故障转移的选择、无健康 backend 时拒绝连接、VIP 与所选 control-plane backend 位于相同 LAN 前缀或相同私有 `/24` 时自动生成（render）同接口 hairpin SNAT。 |
| `BGPRouter/lan` | `convergenceProfile: fast`、BGP timers `3s/9s/5s`、默认禁用 graceful restart、仅接受 Kubernetes Service 前缀的导入 allow-list。 |
| `DNSResolver/lan-resolver` | 从 VIP 的 `hostname` 字段自动响应 `k8s-api.cluster.example`，同时提供 control plane 与 worker 的静态记录。 |

DHCP 地址池请确保不与 VIP、control-plane 地址、worker 地址及 LoadBalancer /
Service 广播范围重叠。

运用时可使用 `routerctl show bgp`、`routerctl show vrrp`、
`routerctl show ingress` 以表格形式（而非原始 status JSON）确认 peer 状态、VIP 角色及 backend 健康状况。
若需确认 dataplane，使用 `routerctl show ingress --verbose` 可显示运行期的 forwarding sysctl、
nftables 的 DNAT/SNAT 规则数，以及对应该 API ingress 的 conntrack 流量数。
