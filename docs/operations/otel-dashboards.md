---
title: OpenTelemetry dashboards
---

# OpenTelemetry dashboards

routerd exports metrics with the `routerd.<domain>.<metric>` naming pattern.
Use these starter panels in Grafana, OpenObserve, or any OTLP metrics backend:

| Panel | Metric |
|---|---|
| Controller dry-run count | `routerd.controller.dry_run.count` |
| Resource phases | `routerd.resource.phase.count` grouped by `routerd.resource.phase` |
| Active DHCP leases | `routerd.dhcp.lease.active` grouped by `network.address.family` |
| Sticky DHCP holds | `routerd.dhcp.sticky.held` grouped by `network.address.family` |
| Active clients | `routerd.client.active.count` |
| Conntrack usage | `routerd.conntrack.count` / `routerd.conntrack.max` |
| Firewall denies | `routerd.firewall.deny.total` grouped by `network.protocol.name` |

Resource attributes include `service.name`, `service.version`, `host.name`,
`routerd.host.role`, and `routerd.os`.

Example PromQL-style queries:

```promql
routerd_resource_phase_count
routerd_dhcp_lease_active{network_address_family="ipv4"}
routerd_dhcp_sticky_held
rate(routerd_firewall_deny_total[5m])
```
