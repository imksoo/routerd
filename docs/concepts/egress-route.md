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

Private destinations that must not leave through DS-Lite can be modeled as
ordinary routes. Use a WAN-side route for NTT HGW networks, an IX2215 route for
internal 172.16.0.0/12 networks, and `type: blackhole` for ranges that should be
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
  daemon: routerd-healthcheck
  target: 1.1.1.1
  protocol: tcp
  port: 443
  sourceInterface: ds-routerd-test
```

On Linux, `routerd-healthcheck` uses `SO_BINDTODEVICE` for
`sourceInterface`. In routerd config, `sourceInterface` names an `Interface`,
`DSLiteTunnel`, or similar network resource. routerd resolves that resource to
the OS interface name before running the probe. Standalone
`routerd-healthcheck` flags still take the OS interface name directly. It also
binds to `sourceAddress` when that field is set. Use `sourceAddressFrom` when
the probe source should follow a managed address resource such as
`IPv4StaticAddress/lan-base.status.address`. `via` records the intended gateway
for the probe path. Route installation still belongs to route policy resources.
