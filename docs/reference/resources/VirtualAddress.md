# VirtualAddress

[Resource configuration reference](../resources.md#virtualaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Maintain a static address or an elected floating address. Linux uses iproute2 for static addresses and keepalived for VRRP; FreeBSD uses ifconfig and CARP.

## Conditions and references

- family, interface and exactly one address/addressFrom are required. Literal IPv4 must be /32; IPv6 VRRP/CARP must be /128, while static IPv6 may retain a different prefix length. A bare address obtained from addressFrom is promoted to /32 or /128.
- interface must exist in the configuration interface index. IPv4StaticAddress and VirtualAddress cannot manage the same indexed address on one interface; using addressFrom=IPv4StaticAddress on that same interface is rejected. VRRP IDs must be unique per family/interface.
- addressFrom passes ordinary source/field validation but the address renderer reads declared IPv4StaticAddress.address or VirtualAddress.address only, not arbitrary runtime status. optional does not allow an empty final VIP; rendering/apply may report Error or Pending.
- mode=vrrp requires virtualRouterID and, on Linux, a nonempty unicast peer list. FreeBSD uses native CARP and does not use peers as a unicast delivery list. Omitted preempt disables preemption; CARP preemption is host-global and any true instance enables that sysctl.
- Failover VMACs follow the single VIP’s authoritative election, rather than holding separate VRRP elections. Linux-only VMAC helpers validate names/MAC/link-local values again when invoked; additionalFailoverVMACs has weaker YAML validation than the primary entry. A valid parent reference and installed helper are required at render/apply time.
- gracefulActivation is Linux IPv4 VRRP only. Election/VMAC activation can happen before publishing the VIP; readiness admits the VIP. Timeout reports failure while keeping the VIP withheld and retrying, never forcing publication. After a successfully published VIP, later readiness degradation does not withdraw it while still master.
- VRRP advertisement timing is derived (normally 1s) rather than configurable through advertInterval/preemptDelay; these legacy members are excluded from YAML/JSON schema. Protocol priority/authentication behavior still depends on keepalived or CARP.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: VirtualAddress
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # Required; string
    addressFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    externalDNS: true # optional; boolean
    family: "ipv4" # Required; string; enum=["ipv4", "ipv6"]
    gratuitousARP: true # optional; boolean
    hostname: "<string>" # optional; string
    interface: "<string>" # Required; string
    mode: "" # optional; string; enum=["", "static", "vrrp"]
    track: # optional; array
      - # object
        confirmConsecutiveHealthy: 1 # optional; integer; minimum=1; maximum=255
        confirmConsecutiveUnhealthy: 1 # optional; integer; minimum=1; maximum=255
        resource: "<string>" # Required; string
        unhealthyPenalty: 0 # optional; integer; minimum=0; maximum=254
    vrrp: # optional; object
      additionalFailoverVMACs: # optional; array
        - # object
          interface: "<string>" # Required; string
          linkLocalAddress: "<string>" # optional; string
          macAddress: "<string>" # Required; string
          parentInterface: "<string>" # Required; string
          withdrawRouterAdvertisement: true # optional; boolean
      authentication: "<string>" # optional; string
      authenticationFrom: # optional; object
        base64: true # optional; boolean
        env: "<string>" # optional; string
        file: "<string>" # optional; string
      failoverVMAC: # optional; object
        interface: "<string>" # Required; string
        linkLocalAddress: "<string>" # optional; string
        macAddress: "<string>" # Required; string
        parentInterface: "<string>" # Required; string
        withdrawRouterAdvertisement: true # optional; boolean
      gracefulActivation: # optional; object
        readyWhen: # Required; object
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
        timeout: "<string>" # optional; string
      peers: # optional; array
        - "<string>" # string
      preempt: true # optional; boolean
      priority: 1 # optional; integer; minimum=1; maximum=254
      virtualRouterID: 1 # Required; integer; minimum=1; maximum=255
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
| `spec.family` | Required ipv4 or ipv6; no default. Must match the address family. |
| `spec.interface` | Required bare declared logical interface name; resolves to the OS device. Not Kind/name. |
| `spec.address` | Literal prefix, excluding addressFrom. IPv4 /32; IPv6 /128 unless static mode. JSON Schema marks address required even though semantic validation permits addressFrom instead. |
| `spec.addressFrom` | Common resource/field/optional source, mutually exclusive with address. Effective renderer support is declared IPv4StaticAddress.address/VirtualAddress.address; use an explicit field. Nested VirtualAddress sources are not recursively resolved. |
| `spec.hostname` | Optional FQDN for automatic local DNS records in a served DNSZone and for status. Does not change OS hostname or create an external DNS record. |
| `spec.externalDNS` | Default false. True opts out of automatic local hostname records/DNS-coverage warnings; does not contact an external DNS provider. |
| `spec.mode` | static (default) or vrrp. static applies on this host without election; vrrp selects keepalived on Linux or CARP on FreeBSD. VRRP and tracking objects do not gate static ownership. |
| `spec.gratuitousARP` | Default false. Only Linux static IPv4 permits true. After adding/restoring a missing VIP it runs arping -U -c 3; normal reconciliation does not announce repeatedly. An announcement failure is reported and retried. |
| `spec.when` | Optional common resource activation condition, default unconditional. This filters the entire resource; separate from gracefulActivation.readyWhen’s admission gate. |
| `spec.vrrp` | Election settings used only by mode=vrrp. Static mode ignores them; not all nested values are validated when static. |
| `spec.vrrp.virtualRouterID` | Required 1–255 in vrrp mode; mapped to CARP vhid. Unique for family/interface in this Router; peers for the same election must agree. |
| `spec.vrrp.priority` | 1–254, omitted/0 means 100. Tracking penalties lower effective priority, clamped to at least 1. CARP converts priority to advskew=max(1,254-priority). |
| `spec.vrrp.preempt` | Omitted/false disables taking ownership back solely for higher priority. True enables preemption. CARP applies a host-global net.inet.carp.preempt, true if any configured instance requests true. |
| `spec.vrrp.peers` | Linux vrrp requires nonempty peer address/hostname list for unicast. Values are syntax-validated, deduplicated/sorted for rendering. FreeBSD CARP does not use this list as peers. |
| `spec.vrrp.authentication` | Optional plaintext authentication value, default absent. Excludes authenticationFrom. Linux emits keepalived auth_type PASS; CARP uses pass. This is protocol authentication, not encrypted VRRP traffic. |
| `spec.vrrp.authenticationFrom` | Optional secret source, excludes plaintext authentication. Exactly one file/env when configured. Read during rendering; missing file/env or invalid Base64 fails then. Empty resolved value omits authentication. |
| `spec.vrrp.authenticationFrom.file` | Secret file path on the router, mutually exclusive with env. Renderer strips trailing CR/LF, not all leading/trailing spaces. |
| `spec.vrrp.authenticationFrom.env` | Environment variable of the routerd process, mutually exclusive with file. Not a shell expansion or a remote environment variable. |
| `spec.vrrp.authenticationFrom.base64` | Default false; true decodes standard Base64 after trimming surrounding encoded whitespace, then strips trailing CR/LF from decoded bytes. |
| `spec.vrrp.failoverVMAC` | Optional Linux macvlan L2 identity controlled by this VRRP election; no independent election. Primary entry is validated in Linux VRRP mode; unavailable on FreeBSD. |
| `spec.vrrp.additionalFailoverVMACs` | Optional additional VMAC entries following the same role transition, default empty. Same field meanings as failoverVMAC. Current YAML validation does not fully validate these entries; the Linux helper/rendering still requires valid parents, interface names and Ethernet MACs. |
| `spec.vrrp.gracefulActivation` | Optional Linux IPv4 VRRP admission gate. No gate by default; other families/FreeBSD are rejected. Waits to publish VIP after election while preparing dependent state. |
| `spec.vrrp.gracefulActivation.readyWhen` | Required nonempty common state/all/any condition when gate is supplied. Resource references and predicates are validated. It gates initial publication, not ongoing withdrawal after successful publication. |
| `spec.vrrp.gracefulActivation.timeout` | Strictly positive Go duration, default 45s. Expiry changes activation status to Failed/ReadinessTimeout; it does not force VIP publication, stop retries or restart the router. |
| `spec.track` | Optional resource-health tracking list, default empty; vrrp only. Penalties add across unhealthy tracks and affect effective election priority. It does not directly remove static VIPs. |
| `spec.track[].resource` | Required Kind/name format; validator checks format, not referenced existence here. Reads that resource phase: common ready phases are healthy; BGPRouter/BGPPeer require Established. Disabled/Standby/NotApplicable and when-false are neutral and preserve the current penalty decision. |
| `spec.track[].unhealthyPenalty` | 0–254; omitted/0 means 50, so zero cannot disable a track penalty. Applied after unhealthy confirmation, removed after healthy confirmation. |
| `spec.track[].confirmConsecutiveUnhealthy` | Consecutive unhealthy evaluations before penalizing, omitted/0 means 3; explicit 1–255. Evaluations are reconciliation observations, not elapsed seconds. |
| `spec.track[].confirmConsecutiveHealthy` | Consecutive healthy evaluations to remove a penalty, omitted/0 means 2; explicit 1–255. Track decisions/counters can restore from saved status after restart. |
| `spec.vrrp.failoverVMAC.parentInterface` | Required configured logical parent interface, resolved to OS name during rendering. Not an arbitrary status source. |
| `spec.vrrp.additionalFailoverVMACs[].parentInterface` | Required configured logical parent interface, resolved to OS name during rendering. Not an arbitrary status source. |
| `spec.vrrp.failoverVMAC.interface` | Required Linux VMAC device name, at most 15 characters with valid interface syntax. Created/activated or deactivated by the role helper. |
| `spec.vrrp.additionalFailoverVMACs[].interface` | Required Linux VMAC device name, at most 15 characters with valid interface syntax. Created/activated or deactivated by the role helper. |
| `spec.vrrp.failoverVMAC.macAddress` | Required six-byte Ethernet MAC. Peers sharing the failover identity must configure the same value; not auto-derived from VRID. |
| `spec.vrrp.additionalFailoverVMACs[].macAddress` | Required six-byte Ethernet MAC. Peers sharing the failover identity must configure the same value; not auto-derived from VRID. |
| `spec.vrrp.failoverVMAC.linkLocalAddress` | Optional bare IPv6 link-local identity (fe80::/10), no prefix/zone suffix. The Linux helper validates it at invocation; omission leaves its normal link-local handling. |
| `spec.vrrp.additionalFailoverVMACs[].linkLocalAddress` | Optional bare IPv6 link-local identity (fe80::/10), no prefix/zone suffix. The Linux helper validates it at invocation; omission leaves its normal link-local handling. |
| `spec.vrrp.failoverVMAC.withdrawRouterAdvertisement` | Default false. With the multi-VMAC argument form, true sends a zero-router-lifetime RA before deactivation when a link-local source exists. Single-VMAC legacy argument form with no explicit linkLocalAddress does not pass this flag. |
| `spec.vrrp.additionalFailoverVMACs[].withdrawRouterAdvertisement` | Default false. With the multi-VMAC argument form, true sends a zero-router-lifetime RA before deactivation when a link-local source exists. Single-VMAC legacy argument form with no explicit linkLocalAddress does not pass this flag. |
| `spec.vrrp.gracefulActivation.readyWhen.state` | Optional state-key predicate map within readyWhen; all entries must match. Select exactly one nonempty state/all/any branch. Predicate fields (equals/in/contains/exists/status/for/maxAge) follow common fields; resource-style keys must reference declared resources and provided fields. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/controller/vrrp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/vrrp/controller.go) · [pkg/controller/vrrp/backend.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/vrrp/backend.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/render/vrrp.go](https://github.com/imksoo/routerd/blob/main/pkg/render/vrrp.go) · [pkg/render/carp.go](https://github.com/imksoo/routerd/blob/main/pkg/render/carp.go) · [pkg/render/status_source.go](https://github.com/imksoo/routerd/blob/main/pkg/render/status_source.go) · [pkg/render/secrets.go](https://github.com/imksoo/routerd/blob/main/pkg/render/secrets.go) · [cmd/routerd-vrrp-vmac/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-vrrp-vmac/main.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `address`, `ifname`, `role`, `hostname`, `virtualRouterID`, `priority`, `failoverVMACs`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
