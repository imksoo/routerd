# DHCPv6PrefixDelegation

[Resource configuration reference](../resources.md#dhcpv6prefixdelegation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Acquire an IPv6 delegated prefix on an uplink. Use IPv6DelegatedAddress to derive and apply downstream or host addresses; obtaining a prefix alone does not advertise it to clients.

## Conditions and references

- interface must name an existing logical interface. The default client is routerd-dhcpv6-client. networkd, dhcp6c and dhcpcd are recognized alternatives with platform-specific generation, but the live supervisor/status socket path is built around the routerd daemon; do not assume equivalent lifecycle support.
- profile and prefixLength influence generated external-client configuration and restored-snapshot matching. The supervised routerd-dhcpv6-client command does not receive a prefix-length or profile flag: its acquisition does not use these fields as a length hint. clientDUID is passed to it.
- IAID/duidType YAML controls are not supported; the routerd daemon defaults IA_PD IAID to 1 and derives DUID-LL from the uplink MAC unless clientDUID is specified. required does not implement a boot-blocking requirement.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # optional; string
    clientDUID: "<string>" # optional; string
    interface: "<string>" # Required; string
    prefixLength: 1 # optional; integer; minimum=1; maximum=128
    profile: "default" # optional; string; enum=["default", "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd"]
    required: true # optional; boolean
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
| `spec.interface` | Required logical uplink interface in this Router. An external dhcp6c/dhcpcd PD client must match a DHCPv6Address client declared on the same interface. |
| `spec.client` | Default routerd-dhcpv6-client; accepted alternatives networkd/dhcp6c/dhcpcd. Current supervision can also synthesize a routerd DHCPv6 systemd unit, so selecting an external client is not a general guarantee that the built-in daemon is disabled. |
| `spec.profile` | Empty/default, ntt-ngn-direct-hikari-denwa or ntt-hgw-lan-pd. NTT profiles supply /60 and link-layer DUID defaults to supporting generation paths; they do not configure provider credentials or change the built-in daemon command. |
| `spec.prefixLength` | Optional 1–128. Omitted/0 means /60 for NTT profiles and no configured hint for default; it is not a guarantee of the prefix size assigned by the server. See the built-in acquisition limitation above. |
| `spec.clientDUID` | Optional DUID as unseparated even-length hexadecimal. Colons/whitespace are rejected. Empty lets the built-in client derive DUID-LL from MAC; identity affects whether a restored lease can be reused. |
| `spec.required` | Boolean default false, decoded but not consumed as a required/startup wait policy by current runtime. Dependents should use actual Bound status and dependency predicates. |
| `spec.when` | Optional common state condition, default unconditional. Supervised client generation filters resolved conditions; false can stop acquisition while the declared resource remains. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/profiles.go](https://github.com/imksoo/routerd/blob/main/pkg/api/profiles.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [cmd/routerd-dhcpv6-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dhcpv6-client/main.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
