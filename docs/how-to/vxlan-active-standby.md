# Fail-closed VXLAN active/standby

This feature gates the forwarding `VXLANTunnel` resource. Keep the bridge and
WireGuard transport warm, but create and attach the VXLAN only while both the
local VRRP election and an independent fencing authority agree that this node
owns forwarding.

```yaml
- apiVersion: routerd.net/v1alpha1
  kind: VXLANTunnel
  metadata:
    name: legacy-overlay
  spec:
    ifname: vx-l2
    vni: 200001
    localAddress: 10.254.200.1
    peers: [10.254.200.2]
    underlayInterface: wg-l2
    bridge: legacy-l2
    mtu: 1280
    when:
      all:
        - state:
            "${VirtualAddress/overlay-election.status.role}": {equals: master, maxAge: 10s}
        - state:
            "${RouterdCluster/site-overlay-ha.status.phase}": {equals: Leader, maxAge: 10s}
```

`false`, missing, stale, or explicitly unknown input disables forwarding. The
controller first brings down the owned VXLAN link, removes every all-zero flood
FDB entry, detaches and deletes the link, then removes only persistent artifacts
carrying routerd's generated-file marker. Status is `Disabled`, never `Healthy`,
while gated off. An unowned link or artifact is not deleted; status becomes
`Blocked/ForeignStateWhileGated` and forwarding is reported as unknown.

Gated VXLANs are intentionally runtime-only. routerd writes a mode-0600
non-activating ownership manifest, never a networkd `.netdev`/`.network`, so a
reboot cannot enable forwarding before the predicates are reevaluated. Role and
witness observations must both have timestamps from the current routerd boot;
persisted pre-boot `master`/`Leader` values are treated as unknown. A restart
also inventories owner manifests and removes an exact-matching orphan created
by resource deletion or `ifname` rename. Legacy routerd-generated auto-start
artifacts are removed; foreign artifacts remain blocked for manual isolation.
When `maxAge` is omitted the VXLAN controller applies a conservative 30-second
default; its periodic reconciliation fences an expired observation even if no
status-change event arrives.

The second predicate is mandatory for production HA. VRRP alone cannot fence a
partition that elects two masters. `RouterdCluster` currently supplies only a
shared-filesystem advisory lease; deployments spanning failure domains need an
external strongly consistent lease/witness provider before using this design.
Do not substitute a second local VRRP observation for fencing.

## Cloud constraints

- Use unicast VRRP between fixed private peer addresses. OCI VCN does not supply
  a shared Ethernet multicast domain for VRRP advertisements.
- A reserved public endpoint belongs to only one WireGuard listener at a time.
  Moving an OCI reserved public IP, private IP, or route is a separate fenced
  provider action; disabling source/destination checks is necessary only for
  VNICs that actually forward routed traffic.
- Do not run DHCP, DNS, RA, or NAT services on either overlay node. They are
  transparent users of the existing LAN services.
- Active/active VXLAN peers on the same bridged LAN are unsupported: duplicated
  BUM frames can duplicate DHCP DORA, flap MAC learning, and form a loop.

## Transition checks

Before enabling a node, confirm the peer is disabled, the witness lease belongs
to this node, WireGuard is current, and no `vx-l2` or all-zero FDB remains on the
standby. During failover capture both site LANs and assert one DHCP Discover,
one Offer path, no duplicate BUM sequence, and no MAC movement between ports.
On witness loss both nodes must converge to `Disabled`.

Rollback is fail closed: force either predicate away from its accepted value
and verify link-down precedes FDB and link deletion. If status is `Blocked`,
isolate the host and resolve ownership manually; routerd deliberately will not
delete foreign kernel or networkd state.
