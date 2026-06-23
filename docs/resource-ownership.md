---
title: Resource ownership
slug: /reference/resource-ownership
---

# Resource ownership and the apply model

routerd associates host-side artefacts with the resource that produced them. Recording who owns what makes diffs, deletions, and incident debugging tractable.

![Owner-reference lifecycle GC diagram showing desired resources, ledger rows, object statuses, host inventory, teardown contracts, skip protections, backups, and audit events](/img/diagrams/lifecycle-gc.png)

## Ownership categories

| Category | Meaning |
| --- | --- |
| Created | routerd produced the artefact itself. |
| Adopted | routerd took over an existing artefact and now manages it. |
| Observed | routerd only reads the state; it does not change it. |

The stable owner identity is `apiVersion/kind/name`. Apply generations are not
part of the owner key: a new generation can replace or remove artifacts owned by
the same resource without changing the ownership identity. Object status records
also carry owner metadata and a lifecycle class so stale-state cleanup can make
the same distinction as the apply path.

## Resource → host artefact map

| Resource | Host artefact |
| --- | --- |
| `Interface` | OS interface name and admin state |
| `DHCPv6PrefixDelegation` | `routerd-dhcpv6-client` socket, lease, events |
| `DHCPv4Client` | `routerd-dhcpv4-client` socket, lease, events |
| `PPPoESession` | `routerd-pppoe-client` socket, state, pppd/ppp config |
| `HealthCheck` | `routerd-healthcheck` socket, state, events |
| `DHCPv4Server` / `DHCPv6Server` / `IPv6RouterAdvertisement` | Managed dnsmasq configuration |
| `RogueRADetector` | Auto-derived `routerd-ra-observer` socket, passive IPv6 RA observations, rogue RA events |
| `DNSZone` | `routerd-dns-resolver` local authoritative zone |
| `DNSResolver` | `routerd-dns-resolver` socket, state, events, listener configuration |
| `DNSForwarder` | `routerd-dns-resolver` runtime forwarding rule derived into the resolver config |
| `DNSUpstream` | `routerd-dns-resolver` runtime upstream endpoint derived into forwarder rules |
| `DSLiteTunnel` | Linux `ip6tnl` interface |
| `TunnelInterface` | Linux `ipip`/`gre` tunnel device; FOU/GUE modes also ensure the matching `ip fou` listener port |
| `SAMTransportProfile` | A `DynamicConfigPart` containing generated `TunnelInterface`, endpoint `/32` `IPv4Route`, and `BGPPeer` resources |
| `MobilityPool` | Dynamic SAM capture/control-plane resources, BGP `/32` advertisements, provider action plans, and ownership observations |
| `RemoteAddressClaim` | Low-level SAM capture state, proxy-ARP sysctl/neighbor state, provider-secondary capture status, and resource-specific teardown |
| `IPAddressSet` | nftables IPv4/IPv6 named sets when referenced by a Linux renderer |
| `IPv4Route` | Kernel route |
| `ClusterNetworkRoute` | Generated `IPv4StaticRoute` intents for Pod / Service CIDRs through configured next hops |
| `NAT44Rule` | nftables `routerd_nat` table |
| `PortForward` / `IngressService` | Linux nftables `routerd_nat` / `routerd_filter` DNAT, optional hairpin SNAT, or FreeBSD `pf.conf` `rdr pass` / optional NAT reflection rules |
| `BGPRouter` / `BGPPeer` | Long-lived `routerd-bgp` daemon state controlled through local GoBGP gRPC; learned IPv4 best paths installed into the kernel FIB with routerd-owned protocol/metric values |
| `BFD` | GoBGP native BFD peer configuration and observed status for referenced GoBGP peers |
| `VirtualAddress` | Static VIP through `ip addr` or `ifconfig`; VRRP/VRRPv3 VIP ownership through keepalived on Linux or CARP on FreeBSD |
| `ObservabilityPipeline` | In-process routerd event exporter and generated OpenTelemetry environment for managed units |
| `RouterdCluster` | File lease under `spec.leasePath`; leader-only apply/controller mutation gate |
| `WireGuardInterface` / `WireGuardPeer` | WireGuard configuration |
| `TailscaleNode` | `routerd-tailscale-<name>` service unit/script and `tailscale up` arguments |
| `VRF` | Linux VRF device and routing table |
| `VXLANTunnel` | VXLAN device |
| `Package` | Optional package override; normal host package intents are derived from router resources |
| `Sysctl` | One sysctl value |
| `SysctlProfile` | A set of sysctl values |
| Derived host runtime | Kernel module load state and systemd-networkd / systemd-resolved drop-ins derived from router resources |
| `NTPClient` | NTP client configuration |

Linux nftables tables rendered by routerd carry a table comment marker
(`routerd.owner=routerd routerd.generation=1`). Doctor uses this marker, not a
name-prefix heuristic alone, when deciding whether a present `routerd_*` table
is stale relative to the current rendered config.

## Lifecycle contracts

Every config resource kind is declared in the lifecycle registry. The
declaration records the resource class and one teardown contract:

- `ArtifactKinds`: the resource records concrete host artifacts in the ownership
  ledger, and the generic artifact teardown registry knows how to remove those
  artifact kinds.
- `TeardownLifecycle: resource`: the resource has resource-specific teardown
  that runs from object status, for example kernel route removal, WireGuard
  adopted/external checks, or SAM proxy-ARP cleanup.
- `NoHostTeardownReason`: the resource is renderer input, external policy, or a
  dynamic source that does not own standalone host artifacts.

CI checks that every config kind has exactly one explicit contract and that each
declared artifact kind exists in the teardown registry. This is meant to prevent
new resources from silently bypassing cleanup.

## How removal works

routerd does **not** silently delete artefacts it does not own. When a resource
is removed from YAML, the GC planner compares the current effective resource set
with the ownership ledger, object status rows, and host inventory. It then builds
a dry-run-capable plan containing artifact removals, resource-specific teardown,
ledger forgets, stale status deletion, state backup, and audit events.

The desired set is the same effective view used by apply and serve:
`FilterRouterByWhen`, dynamic SAM resources, and `DynamicConfigPart` merges are
included. This prevents `when: false` resources from being treated as active
cleanup targets and prevents generated SAM tunnel/BGP/route resources from being
mistaken for orphans while their profile is still present.

Profile-derived resources clean up by normal ownership flow. For example,
deleting a `SAMTransportProfile` leaves an empty active dynamic part, generated
`TunnelInterface` / `BGPPeer` / endpoint-route resources disappear from the
effective config, and the generated resources' owners drive artifact teardown.

Destructive cleanup requires a state backup and records an event. Unsupported OS
integrations are non-destructive. Adopted or externally managed object statuses
are not torn down by resource lifecycle GC.

Full configuration rollback is not a current goal. For changes that affect production traffic, follow this order:

1. Validate.
2. Inspect the plan.
3. Run a dry-run apply.
4. Confirm the management connection survives the change.
5. Apply.
6. Verify state and connectivity.

## Legacy configurations

Older experimental DHCPv6 packages and renderers have been removed. The current DHCPv6-PD path is `routerd-dhcpv6-client`. Examples that referenced `dhcpcd` or `dhcp6c` directly are no longer part of the supported configuration set; the legacy resources have been retired without aliases.
