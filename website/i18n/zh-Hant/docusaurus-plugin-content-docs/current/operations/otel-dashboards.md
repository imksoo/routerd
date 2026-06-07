---
title: OpenTelemetry dashboards
---

# OpenTelemetry 儀表板

![Diagram showing OpenTelemetry dashboard inputs from routerd metric families and resource attributes into Grafana, OpenObserve, and PromQL-style panels](/img/diagrams/operations-otel-dashboards.png)

routerd 以 `routerd.<domain>.<metric>` 命名規則輸出指標。
您可在 Grafana、OpenObserve 或其他 OTLP 指標後端使用下列面板作為起點。

| 面板 | 指標 |
|---|---|
| 控制器 dry-run 次數 | `routerd.controller.dry_run.count` |
| 資源 phase | `routerd.resource.phase.count`（依 `routerd.resource.phase` 分組） |
| 作用中的 DHCP 租約 | `routerd.dhcp.lease.active`（依 `network.address.family` 分組） |
| sticky DHCP hold | `routerd.dhcp.sticky.held`（依 `network.address.family` 分組） |
| 作用中的用戶端 | `routerd.client.active.count` |
| BGP peer 與前綴 | `routerd.bgp.peer.established` / `routerd.bgp.prefix.accepted` |
| VIP 與 ingress 故障切換 | `routerd.vip.active` / `routerd.ingress.service.active` / `routerd.ingress.backend.healthy` |
| conntrack 使用量 | `routerd.conntrack.count` / `routerd.conntrack.max` |
| 防火牆拒絕 | `routerd.firewall.deny.total`（依 `network.protocol.name` 分組） |

資源屬性包含 `service.name`、`service.version`、`host.name`、`routerd.host.role`、`routerd.os`。

PromQL 格式的查詢範例如下：

```promql
routerd_resource_phase_count
routerd_dhcp_lease_active{network_address_family="ipv4"}
routerd_dhcp_sticky_held
rate(routerd_firewall_deny_total[5m])
```
