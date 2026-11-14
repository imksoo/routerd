# IPAddressSet

[Resource configuration reference](../resources.md#ipaddressset) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Share literal and DNS-resolved IP addresses among supported nftables NAT, routing and firewall rules. It does not create routes or start a DNS service.

## Conditions and references

- At least one address or name is required; both lists may coexist. Addresses are individual IPv4/IPv6 literals, not CIDRs. Duplicates in each configured list are rejected; resolved results are combined and deduplicated.
- Consumers reference a bare name or IPAddressSet/name. Supported nftables consumers include LocalServiceRedirect, NAT44Rule/FlowDNATPinhole, EgressRoutePolicy and FirewallRule/FlowPinhole. Declaring an unused set only reports Observed/referenced:false and does not resolve names or create a kernel set.
- The consumer renderer must create the target nftables set before refresh; missing sets yield Pending/SetMissing. Name resolution uses A and AAAA queries through /etc/resolv.conf, not DNSUpstream resource references. Partial DNS failure is reported while usable remaining addresses can still be applied. Total resolution failure yields Pending/ResolveFailed and does not replace the existing set in that round.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPAddressSet
  metadata:
    name: "<name>"
  spec:
    addresses: # optional; array
      - "<string>" # string
    names: # optional; array
      - "<string>" # string
    refreshInterval: "<string>" # optional; string
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
| `spec.addresses` | Optional literal IPv4/IPv6 list, default empty; required when names is empty. IPv4-mapped IPv6 is normalized to IPv4. No subnet expansion. |
| `spec.names` | Optional DNS names, default empty; required when addresses is empty. Blank names and duplicates after trimming are rejected. A/AAAA results are merged with literals; no wildcard-name expansion. |
| `spec.refreshInterval` | Optional Go duration. Default schedule is half the smallest positive DNS TTL, with a 1m floor; without a positive TTL, 5m is assumed, giving 2m30s. A positive configured interval only shortens that schedule and can be below 1m. Zero/negative parses but has no shortening effect. Actual refresh occurs on controller reconciliation. |
| `spec.when` | Optional common activation predicate; default unconditional. Consult common fields for state/all/any and consumer activation behavior. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/controller/chain/ipaddressset.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipaddressset.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-local-dns-redirect.yaml)
