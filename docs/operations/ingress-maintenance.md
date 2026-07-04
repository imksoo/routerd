---
title: Ingress maintenance
---

# Ingress maintenance

![Diagram showing ingress maintenance with routerctl ingress drain writing temporary state, reconcile marking a backend drained and unhealthy, existing conntrack preserved, and undrain or expiry restoring service](/img/diagrams/operations-ingress-maintenance.png)

Use `routerctl ingress drain` when an `IngressService` backend needs temporary
maintenance without editing the router YAML:

```sh
routerctl ingress drain ingress/kubernetes-api backend=cp-01 --duration 10m
routerctl get IngressService
```

The drain state is stored in the routerd state database. During the drain
window, the ingress controller marks that backend as `drained: true`,
`healthy: false`, and `reason: Drained`; new flows are sent to the remaining
healthy backends on the next reconcile. Existing conntrack entries are not
flushed.

The backend is restored automatically when `--duration` expires. To restore it
immediately:

```sh
routerctl ingress undrain ingress/kubernetes-api backend=cp-01
```
