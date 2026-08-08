---
title: Stretching a PVE LAN to QEMU guests on OCI
---

# Stretching a PVE LAN to QEMU guests on OCI

This pattern carries one deliberately selected Ethernet broadcast domain from
a PVE bridge to QEMU guests on an OCI Compute instance. The OCI VCN remains an
L3 transport. It is never joined to the stretched bridge.

## Topology

```text
PVE vmbr0 -- lan0 [routerd VM] wg0 -- VXLAN/200001 -- wg0 [routerd QEMU guest] lan0
                    |                                      |
                  br-l2                                  br-l2
                                                           |
                                              OCI host br-legacy -- Windows taps

OCI VCN VNIC -- OCI host routed/NAT transit -- routerd guest underlay NIC
```

The OCI routerd appliance is itself a QEMU guest. On VM.Standard.E5.Flex it
must initially be treated as a TCG guest unless `/dev/kvm` is positively
verified. Give it two NICs:

1. an underlay/management NIC on an internal host transit network; and
2. an unnumbered LAN NIC attached to the host-only `br-legacy` bridge.

Attach every legacy Windows tap to `br-legacy`. Never attach the OCI VCN VNIC
to that bridge. If the routerd guest uses a private transit address, forward
only its WireGuard UDP port on the OCI host and keep VXLAN/UDP 4789 inside
WireGuard.

## routerd overlay resource

Use the same VNI and UDP port at both endpoints, exchanging the two WireGuard
addresses. The LAN-facing NIC and VXLAN device must share one bridge.

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: Bridge
metadata:
  name: legacy-l2
spec:
  ifname: br-l2
  members:
    - lan0
  stp: true
  multicastSnooping: false
---
apiVersion: net.routerd.net/v1alpha1
kind: VXLANTunnel
metadata:
  name: legacy-l2
spec:
  ifname: vx-l2
  vni: 200001
  localAddress: 10.254.200.1
  peers:
    - 10.254.200.2
  underlayInterface: wg-l2
  udpPort: 4789
  mtu: 1370
  # Keep the compatibility default unless the trusted underlay has been tested.
  # `unset` permits outer IPv4 fragmentation but does not make inner DF packets
  # ignore the learned VXLAN path MTU.
  outerDF: inherit
  bridge: legacy-l2
  tcpMSSClamp: true
```

`VXLANTunnel.spec.bridge` references the Bridge resource name (`legacy-l2`),
not its kernel `ifname` (`br-l2`). Swap `.1` and `.2` on the OCI endpoint.
`VXLANTunnel` installs an all-zero MAC
FDB entry per peer, so broadcast and unknown-unicast frames are replicated over
the unicast underlay. It does not apply the default `VXLANSegment` control-plane
filter. ARP, DHCPv4, IPv6 RS/RA/NS/NA, and DHCPv6 therefore cross the bridge.

Set `spec.tcpMSSClamp: true` when the stretched L2 path has a smaller effective
MTU than its attached LAN. routerd then owns `table bridge routerd_l2_mss` and
lowers only oversized IPv4 and IPv6 SYN MSS values in both directions. The
ceiling is the minimum of the VXLAN, Bridge, and member-interface MTUs; smaller
MSS values and non-TCP Ethernet control traffic are unchanged. Disabling the
field or removing the tunnel removes only the table carrying routerd's owner
marker.

## Safety gates

- First run `sudo tests/netns/vxlan-l2-control-plane-transparency.sh` on a
  disposable Linux host.
- Next connect both appliances to isolated PVE/QEMU bridges and capture both
  sides. Do not use `vmbr0` yet.
- Reserve one guest IP and MAC with the network administrator before the final
  attachment to `vmbr0`.
- The routerd appliances must not run DHCP, DNS, or RA services on `br-l2`.
- Full transparency is symmetric: a guest could emit DHCP Offers or RAs. Keep
  the Windows guests trusted and offline until a directional bridge-firewall
  policy has separately passed positive and negative tests.
- Verify PVE firewall/MAC filtering and the physical switch allow the remote
  guest MAC. Watch for MAC flaps, duplicate IPs, and broadcast storms.

## Evidence

Record the routerd commit, rendered plan, `ip -d link`, `bridge link`,
`bridge fdb`, WireGuard handshake, VXLAN packet capture, protocol test output,
MTU probes, and rollback timestamps. Rollback starts by disconnecting the PVE
appliance LAN NIC from `vmbr0`; then stop the VXLAN and WireGuard resources.
