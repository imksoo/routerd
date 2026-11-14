# MobilityPool

[Resource configuration reference](../resources.md#mobilitypool) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

Combine shared SAM topology, local ownership observations and BGP/federation state to plan address mobility and local capture. The configured YAML is local intent; shared identity lives in SAMNodeSet.

## Conditions and references

- prefix must be IPv4; groupRef must resolve to a local EventGroup with nodeName, and that exact node must appear in required membersFrom topology. Every member needs nonempty site and onprem/cloud role. Only this local node may carry capture/discovery/static ownership overlays.
- onprem proxy-ARP requires capture.interface and explicit activeWhen single-router or vrrp-master. Provider capture requires providerRef and nicRef unless NIC is discovered; route-table additionally requires target.routeTableRef. Provider capture rejects activeWhen.
- Linux uses per-interface proxy-ARP controls; FreeBSD uses route-aware global proxyall handling rather than Linux sysctl names. Capture activation still depends on real ownership/readiness facts; a valid YAML or planned intent is not proof of a working extended segment.
- Profiles use nonzero merge semantics: empty strings/lists and false do not clear inherited fields. Source-address literal takes precedence over its status source. Maps merge with local entries winning; target literals then win over targetFrom. Profile use is cloud-only.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: MobilityPool
  metadata:
    name: "<name>"
  spec:
    groupRef: "<string>" # Required; string
    members: # optional; array
      - # object
        capture: # optional; object
          activeWhen: # optional; object
            type: "" # optional; string; enum=["", "single-router", "vrrp-master"]
            virtualAddressRef: "<string>" # optional; string
          captureStrategy: "" # optional; string; enum=["", "route-table"]
          excludeAddresses: # optional; array
            - "<string>" # string
          gratuitousARP: true # optional; boolean
          interface: "<string>" # optional; string
          nicRef: "<string>" # optional; string
          providerRef: "<string>" # optional; string
          sourceAddress: "<string>" # optional; string
          sourceAddressFrom: # optional; object
            field: "<string>" # optional; string
            optional: true # optional; boolean
            resource: "<string>" # Required; string
          target: # optional; object
            "<key>": "<string>" # optional; string
          targetFrom: # optional; object
            "<key>": "<string>" # optional; string
          type: "provider-secondary-ip" # optional; string; enum=["provider-secondary-ip", "proxy-arp"]
        nodeRef: "<string>" # Required; string
        ownershipDiscovery: # optional; object
          allowEmptyAfter: "<string>" # optional; string
          leaseTTL: "<string>" # optional; string
          mode: "" # optional; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
          pluginRef: "<string>" # optional; string
          providerRef: "<string>" # optional; string
          scanInterval: "<string>" # optional; string
          scope: # optional; object
            excludeAddresses: # optional; array
              - "<string>" # string
            includeAddresses: # optional; array
              - "<string>" # string
          selector: # optional; object
            tags: # optional; object
              "<key>": "<string>" # optional; string
          sources: # optional; array
            - # object
              bridge: "<string>" # optional; string
              interface: "<string>" # optional; string
              leaseTTL: "<string>" # optional; string
              network: "<string>" # optional; string
              probeRetries: 0 # optional; integer; minimum=0; maximum=20
              probeTimeout: "<string>" # optional; string
              resource: "<string>" # optional; string
              scanInterval: "<string>" # optional; string
              sourceAddressFrom: # optional; object
                field: "<string>" # optional; string
                optional: true # optional; boolean
                resource: "<string>" # Required; string
              type: "dhcpv4-lease" # Required; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
          stoppedInstancePolicy: "" # optional; string; enum=["", "hold", "release"]
          subnetRef: "<string>" # optional; string
          subnetRefFrom: "<string>" # optional; string
        profileRef: "<string>" # optional; string
        staticOwnedAddresses: # optional; array
          - "<string>" # string
    membersFrom: # Required; array; minItems=1
      - # object
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    prefix: "<string>" # Required; string
    profiles: # optional; object
      cloudCaptures: # optional; object
        "<key>": # optional; object
          capture: # optional; object
            activeWhen: # optional; object
              type: "" # optional; string; enum=["", "single-router", "vrrp-master"]
              virtualAddressRef: "<string>" # optional; string
            captureStrategy: "" # optional; string; enum=["", "route-table"]
            excludeAddresses: # optional; array
              - "<string>" # string
            gratuitousARP: true # optional; boolean
            interface: "<string>" # optional; string
            nicRef: "<string>" # optional; string
            providerRef: "<string>" # optional; string
            sourceAddress: "<string>" # optional; string
            sourceAddressFrom: # optional; object
              field: "<string>" # optional; string
              optional: true # optional; boolean
              resource: "<string>" # Required; string
            target: # optional; object
              "<key>": "<string>" # optional; string
            targetFrom: # optional; object
              "<key>": "<string>" # optional; string
            type: "provider-secondary-ip" # optional; string; enum=["provider-secondary-ip", "proxy-arp"]
          ownershipDiscovery: # optional; object
            allowEmptyAfter: "<string>" # optional; string
            leaseTTL: "<string>" # optional; string
            mode: "" # optional; string; enum=["", "disabled", "provider-private-ip", "onprem-l2"]
            pluginRef: "<string>" # optional; string
            providerRef: "<string>" # optional; string
            scanInterval: "<string>" # optional; string
            scope: # optional; object
              excludeAddresses: # optional; array
                - "<string>" # string
              includeAddresses: # optional; array
                - "<string>" # string
            selector: # optional; object
              tags: # optional; object
                "<key>": "<string>" # optional; string
            sources: # optional; array
              - # object
                bridge: "<string>" # optional; string
                interface: "<string>" # optional; string
                leaseTTL: "<string>" # optional; string
                network: "<string>" # optional; string
                probeRetries: 0 # optional; integer; minimum=0; maximum=20
                probeTimeout: "<string>" # optional; string
                resource: "<string>" # optional; string
                scanInterval: "<string>" # optional; string
                sourceAddressFrom: # optional; object
                  field: "<string>" # optional; string
                  optional: true # optional; boolean
                  resource: "<string>" # Required; string
                type: "dhcpv4-lease" # Required; string; enum=["dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet"]
            stoppedInstancePolicy: "" # optional; string; enum=["", "hold", "release"]
            subnetRef: "<string>" # optional; string
            subnetRefFrom: "<string>" # optional; string
    staticHandovers: # optional; array
      - # object
        address: "<string>" # Required; string
        fromNodeRef: "<string>" # Required; string
        toNodeRef: "<string>" # Required; string
    values: # optional; object
      "<key>": "<string>" # optional; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.prefix` | Required IPv4 CIDR managed by the pool. Ownership/capture addresses must stay inside it; no default prefix or address auto-allocation. |
| `spec.groupRef` | Required local EventGroup reference with nodeName. Its exact local node identity must exist in membersFrom topology; used for ownership and expiry events. |
| `spec.values` | Optional string constants for provider identifiers. Referenced by targetFrom/subnetRefFrom; no automatic variable expansion or secret storage. |
| `spec.profiles` | Optional reusable local capture/discovery defaults. Does not contain shared topology identities. |
| `spec.profiles.cloudCaptures` | Map keyed by profile name. Used only when a local cloud member selects profileRef; cannot enable onprem profile shorthand. Every profile target/value reference is normalized, including unused profiles. |
| `spec.membersFrom` | Required nonempty SAMNodeSet source list; sole source of member identity, site, role, placement, maintenance and capacity. No inline topology fallback from members overlays. |
| `spec.membersFrom[].resource` | Required SAMNodeSet/name. Imported nodeRefs must be distinct and each resolved member requires explicit site and onprem/cloud role. |
| `spec.membersFrom[].optional` | Default false. A missing nonoptional runtime source is Pending; true can tolerate it, but does not waive the requirement that the exact local EventGroup node resolve in the topology. |
| `spec.members` | Optional local-only overlays keyed by nodeRef already imported from membersFrom. Cannot set shared site/role/placement/maintenance/capacity or configure remote captures. |
| `spec.members[].nodeRef` | Required imported nodeRef selecting the EventGroup local member. This overlay does not introduce a new member. |
| `spec.members[].profileRef` | Optional key under profiles.cloudCaptures, supported only for role cloud. Missing profile fails normalization. Nonempty local strings/lists override profile defaults; maps merge, true booleans are additive, empty lists/false cannot clear inherited values. |
| `spec.members[].staticOwnedAddresses` | Optional explicit IPv4 /32 CIDRs inside prefix, onprem role only. Bare IPv4 is rejected here. Duplicate static ownership is rejected; these declarations do not depend on learned-client events. |
| `spec.staticHandovers` | Optional planned static ownership transfers. From-member release must be observed before projecting the target; not simultaneous dual ownership. |
| `spec.staticHandovers[].address` | Required IPv4 /32 CIDR within pool, unique across handovers. If declared as static-owned, its owner must equal fromNodeRef. |
| `spec.staticHandovers[].fromNodeRef` | Required existing imported onprem member nodeRef. Must differ from toNodeRef. |
| `spec.staticHandovers[].toNodeRef` | Required existing imported target member nodeRef, different from source. No target member is created implicitly. |
| `spec.members[].capture` | Local capture settings; type is explicit, not inferred from role. Only the local member may carry capture/discovery overlays. |
| `spec.members[].capture.type` | proxy-arp for role onprem, provider-secondary-ip for role cloud; empty means no capture mechanism. Local cloud member requires a resolved capture configuration. |
| `spec.members[].capture.providerRef` | CloudProviderProfile name required for provider-secondary-ip. Not a status source. Ownership discovery defaults its providerRef to this value. |
| `spec.members[].capture.captureStrategy` | Optional route-table only for provider-secondary-ip, requiring target.routeTableRef. Omit for normal secondary-IP or proxy-ARP; explicit secondary-ip/proxy-arp strategy values are rejected. |
| `spec.members[].capture.nicRef` | Provider NIC identifier, required for provider capture unless provider-private-ip discovery can resolve it. Do not place nicRef under target/targetFrom; that spelling is rejected. |
| `spec.members[].capture.interface` | Logical/OS capture interface, required for proxy-arp. Alias is resolved by consumers; no automatic LAN choice. |
| `spec.members[].capture.sourceAddress` | Optional local IPv4 sender address inside the pool for proxy-ARP/BGP delivery. Literal takes precedence over sourceAddressFrom, without an exclusivity check here. Lowered to a /32 address and preferred source when valid; not a remote owner address. |
| `spec.members[].capture.sourceAddressFrom` | Optional source-address status reference. Accepts valid pool-local IPv4 values; missing/invalid values remain unavailable. Unlike a literal, it does not generate IPv4StaticAddress because the source resource owns the address lifecycle. |
| `spec.members[].capture.excludeAddresses` | Optional pool-local IPv4 addresses/CIDRs excluded from proxy-ARP capture, default none. Use for local-only gateways/infrastructure; not a filter of all BGP routes. |
| `spec.members[].capture.gratuitousARP` | Default false. True requests unsolicited ARP announcements for capture changes. Profile inheritance is additive: a false local override does not clear a true profile setting. |
| `spec.members[].capture.activeWhen` | Required explicit authority for onprem proxy-arp; forbidden for provider-secondary-ip. Not the generic state/all/any predicate. |
| `spec.members[].capture.activeWhen.type` | single-router unconditionally authorizes local proxy-ARP; vrrp-master requires the referenced VirtualAddress MASTER state. No default for proxy-ARP. |
| `spec.members[].capture.activeWhen.virtualAddressRef` | Required existing local VirtualAddress name (or accepted VirtualAddress/name spelling) for vrrp-master, forbidden for single-router. Checked only for the local member. |
| `spec.members[].capture.target` | Optional nonsecret provider target map. Keys/values cannot be empty; secret/token/password/credential/private-key/access-key-like keys are rejected. Explicit target entries override targetFrom values. |
| `spec.members[].capture.targetFrom` | Optional map of provider target keys to spec.values keys. Missing/blank referenced constants fail normalization. nicRef is forbidden here; use capture.nicRef. |
| `spec.members[].capture.sourceAddressFrom.resource` | Source Kind/name; existing resource required by common reference validation. |
| `spec.members[].capture.sourceAddressFrom.field` | Explicit field provided by source kind, required when resource is set. |
| `spec.members[].capture.sourceAddressFrom.optional` | Default false; does not waive reference validation or create a usable sender when missing. |
| `spec.members[].ownershipDiscovery` | Local ownership-fact discovery settings, default disabled. Only the local member may configure these; accepted remote events are separate input. |
| `spec.members[].ownershipDiscovery.mode` | Empty/disabled does nothing. provider-private-ip requires cloud/provider-secondary-ip; onprem-l2 requires onprem/proxy-arp, capture.interface and nonempty sources. |
| `spec.members[].ownershipDiscovery.providerRef` | Provider inventory profile name, default capture.providerRef. Used by provider-private-ip, not an event resource reference. |
| `spec.members[].ownershipDiscovery.pluginRef` | Optional inventory Plugin name. Empty resolves by provider name or sole observe.providerPrivateIPs-capable plugin; ambiguity/missing capability fails discovery. |
| `spec.members[].ownershipDiscovery.subnetRef` | Optional provider subnet identifier. Explicit value overrides subnetRefFrom; empty lets the plugin infer from the self NIC where supported. |
| `spec.members[].ownershipDiscovery.subnetRefFrom` | Optional spec.values key, used only when subnetRef is empty. Missing/empty constant fails normalization; not Kind/name/field. |
| `spec.members[].ownershipDiscovery.scanInterval` | Provider scan interval, default 60s, explicit >=30s. Also validated for onprem-l2, but event-driven sources need no provider scan; observer polling has per-source scanInterval. |
| `spec.members[].ownershipDiscovery.leaseTTL` | Positive Go duration for emitted ownership events, default 5m. Per-source leaseTTL overrides it for onprem observations. Expiry is a protocol ownership lease, not a reboot deadline. |
| `spec.members[].ownershipDiscovery.stoppedInstancePolicy` | provider-private-ip only: hold/omitted retains stopped-instance ownership facts, release expires them. Forbidden for other modes when nonempty. |
| `spec.members[].ownershipDiscovery.allowEmptyAfter` | onprem-l2 only, positive duration after sources are armed before zero clients may count as complete; omitted leaves zero-client passive discovery pending. With profileRef, the merge currently does not copy the local override of this field; the profile value remains. |
| `spec.members[].ownershipDiscovery.sources` | Nonempty list required for onprem-l2; forbidden for provider-private-ip. Duplicate type/resource/effective-interface/network/bridge combinations are rejected. |
| `spec.members[].ownershipDiscovery.sources[].type` | Required dhcpv4-lease, arp-observer, on-demand-arp or pve-svnet. ARP/PVE observers derive local observer-daemon intents; DHCP facts are consumed from actual lease events. |
| `spec.members[].ownershipDiscovery.sources[].resource` | Optional source identity string, default empty. Used in deduplication/observer identity fingerprint, not as the observer process name or an existence-checked Kind/name reference; event matching primarily uses type/interface/network/bridge. |
| `spec.members[].ownershipDiscovery.sources[].interface` | Optional observation interface, default capture.interface. Required after fallback for on-demand-arp; consumers resolve declared aliases to OS names. |
| `spec.members[].ownershipDiscovery.sources[].bridge` | Optional local bridge label for PVE/svnet observation matching; default empty adds no bridge-selector restriction. Not a Bridge creation request. |
| `spec.members[].ownershipDiscovery.sources[].network` | Optional logical PVE/svnet network label, e.g. svnet1. Empty imposes no network-selector match; not an IP CIDR. |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom` | Optional status source for active ARP sender. If missing/unusable, reuses a verified capture source address; if neither resolves, no on-demand probe intent is emitted. |
| `spec.members[].ownershipDiscovery.sources[].scanInterval` | Optional observer poll interval, explicit >=1s, default daemon 10s for generated ARP/PVE observers. Event-driven DHCP sources ignore polling. |
| `spec.members[].ownershipDiscovery.sources[].probeTimeout` | Positive Go duration between active ARP retry attempts, default observer 1s. Only meaningful for on-demand probing. |
| `spec.members[].ownershipDiscovery.sources[].probeRetries` | Integer 0–20 additional ARP retries, default 0. Not total attempts; one initial attempt is separate. |
| `spec.members[].ownershipDiscovery.sources[].leaseTTL` | Optional positive event-lease duration overriding ownershipDiscovery.leaseTTL for this source. Omitted inherits the pool discovery lease (default 5m). |
| `spec.members[].ownershipDiscovery.scope` | Optional ownership observation address filters inside the pool. Not an authorization to advertise arbitrary external prefixes. |
| `spec.members[].ownershipDiscovery.scope.includeAddresses` | Optional IPv4 bare-address/CIDR allowlist contained in the pool; empty allows all candidate pool addresses. Active shard assignment can replace this provider-discovery include list. |
| `spec.members[].ownershipDiscovery.scope.excludeAddresses` | Optional pool-contained IPv4 bare-address/CIDR denylist, default none. Exclusions are applied after inclusion matching. |
| `spec.members[].ownershipDiscovery.selector` | Optional provider-inventory tag selector; does not configure cloud tags. |
| `spec.members[].ownershipDiscovery.selector.tags` | Nonempty tag keys with expected string values. All keys must match after trimming values; empty map imposes no tag filter. Used for provider inventory, not onprem event source matching. |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.resource` | Existing source Kind/name, required if a status source is used. |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.field` | Explicit field provided by the source kind, required with resource. |
| `spec.members[].ownershipDiscovery.sources[].sourceAddressFrom.optional` | Default false; does not waive reference validation or supply a sender address if unresolved. |
| `spec.profiles.cloudCaptures.<name>.capture` | Local capture settings; type is explicit, not inferred from role. Only the local member may carry capture/discovery overlays. |
| `spec.profiles.cloudCaptures.<name>.capture.type` | proxy-arp for role onprem, provider-secondary-ip for role cloud; empty means no capture mechanism. Local cloud member requires a resolved capture configuration. |
| `spec.profiles.cloudCaptures.<name>.capture.providerRef` | CloudProviderProfile name required for provider-secondary-ip. Not a status source. Ownership discovery defaults its providerRef to this value. |
| `spec.profiles.cloudCaptures.<name>.capture.captureStrategy` | Optional route-table only for provider-secondary-ip, requiring target.routeTableRef. Omit for normal secondary-IP or proxy-ARP; explicit secondary-ip/proxy-arp strategy values are rejected. |
| `spec.profiles.cloudCaptures.<name>.capture.nicRef` | Provider NIC identifier, required for provider capture unless provider-private-ip discovery can resolve it. Do not place nicRef under target/targetFrom; that spelling is rejected. |
| `spec.profiles.cloudCaptures.<name>.capture.interface` | Logical/OS capture interface, required for proxy-arp. Alias is resolved by consumers; no automatic LAN choice. |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddress` | Optional local IPv4 sender address inside the pool for proxy-ARP/BGP delivery. Literal takes precedence over sourceAddressFrom, without an exclusivity check here. Lowered to a /32 address and preferred source when valid; not a remote owner address. |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom` | Optional source-address status reference. Accepts valid pool-local IPv4 values; missing/invalid values remain unavailable. Unlike a literal, it does not generate IPv4StaticAddress because the source resource owns the address lifecycle. |
| `spec.profiles.cloudCaptures.<name>.capture.excludeAddresses` | Optional pool-local IPv4 addresses/CIDRs excluded from proxy-ARP capture, default none. Use for local-only gateways/infrastructure; not a filter of all BGP routes. |
| `spec.profiles.cloudCaptures.<name>.capture.gratuitousARP` | Default false. True requests unsolicited ARP announcements for capture changes. Profile inheritance is additive: a false local override does not clear a true profile setting. |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen` | Required explicit authority for onprem proxy-arp; forbidden for provider-secondary-ip. Not the generic state/all/any predicate. |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen.type` | single-router unconditionally authorizes local proxy-ARP; vrrp-master requires the referenced VirtualAddress MASTER state. No default for proxy-ARP. |
| `spec.profiles.cloudCaptures.<name>.capture.activeWhen.virtualAddressRef` | Required existing local VirtualAddress name (or accepted VirtualAddress/name spelling) for vrrp-master, forbidden for single-router. Checked only for the local member. |
| `spec.profiles.cloudCaptures.<name>.capture.target` | Optional nonsecret provider target map. Keys/values cannot be empty; secret/token/password/credential/private-key/access-key-like keys are rejected. Explicit target entries override targetFrom values. |
| `spec.profiles.cloudCaptures.<name>.capture.targetFrom` | Optional map of provider target keys to spec.values keys. Missing/blank referenced constants fail normalization. nicRef is forbidden here; use capture.nicRef. |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.resource` | Source Kind/name; existing resource required by common reference validation. |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.field` | Explicit field provided by source kind, required when resource is set. |
| `spec.profiles.cloudCaptures.<name>.capture.sourceAddressFrom.optional` | Default false; does not waive reference validation or create a usable sender when missing. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery` | Local ownership-fact discovery settings, default disabled. Only the local member may configure these; accepted remote events are separate input. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.mode` | Empty/disabled does nothing. provider-private-ip requires cloud/provider-secondary-ip; onprem-l2 requires onprem/proxy-arp, capture.interface and nonempty sources. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.providerRef` | Provider inventory profile name, default capture.providerRef. Used by provider-private-ip, not an event resource reference. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.pluginRef` | Optional inventory Plugin name. Empty resolves by provider name or sole observe.providerPrivateIPs-capable plugin; ambiguity/missing capability fails discovery. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.subnetRef` | Optional provider subnet identifier. Explicit value overrides subnetRefFrom; empty lets the plugin infer from the self NIC where supported. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.subnetRefFrom` | Optional spec.values key, used only when subnetRef is empty. Missing/empty constant fails normalization; not Kind/name/field. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scanInterval` | Provider scan interval, default 60s, explicit >=30s. Also validated for onprem-l2, but event-driven sources need no provider scan; observer polling has per-source scanInterval. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.leaseTTL` | Positive Go duration for emitted ownership events, default 5m. Per-source leaseTTL overrides it for onprem observations. Expiry is a protocol ownership lease, not a reboot deadline. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.stoppedInstancePolicy` | provider-private-ip only: hold/omitted retains stopped-instance ownership facts, release expires them. Forbidden for other modes when nonempty. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.allowEmptyAfter` | onprem-l2 only, positive duration after sources are armed before zero clients may count as complete; omitted leaves zero-client passive discovery pending. With profileRef, the merge currently does not copy the local override of this field; the profile value remains. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources` | Nonempty list required for onprem-l2; forbidden for provider-private-ip. Duplicate type/resource/effective-interface/network/bridge combinations are rejected. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].type` | Required dhcpv4-lease, arp-observer, on-demand-arp or pve-svnet. ARP/PVE observers derive local observer-daemon intents; DHCP facts are consumed from actual lease events. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].resource` | Optional source identity string, default empty. Used in deduplication/observer identity fingerprint, not as the observer process name or an existence-checked Kind/name reference; event matching primarily uses type/interface/network/bridge. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].interface` | Optional observation interface, default capture.interface. Required after fallback for on-demand-arp; consumers resolve declared aliases to OS names. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].bridge` | Optional local bridge label for PVE/svnet observation matching; default empty adds no bridge-selector restriction. Not a Bridge creation request. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].network` | Optional logical PVE/svnet network label, e.g. svnet1. Empty imposes no network-selector match; not an IP CIDR. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom` | Optional status source for active ARP sender. If missing/unusable, reuses a verified capture source address; if neither resolves, no on-demand probe intent is emitted. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].scanInterval` | Optional observer poll interval, explicit >=1s, default daemon 10s for generated ARP/PVE observers. Event-driven DHCP sources ignore polling. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].probeTimeout` | Positive Go duration between active ARP retry attempts, default observer 1s. Only meaningful for on-demand probing. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].probeRetries` | Integer 0–20 additional ARP retries, default 0. Not total attempts; one initial attempt is separate. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].leaseTTL` | Optional positive event-lease duration overriding ownershipDiscovery.leaseTTL for this source. Omitted inherits the pool discovery lease (default 5m). |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope` | Optional ownership observation address filters inside the pool. Not an authorization to advertise arbitrary external prefixes. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope.includeAddresses` | Optional IPv4 bare-address/CIDR allowlist contained in the pool; empty allows all candidate pool addresses. Active shard assignment can replace this provider-discovery include list. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.scope.excludeAddresses` | Optional pool-contained IPv4 bare-address/CIDR denylist, default none. Exclusions are applied after inclusion matching. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.selector` | Optional provider-inventory tag selector; does not configure cloud tags. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.selector.tags` | Nonempty tag keys with expected string values. All keys must match after trimming values; empty map imposes no tag filter. Used for provider inventory, not onprem event source matching. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.resource` | Existing source Kind/name, required if a status source is used. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.field` | Explicit field provided by the source kind, required with resource. |
| `spec.profiles.cloudCaptures.<name>.ownershipDiscovery.sources[].sourceAddressFrom.optional` | Default false; does not waive reference validation or supply a sender address if unresolved. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/mobilityconfig/members.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/members.go) · [pkg/mobilityconfig/normalize.go](https://github.com/imksoo/routerd/blob/main/pkg/mobilityconfig/normalize.go) · [pkg/controller/mobility/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/controller.go) · [pkg/controller/mobility/controller_effects.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/controller_effects.go) · [pkg/controller/mobility/bgp_delivery_planner.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/bgp_delivery_planner.go) · [pkg/controller/mobility/discovery.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery.go) · [pkg/controller/mobility/discovery_support.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery_support.go) · [pkg/controller/mobility/discovery_facts.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/discovery_facts.go) · [pkg/controller/mobility/arp_observer_intent.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/arp_observer_intent.go) · [pkg/controller/mobility/pool_plan.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/pool_plan.go) · [pkg/controller/mobility/shard_controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/shard_controller.go) · [pkg/controller/chain/sam.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sam.go) · [pkg/controller/chain/sam_freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/sam_freebsd.go) · [cmd/routerd-arp-observer/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-arp-observer/main.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `groupRef`, `prefix`, `reason`, `generatedBGPPaths`, `resolvedMemberCount`, `pendingSources`, `placementGroup`, `placementActive`, `placementActiveNode`, `providerActionPhase`, `providerActionError`, `providerActionFailedAddresses`, `providerObservationPendingAddresses`, `ownershipResolverControlPlaneOwnerTable`

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
