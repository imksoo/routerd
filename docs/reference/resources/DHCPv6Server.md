# DHCPv6Server

[Resource configuration reference](../resources.md#dhcpv6server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Provide LAN DHCPv6 address/information service and associated RA behavior using the shared dnsmasq process.

## Conditions and references

- The current live writer differs from the older static renderer: interface is needed, delegatedAddress-only does not create a live range, dnsSource/selfAddressPolicy do not supply self DNS, and ra-only follows the stateless branch. Use explicit fields documented below and verify actual DHCP/RA traffic.
- Normal validation limits delegatedAddress mode to stateless/ra-only. Without delegatedAddress, every non-stateless mode including ra-only requires a valid IPv6 start/end pair. domainSearch and domainSearchFrom are exclusive; literals and DNS/SNTP value sources may coexist.
- One dnsmasq instance serves the resources; mode constructors use a /64 on the actual interface. Applied records successful reconciliation, not a client lease, RA reception or default-route installation. Removed ready_when is rejected.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # optional; object
      end: "<string>" # optional; string
      leaseTime: "<string>" # optional; string
      start: "<string>" # optional; string
    configPath: "<string>" # optional; string
    defaultRoute: true # optional; boolean
    delegatedAddress: "<string>" # optional; string
    dependsOn: # optional; array
      - # object
        equals: "<string>" # optional; string
        field: "<string>" # optional; string
        notEmpty: true # optional; boolean
        optional: true # optional; boolean
        phase: "<string>" # optional; string
        resource: "<string>" # Required; string
    dnsServerFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    dnsServers: # optional; array
      - "<string>" # string
    dnsSource: "self" # optional; string; enum=["self", "static", "none"]
    domainSearch: # optional; array
      - "<string>" # string
    domainSearchFrom: # optional; array
      - # object
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
    mode: "stateless" # optional; string; enum=["stateless", "stateful", "both", "ra-only"]
    pidFile: "<string>" # optional; string
    rapidCommit: true # optional; boolean
    role: "server" # optional; string; enum=["server", "transit"]
    selfAddressPolicy: "<string>" # optional; string
    server: "dnsmasq" # optional; string; enum=["dnsmasq"]
    sntpServerFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    sntpServers: # optional; array
      - "<string>" # string
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
| `spec.server` | Optional dnsmasq, default dnsmasq. No other backend accepted. The live service is shared with DHCPv4 and RA resources. |
| `spec.managed` | Boolean default false; current live service writer/controller does not use this flag to disable mutation. |
| `spec.role` | server/transit, default server in validation/static rendering. Current live writer does not honor transit to suppress service. |
| `spec.listenInterfaces` | Optional declared logical interfaces, all validated for existence. If delegatedAddress is given and this list is nonempty, it must include that address resource interface. Used in family/firewall planning; current live pool writer still requires interface itself. |
| `spec.logDHCP` | Default false; static renderer can emit log-dhcp, but current live writer does not consume it. |
| `spec.stickyHoldDays` | Nonnegative days, default 0/off. Positive dnsmasq values retain lease assignments in dhcp-sticky.db for reuse; runtime uses the maximum across IPv6 servers for all IPv6 lease events, not per-interface selection. DHCPv6ServerLeaseSync does not copy this sticky DB. |
| `spec.interface` | Declared logical LAN interface. Required for current live writer even when validation permits delegatedAddress alone; an absent interface skips live pool rendering. |
| `spec.delegatedAddress` | Optional bare existing IPv6DelegatedAddress name. Static renderer derives interface/prefix/self DNS from it, but current live writer uses interface and constructor directly and does not substitute this resource. When set, validation limits mode to stateless or ra-only. |
| `spec.mode` | Default stateless. stateful emits a constructor range; both adds slaac; current live writer treats ra-only through the same ra-stateless default branch, so it does not actually disable DHCPv6 information service. Without delegatedAddress, validation also requires a pool for ra-only. |
| `spec.addressPool` | IPv6 stateful allocation range. Required for non-stateless modes when delegatedAddress is absent; not used as an allocation range by stateless. No endpoints inferred. |
| `spec.addressPool.start` | IPv6 start address, inclusive and <= end. Live stateful range uses constructor:<ifname> and prefix length 64. |
| `spec.addressPool.end` | IPv6 end address, inclusive and >= start; paired with start. |
| `spec.addressPool.leaseTime` | Backend duration, preferred over leaseTime; empty falls back to leaseTime then 12h. Not parsed as Go duration here. |
| `spec.defaultRoute` | Boolean default false. Static renderer can control default-router lifetime, but current live writer does not read this field. DHCPv6 itself does not distribute an IPv6 default route. |
| `spec.dnsSource` | Validated self/static/none, default self in static rendering; static requires dnsServers. Current live writer ignores this selector and uses explicit dnsServers/dnsServerFrom only; none does not suppress those explicit options. |
| `spec.selfAddressPolicy` | Optional bare existing SelfAddressPolicy reference; validated even though current live LAN generation does not consume it. Older static self-DNS selection uses it. |
| `spec.dnsServers` | Optional literal IPv6 DNS-server list; default empty. Merged with dnsServerFrom and emitted as tagged option6:dns-server. No DNS listener is started. |
| `spec.dnsServerFrom` | Optional common resource/field source list, optional false by default. Required missing values make the server Pending even when literal DNS entries exist. Resolved status values are passed through without full IP-family validation by this reader. |
| `spec.sntpServers` | Optional literal IPv6 SNTP-server list, default empty; merged with sntpServerFrom for tagged option6:sntp-server. Does not start an NTP daemon. |
| `spec.sntpServerFrom` | Optional common status-source list. Same required/optional blocking behavior as dnsServerFrom; default no references. |
| `spec.domainSearch` | Optional single-domain strings, default empty; no whitespace/slash/comma. Exclusive with domainSearchFrom. Emitted as tagged option6:domain-search. |
| `spec.domainSearchFrom` | Optional sources restricted to DNSZone/<name>, field: zone; resources must exist. Reads declared zones before status. Required missing values block this server; optional true skips missing values. Exclusive with domainSearch. |
| `spec.leaseTime` | Fallback backend duration after addressPool.leaseTime; default 12h. |
| `spec.rapidCommit` | Boolean default false. Current writer emits tagged option6:rapid-commit; it does not emit a separate dnsmasq dhcp-rapid-commit directive, so do not treat the flag alone as proof of negotiated two-message Rapid Commit. |
| `spec.configPath` | Optional decoded string, default empty; current controller uses shared dnsmasq options instead of this per-resource path. |
| `spec.pidFile` | Optional decoded string, default empty; current controller uses shared dnsmasq options instead of this per-resource path. |
| `spec.leaseFile` | Optional lease database path, default <StateDir>/dnsmasq/dnsmasq.leases. All explicit DHCPv4/6 leaseFile values must match because one dnsmasq service shares the file; disagreement fails runtime generation. |
| `spec.dependsOn` | Common dependency list accepted and references validated, default empty. Current live DHCPv6 server writer/status path does not evaluate this list; it is not a reliable service gate here. Dynamic-value requirements and when are evaluated separately. |
| `spec.when` | Optional common condition, default unconditional. False removes this resource from effective config and reports Pending/WhenFalse. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [cmd/routerd/runtime_services.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/runtime_services.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
