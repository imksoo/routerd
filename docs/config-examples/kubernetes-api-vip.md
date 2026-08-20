---
title: Kubernetes API VIP with BGP
---

# Kubernetes API VIP with BGP

![Diagram showing a routerd edge pair with VRRP VIP, Kubernetes API ingress health checks, and BGP peering to cluster speakers](/img/diagrams/config-example-kubernetes-api-vip.png)

This example shows a routerd edge pair pattern for bootstrapping Kubernetes
without putting the API endpoint inside the cluster. The router owns a VRRP
VIP, forwards `k8s-api.cluster.example:6443` to three control-plane backends, checks
`/readyz` over HTTPS, and peers with Kubernetes BGP speakers for Service
prefixes.

Use it only after a small lab router works. It is an end-to-end pattern with
multiple failure domains, not a first copy-and-paste configuration. Validate
and dry-run it before a daemon exists. First replace the interface names, VIP
and peer addresses, backend names, and BGP peers with values from your lab.
The supplied `*.cluster.example` backend names are placeholders, so validation
can succeed while the dry-run correctly stops until those names resolve:

```bash
routerd validate --config examples/kubernetes-api-vip.yaml

workdir=$(mktemp -d)
routerd apply --config examples/kubernetes-api-vip.yaml --once --dry-run \
  --state-file "$workdir/state.db" \
  --ledger-file "$workdir/ledger.db" \
  --status-file "$workdir/status.json"
rm -rf "$workdir"
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
| `VirtualAddress/k8s-api-vip` | VRRP preempt settings and track entries for API health and BGP health. |
| `IngressService/kubernetes-api` | HTTPS health check on `/readyz`, `tlsSkipVerify: true` for kubeadm self-signed bootstrap certs, failover selection, reject on no healthy backend, and automatic same-interface hairpin SNAT when selected control-plane backends are on the VIP LAN prefix or the same private `/24`. |
| `BGPRouter/lan` | `convergenceProfile: fast`, BGP timers `3s/9s/5s`, graceful restart disabled by default, and an import allow-list for Kubernetes Service prefixes only. |
| `DNSResolver/lan` | Automatically serves `k8s-api.cluster.example` from the VIP `hostname` field, plus static control-plane and worker records. |

Keep the DHCP pool away from the VIP, control-plane addresses, worker
addresses, and LoadBalancer/Service advertisement ranges.

For operations, `routerctl get BGPRouter`, `routerctl get VirtualAddress`, and
`routerctl get IngressService` provide table views for peer state, VIP role, and
backend health without dumping raw status JSON. Inspect host `ip`, `nft`, and
conntrack state directly when debugging live dataplane behavior.
