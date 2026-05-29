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
metadata: { name: azure-vm-10-0-0-9 }
spec:
  domainRef: lab-same-subnet
  address: 10.0.0.9/32
  ownerSide: cloud
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
live capture and `/32` forwarding dataplane is intentionally deferred to a
later implementation step.

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

In a `10.0.0.0/24` lab, suppose `10.0.0.7/32` is owned on-prem and
`10.0.0.9/32` is owned in Azure.

1. A cloud host sends to `10.0.0.7`.
2. The cloud-side capture mechanism directs packets for `10.0.0.7/32` to the
   cloud routerd node.
3. routerd delivers the packet over `wg-hybrid` to the on-prem routerd peer.
4. The on-prem side forwards it to the owner of `10.0.0.7`.
5. Source and destination IPs remain the original endpoint addresses.

The reverse path for `10.0.0.9/32` works the same way with the provider
secondary IP capture on the cloud side.

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

## Out Of Scope

The MVP does not implement full L2 extension, EVPN, BUM forwarding,
broadcast/multicast extension, automatic cloud API mutation, dynamic patch or
replace semantics, netlink programming, proxy-ARP programming, `/32` route
forwarding, or `ip_forward` changes.
