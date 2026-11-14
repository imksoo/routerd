# HealthCheck

[Resource configuration reference](../resources.md#healthcheck) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Probe an IP target and publish Healthy/Unhealthy state for route selection. A successful probe verifies only its configured protocol and path.

## Conditions and references

- Specify target explicitly for operation. targetSource and interface influence validation/plan presentation, but the current daemon renderer passes target directly without resolving an automatic default gateway or DS-Lite remote target; the helper rejects an empty target.
- Source binding and socket marks are derived from referring EgressRoutePolicy candidates/targets, not exposed as HealthCheck YAML fields. Conflicting derived bindings cannot select a unique path. DS-Lite-associated probes require their binding to be available.
- TCP checks connection establishment only. DNS resolves example.com A records through the target server. HTTP sends GET http://<target>:<port>/ and accepts status 200–499, including 4xx; it does not check an application-specific URL or response body.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: HealthCheck
  metadata:
    name: "<name>"
  spec:
    [ addressFamily: ("ipv4" | "ipv6") ]
    [ enabled: <boolean> ]
    [ healthyThreshold: <integer> ]
    [ interface: <string> ]
    [ interval: <string> ]
    [ port: <integer> ]
    [ protocol: ("" | "icmp" | "tcp" | "dns" | "http") ]
    [ role: ("link" | "next-hop" | "internet" | "service" | "policy") ]
    [ target: <string> ]
    [ targetSource: ("auto" | "static" | "defaultGateway" | "dsliteRemote") ]
    [ timeout: <string> ]
    [ type: ("ping") ]
    [ unhealthyThreshold: <integer> ]
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
| `spec.enabled` | *Optional* | boolean | Default true; false disables probing. |
| `spec.type` | *Optional* | string; enum=["ping"] | Default ping; only ping is accepted. Use protocol to choose TCP/DNS/HTTP despite the type name. |
| `spec.role` | *Optional* | string; enum=["link", "next-hop", "internet", "service", "policy"] | Optional link/next-hop/internet/service/policy classification; validation/plan default next-hop. It does not choose the probe target or protocol at runtime. |
| `spec.addressFamily` | *Optional* | string; enum=["ipv4", "ipv6"] | Optional ipv4/ipv6. Target validation treats omission as ipv4, so specify ipv6 with an IPv6 target. This is not DNS hostname resolution: target must be an IP literal. |
| `spec.target` | **Conditionally required** | string | Bare IP address matching addressFamily. Semantically required when targetSource is static; also operationally required by the current helper for all modes. No usable runtime default. |
| `spec.targetSource` | *Optional* | string; enum=["auto", "static", "defaultGateway", "dsliteRemote"] | auto by validation default; static/defaultGateway/dsliteRemote also accepted. The current renderer does not implement their automatic target lookup; see the operational restriction above. |
| `spec.interface` | *Optional* | string | Optional logical interface/PPPoESession/DSLiteTunnel name; if supplied it must exist in the shared interface/tunnel index. This field alone does not become --source-interface in the current helper renderer. |
| `spec.protocol` | *Optional* | string; enum=["", "icmp", "tcp", "dns", "http"] | icmp by default; tcp/dns/http select the checks described above. HTTP is plaintext, not HTTPS. |
| `spec.port` | *Optional* | integer; minimum=0; maximum=65535 | 0–65535; default 0 delegates to the protocol: TCP 443, DNS 53, HTTP 80. ICMP does not use a port. |
| `spec.interval` | *Optional* | string | Positive Go duration; helper/runtime default 30s. The plan observer currently displays 60s on omission, which is not the helper default. |
| `spec.timeout` | *Optional* | string | Positive Go duration per probe, default 3s. This is the configured probe timeout, not a router reboot deadline. |
| `spec.healthyThreshold` | *Optional* | integer; minimum=1 | Consecutive successful probes to Healthy, default 1. A failure resets the success streak. Use positive integers; explicit 0 is outside the schema minimum. |
| `spec.unhealthyThreshold` | *Optional* | integer; minimum=1 | Consecutive failures to Unhealthy, default 3. A success resets the failure streak; intermediate failures report Failing. |
| `spec.when` | *Optional* | object | Optional common activation predicate; omitted means unconditional. Refer to common fields for nested state/all/any and their conditions. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/healthcheck/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/healthcheck/engine.go) · [pkg/healthcheck/dslite_binding.go](https://github.com/imksoo/routerd/blob/main/pkg/healthcheck/dslite_binding.go) · [pkg/render/healthcheck_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/healthcheck_systemd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [cmd/routerd-healthcheck/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-healthcheck/main.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `role`, `target`, `sourceAddress`, `sourceInterface`, `protocol`, `consecutiveFailed`, `lastCheckedAt`

[Example](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
