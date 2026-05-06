# OpenTelemetry real-machine validation, 2026-05-06

Scope: pve05-07 lab only. The collector ran on `nwadmin03`
(`192.168.123.119`) and router05 (`192.168.123.127`) exported telemetry over
OTLP/gRPC.

Collector:

- `otelcol-contrib` v0.151.0
- OTLP/gRPC receiver on `0.0.0.0:4317`
- debug exporter with detailed verbosity for logs, metrics, and traces

Observed signals:

- `routerd` exported `controller.reconcile` spans.
- `routerd` exported the `routerd.controller.reconcile` metric with controller
  attributes such as `routerd.controller.name=firewall`.
- `routerd-healthcheck` exported lifecycle logs, `healthcheck.probe` spans, and
  `routerd.healthcheck.probes` / `routerd.healthcheck.phase` metrics.
- `routerd-dhcpv6-client` exported `dhcpv6.tick` spans and the
  `routerd.dhcpv6.client.lease.state` metric with `routerd.dhcpv6.state=bound`.

Finding:

`routerd-dhcpv6-client@.service` originally restricted the unit to
`AF_UNIX AF_INET6 AF_NETLINK`. That is enough for DHCPv6 packet handling, but it
blocks OTLP export to an IPv4 collector. The systemd and NixOS renderers now
include `AF_INET` for the DHCPv6 client unit.

Declarative follow-up:

A small `Telemetry` resource should own OTLP activation instead of ad-hoc
environment variables. The resource should declare the OTLP endpoint, protocol,
insecure/TLS settings, resource attributes, and target services. The
`SystemdUnit` and NixOS renderers can then generate the environment block for
each managed daemon while keeping OpenTelemetry disabled by default when no
`Telemetry` resource is present.
