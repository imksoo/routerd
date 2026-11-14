# NTPServer

[Resource configuration reference](../resources.md#ntpserver) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Configures a time server and its upstream servers using chrony or FreeBSD ntpd.

## Conditions and references

- managed defaults to false. With true, server-source validation requires a declared upstream source; static additionally requires servers. listen/allow source arrays are merged, while upstream source alternatives follow source selection order.
- The ntpd renderer does not emit allowCIDRs restrictions. Do not assume that declaring this list enforces a client ACL on ntpd.
- In a dynamic source mode, unresolved upstream values do not themselves stop the server renderer; it may emit a configuration with no upstreams. Applied denotes configuration/service work, not proof of clock synchronization.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPServer
  metadata:
    name: "<name>"
  spec:
    allowCIDRFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    allowCIDRs: # optional; array
      - "<string>" # string
    fallbackServers: # optional; array
      - "<string>" # string
    listenAddressFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    listenAddresses: # optional; array
      - "<string>" # string
    managed: true # optional; boolean
    provider: "chrony" # optional; string; enum=["chrony", "ntpd"]
    serverFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    servers: # optional; array
      - "<string>" # string
    source: "static" # optional; string; enum=["static", "auto", "dhcp", "dhcpv6"]
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
| `spec.managed` | Default false: record Observed without configuring or starting the time service. Set true to manage files and services. |
| `spec.source` | Default static: use servers. auto/dhcp/dhcpv6 all use the same order: resolved serverFrom, then fallbackServers, then servers. They do not automatically discover DHCP sources. |
| `spec.servers` | Upstream hostnames or IP addresses, default empty. Required when managed=true and source is omitted/static. Entries must be nonblank single tokens; they are not resource references. |
| `spec.serverFrom` | List of explicit value sources, default empty. Use for example DHCPv4Client/wan field ntpServers or DHCPv6PrefixDelegation/wan-pd field sntpServers. Used only for auto/dhcp/dhcpv6; each resource and field must exist. This resolver ignores the optional flag; missing values simply lead to the next source alternative. |
| `spec.when` | Activation condition; omitted means no gate. See the shared condition reference for state/all/any, unknown handling and age predicates. |
| `spec.provider` | Server implementation, default chrony except on FreeBSD, where it is ntpd. ntpd is supported only on FreeBSD. |
| `spec.fallbackServers` | Fallback upstream list, default empty. Selected only in auto/dhcp/dhcpv6 mode when serverFrom yields nothing, before servers. No separate fallback directives are written. |
| `spec.listenAddresses` | Static listen IPs, default empty. CIDR values are reduced to their host address. Empty resolved listen settings produce no bindaddress/interface-listen directives, leaving backend defaults. |
| `spec.listenAddressFrom` | Additional listen-address sources, default empty, combined with listenAddresses. Both resource and field are required per source. If any listen setting was declared but the combined result is empty, status is Pending/NoListenAddresses. |
| `spec.allowCIDRs` | Allowed client CIDRs, default empty. chrony emits allow for each; ntpd does not consume this list in its generated configuration. Static entries are checked for nonblank text, not CIDR syntax, during validation. |
| `spec.allowCIDRFrom` | CIDR value sources combined with allowCIDRs, default empty. Missing values from a nonoptional source cause Pending/AllowCIDRFromPending; invalid derived CIDRs cause Pending/AllowCIDRFromInvalid. Bare addresses are not CIDRs. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/ntp.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ntp.go)
