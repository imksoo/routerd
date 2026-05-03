# Multi-WAN Route Selection

Use `EgressRoutePolicy` when one router has more than one outbound path. The
policy chooses the highest-weight candidate that is ready.

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
  hysteresis: 30s
  candidates:
    - name: ds-lite
      source: DSLiteTunnel/ds-lite
      device: ${DSLiteTunnel/ds-lite.status.interface}
      gatewaySource: none
      weight: 80
      healthCheck: internet-tcp443
    - name: fallback
      source: Interface/fallback
      device: ${Interface/fallback.status.ifname}
      gatewaySource: static
      gateway: 172.17.0.1
      weight: 50
```

Add a health check for each path that must prove internet reachability.

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
  interval: 30s
  timeout: 3s
```

The route and NAT resources can then follow the selected device.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: NAT44Rule
metadata:
  name: lan-to-wan
spec:
  type: masquerade
  egressPolicyRef: ipv4-default
  sourceRanges:
    - 192.168.0.0/16
```

Keep management traffic on a management interface. Do not test router SSH
through the untrusted WAN path when applying firewall changes.
