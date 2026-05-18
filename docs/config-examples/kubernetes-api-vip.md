---
title: Kubernetes API VIP with BGP
---

# Kubernetes API VIP with BGP

This example shows a routerd edge pair pattern for bootstrapping Kubernetes
without putting the API endpoint inside the cluster. The router owns a VRRP
VIP, forwards `k8s-api.cluster.example:6443` to three control-plane backends, checks
`/readyz` over HTTPS, and peers with Kubernetes BGP speakers for Service
prefixes.

Use it as an end-to-end starting point:

```bash
routerd validate --config examples/kubernetes-api-vip.yaml
routerd plan --config examples/kubernetes-api-vip.yaml
routerd apply --config examples/kubernetes-api-vip.yaml --once --dry-run
```

Topology:

```text
routerd-01/02  VRRP VIP 192.168.70.10
       |
       +-- k8s-cp-01..03 :6443  HTTPS /readyz
       |
       +-- k8s-wk-01..04  BGP ASN 64513
```

The important production-oriented settings are:

| Resource | Setting |
| --- | --- |
| `VirtualIPv4Address/k8s-api-vip` | VRRP `advertInterval: 1s`, `preemptDelay: 30s`, and track entries for API health and BGP health. |
| `IngressService/kubernetes-api` | HTTPS health check on `/readyz`, `tlsSkipVerify: true` for kubeadm self-signed bootstrap certs, failover selection, and reject on no healthy backend. |
| `BGPRouter/lan` | BGP timers `3s/9s/5s`, graceful restart, and an import allow-list for Kubernetes Service prefixes only. |
| `DNSResolver/lan-resolver` | Automatically serves `k8s-api.cluster.example` from the VIP `hostname` field, plus static control-plane and worker records. |

Keep the DHCP pool away from the VIP, control-plane addresses, worker
addresses, and LoadBalancer/Service advertisement ranges.

For operations, `routerctl show bgp`, `routerctl show vrrp`, and
`routerctl show ingress` provide table views for peer state, VIP role, and
backend health without dumping raw status JSON.
