# CloudEdge Mobility Demo

This example packages the live CloudEdge Mobility demo that exercised:

- D3: four-site selective address mobility across on-prem, AWS, Azure, and OCI.
- D5: AWS cloud-router maintenance drain with provider capture migration from
  router A to router B.
- Phase F: on-prem static-owned address declaration plus release/handover
  examples for planned `.10` ownership movement.

> For an illustrated, conceptual walkthrough of what this demo shows (topology,
> address design, data/control plane), see the how-to:
> [`docs/how-to/cloudedge-mobility-demo.md`](../../docs/how-to/cloudedge-mobility-demo.md).

The demo assumes the lab instances, NICs/VNICs, IAM/identity permissions, SSH,
WireGuard keys, and provider CLIs are already prepared. The scripts here do not
provision cloud resources.

## Topology

All four sites share one logical mobility subnet. The overlay is a separate
RFC1918 WireGuard network and avoids link-local and CGNAT ranges.

| Node | Overlay | Demo client /32 | Capture mode |
| --- | --- | --- | --- |
| `onprem-router` | `10.99.0.1/32` | `10.77.60.10/32` | proxy ARP on LAN |
| `aws-router-a` | `10.99.0.2/32` | `10.77.60.11/32` | AWS ENI secondary IP |
| `azure-router` | `10.99.0.3/32` | `10.77.60.12/32` | Azure NIC secondary ipConfig |
| `oci-router` | `10.99.0.4/32` | `10.77.60.13/32` | OCI VNIC secondary private IP |
| `aws-router-b` | `10.99.0.5/32` | standby for D5 | AWS ENI secondary IP |

D3 acceptance is twelve directed ping and SSH flows between the four demo
clients, with source addresses preserved and default gateways unchanged. D5
acceptance drains `aws-router-a`, verifies `.10` is unassigned from ENI-A and
assigned to ENI-B, and confirms traffic recovers via `aws-router-b`.

The `onprem-router` demo `/32` is router-originated and configured on the
router loopback. Do not use `10.77.60.10/32` as an independent dataplane
endpoint for an on-prem router OS reboot HA test: rebooting the router removes
the endpoint itself. For T9-style on-prem router reboot validation, use a
separate on-prem client VM/address behind the routers and let
`ownershipDiscovery.onprem-l2` observe that client from the LAN.

## Files

- `env.example`: copy to `env` and fill all account-specific values.
- `onprem.yaml`, `aws.yaml`, `azure.yaml`, `oci.yaml`: routerd config templates.
- `run-demo.sh`: renders configs, deploys them, waits for cloud provider
  private-IP discovery, runs D3 connectivity, then performs D5 AWS drain/capture
  migration.
- `phase-f/`: declarative diff snippets for static-owned release, handover to
  AWS, and rollback. These snippets are not standalone router configs.
- `iam/`: least-privilege provider policy templates for the router instance
  identities. These replace broad admin-style demo roles after review.
- `collect-evidence.sh`: collects provider state, routerd journals, dynamic
  state, doctor output, and connectivity evidence.
- `reset-lab.sh`: best-effort cleanup to remove secondary captures, restore
  forwarding checks, stop cloud compute, and prevent unattended cost.

The D3/D5 demo path uses `MobilityPool` as the declarative control-plane object
that consumes federation observed events and derives leases, BGP /32
advertisements, routes, and provider action plans. The on-prem `.10` owner is
declared through `staticOwnedAddresses`. Cloud `.11/.12/.13` ownership is
observed by the router-local `observe.providerPrivateIPs` inventory plugin, so
`run-demo.sh` no longer injects owner events manually.
Cloud router `capture.nicRef` is intentionally omitted from the demo templates:
each router resolves its own NIC/VNIC through the inventory plugin from the
declared provider subnet segment.
On-prem clients are discovered through `ownershipDiscovery` with
`ownershipDiscovery.mode: onprem-l2` and a configured `ownershipDiscovery.sources`
`type: arp-observer` on `ens21`; otherwise only the static owner `.10` is known.
The discovery scope excludes provider-primary private IPs so infrastructure
addresses on the same subnet are not advertised or trapped as mobility /32s.
It does not include an `EventSubscription` plugin path; that narrower Phase 3
mechanism remains documented in
`examples/event-federation/`.

## Prerequisites

1. Build and install `routerd`, `routerctl`, `routerd-eventd`, and the provider
   executor plugins on each router node.
2. Generate unique WireGuard keys per router. Do not commit private keys.
3. Create one shared HMAC secret for federation and install it on every router.
4. Prepare SSH access from the operator host to each router and client.
5. Grant each cloud router only the provider mutation permissions needed for its
   own NIC/VNIC. See `iam/` for least-privilege templates; harness permissions
   for VM lifecycle and deployment are separate.
6. Fill `env` from `env.example`.

## Usage

```sh
cd examples/cloudedge-mobility-demo
cp env.example env
$EDITOR env

./run-demo.sh
./collect-evidence.sh
./reset-lab.sh
```

Run `reset-lab.sh` after every demo run, even on failure.

OCI clean Ubuntu images do not provide the `oci` CLI. The demo therefore ships
`oci-routerd-helper`, a small Go SDK helper installed under
`/usr/local/libexec/routerd/plugins/oci-routerd-helper/bin/`, and the OCI
inventory/executor plugins use that helper with instance principal auth.

## Validation

These templates are intended to validate offline:

```sh
routerctl validate --config examples/cloudedge-mobility-demo/onprem.yaml
routerctl validate --config examples/cloudedge-mobility-demo/aws.yaml
routerctl validate --config examples/cloudedge-mobility-demo/azure.yaml
routerctl validate --config examples/cloudedge-mobility-demo/oci.yaml
```

Before committing local changes, grep the diff for real cloud identifiers,
public endpoints, private-key material, and filled HMAC secret values. Only
placeholder variable names should remain in these example files.

## References

- `docs/reference/selective-address-mobility.md`
- `docs/adr/0006-event-federation.md`
- `docs/adr/0007-provider-action-execution.md`
- `docs/adr/0008-capture-coordination-fencing.md`
- `docs/releases/evidence/cloudedge-mobility-d5-aws-maintenance-20260531.md`
