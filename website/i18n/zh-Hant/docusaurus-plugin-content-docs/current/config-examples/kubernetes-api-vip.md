---
title: 帶有 BGP 的 Kubernetes API VIP
---

# 帶有 BGP 的 Kubernetes API VIP

本範例說明如何不將 Kubernetes API 的 endpoint 放在 cluster 內部，
而是透過 routerd 的 edge pair 進行啟動設定（bootstrap）。
路由器持有 VRRP VIP，將 `k8s-api.cluster.example:6443` 轉送至 3 台 control-plane backend，
確認 HTTPS 的 `/readyz`，並與 Kubernetes 的 BGP speaker 建立 peer 連線以接收 Service 前綴。

作為出發點，依下列順序確認：

```bash
routerd validate --config examples/kubernetes-api-vip.yaml
routerd plan --config examples/kubernetes-api-vip.yaml
routerd apply --config examples/kubernetes-api-vip.yaml --once --dry-run
```

構成：

```text
routerd-01/02  VRRP VIP 192.168.70.10
       |
       +-- k8s-cp-01..03 :6443  HTTPS /readyz
       |
       +-- k8s-wk-01..04  BGP ASN 64513
```

重要設定：

| 資源 | 設定 |
| --- | --- |
| `VirtualAddress/k8s-api-vip` | VRRP 的 preempt 設定、API 健康狀態與 BGP 健康狀態的追蹤。 |
| `IngressService/kubernetes-api` | 對 `/readyz` 的 HTTPS 健康檢查、用於 kubeadm 啟動設定（bootstrap）時自簽憑證的 `tlsSkipVerify: true`、故障切換的選擇、無健康 backend 時拒絕連線、VIP 與所選 control-plane backend 位於相同 LAN 前綴或相同私有 `/24` 時自動產生（render）同介面 hairpin SNAT。 |
| `BGPRouter/lan` | `convergenceProfile: fast`、BGP timers `3s/9s/5s`、預設停用 graceful restart、僅接受 Kubernetes Service 前綴的匯入 allow-list。 |
| `DNSResolver/lan-resolver` | 從 VIP 的 `hostname` 欄位自動回應 `k8s-api.cluster.example`，同時提供 control plane 與 worker 的靜態記錄。 |

DHCP 位址池請確保不與 VIP、control-plane 位址、worker 位址及 LoadBalancer /
Service 廣播範圍重疊。

運用時可使用 `routerctl show bgp`、`routerctl show vrrp`、
`routerctl show ingress` 以表格形式（而非原始 status JSON）確認 peer 狀態、VIP 角色及 backend 健康狀況。
若需確認 dataplane，使用 `routerctl show ingress --verbose` 可顯示執行期的 forwarding sysctl、
nftables 的 DNAT/SNAT 規則數，以及對應該 API ingress 的 conntrack 流量數。
