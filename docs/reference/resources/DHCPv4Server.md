# DHCPv4Server

[Resource configuration reference](../resources.md#dhcpv4server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Serve IPv4 DHCP pools, tagged selector scopes and reservations through the current shared dnsmasq service.

## Conditions and references

- Use interface plus addressPool and gateway/gatewayFrom, dnsServers/dnsServerFrom, ntpServers/ntpServerFrom for the current live controller. The static renderer and runtime writer have different behavior; older routerSource/dnsSource, embedded dns and authoritative settings must not be presented as working runtime controls.
- An existing logical interface and valid IPv4 start/end are needed for an active pool. Pool start <= end is checked; subnet membership, cross-pool overlaps and reachability of offered gateways are not established by validation.
- Common value sources require an existing resource and explicit provided field. A missing required gateway/DNS/NTP/domain source with optional false suspends the pool even if literal alternatives exist; optional true skips missing values. Dynamic address readers do not themselves enforce IPv4 on every resolved status value.
- Scopes use MAC/OUI alternatives and either a local profileRef or inline settings, never both. Profiles/scopes require valid endpoint pairs. When a dynamic lease matches a selector but lies outside all matching pools, apply prunes it and restarts dnsmasq; explicit reservations are preserved. This does not force the client to renew immediately.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # optional; object
      end: "<string>" # optional; string
      leaseTime: "<string>" # optional; string
      start: "<string>" # optional; string
    authoritative: true # optional; boolean
    dns: # optional; object
      cacheSize: 0 # optional; integer; minimum=0
      enabled: true # optional; boolean
      upstreamInterface: "<string>" # optional; string
      upstreamServers: # optional; array
        - "<string>" # string
      upstreamSource: "dhcpv4" # optional; string; enum=["dhcpv4", "static", "system", "none"]
    dnsInterface: "<string>" # optional; string
    dnsServerFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    dnsServers: # optional; array
      - "<string>" # string
    dnsSource: "dhcpv4" # optional; string; enum=["dhcpv4", "static", "self", "none"]
    domain: "<string>" # optional; string
    domainFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    gateway: "<string>" # optional; string
    gatewayFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    interface: "<string>" # optional; string
    leaseFile: "<string>" # optional; string
    leaseTime: "<string>" # optional; string
    listenInterfaces: # optional; array
      - "<string>" # string
    logDHCP: true # optional; boolean
    managed: true # optional; boolean
    ntpServerFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    ntpServers: # optional; array
      - "<string>" # string
    options: # optional; array
      - # object
        code: 1 # optional; integer; minimum=1; maximum=65535
        name: "<string>" # optional; string
        value: "<string>" # optional; string
    profiles: # optional; array
      - # object
        addressPool: # Required; object
          end: "<string>" # optional; string
          leaseTime: "<string>" # optional; string
          start: "<string>" # optional; string
        dnsServers: # optional; array
          - "<string>" # string
        gateway: "<string>" # optional; string
        name: "<string>" # Required; string
        netmask: "<string>" # optional; string
        ntpServers: # optional; array
          - "<string>" # string
        options: # optional; array
          - # object
            code: 1 # optional; integer; minimum=1; maximum=65535
            name: "<string>" # optional; string
            value: "<string>" # optional; string
    rangeEnd: "<string>" # optional; string
    rangeStart: "<string>" # optional; string
    role: "server" # optional; string; enum=["server", "transit"]
    router: "<string>" # optional; string
    routerSource: "interfaceAddress" # optional; string; enum=["interfaceAddress", "static", "none"]
    scopes: # optional; array
      - # object
        addressPool: # Required; object
          end: "<string>" # optional; string
          leaseTime: "<string>" # optional; string
          start: "<string>" # optional; string
        dnsServers: # optional; array
          - "<string>" # string
        gateway: "<string>" # optional; string
        match: # Required; object
          macAddresses: # optional; array
            - "<string>" # string
          ouiPrefixes: # optional; array
            - "<string>" # string
        name: "<string>" # Required; string
        netmask: "<string>" # optional; string
        ntpServers: # optional; array
          - "<string>" # string
        options: # optional; array
          - # object
            code: 1 # optional; integer; minimum=1; maximum=65535
            name: "<string>" # optional; string
            value: "<string>" # optional; string
        profileRef: "<string>" # optional; string
    server: "dnsmasq" # optional; string; enum=["dnsmasq", "kea", "dhcpd"]
    stickyHoldDays: 0 # optional; integer; minimum=0
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
| `spec.server` | Schema accepts dnsmasq/kea/dhcpd; default dnsmasq in planners/dependency selection. Current live controller still launches dnsmasq regardless of this field; kea/dhcpd are not working backend switches. Sticky-retention day selection only includes dnsmasq servers. |
| `spec.managed` | Boolean default false. Current live DHCP service controller does not check it: false is not an observe-only or disabled mode. |
| `spec.role` | Validated server/transit, default server. The static renderer distinguishes transit, but current live LAN-line generation does not honor this switch. Do not use transit to suppress allocations. |
| `spec.listenInterfaces` | Optional declared logical interfaces; all names must exist. Used by validation/firewall planning, but current live pool generation uses only interface, not this list. It cannot replace interface. |
| `spec.logDHCP` | Default false. Static rendering can emit log-dhcp, but current live config writer does not consume this flag; lease-event callback generation is independent. |
| `spec.stickyHoldDays` | Nonnegative retention days, default 0 (off). Positive values retain released/expired DHCP assignments in dhcp-sticky.db for reuse. Runtime uses the maximum value across dnsmasq DHCPv4Server resources for all IPv4 lease events, not a per-interface lookup. Explicit reservations take precedence. |
| `spec.interface` | Declared logical interface for the primary pool; required for useful current live pool generation, although schema/validation permit omission. Must exist in the Router interface index. |
| `spec.addressPool` | Primary IPv4 allocation pool; start/end required when interface is set. These fields take precedence over legacy rangeStart/rangeEnd/leaseTime. |
| `spec.addressPool.start` | Bare IPv4 first address, inclusive; required unless rangeStart supplies it. Must be <= end. No inferred subnet/pool. |
| `spec.addressPool.end` | Bare IPv4 last address, inclusive; required unless rangeEnd supplies it, and >= start. |
| `spec.addressPool.leaseTime` | Backend lease-duration string; precedence addressPool.leaseTime → leaseTime → 12h. Not parsed as Go duration by this resource validator. |
| `spec.rangeStart` | Legacy fallback for addressPool.start, default empty. Also switches the static renderer to its older scope-style path; prefer addressPool. |
| `spec.rangeEnd` | Legacy fallback for addressPool.end; same static-renderer switch as rangeStart. |
| `spec.leaseTime` | Fallback backend lease duration when addressPool.leaseTime is empty; default 12h. |
| `spec.routerSource` | Validated interfaceAddress/static/none; static requires router. Static renderer defaults interfaceAddress and derives from IPv4StaticAddress, but current live writer ignores routerSource/router and uses gateway/gatewayFrom instead. |
| `spec.router` | IPv4 value required with routerSource: static. Used by the older static renderer only, not the current live gateway option path. |
| `spec.gateway` | Optional bare IPv4 DHCP router option; default no explicit router option. A usable gatewayFrom value takes precedence. Not exclusive with gatewayFrom. |
| `spec.gatewayFrom` | Optional common status source with resource and field. Default optional false: unresolved source makes this server Pending even if gateway is set. With optional true, missing values can use gateway. |
| `spec.dnsSource` | Validated dhcpv4/static/self/none; static requires dnsServers, dhcpv4 requires dnsInterface. Default self in static renderer. Current live writer ignores this selector and uses dnsServers plus dnsServerFrom. |
| `spec.dnsInterface` | Declared logical interface required by dnsSource: dhcpv4. Reference is validated even when another selector is used; current live writer does not use this field to discover DNS servers. |
| `spec.dnsServers` | Optional bare IPv4 DNS-server list. Default empty; merged with resolved dnsServerFrom values for DHCP option 6. Does not start DNSResolver or a DNS listener. |
| `spec.dnsServerFrom` | Optional common resource/field source list; optional defaults false. Required missing values block the whole server despite static DNS entries. Runtime reads status and strips CIDR to addresses; use IPv4-providing fields. |
| `spec.ntpServers` | Optional bare IPv4 NTP-server list; default empty. Merged with ntpServerFrom for DHCP option 42; does not run NTPServer. |
| `spec.ntpServerFrom` | Optional common resource/field source list. Same required/optional gating and status-only resolution as dnsServerFrom; default no references. |
| `spec.domain` | Optional single domain without whitespace, slash or comma. Exclusive with domainFrom. Emits domain-name and, unless option 119/name domain-search is explicitly supplied, domain-search. |
| `spec.domainFrom` | Optional source restricted to DNSZone/<name>, field: zone; resource must exist. Declared zone value is readable before status. optional false blocks on no value; true can omit the domain. Exclusive with domain. |
| `spec.authoritative` | Default false. Affects the older static renderer and selects its scope-style path; current live writer does not emit dhcp-authoritative from it. |
| `spec.leaseFile` | Optional path, default <StateDir>/dnsmasq/dnsmasq.leases (normally /var/lib/routerd/dnsmasq/dnsmasq.leases). One dnsmasq instance shares the file: all explicit DHCPv4/6 server leaseFile values must agree or runtime fails. |
| `spec.when` | Optional common state predicate; default unconditional. False excludes the pool and yields Pending/WhenFalse. Required dynamic values can separately keep the server Pending. |
| `spec.dns` | Legacy embedded dnsmasq DNS settings. Current live writer forces port=0 and no-resolv, so these settings do not enable DNS forwarding. Use DNSResolver/DNSForwarder/DNSUpstream instead. Some values still affect validation. |
| `spec.dns.enabled` | Default false. true reserves TCP/UDP 53 in listener-conflict validation but does not actually open a live DNS listener. |
| `spec.dns.upstreamSource` | Accepted empty/dhcpv4/static/system/none. dhcpv4 requires upstreamInterface, static requires upstreamServers. No current live DNS-forwarding effect. |
| `spec.dns.upstreamInterface` | Required nonempty string with dns.upstreamSource: dhcpv4; not used for current live DNS resolution. Unlike dnsInterface, this legacy field is not checked against the interface index here. |
| `spec.dns.upstreamServers` | IPv4/IPv6 literals required when upstreamSource is static; default empty. Validated but not consumed by current live DNS forwarding. |
| `spec.dns.cacheSize` | Nonnegative schema integer, default 0; not used by the current live config writer. DNSResolver.cache is separate. |
| `spec.profiles` | Optional reusable settings local to this server. Profiles are inert unless selected by scopes[].profileRef; they are not separate resources. Default empty. |
| `spec.profiles[].name` | Required unique profile name within the server, with no whitespace or commas. No Kind/name prefix. |
| `spec.scopes` | Optional selector-specific pools rendered before the primary catch-all pool. Clients that match no selector use the primary pool. Default empty. Multiple matching scopes are not rejected by validation. |
| `spec.scopes[].name` | Required unique scope name within this server; no whitespace or commas. Used to derive dnsmasq tags, not an external resource reference. |
| `spec.scopes[].match` | Required selector with at least one macAddresses or ouiPrefixes entry. Entries are alternatives, not a requirement to match both lists. |
| `spec.scopes[].match.macAddresses` | Optional exact MAC list parsed by net.ParseMAC. Default empty. Use canonical colon-separated six-octet MACs for dnsmasq output. |
| `spec.scopes[].match.ouiPrefixes` | Optional three-octet OUI prefixes. Use aa:bb:cc: runtime appends :*:*:* to the literal value. Validator normalizes alternate spellings for checking but live writer does not reuse that normalization. |
| `spec.scopes[].profileRef` | Bare name of an existing profiles[] entry in the same server. Exclusive with inline addressPool fields, netmask, gateway, dnsServers, ntpServers and options. Empty selects inline settings. |
| `spec.profiles[].addressPool` | Required IPv4 pool settings in profiles and inline scopes; no default endpoints. |
| `spec.profiles[].addressPool.start` | Required inclusive bare IPv4 start, <= end. No cross-pool overlap validation. |
| `spec.profiles[].addressPool.end` | Required inclusive bare IPv4 end, >= start. |
| `spec.profiles[].addressPool.leaseTime` | Optional backend lease duration. Empty inherits the server effective lease duration (addressPool.leaseTime → leaseTime → 12h). |
| `spec.profiles[].netmask` | Optional contiguous IPv4 dotted netmask with 1–32 one bits. Empty omits the explicit mask and lets dnsmasq derive it; not inherited from another profile. |
| `spec.profiles[].gateway` | Optional bare IPv4 router option for this tagged pool. Empty does not copy server.gateway. |
| `spec.profiles[].dnsServers` | Optional IPv4 DNS addresses for this tagged pool, default empty; no dynamic-source field or automatic server-list inheritance. |
| `spec.profiles[].ntpServers` | Optional IPv4 NTP addresses for this tagged pool, default empty; no dynamic-source field. |
| `spec.profiles[].options` | Optional extra options for this pool; exactly one code/name and a nonempty value per item. Default empty. |
| `spec.profiles[].options[].code` | Numeric option, exclusive with name; schema 1–65535. |
| `spec.profiles[].options[].name` | dnsmasq option name, exclusive with code; whitespace and comma prohibited. |
| `spec.profiles[].options[].value` | Required backend-formatted value, no CR/LF; commas may separate multiple values. |
| `spec.scopes[].addressPool` | Required IPv4 pool settings in profiles and inline scopes; no default endpoints. |
| `spec.scopes[].addressPool.start` | Required inclusive bare IPv4 start, <= end. No cross-pool overlap validation. |
| `spec.scopes[].addressPool.end` | Required inclusive bare IPv4 end, >= start. |
| `spec.scopes[].addressPool.leaseTime` | Optional backend lease duration. Empty inherits the server effective lease duration (addressPool.leaseTime → leaseTime → 12h). |
| `spec.scopes[].netmask` | Optional contiguous IPv4 dotted netmask with 1–32 one bits. Empty omits the explicit mask and lets dnsmasq derive it; not inherited from another profile. |
| `spec.scopes[].gateway` | Optional bare IPv4 router option for this tagged pool. Empty does not copy server.gateway. |
| `spec.scopes[].dnsServers` | Optional IPv4 DNS addresses for this tagged pool, default empty; no dynamic-source field or automatic server-list inheritance. |
| `spec.scopes[].ntpServers` | Optional IPv4 NTP addresses for this tagged pool, default empty; no dynamic-source field. |
| `spec.scopes[].options` | Optional extra options for this pool; exactly one code/name and a nonempty value per item. Default empty. |
| `spec.scopes[].options[].code` | Numeric option, exclusive with name; schema 1–65535. |
| `spec.scopes[].options[].name` | dnsmasq option name, exclusive with code; whitespace and comma prohibited. |
| `spec.scopes[].options[].value` | Required backend-formatted value, no CR/LF; commas may separate multiple values. |
| `spec.options` | Optional extra options for this pool; exactly one code/name and a nonempty value per item. Default empty. |
| `spec.options[].code` | Numeric option, exclusive with name; schema 1–65535. |
| `spec.options[].name` | dnsmasq option name, exclusive with code; whitespace and comma prohibited. |
| `spec.options[].value` | Required backend-formatted value, no CR/LF; commas may separate multiple values. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/listen_ports.go](https://github.com/imksoo/routerd/blob/main/pkg/config/listen_ports.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/dhcp_profile_leases.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/dhcp_profile_leases.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [cmd/routerd/runtime_services.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/runtime_services.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `interface`, `dnsServers`, `ntpServers`, `domain`, `configPath`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
