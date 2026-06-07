# IPv6 dual-stack BGP and VIPs

![Diagram showing dual-stack BGPPeer address families, IPv4 and IPv6 VirtualAddress resources, DNS A and AAAA records, and BGP or VRRP firewall openings](/img/diagrams/how-to-ipv6-dual-stack.png)

routerd can run GoBGP IPv4 and IPv6 unicast BGP through `routerd-bgp` for the same `BGPRouter`.
Keep `spec.importPolicy.allowedPrefixes`, `spec.exportPolicy.allowedPrefixes`,
and `redistribute.*.allowedPrefixes` as mixed IPv4/IPv6 lists; routerd maps
prefixes to typed GoBGP address families directly.

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

For API or service VIPs, use `VirtualAddress` with `spec.family: ipv4` and
`spec.family: ipv6` as parallel resources. IPv4 VIPs render keepalived
VRRPv2-style host prefixes, while IPv6 VIPs render keepalived VRRPv3 with
`family inet6`. On FreeBSD both
families use CARP aliases on the parent interface.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: VirtualAddress
metadata:
  name: k8s-api-vip-v6
spec:
  interface: lan
  address: fd00:70::10/128
  family: ipv6
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
