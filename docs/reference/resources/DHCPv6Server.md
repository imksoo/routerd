# DHCPv6Server

[Resource configuration reference](../resources.md#dhcpv6server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Provide LAN DHCPv6 address/information service and associated RA behavior using the shared dnsmasq process.

## Conditions and references

- The current live writer differs from the older static renderer: interface is needed, delegatedAddress-only does not create a live range, dnsSource/selfAddressPolicy do not supply self DNS, and ra-only follows the stateless branch. Use explicit fields documented below and verify actual DHCP/RA traffic.
- Normal validation limits delegatedAddress mode to stateless/ra-only. Without delegatedAddress, every non-stateless mode including ra-only requires a valid IPv6 start/end pair. domainSearch and domainSearchFrom are exclusive; literals and DNS/SNTP value sources may coexist.
- One dnsmasq instance serves the resources; mode constructors use a /64 on the actual interface. Applied records successful reconciliation, not a client lease, RA reception or default-route installation. Removed ready_when is rejected.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: "<name>"
  spec:
    [ addressPool:
        [ end: <string> ]
        [ leaseTime: <string> ]
        [ start: <string> ]
    ]
    [ configPath: <string> ]
    [ defaultRoute: <boolean> ]
    [ delegatedAddress: <string> ]
    [ dependsOn:
        -
          [ equals: <string> ]
          [ field: <string> ]
          [ notEmpty: <boolean> ]
          [ optional: <boolean> ]
          [ phase: <string> ]
          resource: <string>
    ]
    [ dnsServerFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ dnsServers:
        - <string>
    ]
    [ dnsSource: ("self" | "static" | "none") ]
    [ domainSearch:
        - <string>
    ]
    [ domainSearchFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ interface: <string> ]
    [ leaseFile: <string> ]
    [ leaseTime: <string> ]
    [ listenInterfaces:
        - <string>
    ]
    [ logDHCP: <boolean> ]
    [ managed: <boolean> ]
    [ mode: ("stateless" | "stateful" | "both" | "ra-only") ]
    [ pidFile: <string> ]
    [ rapidCommit: <boolean> ]
    [ role: ("server" | "transit") ]
    [ selfAddressPolicy: <string> ]
    [ server: ("dnsmasq") ]
    [ sntpServerFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ sntpServers:
        - <string>
    ]
    [ stickyHoldDays: <integer> ]
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.server` | *Optional* | string; enum=["dnsmasq"] | Optional dnsmasq, default dnsmasq. No other backend accepted. The live service is shared with DHCPv4 and RA resources. |
| `spec.managed` | *Optional* | boolean | Boolean default false; current live service writer/controller does not use this flag to disable mutation. |
| `spec.role` | *Optional* | string; enum=["server", "transit"] | server/transit, default server in validation/static rendering. Current live writer does not honor transit to suppress service. |
| `spec.listenInterfaces` | *Optional* | array | Optional declared logical interfaces, all validated for existence. If delegatedAddress is given and this list is nonempty, it must include that address resource interface. Used in family/firewall planning; current live pool writer still requires interface itself. |
| `spec.logDHCP` | *Optional* | boolean | Default false; static renderer can emit log-dhcp, but current live writer does not consume it. |
| `spec.stickyHoldDays` | *Optional* | integer; minimum=0 | Nonnegative days, default 0/off. Positive dnsmasq values retain lease assignments in dhcp-sticky.db for reuse; runtime uses the maximum across IPv6 servers for all IPv6 lease events, not per-interface selection. DHCPv6ServerLeaseSync does not copy this sticky DB. |
| `spec.interface` | **Conditionally required** | string | Declared logical LAN interface. Required for current live writer even when validation permits delegatedAddress alone; an absent interface skips live pool rendering. |
| `spec.delegatedAddress` | **Conditionally required** | string | Optional bare existing IPv6DelegatedAddress name. Static renderer derives interface/prefix/self DNS from it, but current live writer uses interface and constructor directly and does not substitute this resource. When set, validation limits mode to stateless or ra-only. |
| `spec.mode` | *Optional* | string; enum=["stateless", "stateful", "both", "ra-only"] | Default stateless. stateful emits a constructor range; both adds slaac; current live writer treats ra-only through the same ra-stateless default branch, so it does not actually disable DHCPv6 information service. Without delegatedAddress, validation also requires a pool for ra-only. |
| `spec.addressPool` | **Conditionally required** | object | IPv6 stateful allocation range. Required for non-stateless modes when delegatedAddress is absent; not used as an allocation range by stateless. No endpoints inferred. |
| `spec.addressPool.start` | **Conditionally required** | string | IPv6 start address, inclusive and <= end. Live stateful range uses constructor:<ifname> and prefix length 64. |
| `spec.addressPool.end` | **Conditionally required** | string | IPv6 end address, inclusive and >= start; paired with start. |
| `spec.addressPool.leaseTime` | *Optional* | string | Backend duration, preferred over leaseTime; empty falls back to leaseTime then 12h. Not parsed as Go duration here. |
| `spec.defaultRoute` | *Optional* | boolean | Boolean default false. Static renderer can control default-router lifetime, but current live writer does not read this field. DHCPv6 itself does not distribute an IPv6 default route. |
| `spec.dnsSource` | *Optional* | string; enum=["self", "static", "none"] | Validated self/static/none, default self in static rendering; static requires dnsServers. Current live writer ignores this selector and uses explicit dnsServers/dnsServerFrom only; none does not suppress those explicit options. |
| `spec.selfAddressPolicy` | *Optional* | string | Optional bare existing SelfAddressPolicy reference; validated even though current live LAN generation does not consume it. Older static self-DNS selection uses it. |
| `spec.dnsServers` | *Optional* | array | Optional literal IPv6 DNS-server list; default empty. Merged with dnsServerFrom and emitted as tagged option6:dns-server. No DNS listener is started. |
| `spec.dnsServerFrom` | *Optional* | array | Optional common resource/field source list, optional false by default. Required missing values make the server Pending even when literal DNS entries exist. Resolved status values are passed through without full IP-family validation by this reader. |
| `spec.sntpServers` | *Optional* | array | Optional literal IPv6 SNTP-server list, default empty; merged with sntpServerFrom for tagged option6:sntp-server. Does not start an NTP daemon. |
| `spec.sntpServerFrom` | *Optional* | array | Optional common status-source list. Same required/optional blocking behavior as dnsServerFrom; default no references. |
| `spec.domainSearch` | *Optional* | array | Optional single-domain strings, default empty; no whitespace/slash/comma. Exclusive with domainSearchFrom. Emitted as tagged option6:domain-search. |
| `spec.domainSearchFrom` | *Optional* | array | Optional sources restricted to DNSZone/<name>, field: zone; resources must exist. Reads declared zones before status. Required missing values block this server; optional true skips missing values. Exclusive with domainSearch. |
| `spec.leaseTime` | *Optional* | string | Fallback backend duration after addressPool.leaseTime; default 12h. |
| `spec.rapidCommit` | *Optional* | boolean | Boolean default false. Current writer emits tagged option6:rapid-commit; it does not emit a separate dnsmasq dhcp-rapid-commit directive, so do not treat the flag alone as proof of negotiated two-message Rapid Commit. |
| `spec.configPath` | *Optional* | string | Optional decoded string, default empty; current controller uses shared dnsmasq options instead of this per-resource path. |
| `spec.pidFile` | *Optional* | string | Optional decoded string, default empty; current controller uses shared dnsmasq options instead of this per-resource path. |
| `spec.leaseFile` | *Optional* | string | Optional lease database path, default <StateDir>/dnsmasq/dnsmasq.leases. All explicit DHCPv4/6 leaseFile values must match because one dnsmasq service shares the file; disagreement fails runtime generation. |
| `spec.dependsOn` | *Optional* | array | Common dependency list accepted and references validated, default empty. Current live DHCPv6 server writer/status path does not evaluate this list; it is not a reliable service gate here. Dynamic-value requirements and when are evaluated separately. |
| `spec.when` | *Optional* | object | Optional common condition, default unconditional. False removes this resource from effective config and reports Pending/WhenFalse. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [cmd/routerd/runtime_services.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/runtime_services.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interface`, `dnsServers`, `sntpServers`, `configPath`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
