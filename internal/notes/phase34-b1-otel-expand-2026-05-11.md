# Phase 3.4 B1: OpenTelemetry expansion validation (2026-05-11)

## Code changes

- `pkg/otel.Setup` now builds a common OpenTelemetry resource for every routerd binary.
- Default resource attributes:
  - `service.name`
  - `service.namespace` from `OTEL_SERVICE_NAMESPACE`
  - `service.version`
  - `host.name`
  - `os.type`
  - `routerd.service.name`
  - `routerd.version`
- `OTEL_RESOURCE_ATTRIBUTES` is parsed and merged into the SDK resource.
- Explicit attributes passed by a daemon still win over environment attributes.
- Version string is centralized in `pkg/version` and used by `cmd/routerd`, `cmd/routerctl`, and OTel resources.

## Metric / trace / log naming surface

Observed metric names already suitable for dashboard panels:

- `routerd.controller.reconcile` with `routerd.controller.name` and `routerd.controller.error`.
- `routerd.conntrack.entries.count` and `routerd.conntrack.entries.max`.
- `routerd.conntrack.entries.created`.
- `routerd.healthcheck.probes` with `routerd.healthcheck.result`, `network.protocol.name`, `server.address`, and `routerd.resource.name`.
- `routerd.healthcheck.phase` with `routerd.healthcheck.phase` and `routerd.resource.name`.
- `routerd.dhcpv6.client.lease.state` with `routerd.dhcpv6.state` and `routerd.resource.name`.

Observed trace spans:

- `controller.reconcile` from `routerd`.
- `healthcheck.probe` from `routerd-healthcheck`.
- `dhcpv6.tick` from `routerd-dhcpv6-client`.

Observed log fields:

- `event.type`.
- `resource`.
- `reason`.

## Collector evidence

Collector: `nwadmin03` `/var/log/otelcol/{logs,metrics,traces}.jsonl`.

Existing OTel traffic count by node name across logs/metrics/traces:

| node | matching lines |
| --- | ---: |
| router02 | 1803 |
| router04 | 918 |
| router05 | 882 |
| homert02 | 1230 |

Router05 was upgraded with the new Linux static binaries and `routerd.service` was restarted.
Status after restart:

```text
Healthy resources=35
```

New resource attribute sample from `metrics.jsonl` after router05 restart:

```text
host.name=router05.lain.local
os.type=linux
routerd.node=router05
routerd.service.name=routerd
routerd.version=v20260511.1240
service.name=routerd
service.namespace=routerd
service.version=v20260511.1240
```

A local SDK probe against the same collector also produced the full default resource attribute set.

## Caveat

Only router05 was upgraded for this B1 validation. router02, router04, and homert02 already send telemetry, but their records will gain `service.version`, `host.name`, and `os.type` after their next binary upgrade or restart with a binary containing this change.
