# DHCPv4Server

[Resource configuration reference](../resources.md#dhcpv4server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Serve IPv4 DHCP pools, tagged selector scopes and reservations through the current shared dnsmasq service.

## Conditions and references

- Use interface plus addressPool and gateway/gatewayFrom, dnsServers/dnsServerFrom, ntpServers/ntpServerFrom for the current live controller. The static renderer and runtime writer have different behavior; older routerSource/dnsSource, embedded dns and authoritative settings must not be presented as working runtime controls.
- An existing logical interface and valid IPv4 start/end are needed for an active pool. Pool start <= end is checked; subnet membership, cross-pool overlaps and reachability of offered gateways are not established by validation.
- Common value sources require an existing resource and explicit provided field. A missing required gateway/DNS/NTP/domain source with optional false suspends the pool even if literal alternatives exist; optional true skips missing values. Dynamic address readers do not themselves enforce IPv4 on every resolved status value.
- Scopes use MAC/OUI alternatives and either a local profileRef or inline settings, never both. Profiles/scopes require valid endpoint pairs. When a dynamic lease matches a selector but lies outside all matching pools, apply prunes it and restarts dnsmasq; explicit reservations are preserved. This does not force the client to renew immediately.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: "<name>"
  spec:
    [ addressPool:
        [ end: <string> ]
        [ leaseTime: <string> ]
        [ start: <string> ]
    ]
    [ authoritative: <boolean> ]
    [ dns:
        [ cacheSize: <integer> ]
        [ enabled: <boolean> ]
        [ upstreamInterface: <string> ]
        [ upstreamServers:
            - <string>
        ]
        [ upstreamSource: ("dhcpv4" | "static" | "system" | "none") ]
    ]
    [ dnsInterface: <string> ]
    [ dnsServerFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ dnsServers:
        - <string>
    ]
    [ dnsSource: ("dhcpv4" | "static" | "self" | "none") ]
    [ domain: <string> ]
    [ domainFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ gateway: <string> ]
    [ gatewayFrom:
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
    [ ntpServerFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ ntpServers:
        - <string>
    ]
    [ options:
        -
          [ code: <integer> ]
          [ name: <string> ]
          [ value: <string> ]
    ]
    [ profiles:
        -
          addressPool:
            [ end: <string> ]
            [ leaseTime: <string> ]
            [ start: <string> ]
          [ dnsServers:
              - <string>
          ]
          [ gateway: <string> ]
          name: <string>
          [ netmask: <string> ]
          [ ntpServers:
              - <string>
          ]
          [ options:
              -
                [ code: <integer> ]
                [ name: <string> ]
                [ value: <string> ]
          ]
    ]
    [ rangeEnd: <string> ]
    [ rangeStart: <string> ]
    [ role: ("server" | "transit") ]
    [ router: <string> ]
    [ routerSource: ("interfaceAddress" | "static" | "none") ]
    [ scopes:
        -
          addressPool:
            [ end: <string> ]
            [ leaseTime: <string> ]
            [ start: <string> ]
          [ dnsServers:
              - <string>
          ]
          [ gateway: <string> ]
          match:
            [ macAddresses:
                - <string>
            ]
            [ ouiPrefixes:
                - <string>
            ]
          name: <string>
          [ netmask: <string> ]
          [ ntpServers:
              - <string>
          ]
          [ options:
              -
                [ code: <integer> ]
                [ name: <string> ]
                [ value: <string> ]
          ]
          [ profileRef: <string> ]
    ]
    [ server: ("dnsmasq" | "kea" | "dhcpd") ]
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
| `spec.server` | *Optional* | string; enum=["dnsmasq", "kea", "dhcpd"] | Schema accepts dnsmasq/kea/dhcpd; default dnsmasq in planners/dependency selection. Current live controller still launches dnsmasq regardless of this field; kea/dhcpd are not working backend switches. Sticky-retention day selection only includes dnsmasq servers. |
| `spec.managed` | *Optional* | boolean | Boolean default false. Current live DHCP service controller does not check it: false is not an observe-only or disabled mode. |
| `spec.role` | *Optional* | string; enum=["server", "transit"] | Validated server/transit, default server. The static renderer distinguishes transit, but current live LAN-line generation does not honor this switch. Do not use transit to suppress allocations. |
| `spec.listenInterfaces` | *Optional* | array | Optional declared logical interfaces; all names must exist. Used by validation/firewall planning, but current live pool generation uses only interface, not this list. It cannot replace interface. |
| `spec.logDHCP` | *Optional* | boolean | Default false. Static rendering can emit log-dhcp, but current live config writer does not consume this flag; lease-event callback generation is independent. |
| `spec.stickyHoldDays` | *Optional* | integer; minimum=0 | Nonnegative retention days, default 0 (off). Positive values retain released/expired DHCP assignments in dhcp-sticky.db for reuse. Runtime uses the maximum value across dnsmasq DHCPv4Server resources for all IPv4 lease events, not a per-interface lookup. Explicit reservations take precedence. |
| `spec.interface` | **Conditionally required** | string | Declared logical interface for the primary pool; required for useful current live pool generation, although schema/validation permit omission. Must exist in the Router interface index. |
| `spec.addressPool` | **Conditionally required** | object | Primary IPv4 allocation pool; start/end required when interface is set. These fields take precedence over legacy rangeStart/rangeEnd/leaseTime. |
| `spec.addressPool.start` | **Conditionally required** | string | Bare IPv4 first address, inclusive; required unless rangeStart supplies it. Must be <= end. No inferred subnet/pool. |
| `spec.addressPool.end` | **Conditionally required** | string | Bare IPv4 last address, inclusive; required unless rangeEnd supplies it, and >= start. |
| `spec.addressPool.leaseTime` | *Optional* | string | Backend lease-duration string; precedence addressPool.leaseTime → leaseTime → 12h. Not parsed as Go duration by this resource validator. |
| `spec.rangeStart` | *Optional* | string | Legacy fallback for addressPool.start, default empty. Also switches the static renderer to its older scope-style path; prefer addressPool. |
| `spec.rangeEnd` | *Optional* | string | Legacy fallback for addressPool.end; same static-renderer switch as rangeStart. |
| `spec.leaseTime` | *Optional* | string | Fallback backend lease duration when addressPool.leaseTime is empty; default 12h. |
| `spec.routerSource` | *Optional* | string; enum=["interfaceAddress", "static", "none"] | Validated interfaceAddress/static/none; static requires router. Static renderer defaults interfaceAddress and derives from IPv4StaticAddress, but current live writer ignores routerSource/router and uses gateway/gatewayFrom instead. |
| `spec.router` | **Conditionally required** | string | IPv4 value required with routerSource: static. Used by the older static renderer only, not the current live gateway option path. |
| `spec.gateway` | *Optional* | string | Optional bare IPv4 DHCP router option; default no explicit router option. A usable gatewayFrom value takes precedence. Not exclusive with gatewayFrom. |
| `spec.gatewayFrom` | *Optional* | object | Optional common status source with resource and field. Default optional false: unresolved source makes this server Pending even if gateway is set. With optional true, missing values can use gateway. |
| `spec.dnsSource` | *Optional* | string; enum=["dhcpv4", "static", "self", "none"] | Validated dhcpv4/static/self/none; static requires dnsServers, dhcpv4 requires dnsInterface. Default self in static renderer. Current live writer ignores this selector and uses dnsServers plus dnsServerFrom. |
| `spec.dnsInterface` | **Conditionally required** | string | Declared logical interface required by dnsSource: dhcpv4. Reference is validated even when another selector is used; current live writer does not use this field to discover DNS servers. |
| `spec.dnsServers` | *Optional* | array | Optional bare IPv4 DNS-server list. Default empty; merged with resolved dnsServerFrom values for DHCP option 6. Does not start DNSResolver or a DNS listener. |
| `spec.dnsServerFrom` | *Optional* | array | Optional common resource/field source list; optional defaults false. Required missing values block the whole server despite static DNS entries. Runtime reads status and strips CIDR to addresses; use IPv4-providing fields. |
| `spec.ntpServers` | *Optional* | array | Optional bare IPv4 NTP-server list; default empty. Merged with ntpServerFrom for DHCP option 42; does not run NTPServer. |
| `spec.ntpServerFrom` | *Optional* | array | Optional common resource/field source list. Same required/optional gating and status-only resolution as dnsServerFrom; default no references. |
| `spec.domain` | *Optional* | string | Optional single domain without whitespace, slash or comma. Exclusive with domainFrom. Emits domain-name and, unless option 119/name domain-search is explicitly supplied, domain-search. |
| `spec.domainFrom` | *Optional* | object | Optional source restricted to DNSZone/<name>, field: zone; resource must exist. Declared zone value is readable before status. optional false blocks on no value; true can omit the domain. Exclusive with domain. |
| `spec.authoritative` | *Optional* | boolean | Default false. Affects the older static renderer and selects its scope-style path; current live writer does not emit dhcp-authoritative from it. |
| `spec.leaseFile` | *Optional* | string | Optional path, default <StateDir>/dnsmasq/dnsmasq.leases (normally /var/lib/routerd/dnsmasq/dnsmasq.leases). One dnsmasq instance shares the file: all explicit DHCPv4/6 server leaseFile values must agree or runtime fails. |
| `spec.when` | *Optional* | object | Optional common state predicate; default unconditional. False excludes the pool and yields Pending/WhenFalse. Required dynamic values can separately keep the server Pending. |
| `spec.dns` | *Optional* | object | Legacy embedded dnsmasq DNS settings. Current live writer forces port=0 and no-resolv, so these settings do not enable DNS forwarding. Use DNSResolver/DNSForwarder/DNSUpstream instead. Some values still affect validation. |
| `spec.dns.enabled` | *Optional* | boolean | Default false. true reserves TCP/UDP 53 in listener-conflict validation but does not actually open a live DNS listener. |
| `spec.dns.upstreamSource` | *Optional* | string; enum=["dhcpv4", "static", "system", "none"] | Accepted empty/dhcpv4/static/system/none. dhcpv4 requires upstreamInterface, static requires upstreamServers. No current live DNS-forwarding effect. |
| `spec.dns.upstreamInterface` | **Conditionally required** | string | Required nonempty string with dns.upstreamSource: dhcpv4; not used for current live DNS resolution. Unlike dnsInterface, this legacy field is not checked against the interface index here. |
| `spec.dns.upstreamServers` | **Conditionally required** | array | IPv4/IPv6 literals required when upstreamSource is static; default empty. Validated but not consumed by current live DNS forwarding. |
| `spec.dns.cacheSize` | *Optional* | integer; minimum=0 | Nonnegative schema integer, default 0; not used by the current live config writer. DNSResolver.cache is separate. |
| `spec.profiles` | *Optional* | array | Optional reusable settings local to this server. Profiles are inert unless selected by scopes[].profileRef; they are not separate resources. Default empty. |
| `spec.profiles[].name` | **Conditionally required** | string | Required unique profile name within the server, with no whitespace or commas. No Kind/name prefix. |
| `spec.scopes` | *Optional* | array | Optional selector-specific pools rendered before the primary catch-all pool. Clients that match no selector use the primary pool. Default empty. Multiple matching scopes are not rejected by validation. |
| `spec.scopes[].name` | **Conditionally required** | string | Required unique scope name within this server; no whitespace or commas. Used to derive dnsmasq tags, not an external resource reference. |
| `spec.scopes[].match` | **Conditionally required** | object | Required selector with at least one macAddresses or ouiPrefixes entry. Entries are alternatives, not a requirement to match both lists. |
| `spec.scopes[].match.macAddresses` | *Optional* | array | Optional exact MAC list parsed by net.ParseMAC. Default empty. Use canonical colon-separated six-octet MACs for dnsmasq output. |
| `spec.scopes[].match.ouiPrefixes` | *Optional* | array | Optional three-octet OUI prefixes. Use aa:bb:cc: runtime appends :*:*:* to the literal value. Validator normalizes alternate spellings for checking but live writer does not reuse that normalization. |
| `spec.scopes[].profileRef` | *Optional* | string | Bare name of an existing profiles[] entry in the same server. Exclusive with inline addressPool fields, netmask, gateway, dnsServers, ntpServers and options. Empty selects inline settings. |
| `spec.profiles[].addressPool` | **Conditionally required** | object | Required IPv4 pool settings in profiles and inline scopes; no default endpoints. |
| `spec.profiles[].addressPool.start` | *Optional* | string | Required inclusive bare IPv4 start, <= end. No cross-pool overlap validation. |
| `spec.profiles[].addressPool.end` | *Optional* | string | Required inclusive bare IPv4 end, >= start. |
| `spec.profiles[].addressPool.leaseTime` | *Optional* | string | Optional backend lease duration. Empty inherits the server effective lease duration (addressPool.leaseTime → leaseTime → 12h). |
| `spec.profiles[].netmask` | *Optional* | string | Optional contiguous IPv4 dotted netmask with 1–32 one bits. Empty omits the explicit mask and lets dnsmasq derive it; not inherited from another profile. |
| `spec.profiles[].gateway` | *Optional* | string | Optional bare IPv4 router option for this tagged pool. Empty does not copy server.gateway. |
| `spec.profiles[].dnsServers` | *Optional* | array | Optional IPv4 DNS addresses for this tagged pool, default empty; no dynamic-source field or automatic server-list inheritance. |
| `spec.profiles[].ntpServers` | *Optional* | array | Optional IPv4 NTP addresses for this tagged pool, default empty; no dynamic-source field. |
| `spec.profiles[].options` | *Optional* | array | Optional extra options for this pool; exactly one code/name and a nonempty value per item. Default empty. |
| `spec.profiles[].options[].code` | **Conditionally required** | integer; minimum=1; maximum=65535 | Numeric option, exclusive with name; schema 1–65535. |
| `spec.profiles[].options[].name` | **Conditionally required** | string | dnsmasq option name, exclusive with code; whitespace and comma prohibited. |
| `spec.profiles[].options[].value` | *Optional* | string | Required backend-formatted value, no CR/LF; commas may separate multiple values. |
| `spec.scopes[].addressPool` | **Conditionally required** | object | Required IPv4 pool settings in profiles and inline scopes; no default endpoints. |
| `spec.scopes[].addressPool.start` | *Optional* | string | Required inclusive bare IPv4 start, <= end. No cross-pool overlap validation. |
| `spec.scopes[].addressPool.end` | *Optional* | string | Required inclusive bare IPv4 end, >= start. |
| `spec.scopes[].addressPool.leaseTime` | *Optional* | string | Optional backend lease duration. Empty inherits the server effective lease duration (addressPool.leaseTime → leaseTime → 12h). |
| `spec.scopes[].netmask` | *Optional* | string | Optional contiguous IPv4 dotted netmask with 1–32 one bits. Empty omits the explicit mask and lets dnsmasq derive it; not inherited from another profile. |
| `spec.scopes[].gateway` | *Optional* | string | Optional bare IPv4 router option for this tagged pool. Empty does not copy server.gateway. |
| `spec.scopes[].dnsServers` | *Optional* | array | Optional IPv4 DNS addresses for this tagged pool, default empty; no dynamic-source field or automatic server-list inheritance. |
| `spec.scopes[].ntpServers` | *Optional* | array | Optional IPv4 NTP addresses for this tagged pool, default empty; no dynamic-source field. |
| `spec.scopes[].options` | *Optional* | array | Optional extra options for this pool; exactly one code/name and a nonempty value per item. Default empty. |
| `spec.scopes[].options[].code` | **Conditionally required** | integer; minimum=1; maximum=65535 | Numeric option, exclusive with name; schema 1–65535. |
| `spec.scopes[].options[].name` | **Conditionally required** | string | dnsmasq option name, exclusive with code; whitespace and comma prohibited. |
| `spec.scopes[].options[].value` | *Optional* | string | Required backend-formatted value, no CR/LF; commas may separate multiple values. |
| `spec.options` | *Optional* | array | Optional extra options for this pool; exactly one code/name and a nonempty value per item. Default empty. |
| `spec.options[].code` | **Conditionally required** | integer; minimum=1; maximum=65535 | Numeric option, exclusive with name; schema 1–65535. |
| `spec.options[].name` | **Conditionally required** | string | dnsmasq option name, exclusive with code; whitespace and comma prohibited. |
| `spec.options[].value` | *Optional* | string | Required backend-formatted value, no CR/LF; commas may separate multiple values. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/listen_ports.go](https://github.com/imksoo/routerd/blob/main/pkg/config/listen_ports.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/dhcp_profile_leases.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/dhcp_profile_leases.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [cmd/routerd/runtime_services.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/runtime_services.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interface`, `dnsServers`, `ntpServers`, `domain`, `configPath`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
