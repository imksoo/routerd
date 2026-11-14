# LocalServiceRedirect

[Resource configuration reference](../resources.md#localserviceredirect) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

Redirect matching traffic received on a declared interface to a service port on the router itself, using nftables prerouting NAT.

## Conditions and references

- interface and a nonempty rules list are required. Each rule requires protocols, destinationSetRef and both ports. These are AND conditions per rule; selected protocol alternatives are OR.
- Matching uses the received interface, destination address set and destination port. It affects incoming packets, not locally originated OUTPUT traffic. The target is local redirect, not forwarding to a remote backend; use PortForward/IngressService for other use cases.
- Provide the local listening service and its firewall access separately. The redirect does not start DNSResolver or another daemon and does not add a universal firewall allow rule. IPv4/IPv6 rules are emitted according to the referenced set families; DNS-backed sets are refreshed by IPAddressSet. This implementation renders nftables rules, not FreeBSD PF redirects.
- Rules are sorted by resource ID and then rule name, not YAML list order. An omitted name becomes its zero-based decimal list index; lexical sorting can put 10 before 2. Use explicit ordered names when rules overlap.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: LocalServiceRedirect
  metadata:
    name: "<name>"
  spec:
    interface: "<string>" # Required; string
    rules: # Required; array
      - # object
        destinationPort: 1 # Required; integer; minimum=1; maximum=65535
        destinationSetRef: "<string>" # Required; string
        name: "<string>" # optional; string
        protocols: # Required; array
          - "<string>" # string
        redirectPort: 1 # Required; integer; minimum=1; maximum=65535
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
| `spec.interface` | Required logical interface declared in the same Router; resolves to an OS input-interface name. No default or status source. |
| `spec.rules` | Required nonempty redirect list. Rules do not inherit missing protocol, address set or ports from another rule. |
| `spec.rules[].name` | Optional ordering/comment label, default decimal zero-based index. It is not a resource reference; duplicates are not rejected here. |
| `spec.rules[].protocols` | Required nonempty subset of tcp/udp; duplicates and other protocols are rejected. No implicit both-protocol default. |
| `spec.rules[].destinationSetRef` | Required bare IPAddressSet name or IPAddressSet/name, declared in the same Router. No field selector or automatic inline address list. |
| `spec.rules[].destinationPort` | Required original destination port 1–65535; no range syntax or default. |
| `spec.rules[].redirectPort` | Required local service port 1–65535. Must have a suitable listener reachable after redirect; YAML validation does not check that listener. |
| `spec.when` | Optional common activation predicate, default unconditional. See common fields for state/all/any and reference constraints. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/ipaddressset.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipaddressset.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)
