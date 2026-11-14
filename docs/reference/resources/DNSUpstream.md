# DNSUpstream

[Resource configuration reference](../resources.md#dnsupstream) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

A DNS server endpoint referenced by DNSForwarder; declaration alone does not start a listener or forward queries.

## Conditions and references

- protocol is required despite an internal URL-builder fallback to udp. At least one of address or addressFrom is required; they may coexist and their endpoints are combined. Dynamic addressFrom is currently UDP-only.
- Required unresolved addressFrom values suspend the containing forwarder source even if static addresses are available; optional missing values are skipped. Bootstrap references differ: a static bootstrap list can satisfy a missing required dynamic bootstrap.
- sourceInterface is passed verbatim to socket binding, with no logical-name or Kind/name resolution. On Linux use a declared interface whose metadata.name equals its OS ifname. The FreeBSD socket path rejects direct ifname binding, while normal configuration validation does not offer a valid fib:<n> alternative here.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DNSUpstream
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # optional; string
    addressFrom: # optional; array
      - # object
        field: "<string>" # optional; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    bootstrap: # optional; array
      - "<string>" # string
    bootstrapFrom: # optional; array
      - # object
        field: "<string>" # Required; string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    path: "<string>" # optional; string
    port: 1 # optional; integer; minimum=1; maximum=65535
    protocol: "udp" # Required; string; enum=["udp", "tcp", "dot", "doh"]
    sourceInterface: "<string>" # optional; string
    tlsName: "<string>" # optional; string
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
| `spec.protocol` | Required udp, tcp, dot or doh. Use lowercase canonical spelling. UDP/TCP use plaintext DNS, DoT TLS, DoH HTTPS. |
| `spec.address` | Server IP or hostname without scheme, port or path. Required if addressFrom is empty. No implicit server; hostname resolution uses bootstrap when supplied. |
| `spec.addressFrom` | Optional UDP-only common status-value source list. Each item requires existing resource and explicit provided field; optional defaults false. Dynamic bare addresses normalize to UDP port 53 independently of this resource port. Sources may return multiple servers. |
| `spec.port` | Optional 1–65535; omitted/0 defaults to 53 for udp/tcp, 853 for dot, 443 for doh. Used for the static address endpoint, not bare values returned by addressFrom. |
| `spec.path` | DoH URL path, default /dns-query; when supplied for doh it must start with /. Other protocols do not consume it. |
| `spec.tlsName` | DoT TLS server name for certificate verification/SNI; omitted uses the endpoint hostname. Explicit tlsName is rejected for DoH. UDP/TCP ignore it even though accepted by validation. |
| `spec.bootstrap` | Optional DNS server list for resolving upstream hostnames. Empty together with unresolved/absent bootstrapFrom uses the OS resolver only when no required dynamic dependency is waiting. All referenced upstreams in a forwarder share merged bootstrap settings. |
| `spec.bootstrapFrom` | Optional required-field source list. resource and field must be nonempty, optional defaults false. Use an existing Kind/name and its provided status field; unlike ordinary StatusValueSourceSpec, current validation does not check target existence or provided-field membership for this distinct type. Dynamic and static bootstrap servers are combined; static bootstrap satisfies a missing required dynamic value. |
| `spec.sourceInterface` | Optional declared Interface or WireGuardInterface name. Default empty lets OS routing choose. Binding is shared at forwarder-pool level; see the literal-name and FreeBSD restrictions above. |
| `spec.when` | Optional common state predicate; default unconditional. Filtering this upstream can leave its forwarder without usable endpoints. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dns.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dns.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/dnsresolver/dnsresolver.go](https://github.com/imksoo/routerd/blob/main/pkg/dnsresolver/dnsresolver.go) · [cmd/routerd-dns-resolver/upstream.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream.go) · [cmd/routerd-dns-resolver/upstream_bind_linux.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream_bind_linux.go) · [cmd/routerd-dns-resolver/upstream_bind_freebsd.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dns-resolver/upstream_bind_freebsd.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `address`, `url`

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
