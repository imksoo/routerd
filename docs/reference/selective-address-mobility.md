---
title: Selective Address Mobility
---

# Selective Address Mobility

This is not full L2 extension. routerd CloudEdge does not stretch an Ethernet
segment into public cloud. Public cloud fabrics do not expose an
operator-controlled broadcast domain, and each provider has its own route and
address ownership model.

Selective Address Mobility captures selected `/32` IPv4 addresses at one side
and delivers packets for those addresses to the owning side over a
routerd-to-routerd overlay. TCP/IP source and destination addresses are
preserved by the abstraction. Firewall and NAT are separate routerd layers, not
fields on mobility resources.

![Selective Address Mobility transport diagram showing MobilityPool and SAMTransportProfile as the authoring surface, generated IPIP delivery, BGP peers, ECMP next hops, and capture by secondary IP or proxy ARP](/img/diagrams/cloudedge-sam-ipip.png)

## Resource Model

For the CloudEdge Mobility control plane, `MobilityPool` is the only
operator-authored mobility intent. It declares the logical IPv4 pool, the
EventGroup to read, member nodes and sites, BGP delivery mode, capture policy,
and provider trap placement. Treat the member list like a BGP peer list:
every node must know the identity, site, role, and placement of the other
participants, but it does not need the other nodes' NIC IDs, provider resource
names, or subnet IDs.

The north-star config shape is:

- declare the **self site** completely, including capture and provider
  discovery details;
- declare **remote sites** as identity-only members (`nodeRef`, `site`, `role`,
  and optional `placement`/`maintenance`);
- for larger fabrics, keep the shared identity-only member list in a
  `MobilityMemberSet` and import it with `MobilityPool.spec.membersFrom`;
- keep reusable local cloud capture details in `profiles.cloudCaptures`;
- keep non-secret node-local values in `spec.values`, then project them with
  `capture.targetFrom` and `ownershipDiscovery.subnetRefFrom`.

`MobilityMemberSet` is the mobility counterpart to `SAMPeerGroup`: it contains
only the shared member identity fields (`nodeRef`, `site`, `role`, and optional
`placement`/`maintenance`). It deliberately does not carry `capture`,
`ownershipDiscovery`, `profileRef`, delivery fields, or static owned addresses;
those remain local to the `MobilityPool` on the node that needs them.

```yaml
apiVersion: mobility.routerd.net/v1alpha1
kind: MobilityMemberSet
metadata: { name: svnet1-members }
spec:
  members:
    - nodeRef: pve-rt01
      site: pve01
      role: onprem
    - nodeRef: pve-rt02
      site: pve02
      role: onprem
    - nodeRef: rr01
      site: backbone
      role: cloud
```

A pool can import one or more member sets. Imported members are added first and
local `spec.members` entries are overlaid by `nodeRef`, so a leaf can keep only
its self member with capture/discovery details while still learning the shared
topology from the member set.

```yaml
apiVersion: mobility.routerd.net/v1alpha1
kind: MobilityPool
metadata: { name: svnet1 }
spec:
  prefix: 10.88.60.0/24
  groupRef: svnet1
  membersFrom:
    - resource: MobilityMemberSet/svnet1-members
  members:
    - nodeRef: pve-rt01
      site: pve01
      role: onprem
      capture:
        type: proxy-arp
        interface: vmbr0
      ownershipDiscovery:
        mode: onprem-l2
        sources:
          - type: pve-svnet
            bridge: vmbr0
```

If a required `membersFrom` source is not yet present, the pool reports
`Pending`. Mark the source `optional: true` only when a partial local member list
is acceptable during bootstrap.

For example, on an AWS router:

```yaml
apiVersion: mobility.routerd.net/v1alpha1
kind: MobilityPool
metadata: { name: lab-same-subnet }
spec:
  prefix: 10.0.0.0/24
  groupRef: cloudedge
  values:
    self.region: ap-northeast-1
    self.subnetRef: subnet-0123456789abcdef0
  profiles:
    cloudCaptures:
      aws-self:
        capture:
          type: provider-secondary-ip
          providerRef: aws-lab
          providerMode: eni-secondary-ip
          nicRef: eni-0123456789abcdef0
          configureOSAddress: false
          targetFrom:
            region: self.region
        ownershipDiscovery:
          mode: provider-private-ip
          scanInterval: 60s
          subnetRefFrom: self.subnetRef
          scope:
            includePrimary: false
  members:
    - nodeRef: onprem-router
      site: onprem
      role: onprem
    - nodeRef: cloud-router
      site: aws
      role: cloud
      profileRef: aws-self
      placement:
        group: aws-edge
        priority: 10
      maintenance:
        drain: false
    - nodeRef: azure-router
      site: azure
      role: cloud
      placement:
        group: azure-edge
        priority: 10
    - nodeRef: oci-router
      site: oci
      role: cloud
      placement:
        group: oci-edge
        priority: 10
  deliveryPolicy:
    mode: bgp
  capturePolicy:
    mode: all-non-owner-sites
```

On the on-prem node, the on-prem member is the complete self declaration
instead: it normally carries `staticOwnedAddresses` and a `proxy-arp` capture
with an explicit `activeWhen.type`. Use `single-router` when one local router
owns capture for the site, or `vrrp-master` when an HA pair gates capture by
VRRP master state. The cloud members remain identity-only. The same rule
applies in every direction: the local router owns its local implementation
details; remote members are peer identities.

routerd uses observed facts from federation or provider discovery to advertise
owned `/32` paths through BGP. Operators keep the control plane declarative by
editing only `MobilityPool`; per-address advertisements and provider trap action
plans are derived by the controller.

For same-provider cloud router maintenance, `members[].placement.group` elects
one non-drained active capture member by `priority` and then `nodeRef`.
`members[].maintenance.drain: true` removes that member from active selection,
so only the active member emits provider trap actions while every member can
advertise its BGP standby path. Distribute the same `MobilityPool` config to
every node in the pool to keep placement projection deterministic.

### North-Star Field Reference

`spec.values`
: Non-secret local values used while normalizing this node's config. Use this
  for region names, compartment IDs, resource group names, subnet IDs, NIC names,
  and similar identifiers. Do not put credentials, tokens, private keys, or
  account secrets here.

`spec.profiles.cloudCaptures.<name>.capture`
: Reusable defaults for a local cloud `provider-secondary-ip` capture. A member
  can opt in with `members[].profileRef`. Explicit member fields override the
  profile.

`spec.profiles.cloudCaptures.<name>.ownershipDiscovery`
: Reusable defaults for provider private-IP inventory scanning. If
  `ownershipDiscovery.providerRef` is omitted, it inherits the effective
  `capture.providerRef`.

`members[].profileRef`
: Applies a named cloud capture profile to that member. Use it for the local
  self member. Remote members should normally omit it.

`members[].capture.targetFrom`
: Maps generated provider action target keys to keys in `spec.values`. Explicit
  `capture.target` entries win when both are present.

`members[].ownershipDiscovery.subnetRefFrom`
: Resolves `ownershipDiscovery.subnetRef` from `spec.values` when the explicit
  field is empty.

`members[].placement`
: Declares deterministic active/standby capture placement. Placement is still
  useful on identity-only remote cloud members because other nodes need to know
  which same-site member is active.

The older "remote-full inline" style, where each node repeats every remote
member's provider details, remains accepted during the pre-release period for
compatibility. It is deprecated. `routerctl validate`, plan, and apply surface a
warning when a remote member declares local capture or discovery details; a
future pre-release may make identity-only remote members mandatory.

## Transport Profile

`SAMTransportProfile` is the higher-level transport profile for BGP-mode SAM.
It derives the per-peer `TunnelInterface`, endpoint `/32` `IPv4Route`, and
`BGPPeer` resources that carry mobility paths. Current CloudEdge examples use
IPIP as the default SAM delivery plane. WireGuard, when present, is an
encryption underlay only: generated or hand-authored WireGuard peers should keep
`AllowedIPs` to transport endpoint prefixes, not mobility `/32`s.

Each router must declare `spec.selfNodeRef`; routerd does not infer the local
node identity from hostname or BGP router ID.

`spec.addressingMode` controls `/31` slot derivation:

- `edge-index` (default): profiles with more than one peer must declare the same
  `spec.topologyNodeRefs` list on every router in the transport domain. The
  controller sorts that shared node list and ranks each unordered node pair
  before allocating a `/31` from `spec.innerPrefix`.
- `pair-stable`: each peer edge derives a slot from a stable hash, so
  leaf/router profiles can omit global `topologyNodeRefs`. Collision detection
  is currently profile-local (within one profile's `spec.peers` list). When a
  collision occurs, set both `override.localInner` and `override.remoteInner`
  for the affected peer to reserve explicit addresses.

For production fabrics, prefer `/20` or larger `innerPrefix` where practical;
smaller pools such as `/24` (128 `/31` slots) collide more easily under
hash+mod allocation.

`SAMPeerGroup` groups reusable transport peers. A profile can set
`spec.peersFrom` to one or more `SAMPeerGroup/<name>` references; the controller
resolves those groups at reconcile time, adds their peers first, then overlays
the profile's local `spec.peers`. When the same `nodeRef` appears in both, the
local `spec.peers` entry wins so operators can keep static bootstrap or override
entries on a leaf. If a required `peersFrom` group is not yet present, the
profile reports `Pending`; optional sources are ignored until they arrive.

Spine or route-reflector profiles can set `spec.publishPeerGroup: true`. In that
mode routerd publishes a `SAMPeerGroup` DynamicConfigPart with this profile's
`selfNodeRef` and concrete local endpoint. `localEndpointFrom` is resolved before
publishing so leaves receive a direct `remoteEndpoint` value.

When `routerd serve` runs on a node with `publishPeerGroup: true`, it also
serves published peer groups over the transport network on TCP port `19652`
(`GET /v1/peer-groups`). A leaf with a missing required `peersFrom` group tries
to query WireGuard peers reachable through `spec.underlayInterface`; a matching
group is stored locally as `peer-group-sync/<group-name>` with the normal
dynamic-config TTL. If the publisher disappears or the group expires, the leaf
returns to `Pending`.

For MobilityPool membership, an RR can set `spec.publishMemberSet: true` on the
canonical pool. routerd strips local-only member fields, publishes a
`MobilityMemberSet` DynamicConfigPart with source `mobility-member-set/<pool>`,
and serves it on the same TCP port via `GET /v1/member-sets`. Leaves with a
missing required `membersFrom` source store a fetched set as
`member-set-sync/<set-name>`.

```yaml
apiVersion: mobility.routerd.net/v1alpha1
kind: SAMTransportProfile
metadata: { name: cloudedge-transport }
spec:
  selfNodeRef: aws-router-a
  mode: ipip
  encryption: wireguard
  innerPrefix: 10.255.0.0/24
  topologyNodeRefs:
    - onprem-router
    - aws-router-a
    - azure-router
  underlayInterface: wg-hybrid
  localEndpointFrom:
    resource: Interface/wg-hybrid
    field: primaryIPv4
  bgp:
    routerRef: BGPRouter/mobility
    peerASN: 64512
    timersPreset: fast
  peers:
    - nodeRef: onprem-router
      remoteEndpoint: 10.252.0.1
```

Core routers can set `spec.bgp.routeReflectorClient` and
`spec.bgp.routeReflectorClusterID`; those fields are copied to each generated
`BGPPeer`. Edge routers can leave them unset and use ordinary iBGP sessions.

Peer removal replaces the profile's generated `DynamicConfigPart` with the new
resource set. Profile deletion replaces the old part with an empty active part,
so effective config drops generated tunnel, BGP peer, and endpoint route
resources. The generated resources then clean up through normal owner-reference
GC and resource-specific teardown.

## Low-Level Compatibility Resources

`AddressMobilityDomain` and `RemoteAddressClaim` are the lower-level SAM
representation. Existing hand-authored SAM configs remain supported during the
pre-release period for compatibility, but they are not the primary authoring
surface for CloudEdge Mobility. Prefer `MobilityPool` for address ownership and
capture intent, and `SAMTransportProfile` for transport/BGP generation.

`AddressMobilityDomain` defines the IPv4 prefix where selected addresses may
move:

```yaml
apiVersion: hybrid.routerd.net/v1alpha1
kind: AddressMobilityDomain
metadata: { name: lab-same-subnet }
spec:
  prefix: 10.0.0.0/24
  mode: selective-address
  peerRef: cloud-main
```

`RemoteAddressClaim` declares one mobile `/32`, how it is captured, and how it
is delivered:

```yaml
apiVersion: hybrid.routerd.net/v1alpha1
kind: RemoteAddressClaim
metadata: { name: onprem-vm-10-0-0-9 }
spec:
  domainRef: lab-same-subnet
  address: 10.0.0.9/32
  ownerSide: onprem
  capture:
    type: provider-secondary-ip
    providerRef: azure-lab
    providerMode: nic-secondary-ip
    nicRef: /subscriptions/.../networkInterfaces/routerd-nic
    configureOSAddress: false
  delivery:
    peerRef: cloud-main
    mode: route
    tunnelInterface: wg-hybrid
```

`AddressMobilityDomain.spec.peerRef` is a domain-level default/documentation
peer for grouping metadata. The MVP dataplane uses
`RemoteAddressClaim.spec.delivery.peerRef` as the actual delivery peer, and it
is required on each claim.

`CloudProviderProfile` describes provider capabilities and how an external
tool would authenticate. The mobility planner does not call provider APIs
directly. For cloud capture it emits dry-run `ActionPlan` records such as
`assign-secondary-ip` and `ensure-forwarding-enabled`; the separate
provider-action executor path may import and execute those only when explicitly
allowed by `ProviderActionPolicy`.

`OverlayPeer` identifies the remote routerd peer and underlay for legacy
route-lowered configs. `HybridRoute` continues to model ordinary L3
remote-prefix routing. New CloudEdge Mobility configs should not use
`OverlayPeer` to carry mobility `/32`s; use BGP delivery through
`SAMTransportProfile`.

## Capture And Delivery

Supported capture types:

| Type | Meaning |
| --- | --- |
| `provider-secondary-ip` | The cloud fabric captures the `/32` through a provider-owned secondary address object or equivalent. |
| `proxy-arp` | A site router answers ARP locally for a selected address. |

Reserved capture types rejected by MVP validation:

| Type | Status |
| --- | --- |
| `static-host-route` | Reserved for a later dataplane design. |
| `garp` | Reserved for a later dataplane design. |

For `MobilityPool`, delivery mode is BGP. Owned addresses are advertised as
IPv4 unicast `/32` paths; non-owners import the BGP best path into the local FIB
over the selected overlay next hop. `deliveryPolicy.mode: bgp` is the default
and the only supported MobilityPool delivery mode in the current control plane.
Older route-lowered SAM delivery remains available only for hand-authored
`RemoteAddressClaim` compatibility configs.

`members[].capture.target` carries non-secret provider target hints copied into
generated provider `ActionPlan.target` values. Put identifiers such as region,
compartment ID, resource group, NIC name, or IP config name there; credentials,
tokens, and private keys must stay in provider auth mechanisms.

For BGP-mode on-prem `proxy-arp` capture, `members[].capture.sourceAddress`
optionally declares the router's local sender address on the capture
interface. routerd lowers this to an `IPv4StaticAddress` `/32` and uses it as
the capture-prefix route preferred source. This is useful when the capture
interface otherwise has no IPv4 address: Linux ARP for local same-subnet
clients then uses an address inside the mobility prefix instead of falling back
to an unrelated management address.

If that sender address is owned by another lifecycle manager such as DHCP/IPAM,
use `members[].capture.sourceAddressFrom` instead. For example,
`resource: DHCPv4Client/svnet1-source` with `field: currentAddress` uses the
leased address as the capture-prefix route preferred source without lowering it
to an `IPv4StaticAddress`, so routerd does not duplicate ownership of the same
address.

Use `members[].capture.excludeAddresses` for local-only addresses inside the
mobility prefix that must never be proxy-ARP captured across the extended
segment. On PVE Simple SDN, for example, each host may own the same local
gateway address such as `192.168.123.1/32`; excluding it prevents generated BGP
proxy-ARP claims for that address and splits the capture-prefix route so Linux
does not send local gateway ARP across the SAM capture path.

SAM does not provide transparent DHCP broadcast extension. Keep DHCP ownership
with the local fabric, VPC/VNet/VCN, or PVE IPAM. A `DHCPv4Client` used by
`sourceAddressFrom` should usually set `useRoutes: false` and `useDNS: false`
when it exists only to learn the capture-interface source address. DHCP lease
observations can participate in ownership discovery, but they should be combined
with `arp-observer`, `on-demand-arp`, or PVE svnet observations when the IPAM
source is outside routerd.

`on-demand-arp` also performs a conservative proactive sweep of the mobility
prefix: one ARP target is probed per source `scanInterval`, using the same
`probeTimeout`, `probeRetries`, `probeCooldown`, and `sourceAddressFrom`
settings as demand-triggered probes. This lets quiet, already-running L2
clients become observed without a manual `arping` or ping from the owner side.
Keep `scanInterval` conservative on broad prefixes; for `/24` lab validation a
short interval such as `1s` gives fast convergence while still limiting traffic
to one active ARP probe per second.

For `proxy-arp` capture on Linux, routerd:

- enables `net.ipv4.conf.<capture-interface>.proxy_arp=1` through the normal
  sysctl controller,
- installs a proxy neighbor entry equivalent to
  `ip neigh add proxy <address> dev <capture-interface>`, and
- enables `net.ipv4.ip_forward=1` through the normal sysctl controller.

For `provider-secondary-ip`, the provider fabric owns address capture. routerd
does not assign the mobile address to the local OS when
`configureOSAddress: false`; on Linux it also removes that specific address
from local interfaces if cloud-init, netplan, or another guest agent adds it
back. It then ensures IPv4 forwarding; the overlay `/32` delivery route comes
from BGP best-path import. routerd does not re-add the address when the capture
is removed, because it never owned the guest OS assignment.

Status reports this as `captureOSAddressAbsence`. `enforced: true` means
routerd is actively enforcing that the captured address is absent from local OS
interfaces. `lastReconcileRemoved: true` means the most recent reconcile
actually removed the address; it is normally `false` in steady state once the
address is already absent.

FreeBSD and other non-Linux hosts do not have live SAM capture yet. The
controller no-ops and reports `SAM capture not implemented on this OS`.

The live Linux dataplane has been smoke-tested in an Azure + PVE same-subnet
lab. Treat it as pre-release behavior and validate the exact provider and
firewall policy before production use.

## Reverse Path Filtering

Strict reverse-path filtering can drop forwarded SAM traffic because the mobile
`/32` may appear to belong to a directly attached subnet while the return path
is through the overlay. routerd does not silently change `rp_filter` for SAM,
because that is an invasive interface policy decision.

`routerctl doctor hybrid` reads
`net.ipv4.conf.<capture-or-tunnel-interface>.rp_filter` when host checks are
enabled. It warns when the value is strict (`1`) and recommends evaluating loose
mode (`2`) on the affected interfaces.

## Provider Capabilities

| Provider | MVP capability descriptor |
| --- | --- |
| Azure | NIC secondary IP plus IP forwarding enabled on the router NIC. |
| AWS | ENI secondary private IPv4 plus source/destination check disabled. |
| OCI | VNIC private IP object plus source/destination check disabled. |
| GCP | Alias IP or route capability, gated by the declared provider profile. |

The profile is declarative. The mobility planner can produce provider
`ActionPlan` records, but address assignment and NIC flag changes remain gated
by the provider-action execution policy and executor plugin. The planner itself
never mutates provider state.

## Same-Subnet Flow

In a `10.0.0.0/24` lab, suppose `10.0.0.7/32` is the cloud VM address and
`10.0.0.9/32` is the on-prem/PVE VM address. The goal is for the cloud VM at
`10.0.0.7` to open a TCP connection to the on-prem VM at `10.0.0.9` while both
VMs keep local default gateways and no NAT is introduced.

1. The cloud VM sends to `10.0.0.9`.
2. Azure NIC secondary IP capture directs packets for `10.0.0.9/32` to the
   cloud routerd node.
3. The cloud routerd node delivers the packet over the generated IPIP SAM
   transport; if encryption is enabled, that IPIP packet rides over the
   endpoint-only `wg-hybrid` underlay.
4. The on-prem side forwards it to the owner of `10.0.0.9`.
5. Source and destination IPs remain the original endpoint addresses.

The reverse path for `10.0.0.7/32` is captured on the on-prem side with
proxy-ARP. PVE LAN hosts reach `.7` through the on-prem routerd node, which
delivers the packet over the same generated SAM transport to the cloud routerd
node.

The split example configs are:

- `examples/hybrid-azure-pve-same-subnet-cloud.yaml`, applied on the cloud
  routerd node, contains the provider-secondary-IP claim for on-prem VM
  `10.0.0.9/32`.
- `examples/hybrid-azure-pve-same-subnet-onprem.yaml`, applied on the on-prem
  routerd node, contains the proxy-ARP claim for cloud VM `10.0.0.7/32`.

## Firewall And NAT Composition

Selective Address Mobility lives in the ordinary switching/forwarding plane. It
does not contain `nat`, `preserveSource`, firewall, or zone fields. Address
transparency is intrinsic: the source and destination addresses are preserved.

To firewall or NAT a mobile address, reference its literal `/32` in the
existing `FirewallZone`, `FirewallRule`, or `NAT44Rule` resources. The current
model has no cross-kind reference from firewall or NAT kinds to `MobilityPool`
or low-level `RemoteAddressClaim`; the coupling is intentionally loose by
literal address. A named reference can be added later if it proves useful.

SAM-forwarded traffic still traverses the existing firewall and conntrack path
like any other forwarded traffic. Independence means the mobility resources do
not configure firewall or NAT policy; it does not mean bypass.

In particular, the delivered `/32` traffic crosses the Linux firewall
`FORWARD` chain between the capture interface and the tunnel interface. Permit
that forwarding path for the captured address explicitly when the router has a
default-drop forwarding policy. SAM does not add firewall rules by itself.

## Overlay And Federation Addressing On Cloud Nodes

The Event Federation transport (the `routerd-eventd` listen address and each
`EventPeer.endpoint`), BGP/BFD peer addresses, and the SAM transport endpoint /
inner addresses generated by `SAMTransportProfile` must use address ranges you
control end to end on every node. If you place WireGuard underneath the SAM
transport, its interface/peer endpoint addresses have the same requirement. On
cloud instances, do
**not** draw overlay, BGP/BFD, or federation addresses from ranges the provider
reserves for its own internal use:

- `169.254.0.0/16` (RFC 3927 link-local). Cloud instance metadata (IMDS) lives
  at `169.254.169.254`, and some images reserve the entire block: Oracle
  Cloud's Linux image routes all of `169.254.0.0/16` through an
  `InstanceServices` chain, so a federation SYN to a `169.254.x` overlay
  address is pulled to loopback and reset even though ICMP to the same address
  succeeds. AWS and Azure also use `169.254.169.254` for IMDS. Symptom: local
  ownership facts are present, but `routerd-eventd` or BGP/BFD sessions never
  connect between nodes.
- `100.64.0.0/10` (RFC 6598 carrier-grade NAT). Used by CGNAT on provider
  underlays and by Tailscale (`100.x` tailnet addresses, MagicDNS). An overlay
  in this range collides with any Tailscale membership and with carrier NAT.

Use RFC 1918 ranges you reserve for SAM transport endpoints, the
`SAMTransportProfile.innerPrefix`, any optional WireGuard endpoint addresses,
and the `routerd-eventd` listen / `EventPeer` endpoints and BGP/BFD peering
addresses. Keep them distinct from the mobility pool `/24` (the captured
addresses) and from every cloud-reserved range above. This applies to all
providers (AWS/Azure/OCI); OCI is simply the strictest at enforcing the
link-local reservation.

## Client Endpoint Addressing vs Router-Overlay Reachability

A globally-unique `/32` on a **client** guest's `lo`/dummy interface is **not**
reachable across cloud fabrics just because the guest OS owns the address. The
cloud fabric (VPC/VNet/VCN) only delivers destinations within the provider subnet
CIDR to a client ENI/NIC; a destination outside the VPC CIDR is dropped by the
fabric before it reaches the client, regardless of overlay routes on the routers.

Concretely, in a distinct-addressing 4-site test:

- **Router endpoint `/32`s on the overlay itself** are reachable end-to-end (the
  routers carry them over WireGuard). A distinct-mesh of router endpoints passes
  12 directed ping+SSH.
- **Client dummy/lo `/32`s outside the VPC CIDR are not** — the cloud fabric does
  not deliver them to the client ENI even with overlay routes and provider
  forwarding enabled.

Therefore: treat distinct-mesh shortcut endpoints as **router endpoints only**. To
give clients globally-unique, cross-fabric-routable addresses you need either
provider-routable client subnets or provider-assigned client IPs (a secondary IP /
captured address that the fabric actually delivers), not a guest-local dummy `/32`.
Do not confuse router-overlay reachability with client-fabric reachability when
designing a multi-site lab.

## Out Of Scope

The MVP does not implement full L2 extension, EVPN, BUM forwarding,
broadcast/multicast extension, automatic ungated cloud API mutation, dynamic
patch/replace semantics, or automatic `rp_filter` changes.
