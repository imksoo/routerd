# EgressRoutePolicy

`EgressRoutePolicy` selects the route used for outbound traffic. It replaces
an earlier WAN route policy Kind. This is a clean break. The old Kind name is
not accepted.

The policy watches candidate resources and health checks. It stores the chosen
candidate in status. Other resources can refer to that status.

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
      device: ${DSLiteTunnel/ds-lite.status.interface}
      gatewaySource: none
      weight: 80
      healthCheck: internet-tcp443
    - name: ix2215
      source: Interface/ix2215
      device: ${Interface/ix2215.status.ifname}
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
- `status.destinationCIDRs`

`IPv4Route` can use those status fields:

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4Route
metadata:
  name: default-route
spec:
  destination: 0.0.0.0/0
  device: ${EgressRoutePolicy/ipv4-default.status.selectedDevice}
  gateway: ${EgressRoutePolicy/ipv4-default.status.selectedGateway}
```

## Health Checks

`HealthCheck` can pin probes to a path.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: HealthCheck
metadata:
  name: internet-tcp443
spec:
  daemon: routerd-healthcheck
  target: 1.1.1.1
  protocol: tcp
  port: 443
  sourceInterface: ds-routerd-test
```

On Linux, `routerd-healthcheck` uses `SO_BINDTODEVICE` for
`sourceInterface`. It also binds to `sourceAddress` when that field is set.
`via` records the intended gateway for the probe path. Route installation still
belongs to route policy resources.
