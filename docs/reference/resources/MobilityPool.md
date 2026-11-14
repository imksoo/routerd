# MobilityPool

[Resource configuration reference](../resources.md#mobilitypool) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

Combine shared SAM topology, local ownership observations and BGP/federation state to plan address mobility and local capture. The configured YAML is local intent; shared identity lives in SAMNodeSet.

## Conditions and references

- prefix must be IPv4; groupRef must resolve to a local EventGroup with nodeName, and that exact node must appear in required membersFrom topology. Every member needs nonempty site and onprem/cloud role. Only this local node may carry capture/discovery/static ownership overlays.
- onprem proxy-ARP requires capture.interface and explicit activeWhen single-router or vrrp-master. Provider capture requires providerRef and nicRef unless NIC is discovered; route-table additionally requires target.routeTableRef. Provider capture rejects activeWhen.
- Linux uses per-interface proxy-ARP controls; FreeBSD uses route-aware global proxyall handling rather than Linux sysctl names. Capture activation still depends on real ownership/readiness facts; a valid YAML or planned intent is not proof of a working extended segment.
- Profiles use nonzero merge semantics: empty strings/lists and false do not clear inherited fields. Source-address literal takes precedence over its status source. Maps merge with local entries winning; target literals then win over targetFrom. Profile use is cloud-only.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: MobilityPool
  metadata:
    name: "<name>"
  spec:
    groupRef: <string>
    [ members:
        -
          [ capture:
              [ activeWhen:
                  [ type: ("" | "single-router" | "vrrp-master") ]
                  [ virtualAddressRef: <string> ]
              ]
              [ captureStrategy: ("" | "route-table") ]
              [ excludeAddresses:
                  - <string>
              ]
              [ gratuitousARP: <boolean> ]
              [ interface: <string> ]
              [ nicRef: <string> ]
              [ providerRef: <string> ]
              [ sourceAddress: <string> ]
              [ sourceAddressFrom:
                  field: <string>
                  [ optional: <boolean> ]
                  resource: <string>
              ]
              [ target:
                  [ <name>: <string> ]
              ]
              [ targetFrom:
                  [ <name>: <string> ]
              ]
              [ type: ("provider-secondary-ip" | "proxy-arp") ]
          ]
          nodeRef: <string>
          [ ownershipDiscovery:
              [ allowEmptyAfter: <string> ]
              [ leaseTTL: <string> ]
              [ mode: ("" | "disabled" | "provider-private-ip" | "onprem-l2") ]
              [ pluginRef: <string> ]
              [ providerRef: <string> ]
              [ scanInterval: <string> ]
              [ scope:
                  [ excludeAddresses:
                      - <string>
                  ]
                  [ includeAddresses:
                      - <string>
                  ]
              ]
              [ selector:
                  [ tags:
                      [ <name>: <string> ]
                  ]
              ]
              [ sources:
                  -
                    [ bridge: <string> ]
                    [ interface: <string> ]
                    [ leaseTTL: <string> ]
                    [ network: <string> ]
                    [ probeRetries: <integer> ]
                    [ probeTimeout: <string> ]
                    [ resource: <string> ]
                    [ scanInterval: <string> ]
                    [ sourceAddressFrom:
                        field: <string>
                        [ optional: <boolean> ]
                        resource: <string>
                    ]
                    type: ("dhcpv4-lease" | "arp-observer" | "on-demand-arp" | "pve-svnet")
              ]
              [ stoppedInstancePolicy: ("" | "hold" | "release") ]
              [ subnetRef: <string> ]
              [ subnetRefFrom: <string> ]
          ]
          [ profileRef: <string> ]
          [ staticOwnedAddresses:
              - <string>
          ]
    ]
    membersFrom:
      -
        [ optional: <boolean> ]
        resource: <string>
    prefix: <string>
    [ profiles:
        [ cloudCaptures:
            [ <name>:
                [ capture:
                    [ activeWhen:
                        [ type: ("" | "single-router" | "vrrp-master") ]
                        [ virtualAddressRef: <string> ]
                    ]
                    [ captureStrategy: ("" | "route-table") ]
                    [ excludeAddresses:
                        - <string>
                    ]
                    [ gratuitousARP: <boolean> ]
                    [ interface: <string> ]
                    [ nicRef: <string> ]
                    [ providerRef: <string> ]
                    [ sourceAddress: <string> ]
                    [ sourceAddressFrom:
                        field: <string>
                        [ optional: <boolean> ]
                        resource: <string>
                    ]
                    [ target:
                        [ <name>: <string> ]
                    ]
                    [ targetFrom:
                        [ <name>: <string> ]
                    ]
                    [ type: ("provider-secondary-ip" | "proxy-arp") ]
                ]
                [ ownershipDiscovery:
                    [ allowEmptyAfter: <string> ]
                    [ leaseTTL: <string> ]
                    [ mode: ("" | "disabled" | "provider-private-ip" | "onprem-l2") ]
                    [ pluginRef: <string> ]
                    [ providerRef: <string> ]
                    [ scanInterval: <string> ]
                    [ scope:
                        [ excludeAddresses:
                            - <string>
                        ]
                        [ includeAddresses:
                            - <string>
                        ]
                    ]
                    [ selector:
                        [ tags:
                            [ <name>: <string> ]
                        ]
                    ]
                    [ sources:
                        -
                          [ bridge: <string> ]
                          [ interface: <string> ]
                          [ leaseTTL: <string> ]
                          [ network: <string> ]
                          [ probeRetries: <integer> ]
                          [ probeTimeout: <string> ]
                          [ resource: <string> ]
                          [ scanInterval: <string> ]
                          [ sourceAddressFrom:
                              field: <string>
                              [ optional: <boolean> ]
                              resource: <string>
                          ]
                          type: ("dhcpv4-lease" | "arp-observer" | "on-demand-arp" | "pve-svnet")
                    ]
                    [ stoppedInstancePolicy: ("" | "hold" | "release") ]
                    [ subnetRef: <string> ]
                    [ subnetRefFrom: <string> ]
                ]
            ]
        ]
    ]
    [ staticHandovers:
        -
          address: <string>
          fromNodeRef: <string>
          toNodeRef: <string>
    ]
    [ values:
        [ <name>: <string> ]
    ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.prefix` | **Required** | string | Required IPv4 CIDR managed by the pool. Ownership/capture addresses must stay inside it; no default prefix or address auto-allocation. |
| `spec.groupRef` | **Required** | string | Required local EventGroup reference with nodeName. Its exact local node identity must exist in membersFrom topology; used for ownership and expiry events. |
| `spec.values` | *Optional* | object | Optional string constants for provider identifiers. Referenced by targetFrom/subnetRefFrom; no automatic variable expansion or secret storage. |
| `spec.profiles` | *Optional* | object | Optional reusable local capture/discovery defaults. Does not contain shared topology identities. |
| `spec.profiles.cloudCaptures` | *Optional* | object | Map keyed by profile name. Used only when a local cloud member selects profileRef; cannot enable onprem profile shorthand. Every profile target/value reference is normalized, including unused profiles. |
| `spec.membersFrom` | **Required** | array; minItems=1 | Required nonempty SAMNodeSet source list; sole source of member identity, site, role, placement, maintenance and capacity. No inline topology fallback from members overlays. |
| `spec.membersFrom[].resource` | **Required** | string | Required SAMNodeSet/name. Imported nodeRefs must be distinct and each resolved member requires explicit site and onprem/cloud role. |
| `spec.membersFrom[].optional` | *Optional* | boolean | Default false. A missing nonoptional runtime source is Pending; true can tolerate it, but does not waive the requirement that the exact local EventGroup node resolve in the topology. |
| `spec.members` | *Optional* | array | Optional local-only overlays keyed by nodeRef already imported from membersFrom. Cannot set shared site/role/placement/maintenance/capacity or configure remote captures. |
| `spec.members[].nodeRef` | **Conditionally required** | string | Required imported nodeRef selecting the EventGroup local member. This overlay does not introduce a new member. |
| `spec.members[].profileRef` | *Optional* | string | Optional key under profiles.cloudCaptures, supported only for role cloud. Missing profile fails normalization. Nonempty local strings/lists override profile defaults; maps merge, true booleans are additive, empty lists/false cannot clear inherited values. |
| `spec.members[].staticOwnedAddresses` | *Optional* | array | Optional explicit IPv4 /32 CIDRs inside prefix, onprem role only. Bare IPv4 is rejected here. Duplicate static ownership is rejected; these declarations do not depend on learned-client events. |
| `spec.staticHandovers` | *Optional* | array | Optional planned static ownership transfers. From-member release must be observed before projecting the target; not simultaneous dual ownership. |
| `spec.staticHandovers[].address` | **Conditionally required** | string | Required IPv4 /32 CIDR within pool, unique across handovers. If declared as static-owned, its owner must equal fromNodeRef. |
| `spec.staticHandovers[].fromNodeRef` | **Conditionally required** | string | Required existing imported onprem member nodeRef. Must differ from toNodeRef. |
| `spec.staticHandovers[].toNodeRef` | **Conditionally required** | string | Required existing imported target member nodeRef, different from source. No target member is created implicitly. |
| `spec.members[].capture` | *Optional* | object | Local capture settings; type is explicit, not inferred from role. Only the local member may carry capture/discovery overlays. |
| `spec.members[].capture.type` | *Optional* | string; enum=["provider-secondary-ip", "proxy-arp"] | proxy-arp for role onprem, provider-secondary-ip for role cloud; empty means no capture mechanism. Local cloud member requires a resolved capture configuration. |
| `spec.members[].capture.providerRef` | *Optional* | string | CloudProviderProfile name required for provider-secondary-ip. Not a status source. Ownership discovery defaults its providerRef to this value. |
| `spec.members[].capture.captureStrategy` | *Optional* | string; enum=["", "route-table"] | Optional route-table only for provider-secondary-ip, requiring target.routeTableRef. Omit for normal secondary-IP or proxy-ARP; explicit secondary-ip/proxy-arp strategy values are rejected. |
| `spec.members[].capture.nicRef` | *Optional* | string | Provider NIC identifier, required for provider capture unless provider-private-ip discovery can resolve it. Do not place nicRef under target/targetFrom; that spelling is rejected. |
| `spec.members[].capture.interface` | *Optional* | string | Logical/OS capture interface, required for proxy-arp. Alias is resolved by consumers; no automatic LAN choice. |
| `spec.members[].capture.sourceAddress` | *Optional* | string | Optional local IPv4 sender address inside the pool for proxy-ARP/BGP delivery. Literal takes precedence over sourceAddressFrom, without an exclusivity check here. Lowered to a /32 address and preferred source when valid; not a remote owner address. |
| `spec.members[].capture.sourceAddressFrom` | *Optional* | object | Optional source-address status reference. Accepts valid pool-local IPv4 values; missing/invalid values remain unavailable. Unlike a literal, it does not generate IPv4StaticAddress because the source resource owns the address lifecycle. |
| `spec.members[].capture.excludeAddresses` | *Optional* | array | Optional pool-local IPv4 addresses/CIDRs excluded from proxy-ARP capture, default none. Use for local-only gateways/infrastructure; not a filter of all BGP routes. |
| `spec.members[].capture.gratuitousARP` | *Optional* | boolean | Default false. True requests unsolicited ARP announcements for capture changes. Profile inheritance is additive: a false local override does not clear a true profile setting. |
| `spec.members[].capture.activeWhen` | *Optional* | object | Required explicit authority for onprem proxy-arp; forbidden for provider-secondary-ip. Not the generic state/all/any predicate. |
| `spec.members[].capture.activeWhen.type` | *Optional* | string; enum=["", "single-router", "vrrp-master"] | single-router unconditionally authorizes local proxy-ARP; vrrp-master requires the referenced VirtualAddress MASTER state. No default for proxy-ARP. |
| `spec.members[].capture.activeWhen.virtualAddressRef` | *Optional* | string | Required existing local VirtualAddress name (or accepted VirtualAddress/name spelling) for vrrp-master, forbidden for single-router. Checked only for the local member. |
| `spec.members[].capture.target` | *Optional* | object | Optional nonsecret provider target map. Keys/values cannot be empty; secret/token/password/credential/private-key/access-key-like keys are rejected. Explicit target entries override targetFrom values. |
| `spec.members[].capture.targetFrom` | *Optional* | object | Optional map of provider target keys to spec.values keys. Missing/blank referenced constants fail normalization. nicRef is forbidden here; use capture.nicRef. |
| `spec.members[].capture.sourceAddressFrom.resource` | **Conditionally required** | string | Source Kind/name; existing resource required by common reference validation. |
| `spec.members[].capture.sourceAddressFrom.field` | **Conditionally required** | string | Explicit field provided by source kind, required when resource is set. |
| `spec.members[].capture.sourceAddressFrom.optional` | *Optional* | boolean | Default false; does not waive reference validation or create a usable sender when missing. |
| `spec.members[].ownershipDiscovery` | *Optional* | object | Local ownership-fact discovery settings, default disabled. Only the local member may configure these; accepted remote events are separate input. |
| `spec.members[].ownershipDiscovery.mode` | *Optional* | string; enum=["", "disabled", "provider-private-ip", "onprem-l2"] | Empty/disabled does nothing. provider-private-ip requires cloud/provider-secondary-ip; onprem-l2 requires onprem/proxy-arp, capture.interface and nonempty sources. |
| `spec.members[].ownershipDiscovery.providerRef` | *Optional* | string | Provider inventory profile name, default capture.providerRef. Used by provider-private-ip, not an event resource reference. |
| `spec.members[].ownershipDiscovery.pluginRef` | *Optional* | string | Optional inventory Plugin name. Empty resolves by provider name or sole observe.providerPrivateIPs-capable plugin; ambiguity/missing capability fails discovery. |
| `spec.members[].ownershipDiscovery.subnetRef` | *Optional* | string | Optional provider subnet identifier. Explicit value overrides subnetRefFrom; empty lets the plugin infer from the self NIC where supported. |
| `spec.members[].ownershipDiscovery.subnetRefFrom` | *Optional* | string | Optional spec.values key, used only when subnetRef is empty. Missing/empty constant fails normalization; not Kind/name/field. |
| `spec.members[].ownershipDiscovery.scanInterval` | *Optional* | string | Provider scan interval, default 60s, explicit >=30s. Also validated for onprem-l2, but event-driven sources need no provider scan; observer polling has per-source scanInterval. |
| `spec.members[].ownershipDiscovery.leaseTTL` | *Optional* | string | Positive Go duration for emitted ownership events, default 5m. Per-source leaseTTL overrides it for onprem observations. Expiry is a protocol ownership lease, not a reboot deadline. |
| `spec.members[].ownershipDiscovery.stoppedInstancePolicy` | *Optional* | string; enum=["", "hold", "release"] | provider-private-ip only: hold/omitted retains stopped-instance ownership facts, release expires them. Forbidden for other modes when nonempty. |
| `spec.members[].ownershipDiscovery.allowEmptyAfter` | *Optional* | string | onprem-l2 only, positive duration after sources are armed before zero clients may count as complete; omitted leaves zero-client passive discovery pending. With profileRef, the merge currently does not copy the local override of this field; the profile value remains. |
| `spec.members[].ownershipDiscovery.sources` | *Optional* | array | Nonempty list required for onprem-l2; forbidden for provider-private-ip. Duplicate type/resource/effective-interface/network/bridge combinations are rejected. |
| `spec.members[].ownershipDiscovery.sources[].type` | **Conditionally required** | string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"] | Required dhcpv4-lease, arp-observer, on-demand-arp or pve-svnet. ARP/PVE observers derive local observer-daemon intents; DHCP facts are consumed from actual lease events. |
| `spec.members[].ownershipDiscovery.sources[].resource` | *Optional* | string | Optional source identity string, default empty. Used in deduplication/observer identity fingerprint, not as the observer process name or an existence-checked Kind/name reference; event matching primarily uses type/interface/network/bridge. |
| `spec.members[].ownershipDiscovery.sources[].interface` | *Optional* | string | Optional observation interface, default capture.interface. Required after fallback for on-demand-arp; consumers resolve declared aliases to OS names. |
| `spec.members[].ownershipDiscovery.sources[].bridge` | *Optional* | string | Optional local bridge label for PVE/svnet observation matching; default empty adds no bridge-selector restriction. Not a Bridge creation request. |
| `spec.members[].ownershipDiscovery.sources[].network` | *Optional* | string | Optional logical PVE/svnet network label, e.g. svnet1. Empty imposes no network-selector match; not an IP CIDR. |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom` | *Optional* | object | Optional status source for active ARP sender. If missing/unusable, reuses a verified capture source address; if neither resolves, no on-demand probe intent is emitted. |
| `spec.members[].ownershipDiscovery.sources[].scanInterval` | *Optional* | string | Optional observer poll interval, explicit >=1s, default daemon 10s for generated ARP/PVE observers. Event-driven DHCP sources ignore polling. |
| `spec.members[].ownershipDiscovery.sources[].probeTimeout` | *Optional* | string | Positive Go duration between active ARP retry attempts, default observer 1s. Only meaningful for on-demand probing. |
| `spec.members[].ownershipDiscovery.sources[].probeRetries` | *Optional* | integer; minimum=0; maximum=20 | Integer 0–20 additional ARP retries, default 0. Not total attempts; one initial attempt is separate. |
| `spec.members[].ownershipDiscovery.sources[].leaseTTL` | *Optional* | string | Optional positive event-lease duration overriding ownershipDiscovery.leaseTTL for this source. Omitted inherits the pool discovery lease (default 5m). |
| `spec.members[].ownershipDiscovery.scope` | *Optional* | object | Optional ownership observation address filters inside the pool. Not an authorization to advertise arbitrary external prefixes. |
| `spec.members[].ownershipDiscovery.scope.includeAddresses` | *Optional* | array | Optional IPv4 bare-address/CIDR allowlist contained in the pool; empty allows all candidate pool addresses. Active shard assignment can replace this provider-discovery include list. |
| `spec.members[].ownershipDiscovery.scope.excludeAddresses` | *Optional* | array | Optional pool-contained IPv4 bare-address/CIDR denylist, default none. Exclusions are applied after inclusion matching. |
| `spec.members[].ownershipDiscovery.selector` | *Optional* | object | Optional provider-inventory tag selector; does not configure cloud tags. |
| `spec.members[].ownershipDiscovery.selector.tags` | *Optional* | object | Nonempty tag keys with expected string values. All keys must match after trimming values; empty map imposes no tag filter. Used for provider inventory, not onprem event source matching. |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.resource` | **Conditionally required** | string | Existing source Kind/name, required if a status source is used. |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.field` | **Conditionally required** | string | Explicit field provided by the source kind, required with resource. |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.optional` | *Optional* | boolean | Default false; does not waive reference validation or supply a sender address if unresolved. |
| `spec.profiles.cloudCaptures.<name>.capture` | *Optional* | object | Local capture settings; type is explicit, not inferred from role. Only the local member may carry capture/discovery overlays. |
| `spec.profiles.cloudCaptures.<name>.capture.type` | *Optional* | string; enum=["provider-secondary-ip", "proxy-arp"] | proxy-arp for role onprem, provider-secondary-ip for role cloud; empty means no capture mechanism. Local cloud member requires a resolved capture configuration. |
| `spec.profiles.cloudCaptures.<name>.capture.providerRef` | *Optional* | string | CloudProviderProfile name required for provider-secondary-ip. Not a status source. Ownership discovery defaults its providerRef to this value. |
| `spec.profiles.cloudCaptures.<name>.capture.captureStrategy` | *Optional* | string; enum=["", "route-table"] | Optional route-table only for provider-secondary-ip, requiring target.routeTableRef. Omit for normal secondary-IP or proxy-ARP; explicit secondary-ip/proxy-arp strategy values are rejected. |
| `spec.profiles.cloudCaptures.<name>.capture.nicRef` | *Optional* | string | Provider NIC identifier, required for provider capture unless provider-private-ip discovery can resolve it. Do not place nicRef under target/targetFrom; that spelling is rejected. |
| `spec.profiles.cloudCaptures.<name>.capture.interface` | *Optional* | string | Logical/OS capture interface, required for proxy-arp. Alias is resolved by consumers; no automatic LAN choice. |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddress` | *Optional* | string | Optional local IPv4 sender address inside the pool for proxy-ARP/BGP delivery. Literal takes precedence over sourceAddressFrom, without an exclusivity check here. Lowered to a /32 address and preferred source when valid; not a remote owner address. |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom` | *Optional* | object | Optional source-address status reference. Accepts valid pool-local IPv4 values; missing/invalid values remain unavailable. Unlike a literal, it does not generate IPv4StaticAddress because the source resource owns the address lifecycle. |
| `spec.profiles.cloudCaptures.<name>.capture.excludeAddresses` | *Optional* | array | Optional pool-local IPv4 addresses/CIDRs excluded from proxy-ARP capture, default none. Use for local-only gateways/infrastructure; not a filter of all BGP routes. |
| `spec.profiles.cloudCaptures.<name>.capture.gratuitousARP` | *Optional* | boolean | Default false. True requests unsolicited ARP announcements for capture changes. Profile inheritance is additive: a false local override does not clear a true profile setting. |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen` | *Optional* | object | Required explicit authority for onprem proxy-arp; forbidden for provider-secondary-ip. Not the generic state/all/any predicate. |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen.type` | *Optional* | string; enum=["", "single-router", "vrrp-master"] | single-router unconditionally authorizes local proxy-ARP; vrrp-master requires the referenced VirtualAddress MASTER state. No default for proxy-ARP. |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen.virtualAddressRef` | *Optional* | string | Required existing local VirtualAddress name (or accepted VirtualAddress/name spelling) for vrrp-master, forbidden for single-router. Checked only for the local member. |
| `spec.profiles.cloudCaptures.<name>.capture.target` | *Optional* | object | Optional nonsecret provider target map. Keys/values cannot be empty; secret/token/password/credential/private-key/access-key-like keys are rejected. Explicit target entries override targetFrom values. |
| `spec.profiles.cloudCaptures.<name>.capture.targetFrom` | *Optional* | object | Optional map of provider target keys to spec.values keys. Missing/blank referenced constants fail normalization. nicRef is forbidden here; use capture.nicRef. |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.resource` | **Conditionally required** | string | Source Kind/name; existing resource required by common reference validation. |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.field` | **Conditionally required** | string | Explicit field provided by source kind, required when resource is set. |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.optional` | *Optional* | boolean | Default false; does not waive reference validation or create a usable sender when missing. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery` | *Optional* | object | Local ownership-fact discovery settings, default disabled. Only the local member may configure these; accepted remote events are separate input. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.mode` | *Optional* | string; enum=["", "disabled", "provider-private-ip", "onprem-l2"] | Empty/disabled does nothing. provider-private-ip requires cloud/provider-secondary-ip; onprem-l2 requires onprem/proxy-arp, capture.interface and nonempty sources. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.providerRef` | *Optional* | string | Provider inventory profile name, default capture.providerRef. Used by provider-private-ip, not an event resource reference. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.pluginRef` | *Optional* | string | Optional inventory Plugin name. Empty resolves by provider name or sole observe.providerPrivateIPs-capable plugin; ambiguity/missing capability fails discovery. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.subnetRef` | *Optional* | string | Optional provider subnet identifier. Explicit value overrides subnetRefFrom; empty lets the plugin infer from the self NIC where supported. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.subnetRefFrom` | *Optional* | string | Optional spec.values key, used only when subnetRef is empty. Missing/empty constant fails normalization; not Kind/name/field. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scanInterval` | *Optional* | string | Provider scan interval, default 60s, explicit >=30s. Also validated for onprem-l2, but event-driven sources need no provider scan; observer polling has per-source scanInterval. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.leaseTTL` | *Optional* | string | Positive Go duration for emitted ownership events, default 5m. Per-source leaseTTL overrides it for onprem observations. Expiry is a protocol ownership lease, not a reboot deadline. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.stoppedInstancePolicy` | *Optional* | string; enum=["", "hold", "release"] | provider-private-ip only: hold/omitted retains stopped-instance ownership facts, release expires them. Forbidden for other modes when nonempty. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.allowEmptyAfter` | *Optional* | string | onprem-l2 only, positive duration after sources are armed before zero clients may count as complete; omitted leaves zero-client passive discovery pending. With profileRef, the merge currently does not copy the local override of this field; the profile value remains. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources` | *Optional* | array | Nonempty list required for onprem-l2; forbidden for provider-private-ip. Duplicate type/resource/effective-interface/network/bridge combinations are rejected. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].type` | **Conditionally required** | string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"] | Required dhcpv4-lease, arp-observer, on-demand-arp or pve-svnet. ARP/PVE observers derive local observer-daemon intents; DHCP facts are consumed from actual lease events. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].resource` | *Optional* | string | Optional source identity string, default empty. Used in deduplication/observer identity fingerprint, not as the observer process name or an existence-checked Kind/name reference; event matching primarily uses type/interface/network/bridge. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].interface` | *Optional* | string | Optional observation interface, default capture.interface. Required after fallback for on-demand-arp; consumers resolve declared aliases to OS names. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].bridge` | *Optional* | string | Optional local bridge label for PVE/svnet observation matching; default empty adds no bridge-selector restriction. Not a Bridge creation request. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].network` | *Optional* | string | Optional logical PVE/svnet network label, e.g. svnet1. Empty imposes no network-selector match; not an IP CIDR. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom` | *Optional* | object | Optional status source for active ARP sender. If missing/unusable, reuses a verified capture source address; if neither resolves, no on-demand probe intent is emitted. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].scanInterval` | *Optional* | string | Optional observer poll interval, explicit >=1s, default daemon 10s for generated ARP/PVE observers. Event-driven DHCP sources ignore polling. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].probeTimeout` | *Optional* | string | Positive Go duration between active ARP retry attempts, default observer 1s. Only meaningful for on-demand probing. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].probeRetries` | *Optional* | integer; minimum=0; maximum=20 | Integer 0–20 additional ARP retries, default 0. Not total attempts; one initial attempt is separate. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].leaseTTL` | *Optional* | string | Optional positive event-lease duration overriding ownershipDiscovery.leaseTTL for this source. Omitted inherits the pool discovery lease (default 5m). |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope` | *Optional* | object | Optional ownership observation address filters inside the pool. Not an authorization to advertise arbitrary external prefixes. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope.includeAddresses` | *Optional* | array | Optional IPv4 bare-address/CIDR allowlist contained in the pool; empty allows all candidate pool addresses. Active shard assignment can replace this provider-discovery include list. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope.excludeAddresses` | *Optional* | array | Optional pool-contained IPv4 bare-address/CIDR denylist, default none. Exclusions are applied after inclusion matching. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.selector` | *Optional* | object | Optional provider-inventory tag selector; does not configure cloud tags. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.selector.tags` | *Optional* | object | Nonempty tag keys with expected string values. All keys must match after trimming values; empty map imposes no tag filter. Used for provider inventory, not onprem event source matching. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.resource` | **Conditionally required** | string | Existing source Kind/name, required if a status source is used. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.field` | **Conditionally required** | string | Explicit field provided by the source kind, required with resource. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.optional` | *Optional* | boolean | Default false; does not waive reference validation or supply a sender address if unresolved. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/mobilityconfig/members.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/members.go) · [pkg/mobilityconfig/normalize.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/normalize.go) · [pkg/controller/mobility/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/controller.go) · [pkg/controller/mobility/controller_effects.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/controller_effects.go) · [pkg/controller/mobility/bgp_delivery_planner.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/bgp_delivery_planner.go) · [pkg/controller/mobility/discovery.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery.go) · [pkg/controller/mobility/discovery_support.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery_support.go) · [pkg/controller/mobility/discovery_facts.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery_facts.go) · [pkg/controller/mobility/arp_observer_intent.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/arp_observer_intent.go) · [pkg/controller/mobility/pool_plan.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/pool_plan.go) · [pkg/controller/mobility/shard_controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/shard_controller.go) · [pkg/controller/chain/sam.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sam.go) · [pkg/controller/chain/sam_freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sam_freebsd.go) · [cmd/routerd-arp-observer/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-arp-observer/main.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `groupRef`, `prefix`, `reason`, `generatedBGPPaths`, `resolvedMemberCount`, `pendingSources`, `placementGroup`, `placementActive`, `placementActiveNode`, `providerActionPhase`, `providerActionError`, `providerActionFailedAddresses`, `providerObservationPendingAddresses`, `ownershipResolverControlPlaneOwnerTable`

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
