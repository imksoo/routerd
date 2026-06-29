---
title: AWS/Azure/OCI/PVE SAM Hub-Leaf Architecture
---

# AWS/Azure/OCI/PVE SAM Hub-Leaf Architecture

This page summarizes the reviewed live-test shape for dynamic SAM enrollment
across AWS, Azure, OCI, and PVE. The 2026-06-29 merge gate used an Azure RR/hub
and one client-bearing leaf site in each provider. "Full mesh" means every
AWS/Azure/OCI/PVE client pair passed through the SAM fabric; it does not require
direct leaf-to-leaf adjacency for every site pair.

See the detailed runbook in
`docs/operations/dynamic-rr-leaf-enrollment-test.md` and live evidence in
`docs/releases/evidence/sam-aws-azure-oci-pve-fullmesh-20260629.md`.
The PVE dual-RR FOU redundancy gate also passed on 2026-06-29 and is
evidence-frozen in
`docs/releases/manifests/v20260629.samred.yaml`.

## Roles

| Role | Responsibility |
| --- | --- |
| RR/hub | Stable admission point. Runs `BGPRouter`, `BGPDynamicPeer`, `SAMRRSet`, `SAMTransportProfile`, `SAMEnrollmentPolicy`, `MobilityPool`, local transport endpoint, and optional `WireGuardInterface`. |
| Leaf | Runs routerd at each AWS/Azure/OCI/PVE site. Submits a local `SAMEnrollmentClaim`, fetches the authorized `SAMRRSet`, and materializes transport plus BGP peers from dynamic state. |
| Client/workload | Uses the local leaf as the next hop for remote client /32s and advertises or is represented by MobilityPool-owned /32 reachability. |

The RR base config must not list leaves. It must contain zero
`SAMEnrollmentClaim/leaf-*`, zero per-leaf `BGPPeer`, and zero hand-written
per-leaf `WireGuardPeer` resources. Accepted claims are runtime admission
state.

## RR/Hub Shape

The hub config is stable across leaf additions. The important pieces are:

```yaml
- kind: BGPRouter
  metadata: { name: mobility-bgp }
  spec:
    asn: 64577
    listen: { port: 179 }
    importPolicy:
      allowedPrefixes: [10.77.60.0/24]
      allowedPrefixLengthMin: 32
      allowedPrefixLengthMax: 32

- kind: BGPDynamicPeer
  metadata: { name: fullmesh-leaves }
  spec:
    routerRef: BGPRouter/mobility-bgp
    peerASN: 64577
    listen:
      sourcePrefixes: [10.255.0.0/20]
    routeReflectorClient: true
    importPolicy:
      allowedPrefixes: [10.77.60.0/24]
      allowedPrefixLengthMin: 32
      allowedPrefixLengthMax: 32
      nextHopRewrite: peer-address

- kind: SAMRRSet
  metadata: { name: fullmesh-rrs }
  spec:
    enrollmentPolicyRef: SAMEnrollmentPolicy/fullmesh-public-leaves
    mobilityPoolRefs: [MobilityPool/fullmesh]
    routeAdmission:
      allowedPrefixes: [10.77.60.0/24]
      allowedPrefixLengthMin: 32
      allowedPrefixLengthMax: 32
      nextHopRewrite: peer-address
    members:
      - nodeRef: azure-rr
        endpoint: 10.82.10.5
        tunnelAddress: 10.255.0.1/32
        bgp: { asn: 64577, routerID: 10.255.0.1 }

- kind: SAMEnrollmentPolicy
  metadata: { name: fullmesh-public-leaves }
  spec:
    transportProfileRef: SAMTransportProfile/azure-rr-public
    rrSetRef: SAMRRSet/fullmesh-rrs
    joinTokenFrom:
      file: /usr/local/etc/routerd/secrets/fullmesh-join-token
    joinAudience: fullmesh-public-underlay
    allowedLeafIDs:
      pattern: ^(aws|azure|oci|pve)-leaf$
    tunnelAddressPrefixes: [10.255.0.0/20]
    endpointPrefixes: [10.80.0.0/12]
    mobilityPoolRefs: [MobilityPool/fullmesh]
    ttl: 24h
```

`BGPDynamicPeer` is only the BGP acceptor. Leaf identity, join authentication,
tunnel address authorization, and MobilityPool /32 ownership stay in SAM
enrollment and MobilityPool resources.

## Leaf Shape

A leaf keeps only its own claim and a bootstrap client. It learns the RRSet from
the RR admission API, then the SAM transport controller uses that fetched RRSet
to create RR-facing `TunnelInterface` and `BGPPeer` resources.

```yaml
- kind: SAMEnrollmentClaim
  metadata: { name: aws-leaf }
  spec:
    policyRef: SAMEnrollmentPolicy/fullmesh-public-leaves
    rrSetRef: SAMRRSet/fullmesh-rrs
    leafID: aws-leaf
    joinAudience: fullmesh-public-underlay
    joinNonce: aws-leaf-20260629T092330Z
    joinTimestamp: "2026-06-29T09:23:30Z"
    joinHMAC: "<hmac-sha256>"
    tunnelAddress: 10.255.0.11/32
    endpoint: 10.81.10.10
    mobility:
      ownedAddresses: [10.81.10.20/32]
    bgp:
      asn: 64577
      routerID: 10.255.0.11

- kind: SAMEnrollmentClient
  metadata: { name: fullmesh }
  spec:
    claimRef: SAMEnrollmentClaim/aws-leaf
    bootstrap:
      url: http://10.82.10.5:8080
    stateTTLRefreshBefore: 2h

- kind: SAMTransportProfile
  metadata: { name: aws-leaf-public }
  spec:
    selfNodeRef: aws-leaf
    mode: ipip
    encryption: wireguard
    underlayInterface: wg-fullmesh
    peersFrom:
      - resource: SAMRRSet/fullmesh-rrs
    bgp:
      routerRef: BGPRouter/mobility-bgp
      peerASN: 64577
```

For a private WAN or leased-line underlay, use `encryption: none` with
`mode: ipip`, `gre`, or `fou` and omit WireGuard fields. The PVE redundancy
evidence covered FOU with no encryption. The public AWS/Azure/OCI/PVE merge
gate used IPIP plus WireGuard because NAT/public reachability made it the
common practical transport.

## Provider Route Requirements

Each provider must allow the leaf VM to forward traffic and must route remote
client /32s to the local leaf:

| Provider | Required routing control |
| --- | --- |
| AWS | Disable source/destination check on the leaf instance or ENI. Route remote client /32s to the leaf ENI in the site route table. |
| Azure | Enable NIC IP forwarding on RR and leaf NICs. Add UDR entries for remote client /32s with next hop set to the leaf private IP. |
| OCI | Enable skip source/destination check on the leaf VNIC. Add route rules for remote client /32s to the leaf private IP target used by the VCN. Allow forwarding in the guest firewall when the image defaults reject FORWARD. |
| PVE | Attach the leaf and client to the intended bridge/VLAN. Add client routes for remote client /32s via the leaf site gateway when the client default gateway is not the leaf. |

## Client Matrix

The accepted live gate used these client /32s:

| Site | Client /32 |
| --- | --- |
| AWS | `10.81.10.20/32` |
| Azure | `10.82.10.20/32` |
| OCI | `10.83.10.20/32` |
| PVE | `10.99.9.10/32` |

Pass criteria were all 12 directed pairs passing ping and SSH:

- AWS <-> Azure
- AWS <-> OCI
- AWS <-> PVE
- Azure <-> OCI
- Azure <-> PVE
- OCI <-> PVE

## Security Boundary

The TCP control API defaults to `127.0.0.1:65432` and loopback-only source
admission. It is still the mutation/control API and does not preserve Unix
socket filesystem-permission semantics; on multi-user hosts, disable it with a
`ControlAPI` resource using `enabled: false` and use the Unix socket instead.
RR-side SAM enrollment over a private underlay should declare a
`system.routerd.net/v1alpha1` `ControlAPI` resource with a protected
`listenAddress`, `port: 65432`, and a narrow `allowCIDRs` list such as the leaf
management subnet. `0.0.0.0/0` and `::/0` are rejected; this is not an
Internet-safe listener.
For RR-side HTTP enrollment, prefer adding `ControlAPI.spec.tokenFrom` and
configuring each leaf `SAMEnrollmentClient.spec.controlAPITokenFrom` with the
same secret source so submit/fetch calls require both source-CIDR admission and
a bearer token. This bearer token is an additional private-underlay control, not
a substitute for mTLS or a public Internet exposure boundary.

The join token authorizes enrollment requests, not long-term route ownership by
itself. The RR still validates claim TTL, HMAC, tunnel address prefixes,
endpoint prefixes, duplicate constraints, and MobilityPool /32 ownership before
the accepted claim can materialize runtime resources.
