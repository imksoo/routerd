---
title: BGP 付き Kubernetes API VIP
---

# BGP 付き Kubernetes API VIP

この例は、Kubernetes API endpoint を cluster 内に置かず、routerd の edge pair
で bootstrap するための構成です。router は VRRP VIP を保持し、
`k8s-api.cluster.example:6443` を 3 台の control-plane backend へ転送し、
HTTPS の `/readyz` を確認し、Kubernetes BGP speaker と peer して Service
prefix を受け取ります。

出発点として、次の順で確認します。

```bash
routerd validate --config examples/kubernetes-api-vip.yaml
routerd plan --config examples/kubernetes-api-vip.yaml
routerd apply --config examples/kubernetes-api-vip.yaml --once --dry-run
```

構成:

```text
routerd-01/02  VRRP VIP 192.168.70.10
       |
       +-- k8s-cp-01..03 :6443  HTTPS /readyz
       |
       +-- k8s-wk-01..04  BGP ASN 64513
```

重要な設定:

| Resource | 設定 |
| --- | --- |
| `VirtualIPv4Address/k8s-api-vip` | VRRP の `advertInterval: 1s`、`preemptDelay: 30s`、API health と BGP health の track。 |
| `IngressService/kubernetes-api` | `/readyz` の HTTPS health check、kubeadm bootstrap の self-signed 証明書向け `tlsSkipVerify: true`、failover selection、healthy backend 不在時の reject、VIP と選択済み control-plane backend が同じ LAN prefix または同じ private `/24` 上にある場合の同一 interface hairpin SNAT 自動生成。 |
| `BGPRouter/lan` | BGP timers `3s/9s/5s`、graceful restart、Kubernetes Service prefix だけを受ける import allow-list。 |
| `DNSResolver/lan-resolver` | VIP の `hostname` field から `k8s-api.cluster.example` を自動で返し、control plane と worker の static record も提供。 |

DHCP pool は VIP、control-plane address、worker address、LoadBalancer /
Service advertisement range と重ならないようにしてください。

運用時は `routerctl show bgp`、`routerctl show vrrp`、
`routerctl show ingress` で、peer state、VIP role、backend health を
raw status JSON ではなく表形式で確認できます。dataplane を確認する場合は
`routerctl show ingress --verbose` を使うと、runtime forwarding sysctl、nftables
DNAT/SNAT rule 数、API ingress に該当する conntrack flow 数を表示できます。
