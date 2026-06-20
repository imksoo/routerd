---
title: OpenTelemetry ダッシュボード
---

# OpenTelemetry ダッシュボード

![Diagram showing OpenTelemetry dashboard inputs from routerd metric families and resource attributes into Grafana, OpenObserve, and PromQL-style panels](/img/diagrams/operations-otel-dashboards.png)

routerd は、`routerd.<domain>.<metric>` という命名規則でメトリクスを出力します。
Grafana、OpenObserve、その他の OTLP メトリクスバックエンドで、次のパネルを出発点として使えます。

| パネル | メトリクス |
|---|---|
| コントローラーの dry-run 回数 | `routerd.controller.dry_run.count` |
| リソースのフェーズ | `routerd.resource.phase.count`（`routerd.resource.phase` でグループ化） |
| アクティブな DHCP リース | `routerd.dhcp.lease.active`（`network.address.family` でグループ化） |
| sticky な DHCP の hold | `routerd.dhcp.sticky.held`（`network.address.family` でグループ化） |
| アクティブなクライアント | `routerd.client.active.count` |
| BGP ピアとプレフィックス | `routerd.bgp.peer.established` / `routerd.bgp.prefix.accepted` |
| VIP と ingress のフェイルオーバー | `routerd.vip.active` / `routerd.ingress.service.active` / `routerd.ingress.backend.healthy` |
| conntrack の使用量 | `routerd.conntrack.count` / `routerd.conntrack.max` |
| ファイアウォールの拒否 | `routerd.firewall.deny.total`（`network.protocol.name` でグループ化） |

リソース属性には、`service.name`、`service.version`、`host.name`、`routerd.host.role`、`routerd.os` が含まれます。

PromQL 形式のクエリーの例:

```promql
routerd_resource_phase_count
routerd_dhcp_lease_active{network_address_family="ipv4"}
routerd_dhcp_sticky_held
rate(routerd_firewall_deny_total[5m])
```
