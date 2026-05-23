---
title: Ingress maintenance
---

# Ingress 維護

若想在不編輯路由器 YAML 的情況下，暫時將 `IngressService` 的後端移除，
請使用 `routerctl drain`。

```sh
routerctl drain ingress/kubernetes-api backend=cp-01 --duration 10m
routerctl show ingress
```

排液（drain）狀態儲存於 routerd 的狀態資料庫。
排液期間，ingress 控制器會將該後端標記為 `drained: true`、`healthy: false`、`reason: Drained`，
後續的調和（reconcile）將把新流量導向其餘健康的後端。
現有的 conntrack 項目不會被刪除。

`--duration` 到期後自動恢復。若要立即恢復，請執行：

```sh
routerctl undrain ingress/kubernetes-api backend=cp-01
```
