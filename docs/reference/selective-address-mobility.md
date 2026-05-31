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
    - nodeRef: cloud-router
      site: azure
      role: cloud
  leasePolicy:
    ttl: 5m
    holdDuration: 30s
  capturePolicy:
    mode: all-non-owner-sites
```

routerd projects `routerd.client.ipv4.observed` federation events into
read-only `AddressLease` state. A lease is not a config Kind and should not be
hand-authored. Inspect it with `routerctl mobility leases`.

`AddressMobilityDomain` and `RemoteAddressClaim` are the lower-level SAM
representation. Existing hand-authored SAM configs remain supported, but in the
CloudEdge Mobility path they are intended to be derived from `MobilityPool` and
`AddressLease` state by the Step 2 planner.

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
tool would authenticate. It is only a capability/profile descriptor in this MVP;
routerd makes no cloud API calls.

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

The profile is declarative. In the MVP, routerd can validate and display the
intent, but it does not assign cloud addresses, change NIC flags, or replace
provider route tables.

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

## Out Of Scope

The MVP does not implement full L2 extension, EVPN, BUM forwarding,
broadcast/multicast extension, automatic cloud API mutation, dynamic patch or
replace semantics, provider-side address assignment, or automatic `rp_filter`
changes.
