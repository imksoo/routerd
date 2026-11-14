# EgressRoutePolicy

[Resource configuration reference](../resources.md#egressroutepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Select or program egress routing; the actual behavior depends on explicit mode and hostTraffic.

## Conditions and references

- Choose the path explicitly: omitted mode only publishes advisory selection; Linux priority/mark/hash program IPv4 route tables and nftables, while hostTraffic true is a separate local IPv6 path. General IPv6 forwarded-policy support is not implemented by these IPv4 writers despite schema acceptance.
- For useful direct Linux steering, provide interface/device or deviceFrom and a positive table, priority and mark together. The validator permits all three numbers to remain zero, but the live direct-table installer then skips it; Applied/selection status alone is not proof of packet steering. Linux table routes use metric 50 when the configured metric is zero.
- priority chooses the highest-weight ready candidate, with lower priority/name/input-order tie breaks. Fresh health, actual device presence, declared dependencies on direct candidates and DS-Lite Up checks participate; a target-pool candidate needs at least one healthy present target but its candidate dependsOn is not evaluated by that priority availability branch. No ready candidate yields Pending/NoReadyCandidates and removes new-flow steering, not necessarily all probe routes or established states.
- mark mode writes each eligible direct candidate mark in resource/candidate order; overlapping matches can be overwritten by later rules. It does not choose one highest-weight candidate. hash/priority target pools use address-based jhash, a fixed 256-bucket rendezvous map and conntrack marks; target weights are not configurable. Existing flows can retain their marks across changes.
- Common device/gateway references require existing resources and explicit provided fields. Gateway modes do not discover DHCP gateways automatically: supply gatewayFrom. Literal aliases table/routeTable and routeMetric/metric cannot both be nonzero. Candidate targets exclude direct device/gateway/table/mark/metric fields; targets are valid only with explicit hash or priority, and need hashFields even though validation only mandates it for hash.
- Linux target table setup checks actual link presence and skips missing targets. The later hash-map readiness copy checks health for normal non-DS-Lite targets but does not repeat that link-presence check; therefore a configured absent non-DS-Lite device can still receive new hash buckets. Verify target links as well as health before relying on distribution.
- FreeBSD supports only mode hash, family ipv4, hashFields: [sourceAddress], nonempty sourceCIDRs, one enabled candidate and at least two distinct static IPv4 interface/gateway targets. Omit selection/hysteresis and all candidate direct/dynamic/health/dependency fields; target table/priority/mark/metric must be 0 and target health/dynamic gateway forbidden. Destination-set refs are forbidden, at most one excluded CIDR is allowed, and includes/excludes cannot coexist. PF uses route-to round-robin sticky-address, not Linux deterministic hashing.
- The FreeBSD renderer rejects mixing this route-to shape with explicit FirewallRule, generated firewall holes, FirewallEventLog or ClientPolicy. If FirewallZone filtering is present, a broad accepted forwarding path is required. Syntax validation alone does not prove the final PF ruleset can be rendered alongside other resources.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EgressRoutePolicy
  metadata:
    name: "<name>"
  spec:
    candidates: # Required; array
      - # object
        dependsOn: # optional; array
          - # object
            equals: "<string>" # optional; string
            field: "<string>" # optional; string
            notEmpty: true # optional; boolean
            optional: true # optional; boolean
            phase: "<string>" # optional; string
            resource: "<string>" # Required; string
        device: "<string>" # optional; string
        deviceFrom: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        enabled: true # optional; boolean
        gateway: "<string>" # optional; string
        gatewayFrom: # optional; object
          field: "<string>" # optional; string
          optional: true # optional; boolean
          resource: "<string>" # Required; string
        gatewaySource: "" # optional; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
        healthCheck: "<string>" # optional; string
        interface: "<string>" # optional; string
        mark: 0 # optional; integer; minimum=0; maximum=4294967295
        metric: 0 # optional; integer; minimum=0
        name: "<string>" # optional; string
        preferredSource: "" # optional; string; enum=["", "interface"]
        priority: 0 # optional; integer; minimum=0; maximum=32765
        routeMetric: 0 # optional; integer; minimum=0
        routeTable: 0 # optional; integer; minimum=0; maximum=4294967295
        source: "<string>" # optional; string
        table: 0 # optional; integer; minimum=0; maximum=4294967295
        targets: # optional; array
          - # object
            gateway: "<string>" # optional; string
            gatewayFrom: # optional; object
              field: "<string>" # optional; string
              optional: true # optional; boolean
              resource: "<string>" # Required; string
            gatewaySource: "" # optional; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
            healthCheck: "<string>" # optional; string
            interface: "<string>" # optional; string
            mark: 0 # optional; integer; minimum=0; maximum=4294967295
            metric: 0 # optional; integer; minimum=0
            name: "<string>" # optional; string
            outboundInterface: "<string>" # optional; string
            priority: 0 # optional; integer; minimum=0; maximum=32765
            routeMetric: 0 # optional; integer; minimum=0
            routeTable: 0 # optional; integer; minimum=0; maximum=4294967295
            table: 0 # optional; integer; minimum=0; maximum=4294967295
        weight: 0 # optional; integer; minimum=0
        when: # optional; object
          state: # optional; object
            "<key>": # optional; object
              contains: "<string>" # optional; string
              equals: "<string>" # optional; string
              exists: true # optional; boolean
              for: "<string>" # optional; string
              in: # optional; array
                - "<string>" # string
              maxAge: "<string>" # optional; string
              status: "set" # optional; string; enum=["set", "unset", "unknown"]
    destinationCIDRs: # optional; array
      - "<string>" # string
    destinationSetRefs: # optional; array
      - "<string>" # string
    excludeDestinationCIDRs: # optional; array
      - "<string>" # string
    excludeDestinationSetRefs: # optional; array
      - "<string>" # string
    family: "ipv4" # optional; string; enum=["ipv4", "ipv6"]
    hashFields: # optional; array
      - "<string>" # string
    hostTraffic: true # optional; boolean
    hysteresis: "<string>" # optional; string
    mode: "priority" # optional; string; enum=["priority", "mark", "hash"]
    selection: "highest-weight-ready" # optional; string; enum=["highest-weight-ready", "weighted-ecmp"]
    sourceCIDRs: # optional; array
      - "<string>" # string
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.family` | Default ipv4. Schema/validation accept ipv6, but current Linux forwarded mark/priority/hash implementation is IPv4-specific. IPv6 has a separate hostTraffic path; do not infer general IPv6 forwarding parity. FreeBSD subset requires ipv4. |
| `spec.hostTraffic` | Default false. Linux true requires family ipv6 and exactly one candidate with preferredSource: interface, literal static gateway, mark 0 and no targets. Installs an iif lo policy for all local traffic, not source/destination CIDR matches; absent device/address keeps the previous known-good host rule, while first acquisition remains Pending. Candidate health/dependency/selection fields are not evaluated by this separate host-policy builder. |
| `spec.mode` | Omitted/empty is advisory status selection only; it does not install route tables. Explicit priority selects one ready candidate for new-flow marking; mark emits direct packet marks; hash maps flows to candidate targets. Set mode explicitly for Linux forwarding. FreeBSD accepts only its restricted hash shape. |
| `spec.sourceCIDRs` | Optional family-matching source prefixes, default any source. Lists form alternatives. FreeBSD hash subset requires a nonempty IPv4 list; hostTraffic ignores these selectors. |
| `spec.destinationCIDRs` | Optional family-matching destination prefixes, default all destinations when no destinationSetRefs. Combined with destinationSetRefs as alternatives in mark/hash rendering. priority default-mark rendering consumes only CIDR selectors, not address-set refs. |
| `spec.destinationSetRefs` | Optional existing IPAddressSet/<name> references; duplicates rejected. Used by Linux mark/hash policy matches; priority default-mark selection does not apply these refs, and FreeBSD forbids them. Empty adds no match alternative. |
| `spec.excludeDestinationCIDRs` | Optional family-matching destination exclusions, default empty; all exclusions must be avoided. IPv4 /0 exclusion produces no steering matches. FreeBSD allows at most one and forbids combining it with nonempty destinationCIDRs. |
| `spec.excludeDestinationSetRefs` | Optional existing IPAddressSet/<name> exclusions; duplicates rejected. Used in Linux mark/hash policy matches, ignored by priority default-mark rendering and forbidden on FreeBSD. |
| `spec.hashFields` | Ordered list of sourceAddress and/or destinationAddress; required for mode hash and also needed at rendering whenever candidates have targets, including priority target pools. Linux jhash uses these addresses, not ports, with a resource-derived stable seed and 256 rendezvous-mapped buckets. FreeBSD requires exactly [sourceAddress] but implements round-robin sticky-address instead of jhash. |
| `spec.selection` | Default highest-weight-ready: ready candidates sort by descending weight, ascending priority, lexical effective name, then input order. weighted-ecmp is accepted by schema but unsupported for advisory/priority selection (Pending/UnsupportedSelection). Not the hash target algorithm; mark/hash do not use this selection knob. FreeBSD requires empty. |
| `spec.hysteresis` | Go duration, default 30s only in omitted-mode advisory selection. During this period after a change, an otherwise ready previous winner can be retained; an unready winner is not held. Zero/negative duration removes the delay. Explicit priority/mark/hash do not use it; FreeBSD requires empty. |
| `spec.candidates` | Required nonempty list; disabled entries are still structurally validated. Linux can mix direct candidates and target-pool candidates in priority mode. Candidate names are not uniqueness-checked, so choose explicit unique names to avoid ambiguous status/hash identities. |
| `spec.when` | Optional common state condition, default unconditional. A false condition removes the policy from the effective configuration; candidate conditions are filtered separately. Unresolved conditions can remain indeterminate rather than a guaranteed stop. |
| `spec.candidates[].name` | Optional explicit identity. Advisories fall back to source then candidate-N (zero-based); explicit priority status falls back to interface/device then targets, while unnamed hash pools use input index. Use unique names for stable identity. |
| `spec.candidates[].enabled` | Default true. false excludes the candidate from selection/rendering, but does not bypass configuration/reference validation or independently guarantee that all established connections or route-table entries disappear. |
| `spec.candidates[].source` | Optional advisory readiness resource as Kind/name; it does not automatically supply device or gateway. This string is not checked for target existence by common typed-reference validation. Advisory source phases Applied/Bound/Healthy/Installed/Ready/Running/Up qualify; a fresh passing health check can substitute for an unready source. Explicit routing specially guards referenced DS-Lite phase Up, but does not generally check other source kinds. |
| `spec.candidates[].interface` | Optional declared logical interface for explicit Linux routing; takes precedence over device. Supplied interface is validated against the Router interface/DS-Lite indexes. Omitted-mode advisory output does not use this alias: use device/deviceFrom there. Forbidden when targets is nonempty. |
| `spec.candidates[].device` | Optional literal device fallback. Explicit routing uses interface first; advisory uses device directly. No common existence check for this string, but actual link must exist for live installation/selection. ${...} was removed; use deviceFrom. Forbidden with targets. |
| `spec.candidates[].deviceFrom` | Optional common resource/field status source, validated against existing resources/provided fields. First normalized value wins over interface/device; missing values fall back to literals. optional does not create a device. No targets coexistence. |
| `spec.candidates[].gateway` | Literal bare address matching family. Requires gatewaySource: static and is exclusive with gatewayFrom in that mode; DHCP gateway modes prohibit a literal. Default empty. Forbidden on target-pool candidates. |
| `spec.candidates[].gatewayFrom` | Optional common existing-resource/provided-field source. static requires exactly one of gateway/gatewayFrom; DHCP modes may use this source but do not discover a gateway from the interface automatically. In advisory mode any configured unresolved source makes output unready even when optional true. Explicit route installation errors on missing required static/DHCP gateway; optional is not a general bypass. Forbidden with targets. |
| `spec.candidates[].gatewaySource` | Default none: device-only default route and gateway fields prohibited. static requires exactly one literal/source; dhcpv4 requires IPv4, no literal and a usable explicitly supplied gatewayFrom value. dhcpv6 is accepted only with IPv6 but unsupported by the normal IPv4 route-table installer; the separate hostTraffic path requires static. Forbidden on pool candidates. |
| `spec.candidates[].preferredSource` | Empty or interface. Required interface for hostTraffic, which chooses the first global non-tentative/non-deprecated IPv6 address on the candidate interface. Not used by normal IPv4 forwarding or target pools. |
| `spec.candidates[].table` | Linux route-table number; schema 0–4294967295, default 0/unconfigured. Nonzero is exclusive with nonzero routeTable. Useful direct steering needs a positive table plus positive priority and mark (hostTraffic mark remains 0). Forbidden when targets exist; FreeBSD requires 0. |
| `spec.candidates[].routeTable` | Alias used only when table is 0; both nonzero rejected. Same range and requirements as table. |
| `spec.candidates[].priority` | Linux ip rule priority 1–32765 when any direct mark/table/priority is configured; default 0 is not a useful programmed rule. Also a lower-is-better tie breaker for candidate selection; a pool candidate may use it without setting direct table/mark. FreeBSD requires 0. |
| `spec.candidates[].mark` | Linux packet/fwmark identifier, schema 0–4294967295. Direct forwarding requires positive mark together with table/priority; default 0 causes direct route-table installation to be skipped. hostTraffic requires 0. Pool candidates and FreeBSD prohibit nonzero direct mark. |
| `spec.candidates[].routeMetric` | Nonnegative preferred route-metric field. A nonzero value is exclusive with nonzero metric; effective 0 becomes 50 in Linux table installation, including hostTraffic. Advisory status may still report configured 0. Forbidden with targets; FreeBSD requires 0. |
| `spec.candidates[].metric` | Alias fallback when routeMetric is 0; nonzero aliases cannot coexist. Same effective default 50 for programmed Linux routes. |
| `spec.candidates[].weight` | Nonnegative integer default 0. Larger wins advisory/priority candidate selection; not a percentage, ECMP weight or per-target hash weight. mark/hash do not proportionally distribute using this field. FreeBSD requires 0. |
| `spec.candidates[].healthCheck` | Optional bare existing HealthCheck name; empty means no health gate. Selection requires fresh Healthy or tolerated Failing status (below unhealthyThreshold), with lastCheckedAt no older than max(2m,3×interval+timeout). Route tables may still be installed for a check probing through this same mark even while health is unready; that bootstrap is distinct from traffic eligibility. HostTraffic ignores this field; FreeBSD prohibits it. |
| `spec.candidates[].targets` | Optional nonempty target pool, allowed only in mode hash or priority. Exclusive with direct interface/device/deviceFrom/gateway/gatewayFrom/gatewaySource/table/routeTable/mark/routeMetric/metric. Linux requires per-target positive table/priority/mark and hashFields; FreeBSD requires a single candidate with at least two distinct static routehosts and no Linux routing numbers. |
| `spec.candidates[].dependsOn` | Optional common AND-dependencies, default empty; ordinary resource/field checks apply. Evaluated for advisory and direct explicit routing, and candidate filtering in mark/hash. Current priority target-pool availability does not evaluate candidate dependsOn; hostTraffic ignores it. Prefer when for a whole candidate stop condition. FreeBSD prohibits dependencies. |
| `spec.candidates[].when` | Optional common state condition, default unconditional. False removes candidate before controller selection; unresolved values may remain indeterminate and therefore retained. Disabled candidates are also removed. FreeBSD static subset prohibits candidate conditions. |
| `spec.candidates[].targets[].name` | Optional target identity, default empty. Participates in stable hash identity together with effective interface, mark and table; changing these can redistribute new flows. Names/mark/priority/table uniqueness are not generally validated on Linux. |
| `spec.candidates[].targets[].interface` | Target logical interface, required unless outboundInterface is supplied. Validated against declared interface/DS-Lite indexes. On Linux outboundInterface wins if both are set; FreeBSD rejects both being nonempty. |
| `spec.candidates[].targets[].outboundInterface` | Preferred alias for interface; same reference checks. Required together with a usable OS interface for actual delivery; does not create a tunnel. |
| `spec.candidates[].targets[].gateway` | Optional literal matching family; requires gatewaySource static and excludes gatewayFrom. DHCP gateway modes prohibit literal values. FreeBSD target requires this bare static IPv4 value. |
| `spec.candidates[].targets[].gatewayFrom` | Optional common resource/field source; existence/provided-field validation applies even when optional true. static requires exactly one literal/source. The Linux reader uses the first value; optional is not a missing-gateway bypass. FreeBSD prohibits dynamic gateway fields entirely. |
| `spec.candidates[].targets[].gatewaySource` | Default none, prohibiting gateway/gatewayFrom and installing a device-only route. static requires exactly one gateway/gatewayFrom. dhcpv4/6 require corresponding family and no literal, but do not discover a gateway automatically: use gatewayFrom. The normal live installer supports only none/static/dhcpv4; FreeBSD requires explicit static. |
| `spec.candidates[].targets[].table` | Linux requires positive table for every target (schema maximum 4294967295), exclusive with nonzero routeTable. No automatic allocation or reserved-table/uniqueness check. FreeBSD requires omitted/0 and uses PF route-to instead. |
| `spec.candidates[].targets[].routeTable` | Alias used when table is 0. Both aliases nonzero rejected; same Linux-positive / FreeBSD-zero requirement. |
| `spec.candidates[].targets[].priority` | Linux target ip rule priority required 1–32765; no default allocation. Distinct from candidate selection priority. FreeBSD requires omitted/0. |
| `spec.candidates[].targets[].mark` | Linux target fwmark required >0 (schema maximum 4294967295). Used by the hash bucket map and ip rule. No automatic allocation or collision detection; FreeBSD requires omitted/0. |
| `spec.candidates[].targets[].routeMetric` | Nonnegative Linux route metric, preferred over metric; both nonzero rejected. Effective omitted/0 becomes 50 at installation. FreeBSD requires omitted/0. |
| `spec.candidates[].targets[].metric` | Alias fallback when routeMetric is 0; same requirements and effective Linux default 50. |
| `spec.candidates[].targets[].healthCheck` | Optional bare existing HealthCheck name, default no health gate; uses the same freshness/threshold check as candidate health. A failed target is excluded from new hash maps. Existing conntrack marks are restored rather than globally terminated; route tables can remain for same-mark health probes. FreeBSD forbids this field. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/egressroute/selection.go](https://github.com/imksoo/routerd/blob/main/pkg/egressroute/selection.go) · [pkg/controller/chain/policyroute.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/policyroute.go) · [pkg/controller/chain/phase_status.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase_status.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `family`, `selectedCandidate`, `selectedSource`, `selectedDevice`, `selectedGateway`, `selectedGatewaySource`, `selectedRouteTable`, `selectedMetric`, `selectedWeight`, `selectedTargets`, `selectedInterface`, `candidates`, `role`, `advisory`, `lastTransitionAt`, `datapathRoleResource`, `datapathRole`, `datapathRoleTransitionAt`, `datapathReady`, `datapathState`, `datapathReadyAt`, `datapathReadyDuration`, `datapathReadyDurationMillis`, `datapathFallbackBehavior`, `newFlowBehavior`, `updatedAt`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
