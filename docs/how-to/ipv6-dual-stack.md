# IPv6 dual-stack BGP and VIPs

![Diagram showing dual-stack BGPPeer address families, IPv4 and IPv6 VirtualAddress resources, DNS A and AAAA records, and BGP or VRRP firewall openings](/img/diagrams/how-to-ipv6-dual-stack.png)

routerd can run GoBGP IPv4 and IPv6 unicast BGP through `routerd-bgp` for the same `BGPRouter`.
Keep `spec.importPolicy.allowedPrefixes`, `spec.exportPolicy.allowedPrefixes`,
and `redistribute.*.allowedPrefixes` as mixed IPv4/IPv6 lists; routerd maps
prefixes to typed GoBGP address families directly.

On Linux, imported IPv4 and IPv6 routes use a next hop from the same address
family and the main kernel routing table. IPv6 requires a usable global next hop.
The existing FIB owner persists the
exact native route tuple and acknowledges kernel readback before reporting it
installed. Withdrawal and restart reuse that receipt. IPv6 link-local next hops
currently lack an output-interface identity in this boundary and report
`GoBGPIPv6LinkLocalNextHopUnsupported`. IPv6 addresses that are tentative or have
failed duplicate address detection (DAD) remain observed as present, but cannot
become preferred route sources.

The independent FRR acceptance uses the production `routerd-bgp` binary and two
separate network namespaces. It checks both address families, exact kernel
readback, actual peer traffic, advertisement withdrawal, peer and daemon restart,
SQLite reopen, and declared-resource removal:

```sh
sh scripts/linux-bgp-frr-smoke.sh
```

This requires Linux user/network/mount namespaces, iproute2, ping, Go, and FRR
`bgpd` plus `vtysh`. Set `GO` to select a Go executable and `ROUTERD_FRR_BGPD` to
override `/usr/lib/frr/bgpd`. The script creates a private `/run` filesystem and
retains binaries, source hashes and logs in a temporary evidence directory.
Independent-peer ECMP, throughput, multi-router HA and FreeBSD interoperability
require separate qualification.

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
