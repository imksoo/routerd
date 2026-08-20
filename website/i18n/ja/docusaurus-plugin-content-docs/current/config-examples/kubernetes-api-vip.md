---
title: BGP 付き Kubernetes API VIP
---

# BGP 付き Kubernetes API VIP

![routerd エッジペアが VRRP VIP、Kubernetes API 受信ヘルスチェック、クラスタースピーカーへの BGP ピアリングを管理する構成](/img/diagrams/config-example-kubernetes-api-vip.png)

この例は、Kubernetes API のエンドポイントを cluster 内に置かず、routerd の edge pair で
ブートストラップするための構成です。ルーターは VRRP VIP を保持し、
`k8s-api.cluster.example:6443` を 3 台の control-plane backend へ転送し、
HTTPS の `/readyz` を確認し、Kubernetes の BGP speaker と peer を張って Service
prefix を受け取ります。

小さなラボ用ルーターが動作してから使う、複数の障害要因を持つ end-to-end の例です。
最初のコピー＆ペースト用の設定ではありません。daemon を起動する前に validate と隔離した
dry-run を実行します。以下は `sudo` を実行できる通常ユーザーを想定します。

```sh
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config examples/kubernetes-api-vip.yaml
sudo routerd apply --config examples/kubernetes-api-vip.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
sudo sed -n "1,160p" "$LAB_DIR/status.json"
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

| リソース | 設定 |
| --- | --- |
| `VirtualAddress/k8s-api-vip` | VRRP の ID、priority、peer と、API / BGP のヘルスを追跡する `track`。 |
| `IngressService/kubernetes-api` | `/readyz` への HTTPS ヘルスチェック、kubeadm のブートストラップで使う self-signed 証明書向けの `tlsSkipVerify: true`、フェイルオーバーの選択、healthy な backend が無いときの reject、VIP と選択された control-plane backend が同じ LAN prefix または同じプライベート `/24` 上にある場合の、同一インターフェース hairpin SNAT の自動生成。 |
| `BGPRouter/lan` | `convergenceProfile: fast`、BGP timers `3s/9s/5s`、既定で graceful restart を無効化、Kubernetes の Service prefix だけを受け取る import の allow-list。 |
| `DNSResolver/lan` | VIP の `hostname` フィールドから `k8s-api.cluster.example` を自動で返し、control plane の静的レコードも提供。 |

DHCP のプールは、VIP、control-plane のアドレス、worker のアドレス、LoadBalancer /
Service の advertisement の範囲と重ならないようにしてください。

レビュー済みの設定を適用して `routerd.service` を起動した**後で**、
`sudo routerctl get BGPRouter`、`sudo routerctl get VirtualAddress`、
`sudo routerctl get IngressService` を使うと、peer の状態、VIP の役割、backend のヘルスを、
生の status JSON ではなく表形式で確認できます。live dataplane のデバッグでは、
host の `sudo ip`、`sudo nft`、conntrack state を直接確認してください。
