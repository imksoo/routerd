# NTPClient

[Resource configuration reference](../resources.md#ntpclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Manages or observes an upstream time-synchronization client.

## Conditions and references

- With managed=true, at least one of servers/serverFrom/fallbackServers must be declared. Static mode additionally requires servers itself. With no resolved servers the client remains Pending/NoServers. chrony management disables the stock systemd-timesyncd service.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPClient
  metadata:
    name: "<name>"
  spec:
    fallbackServers: # optional; array
      - "<string>" # string
    interface: "<string>" # optional; string
    managed: true # optional; boolean
    provider: "systemd-timesyncd" # optional; string; enum=["systemd-timesyncd", "chrony", "ntpd"]
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
| `spec.provider` | Client implementation. Default systemd-timesyncd except on FreeBSD, where it is ntpd. chrony is also accepted. ntpd requires FreeBSD rc.d support; systemd-timesyncd requires the platform feature. |
| `spec.managed` | Default false: record Observed without configuring or starting the time service. Set true to manage files and services. |
| `spec.source` | Default static: use servers. auto/dhcp/dhcpv6 all use the same order: resolved serverFrom, then fallbackServers, then servers. They do not automatically discover DHCP sources. |
| `spec.interface` | Optional logical interface name, default empty. Router-wide validation requires it to exist when supplied. The NTP controller does not use it for source selection or service binding. |
| `spec.servers` | Upstream hostnames or IP addresses, default empty. Required when managed=true and source is omitted/static. Entries must be nonblank single tokens; they are not resource references. |
| `spec.serverFrom` | List of explicit value sources, default empty. Use for example DHCPv4Client/wan field ntpServers or DHCPv6PrefixDelegation/wan-pd field sntpServers. Used only for auto/dhcp/dhcpv6; each resource and field must exist. This resolver ignores the optional flag; missing values simply lead to the next source alternative. |
| `spec.fallbackServers` | Fallback upstreams, default empty. For dynamic source modes they are selected when serverFrom yields no servers. systemd-timesyncd additionally receives FallbackNTP; chrony and ntpd do not receive a separate fallback directive. |
| `spec.when` | Activation condition; omitted means no gate. See the shared condition reference for state/all/any, unknown handling and age predicates. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/ntp.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ntp.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `servers`, `source`, `updatedAt`

[Example](https://github.com/imksoo/routerd/blob/main/examples/router-lab.yaml)
