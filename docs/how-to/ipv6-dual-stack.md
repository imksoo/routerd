# IPv6 dual-stack BGP and VIPs

routerd can render FRR IPv4 and IPv6 unicast BGP in the same `BGPRouter`.
Keep `spec.importPolicy.allowedPrefixes`, `spec.exportPolicy.allowedPrefixes`,
and `redistribute.*.allowedPrefixes` as mixed IPv4/IPv6 lists; routerd splits
prefixes by address family when it renders FRR prefix-lists, route-maps, and
`address-family` blocks.

Use IPv6 peer addresses directly in `BGPPeer.spec.peers`:

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: BGPPeer
metadata:
  name: k8s-speakers
spec:
  routerRef: BGPRouter/lan
  peerASN: 64513
  peers:
    - 192.168.70.21
    - fd00:70::21
```

For API or service VIPs, use `VirtualIPv4Address` and `VirtualIPv6Address` as
parallel resources. IPv4 VIPs render keepalived VRRPv2-style host prefixes,
while IPv6 VIPs render keepalived VRRPv3 with `family inet6`. On FreeBSD both
families use CARP aliases on the parent interface.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: VirtualIPv6Address
metadata:
  name: k8s-api-vip-v6
spec:
  interface: lan
  address: fd00:70::10/128
  hostname: k8s-api.cluster.example
  mode: vrrp
  vrrp:
    virtualRouterID: 71
    priority: 150
    peers:
      - fd00:70::3
```

When both VIP resources use the same `hostname`, a matching `DNSResolver` and
served `DNSZone` automatically receive A and AAAA records. The firewall renderer
also opens BGP TCP/179 and both IPv4 and IPv6 VRRP protocol 112 control traffic
for routerd-managed resources.

See `examples/dualstack-bgp.yaml` for the BGP-only shape and
`examples/k8s-api-vip-dualstack.yaml` for a Kubernetes API VIP pattern.
