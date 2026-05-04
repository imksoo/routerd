# Replacing a PVE Node-to-Node VPN Overlay

routerd can model the building blocks needed to replace a slow node-to-node
VPN path such as a SoftEther bridge between Proxmox nodes. The current
recommended shape is:

- `WireGuardInterface` for the encrypted L3 underlay.
- `WireGuardPeer` for each remote node.
- `VXLANTunnel` only when an L2 segment must span nodes.
- `EgressRoutePolicy` and `HealthCheck` for L3 failover and readiness.

Prefer L3 routing over L2 extension when possible. VXLAN is useful for a
specific stretched segment, but it makes MTU mistakes easier. If VXLAN is used
over WireGuard, set the MTU explicitly and test with `ping -M do -s <size>`.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: WireGuardInterface
metadata:
  name: wg-pve
spec:
  listenPort: 51820
  mtu: 1420
---
apiVersion: net.routerd.net/v1alpha1
kind: WireGuardPeer
metadata:
  name: pve06
spec:
  interface: wg-pve
  publicKey: "<peer-public-key>"
  endpoint: "pve06.example.net:51820"
  allowedIPs:
    - 10.250.6.0/24
  persistentKeepalive: 25
---
apiVersion: net.routerd.net/v1alpha1
kind: VXLANTunnel
metadata:
  name: vx-svnet1
spec:
  vni: 123001
  localAddress: 10.250.5.1
  peers:
    - 10.250.6.1
  underlayInterface: wg-pve
  udpPort: 4789
  mtu: 1370
```

For a vpn05/vpn06 replacement, start with a single management segment and one
test node pair. Keep PVE console access available. Verify the WireGuard MTU
before adding VXLAN, then verify that the PVE bridge or SDN vNIC path uses the
expected interface.

Useful checks:

```sh
routerctl diagnose egress --no-host
ip -d link show wg-pve
ip -d link show vx-svnet1
ping -M do -s 1340 <remote-underlay-address>
```

