# PortForward

[Resource configuration reference](../resources.md#portforward) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

Forward a TCP/UDP port to one IPv4 endpoint with optional hairpin NAT. Linux generates DNAT plus matching forward allowances; FreeBSD uses rdr pass.

## Conditions and references

- listen and target are required. target needs exactly one address/addressFrom and a port; a literal target must be bare IPv4, not hostname or CIDR. listen may omit both address forms, matching every destination on its interface.
- Address references must exist and declare a supported provided field, but this NAT renderer reads only declared IPv4StaticAddress.address/VirtualAddress.address. optional=true may omit a rule for an unresolved supported source, not enable arbitrary dynamic sources. There is no backend health check in PortForward.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: PortForward
  metadata:
    name: "<name>"
  spec:
    hairpin: # optional; object
      enabled: true # optional; boolean
      interfaces: # optional; array
        - "<string>" # string
      mode: "" # optional; string; enum=["", "auto", "manual", "off"]
    listen: # Required; object
      address: "<string>" # optional; string
      addressFrom: # optional; object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
      interface: "<string>" # Required; string
      port: 1 # Required; integer; minimum=1; maximum=65535
      protocol: "tcp" # Required; string; enum=["tcp", "udp"]
    target: # Required; object
      address: "<string>" # optional; string
      addressFrom: # optional; object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
      port: 1 # Required; integer; minimum=1; maximum=65535
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
| `spec.listen` | Required IPv4 ingress endpoint. This creates NAT/firewall rules, not an application listener or address assignment. |
| `spec.listen.interface` | Required bare declared logical interface name, not Kind/name. Must exist in the configuration interface index and resolve to an OS ifname. |
| `spec.listen.address` | Optional bare IPv4 destination address. Mutually exclusive with addressFrom; omission matches any destination address on the listen interface. |
| `spec.listen.addressFrom` | Optional common resource/field/optional reference, mutually exclusive with address. NAT/static rendering supports declared IPv4StaticAddress.address or VirtualAddress.address, not arbitrary runtime status. Generic reference validation does not guarantee renderer support. |
| `spec.listen.protocol` | Required tcp or udp, no default. This selects forwarded traffic; it does not select the health-check protocol. |
| `spec.listen.port` | Required destination port 1–65535. No range/list and no default. |
| `spec.hairpin` | Optional loopback-through-public-address NAT settings. Requires a resolved listen address when active. PortForward defaults off; IngressService defaults auto. |
| `spec.hairpin.enabled` | Default false. With omitted mode, true selects manual on PortForward; IngressService still defaults auto. Explicit off disables hairpin even when enabled is true. |
| `spec.hairpin.mode` | auto, manual or off. auto detects the listen interface when listen and backend share a declared IPv4 subnet or the same private /24; it does not discover arbitrary LAN routes. manual needs enabled=true and interfaces to produce rules. |
| `spec.hairpin.interfaces` | Optional bare declared logical interface names, no duplicates; enabled/manual requires a nonempty list. auto may additionally use the listen interface. IPv4 DNAT plus masquerade/PF SNAT ensures a return through the router. |
| `spec.when` | Optional common activation condition, default unconditional. False conditions exclude the resource from effective generation; see common fields for predicates/schema limits. |
| `spec.target` | Required single internal endpoint. Setting a target does not assign its IP or start its service. |
| `spec.target.address` | Bare IPv4 target, mutually exclusive with target.addressFrom; one is required. No default. |
| `spec.target.addressFrom` | Common resource/field/optional reference; same declared-field rendering limits as listen.addressFrom. Must resolve to IPv4. |
| `spec.target.port` | Required internal TCP/UDP port 1–65535, independent of listen.port. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/ingress.go](https://github.com/imksoo/routerd/blob/main/pkg/render/ingress.go) · [pkg/render/status_source.go](https://github.com/imksoo/routerd/blob/main/pkg/render/status_source.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `listenAddress`, `target`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-port-forward-web.yaml)
