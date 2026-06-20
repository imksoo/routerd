---
title: BGP 付き Kubernetes API VIP
---

# BGP 付き Kubernetes API VIP

![routerd エッジペアが VRRP VIP、Kubernetes API 受信ヘルスチェック、クラスタースピーカーへの BGP ピアリングを管理する構成](/img/diagrams/config-example-kubernetes-api-vip.png)

Kubernetes API のエンドポイントを cluster 内に置かず、routerd の edge pair でブートストラップするための構成です。
ルーターは VRRP VIP を保持し、`k8s-api.cluster.example:6443` を 3 台の control-plane backend へ転送します。
HTTPS の `/readyz` でヘルスチェックし、Kubernetes の BGP speaker と peer を張って Service prefix を受け取ります。

出発点として、次の順で確認します。

```bash
routerctl validate -f examples/kubernetes-api-vip.yaml --replace
routerctl plan -f examples/kubernetes-api-vip.yaml --replace
```

構成の概要:

```text
routerd-01/02  VRRP VIP 192.168.70.10
       |
       +-- k8s-cp-01..03 :6443  HTTPS /readyz
       |
       +-- k8s-wk-01..04  BGP ASN 64513
```

設定の要点:

| リソース | 設定 |
| --- | --- |
| `VirtualAddress/k8s-api-vip` | VRRP の preempt 設定、API のヘルスと BGP のヘルスの追跡。 |
| `IngressService/kubernetes-api` | `/readyz` への HTTPS ヘルスチェック。kubeadm のブートストラップで使う self-signed 証明書向けの `tlsSkipVerify: true`。フェイルオーバーの選択。healthy な backend が無いときの reject。VIP と選択された control-plane backend が同じ LAN prefix または同じプライベート `/24` 上にある場合は、同一インターフェース hairpin SNAT を自動生成する。 |
| `BGPRouter/lan` | `convergenceProfile: fast`、BGP timers `3s/9s/5s`、既定で graceful restart を無効化、Kubernetes の Service prefix だけを受け取る import の allow-list。 |
| `DNSResolver/lan-resolver` | VIP の `hostname` フィールドから `k8s-api.cluster.example` を自動で返し、control plane と worker の静的レコードも提供。 |

DHCP のプールは、VIP、control-plane のアドレス、worker のアドレス、LoadBalancer / Service の advertisement の範囲と重ならないようにしてください。

運用時は `routerctl show bgp`、`routerctl show vrrp`、`routerctl show ingress` を使うと、peer の状態、VIP の役割、backend のヘルスを表形式で確認できます。
`routerctl show ingress --verbose` では、ランタイムの forwarding sysctl、nftables の DNAT/SNAT ルール数、API の ingress に該当する conntrack のフロー数を表示できます。
