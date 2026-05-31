---
title: Selective Address Mobility
slug: /reference/selective-address-mobility
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

## Resource Model

For the CloudEdge Mobility control plane, `MobilityPool` is the only
operator-authored mobility intent. It declares the logical IPv4 pool, the
EventGroup to read, member nodes and sites, and the lease/capture policy:

```yaml
apiVersion: mobility.routerd.net/v1alpha1
kind: MobilityPool
metadata: { name: lab-same-subnet }
spec:
  prefix: 10.0.0.0/24
  groupRef: cloudedge
  members:
    - nodeRef: onprem-router
      site: onprem
      role: onprem
      capture:
        type: proxy-arp
        interface: lan
      deliveryTo:
        - nodeRef: cloud-router
          peerRef: cloud-main
          mode: route
          tunnelInterface: wg-hybrid
      delivery:
        peerRef: cloud-main
        mode: route
        tunnelInterface: wg-hybrid
    - nodeRef: cloud-router
      site: azure
      role: cloud
      capture:
        type: provider-secondary-ip
        providerRef: azure-lab
        providerMode: nic-secondary-ip
        nicRef: /subscriptions/.../networkInterfaces/routerd-nic
        configureOSAddress: false
        target:
          region: japaneast
          ipConfigName: mobility-capture
      placement:
        group: azure-edge
        priority: 10
      maintenance:
        drain: false
      delivery:
        peerRef: onprem-main
        mode: route
        tunnelInterface: wg-hybrid
  leasePolicy:
    ttl: 5m
    holdDuration: 30s
  capturePolicy:
    mode: all-non-owner-sites
    deprovisionHoldDuration: 30s
```

routerd projects `routerd.client.ipv4.observed` federation events into
read-only `AddressLease` state. A lease is not a config Kind and should not be
hand-authored. Inspect it with `routerctl mobility leases`.

For same-provider cloud router maintenance, `members[].placement.group` elects
one non-drained active capture member by `priority` and then `nodeRef`.
`members[].maintenance.drain: true` removes that member from active selection,
so the planner moves generated capture claims and provider action plans to the
next candidate. Distribute the same `MobilityPool` config to every node in the
pool to keep placement projection deterministic.

`AddressMobilityDomain` and `RemoteAddressClaim` are the lower-level SAM
representation. Existing hand-authored SAM configs remain supported, but in the
CloudEdge Mobility path they are derived from `MobilityPool` and `AddressLease`
state by the mobility planner and stored as a `DynamicConfigPart`.

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

`OverlayPeer` identifies the remote routerd peer and underlay. `HybridRoute`
continues to model ordinary L3 remote-prefix routing. Address mobility uses the
same overlay peer model, but it is a per-address forwarding abstraction rather
than a remote-prefix route.

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

Delivery mode is `route`: routerd represents delivery as forwarding the
captured `/32` over the named overlay peer and optional tunnel interface. The
Linux dataplane lowers this delivery into a managed `IPv4Route` for the exact
claim address, for example `10.0.0.9/32 dev wg-hybrid`. routerd never lowers a
SAM claim into a default route.

`members[].deliveryTo[]` can select delivery by owner `nodeRef`, then `site`,
then `role`; `members[].delivery` is the fallback. This keeps one shared
`MobilityPool` config usable across four-site demos where the on-prem router
must deliver to AWS, Azure, and OCI through different overlay peers.

`members[].capture.target` carries non-secret provider target hints copied into
generated provider `ActionPlan.target` values. Put identifiers such as region,
compartment ID, resource group, NIC name, or IP config name there; credentials,
tokens, and private keys must stay in provider auth mechanisms.

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
back. It then ensures IPv4 forwarding and installs the `/32` delivery route
into the overlay. routerd does not re-add the address when the claim is
removed, because it never owned the guest OS assignment.

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
3. The cloud routerd node delivers the packet over `wg-hybrid` to the on-prem
   routerd peer.
4. The on-prem side forwards it to the owner of `10.0.0.9`.
5. Source and destination IPs remain the original endpoint addresses.

The reverse path for `10.0.0.7/32` is captured on the on-prem side with
proxy-ARP. PVE LAN hosts reach `.7` through the on-prem routerd node, which
delivers the packet over the overlay to the cloud routerd node.

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
existing `FirewallZone`, `FirewallRule`, or `NAT44Rule` resources. The MVP has
no cross-kind reference from firewall or NAT kinds to `RemoteAddressClaim`; the
coupling is intentionally loose by literal address. A named reference can be
added later if it proves useful.

SAM-forwarded traffic still traverses the existing firewall and conntrack path
like any other forwarded traffic. Independence means the mobility resources do
not configure firewall or NAT policy; it does not mean bypass.

In particular, the delivered `/32` traffic crosses the Linux firewall
`FORWARD` chain between the capture interface and the tunnel interface. Permit
that forwarding path for the captured address explicitly when the router has a
default-drop forwarding policy. SAM does not add firewall rules by itself.

## Overlay And Federation Addressing On Cloud Nodes

The Event Federation transport (the `routerd-eventd` listen address and each
`EventPeer.endpoint`) and the WireGuard overlay it rides on (`OverlayPeer`,
`WireGuardInterface`/`WireGuardPeer`) must use an address range you control end
to end on every node. On cloud instances, do **not** draw overlay or federation
addresses from ranges the provider reserves for its own internal use:

- `169.254.0.0/16` (RFC 3927 link-local). Cloud instance metadata (IMDS) lives
  at `169.254.169.254`, and some images reserve the entire block: Oracle
  Cloud's Linux image routes all of `169.254.0.0/16` through an
  `InstanceServices` chain, so a federation SYN to a `169.254.x` overlay
  address is pulled to loopback and reset even though ICMP to the same address
  succeeds. AWS and Azure also use `169.254.169.254` for IMDS. Symptom: leases
  converge but `routerd-eventd` TCP never connects between nodes.
- `100.64.0.0/10` (RFC 6598 carrier-grade NAT). Used by CGNAT on provider
  underlays and by Tailscale (`100.x` tailnet addresses, MagicDNS). An overlay
  in this range collides with any Tailscale membership and with carrier NAT.

Use an RFC 1918 range you reserve for the overlay (for example a `10.x.y.0/24`)
for the WireGuard interface/peer addresses, the `OverlayPeer` endpoints, and the
`routerd-eventd` listen / `EventPeer` endpoints. Keep it distinct from the
mobility pool `/24` (the captured addresses) and from every cloud-reserved range
above. This applies to all providers (AWS/Azure/OCI); OCI is simply the
strictest at enforcing the link-local reservation.

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
