# IngressService

[Resource configuration reference](../resources.md#ingressservice) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

Expose an IPv4 TCP/UDP endpoint through backend selection and active health checks, with optional local DNS publication and hairpin NAT. This is layer-4 NAT, not an HTTP reverse proxy or TLS terminator.

## Conditions and references

- listen and at least one backend are required. Every backend needs exactly one address/addressFrom and a port; literal backend addresses may be IPv4 or hostnames. All data-plane forwarding remains IPv4. Backend names should be stable and unique because health history is keyed by name; unnamed entries use backend-0, backend-1, etc.
- Health checks normally reconcile every 5s regardless of healthCheck.interval; each backend is checked in turn. A missing health-check protocol defaults TCP, even for a UDP listen endpoint. Hostnames resolve to the lexicographically first IPv4; DNS failure may reuse the previous resolved address, which is still health-checked.
- NoHealthyBackends and policy.onNoHealthyBackends are status outputs, not a generated drop/reject guarantee. When there is no active backend, the NAT controller leaves the original backend definition in place; rendering can fall back to its first configured static backend. Do not rely on this setting for fail-closed behavior.
- failover chooses the first healthy backend in declaration order. Linux sourceHash/random can distribute across all healthy resolved backends; weights are unused. PF emits only the selected first target and does not implement those distribution algorithms. Static rendering has no health-store selection and can use the first declared backend.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: IngressService
  metadata:
    name: "<name>"
  spec:
    backends:
      -
        [ address: <string> ]
        [ addressFrom:
            field: <string>
            [ optional: <boolean> ]
            resource: <string>
        ]
        [ name: <string> ]
        port: <integer>
        [ weight: <integer> ]
    [ externalDNS: <boolean> ]
    [ hairpin:
        [ enabled: <boolean> ]
        [ interfaces:
            - <string>
        ]
        [ mode: ("" | "auto" | "manual" | "off") ]
    ]
    [ healthCheck:
        [ expectedBody: <string> ]
        [ expectedStatus:
            - <integer>
        ]
        [ healthyThreshold: <integer> ]
        [ host: <string> ]
        [ interval: <string> ]
        [ path: <string> ]
        [ protocol: ("" | "tcp" | "http" | "https") ]
        [ timeout: <string> ]
        [ tlsSkipVerify: <boolean> ]
        [ unhealthyThreshold: <integer> ]
    ]
    [ hostname: <string> ]
    listen:
      [ address: <string> ]
      [ addressFrom:
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
      ]
      interface: <string>
      port: <integer>
      protocol: ("tcp" | "udp")
    [ policy:
        [ onNoHealthyBackends: ("" | "drop" | "reject") ]
        [ selection: ("" | "failover" | "sourceHash" | "random") ]
    ]
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
| `spec.listen` | **Required** | object | Required IPv4 ingress endpoint. This creates NAT/firewall rules, not an application listener or address assignment. |
| `spec.listen.interface` | **Required** | string | Required bare declared logical interface name, not Kind/name. Must exist in the configuration interface index and resolve to an OS ifname. |
| `spec.listen.address` | *Optional* | string | Optional bare IPv4 destination address. Mutually exclusive with addressFrom; omission matches any destination address on the listen interface. |
| `spec.listen.addressFrom` | *Optional* | object | Optional common resource/field/optional reference, mutually exclusive with address. NAT/static rendering supports declared IPv4StaticAddress.address or VirtualAddress.address, not arbitrary runtime status. Generic reference validation does not guarantee renderer support. |
| `spec.listen.protocol` | **Required** | string; enum=["tcp", "udp"] | Required tcp or udp, no default. This selects forwarded traffic; it does not select the health-check protocol. |
| `spec.listen.port` | **Required** | integer; minimum=1; maximum=65535 | Required destination port 1–65535. No range/list and no default. |
| `spec.hairpin` | *Optional* | object | Optional loopback-through-public-address NAT settings. Requires a resolved listen address when active. PortForward defaults off; IngressService defaults auto. |
| `spec.hairpin.enabled` | *Optional* | boolean | Default false. With omitted mode, true selects manual on PortForward; IngressService still defaults auto. Explicit off disables hairpin even when enabled is true. |
| `spec.hairpin.mode` | *Optional* | string; enum=["", "auto", "manual", "off"] | auto, manual or off. auto detects the listen interface when listen and backend share a declared IPv4 subnet or the same private /24; it does not discover arbitrary LAN routes. manual needs enabled=true and interfaces to produce rules. |
| `spec.hairpin.interfaces` | *Optional* | array | Optional bare declared logical interface names, no duplicates; enabled/manual requires a nonempty list. auto may additionally use the listen interface. IPv4 DNAT plus masquerade/PF SNAT ensures a return through the router. |
| `spec.when` | *Optional* | object | Optional common activation condition, default unconditional. False conditions exclude the resource from effective generation; see common fields for predicates/schema limits. |
| `spec.hostname` | *Optional* | string | Optional FQDN for local DNS publication and status, default empty. Does not select HTTP Host routing or health-check Host/SNI. Requires an appropriate DNSZone served by DNSResolver for publication. |
| `spec.externalDNS` | *Optional* | boolean | Default false. True skips routerd’s automatic local hostname record and DNS-coverage warnings; it does not update an external DNS provider. |
| `spec.backends` | **Required** | array | Required nonempty backend list; order selects failover priority. Configuration permits hostnames, but static NAT rendering needs IPv4 resolution and does not perform DNS itself. |
| `spec.backends[].name` | *Optional* | string | Optional stable health-history key, default backend-<zero-based index>. Duplicate names are not rejected by this validator and can mix history; use unique names. |
| `spec.backends[].address` | *Optional* | string | Bare IPv4 or hostname, mutually exclusive with addressFrom; one is required. Hostname resolution uses IPv4 only and retains a previous address on DNS failure when available. |
| `spec.backends[].addressFrom` | *Optional* | object | Optional common resource/field/optional source, excluding address. The active checker reads runtime status; optional does not make an unresolved backend healthy. Static rendering supports only declared address fields described above. |
| `spec.backends[].port` | **Required** | integer; minimum=1; maximum=65535 | Required backend port 1–65535. Used by data-plane translation and the configured health-check protocol. |
| `spec.backends[].weight` | *Optional* | integer; minimum=0 | Nonnegative integer accepted, default 0; current health selection and Linux/PF NAT rendering do not read it. No weighted distribution. |
| `spec.healthCheck` | *Optional* | object | Optional active check settings. Omission still enables TCP checking rather than disabling checks. |
| `spec.healthCheck.protocol` | *Optional* | string; enum=["", "tcp", "http", "https"] | tcp (default), http or https. Independent from listen.protocol. HTTP(S) sends GET directly to the resolved backend address and port. |
| `spec.healthCheck.interval` | *Optional* | string | Go duration accepted by validation but not read by the controller. Actual scheduling uses the chain’s 5s cadence plus reconciliation events. |
| `spec.healthCheck.timeout` | *Optional* | string | Positive Go duration, effective default 1s. Omitted/zero/negative values use 1s even though syntax validation accepts nonpositive durations. Applies per backend probe, not the whole cycle or DNS lookup. |
| `spec.healthCheck.path` | *Optional* | string | HTTP(S) URL path, default /. Nonempty must begin with /. Not used by TCP checks. |
| `spec.healthCheck.host` | *Optional* | string | Optional HTTP Host header and HTTPS TLS ServerName/SNI override, default empty. Without it, URL uses the resolved IP. No whitespace/control characters. |
| `spec.healthCheck.expectedStatus` | *Optional* | array | Optional exact HTTP status allowlist 100–599. Omitted/empty accepts 200–399; HTTP client follows redirects by default before evaluating the resulting response. |
| `spec.healthCheck.tlsSkipVerify` | *Optional* | boolean | Default false. True disables HTTPS certificate/hostname verification for health probes only, not data-plane TLS handling. |
| `spec.healthCheck.expectedBody` | *Optional* | string | Optional case-sensitive substring in the first 1 MiB of HTTP response body. Default empty disables body matching; not a regular expression. |
| `spec.healthCheck.healthyThreshold` | *Optional* | integer; minimum=1 | Successful consecutive checks to promote healthy; omitted/0 means 1. Healthy state and counts are restored from backend status. Schema minimum 1 differs from accepted zero default. |
| `spec.healthCheck.unhealthyThreshold` | *Optional* | integer; minimum=1 | Consecutive failures to remove an already healthy backend; omitted/0 means 1. Initial unresolved backends start unhealthy. |
| `spec.policy` | *Optional* | object | Optional selection settings. Defaults failover/drop, but the no-healthy action is currently reported only. |
| `spec.policy.selection` | *Optional* | string; enum=["", "failover", "sourceHash", "random"] | failover (default), sourceHash or random. Linux sourceHash hashes client IPv4 source; random selects modulo equal backend count. One healthy backend falls back to failover. PF always emits one target. |
| `spec.policy.onNoHealthyBackends` | *Optional* | string; enum=["", "drop", "reject"] | drop (default) or reject accepted, but only copied to status.conditions.reason. No explicit drop/reject rule is generated; see the no-healthy fallback limitation above. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/ingressservice/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/ingressservice/controller.go) · [pkg/controller/nat44/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/nat44/controller.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/render/ingress.go](https://github.com/imksoo/routerd/blob/main/pkg/render/ingress.go) · [pkg/render/status_source.go](https://github.com/imksoo/routerd/blob/main/pkg/render/status_source.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `hostname`, `listenAddress`, `activeBackend`, `activeBackends`, `healthyBackends`, `totalBackends`, `backends`, `observedAt`, `dryRun`

[Example](https://github.com/imksoo/routerd/blob/main/examples/k8s-api-vip-dualstack.yaml)
