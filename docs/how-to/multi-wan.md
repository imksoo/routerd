# Multi-WAN Route Selection

Use `EgressRoutePolicy` when one router has more than one outbound path. The
policy chooses the highest-weight candidate that is ready and healthy.

This is intentionally convergent. During startup, a lower-weight fallback can
serve traffic as soon as it is ready. When the preferred path later passes its
health check, routerd changes the selected device and reapplies route and NAT
resources. The controller does not flush conntrack. Existing flows keep their
kernel state, and new flows follow the newly selected path.

## Route policy

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
    - name: ds-lite-a
      source: DSLiteTunnel/ds-lite-a
      deviceFrom:
        resource: DSLiteTunnel/ds-lite-a
        field: interface
      gatewaySource: none
      weight: 90
      healthCheck: internet-tcp443-dslite-a

    - name: ds-lite-slaac
      source: DSLiteTunnel/ds-lite
      deviceFrom:
        resource: DSLiteTunnel/ds-lite
        field: interface
      gatewaySource: none
      weight: 70
      healthCheck: internet-tcp443-dslite

    - name: hgw-fallback
      source: Interface/wan
      deviceFrom:
        resource: Interface/wan
        field: ifname
      gatewaySource: static
      gateway: 192.168.1.254
      weight: 50
```

Add a health check for each path that must prove internet reachability.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: HealthCheck
metadata:
  name: internet-tcp443-dslite-a
spec:
  daemon: routerd-healthcheck
  target: 1.1.1.1
  protocol: tcp
  port: 443
  sourceInterface: ds-lite-a
  interval: 30s
  timeout: 3s
```

## NAT follows the selected path

The route and NAT resources can follow the selected egress policy.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: NAT44Rule
metadata:
  name: lan-to-egress
spec:
  type: masquerade
  egressPolicyRef: ipv4-default
  sourceRanges:
    - 172.18.0.0/16
```

If an upstream HGW has a static route back to the LAN subnet, you can avoid NAT
for private destinations while keeping NAT for internet traffic.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: NAT44Rule
metadata:
  name: lan-to-wan-hgw
spec:
  type: masquerade
  egressInterface: wan
  sourceRanges:
    - 172.18.0.0/16
  excludeDestinationCIDRs:
    - 192.168.0.0/16
    - 172.16.0.0/12
    - 10.0.0.0/8
```

Pair that with a static route resource for the HGW LAN.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4Route
metadata:
  name: hgw-lan-via-wan
spec:
  destination: 192.168.0.0/16
  device: wan
```

In this model, RFC 1918 destinations stay inside the local network design.
The public internet follows the selected egress policy.

## Operational notes

- Keep SSH on a management interface.
- Do not test router SSH through the untrusted WAN path while applying firewall
  or route changes.
- Prefer health checks bound to the candidate interface.
- Do not clear conntrack when changing the selected path unless there is a
  deliberate incident response reason.
