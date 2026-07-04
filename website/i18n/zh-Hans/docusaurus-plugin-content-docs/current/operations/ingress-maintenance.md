---
title: Ingress maintenance
---

# Ingress 维护

![Diagram showing ingress maintenance with routerctl ingress drain writing temporary state, reconcile marking a backend drained and unhealthy, existing conntrack preserved, and undrain or expiry restoring service](/img/diagrams/operations-ingress-maintenance.png)

若想在不编辑路由器 YAML 的情况下，临时将 `IngressService` 的后端移除，
请使用 `routerctl ingress drain`。

```sh
routerctl ingress drain ingress/kubernetes-api backend=cp-01 --duration 10m
routerctl get IngressService
```

排空（drain）状态保存于 routerd 的状态数据库。
排空期间，ingress 控制器会将该后端标记为 `drained: true`、`healthy: false`、`reason: Drained`，
后续的调和（reconcile）将把新流量导向其余健康的后端。
现有的 conntrack 条目不会被删除。

`--duration` 到期后自动恢复。若要立即恢复，请执行：

```sh
routerctl ingress undrain ingress/kubernetes-api backend=cp-01
```
