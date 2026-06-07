# EgressRoutePolicy

![Diagram showing EgressRoutePolicy selecting outbound candidates from health status and publishing advisory or applied routing state](/img/diagrams/concept-egress-route.png)

`EgressRoutePolicy` selects the route used for outbound traffic. It replaces
an earlier WAN route policy Kind. This is a clean break. The old Kind name is
not accepted.

The policy watches candidate resources and health checks. It stores the chosen
candidate in status. Other resources can refer to that status.

`spec.mode` decides which controller owns the status. When `mode` is omitted,
the egress-route selector publishes selection-only status and
`routerd.lan.route.changed` events with `role: advisory` / `advisory: true`.
That status is live controller output, not an apply dry run. When `mode` is set
to `priority`, `mark`, or `hash`, the policy-route controller owns the applied
routing/NAT mark state and dependent controllers follow
`routerd.resource.status.changed` instead of the legacy route-changed event.

`mode: priority` still uses `selection: highest-weight-ready`. The highest
weight ready candidate wins; `priority` is the tie-breaker and the policy-route
rule priority, not a replacement for the selection policy. `weighted-ecmp` is
reserved until implemented and is reported as `UnsupportedSelection` rather
than being silently ignored. `enabled: false` removes the candidate from
selection and from generated policy-route rule/table ownership.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: EgressRoutePolicy
metadata:
  name: ipv4-default
spec:
  family: ipv4
  destinationCIDRs:
    - 0.0.0.0/0
  selection: highest-weight-ready
  candidates:
    - name: ds-lite
      source: DSLiteTunnel/ds-lite
      deviceFrom:
        resource: DSLiteTunnel/ds-lite
        field: interface
      gatewaySource: none
      weight: 80
      healthCheck: internet-tcp443
    - name: ix2215
      source: Interface/ix2215
      deviceFrom:
        resource: Interface/ix2215
        field: ifname
      gatewaySource: static
      gateway: 172.17.0.1
      weight: 50
```

`destinationCIDRs` limits the destinations managed by the policy. If it is
omitted, routerd uses `0.0.0.0/0` for IPv4 and `::/0` for IPv6.

`gatewaySource` describes how the gateway is chosen.

- `none`: point-to-point devices such as DS-Lite do not need a gateway.
- `static`: `gateway` contains the next-hop address.
- `dhcpv4` and `dhcpv6`: reserved for gateway values learned from client
  leases.

The selected values are exposed as:

- `status.selectedCandidate`
- `status.selectedDevice`
- `status.selectedGateway`
- `status.selectedWeight`
- `status.selectedTargets`
- `status.destinationCIDRs`

At startup, the policy chooses the first ready candidate instead of waiting for
the highest-weight path forever. If a higher-weight candidate becomes ready
later, routerd updates `IPv4Route` and `NAT44Rule` through
`routerd.lan.route.changed`. It does not flush conntrack. Existing flows keep
the kernel state they already have, while new flows use the new route and NAT
direction.

`IPv4Route` can use those status fields:

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4Route
metadata:
  name: default-route
spec:
  destination: 0.0.0.0/0
  deviceFrom:
    resource: EgressRoutePolicy/ipv4-default
    field: selectedDevice
  gatewayFrom:
    resource: EgressRoutePolicy/ipv4-default
    field: selectedGateway
```

Private destinations that must not leave through a DS-Lite (or any) tunnel can
be modeled as ordinary routes. Use a WAN-side route for the upstream gateway's
private subnet, a dedicated route for any internal `10.0.0.0/8` or
`172.16.0.0/12` ranges, and `type: blackhole` for ranges that should be
discarded:

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4Route
metadata:
  name: private-10-blackhole
spec:
  type: blackhole
  destination: 10.0.0.0/8
```

## Health Checks

`HealthCheck` can pin probes to a path.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: HealthCheck
metadata:
  name: internet-tcp443
spec:
  target: 1.1.1.1
  protocol: tcp
  port: 443
```

When a `HealthCheck` is referenced by an `EgressRoutePolicy` candidate or
target, routerd derives the health-check daemon, socket mark, and source binding
from that route target automatically. The config describes the probe intent;
platform-specific socket mechanics stay inside the controller and renderer.
