---
title: Replacing a hypervisor-to-hypervisor overlay VPN
---

# Replacing a hypervisor-to-hypervisor overlay VPN

## Scenario

You run a hypervisor cluster (Proxmox VE, KVM, etc.) where the inter-node bridge currently rides on top of a heavyweight overlay VPN — for example, a vendor-supplied SoftEther bridge or another tap-based tunnel. Symptoms include poor throughput between guests on different hosts, MTU mismatches, and operational fragility because the overlay is a separate product from the hypervisor and the router.

You want to replace that overlay with something:

- declaratively configured alongside the rest of the network
- routed (L3) by default, with L2 extension only where strictly necessary
- predictable on MTU
- observable through the same `routerctl` and Web Console you already use

## How routerd solves it

routerd models the overlay as four primitives:

| Resource | Role |
| --- | --- |
| `WireGuardInterface` | the encrypted L3 underlay between hypervisor hosts |
| `WireGuardPeer` | one entry per remote host with its public key, endpoint, and allowed IPs |
| `VXLANTunnel` | an L2 segment riding on top of the WireGuard underlay (only when L2 extension is required) |
| `EgressRoutePolicy` + `HealthCheck` | optional readiness gating and L3 failover on top of the underlay |

Prefer L3 routing over L2 extension whenever possible. L2 extension multiplies MTU constraints (Ethernet header + WireGuard overhead + VXLAN header) and turns broadcast storms into multi-host issues. Use `VXLANTunnel` only for the segments that genuinely need to span hosts.

## Minimal configuration

The example below assumes two hypervisor hosts (`alpha` and `beta`) connected through their existing IP transport. We bring up a WireGuard underlay between them, then add a single VXLAN segment.

### Underlay between two hosts

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: WireGuardInterface
metadata:
  name: wg-cluster
spec:
  listenPort: 51820
  mtu: 1420
  privateKeyFromSecret: wg-cluster-key
---
apiVersion: net.routerd.net/v1alpha1
kind: WireGuardPeer
metadata:
  name: beta
spec:
  interface: wg-cluster
  publicKey: "<beta-public-key>"
  endpoint: "beta.cluster.example.net:51820"
  allowedIPs:
    - 10.250.0.2/32
  persistentKeepalive: 25
```

The `mtu: 1420` value matches WireGuard's default with IPv4 underlay (1500 - 20 IP - 8 UDP - 32 WireGuard overhead - 8 nonce/key = 1432; conservative 1420 leaves headroom).

### Stretched L2 segment over the underlay

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: VXLANTunnel
metadata:
  name: vx-bridge1
spec:
  vni: 123001
  localAddress: 10.250.0.1
  peers:
    - 10.250.0.2
  underlayInterface: wg-cluster
  udpPort: 4789
  mtu: 1370
```

VXLAN adds 50 bytes of header (14 outer Ethernet + 20 outer IPv4 + 8 UDP + 8 VXLAN). On a 1420-byte WireGuard MTU, the inner MTU drops to 1370. Set this explicitly; do not rely on default MTU calculation when stacking encapsulations.

## Verification

```sh
# Underlay
routerctl describe WireGuardInterface/wg-cluster
ip -d link show wg-cluster
ping -M do -s 1392 <peer-underlay-address>   # 1420 - 20 IP - 8 ICMP

# Overlay
routerctl describe VXLANTunnel/vx-bridge1
ip -d link show vx-bridge1
ping -M do -s 1342 <peer-overlay-host>        # 1370 - 20 IP - 8 ICMP
```

`routerctl diagnose egress` is also useful when the underlay is itself a candidate egress (for example, when traffic to a remote office should ride the WireGuard underlay rather than the public default route).

## Operational notes

- **Roll out one host pair first.** Keep hypervisor console access available so you can recover if WireGuard or VXLAN does not converge.
- **MTU mistakes are the most common cause of "fast ping but slow large transfers."** Use `ping -M do -s <size>` to confirm both underlay and overlay MTUs before routing real traffic.
- **Do not blindly disable Linux NIC offload features.** TSO/GSO/GRO are usually fine; problems with `mtu greater than device maximum` more often come from the hypervisor host's tap/veth offload settings, not the guest.
- **Avoid running redundant overlays during the transition.** Place the new WireGuard underlay on a different segment from the old SoftEther tunnel and cut over deliberately.

## See also

- [Path MTU and MSS clamping](../concepts/path-mtu.md)
- [Multi-WAN egress with health-based selection](./multi-wan.md)
