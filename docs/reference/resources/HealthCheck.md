# HealthCheck

[Resource configuration reference](../resources.md#healthcheck) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Probe an IP target and publish Healthy/Unhealthy state for route selection. A successful probe verifies only its configured protocol and path.

## Conditions and references

- Specify target explicitly for operation. targetSource and interface influence validation/plan presentation, but the current daemon renderer passes target directly without resolving an automatic default gateway or DS-Lite remote target; the helper rejects an empty target.
- Source binding and socket marks are derived from referring EgressRoutePolicy candidates/targets, not exposed as HealthCheck YAML fields. Conflicting derived bindings cannot select a unique path. DS-Lite-associated probes require their binding to be available.
- TCP checks connection establishment only. DNS resolves example.com A records through the target server. HTTP sends GET http://<target>:<port>/ and accepts status 200–499, including 4xx; it does not check an application-specific URL or response body.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: HealthCheck
  metadata:
    name: "<name>"
  spec:
    addressFamily: "ipv4" # optional; string; enum=["ipv4", "ipv6"]
    enabled: true # optional; boolean
    healthyThreshold: 1 # optional; integer; minimum=1
    interface: "<string>" # optional; string
    interval: "<string>" # optional; string
    port: 0 # optional; integer; minimum=0; maximum=65535
    protocol: "" # optional; string; enum=["", "icmp", "tcp", "dns", "http"]
    role: "link" # optional; string; enum=["link", "next-hop", "internet", "service", "policy"]
    target: "<string>" # optional; string
    targetSource: "auto" # optional; string; enum=["auto", "static", "defaultGateway", "dsliteRemote"]
    timeout: "<string>" # optional; string
    type: "ping" # optional; string; enum=["ping"]
    unhealthyThreshold: 1 # optional; integer; minimum=1
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
| `spec.enabled` | Default true; false disables probing. |
| `spec.type` | Default ping; only ping is accepted. Use protocol to choose TCP/DNS/HTTP despite the type name. |
| `spec.role` | Optional link/next-hop/internet/service/policy classification; validation/plan default next-hop. It does not choose the probe target or protocol at runtime. |
| `spec.addressFamily` | Optional ipv4/ipv6. Target validation treats omission as ipv4, so specify ipv6 with an IPv6 target. This is not DNS hostname resolution: target must be an IP literal. |
| `spec.target` | Bare IP address matching addressFamily. Semantically required when targetSource is static; also operationally required by the current helper for all modes. No usable runtime default. |
| `spec.targetSource` | auto by validation default; static/defaultGateway/dsliteRemote also accepted. The current renderer does not implement their automatic target lookup; see the operational restriction above. |
| `spec.interface` | Optional logical interface/PPPoESession/DSLiteTunnel name; if supplied it must exist in the shared interface/tunnel index. This field alone does not become --source-interface in the current helper renderer. |
| `spec.protocol` | icmp by default; tcp/dns/http select the checks described above. HTTP is plaintext, not HTTPS. |
| `spec.port` | 0–65535; default 0 delegates to the protocol: TCP 443, DNS 53, HTTP 80. ICMP does not use a port. |
| `spec.interval` | Positive Go duration; helper/runtime default 30s. The plan observer currently displays 60s on omission, which is not the helper default. |
| `spec.timeout` | Positive Go duration per probe, default 3s. This is the configured probe timeout, not a router reboot deadline. |
| `spec.healthyThreshold` | Consecutive successful probes to Healthy, default 1. A failure resets the success streak. Use positive integers; explicit 0 is outside the schema minimum. |
| `spec.unhealthyThreshold` | Consecutive failures to Unhealthy, default 3. A success resets the failure streak; intermediate failures report Failing. |
| `spec.when` | Optional common activation predicate; omitted means unconditional. Refer to common fields for nested state/all/any and their conditions. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/healthcheck/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/healthcheck/engine.go) · [pkg/healthcheck/dslite_binding.go](https://github.com/imksoo/routerd/blob/main/pkg/healthcheck/dslite_binding.go) · [pkg/render/healthcheck_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/healthcheck_systemd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [cmd/routerd-healthcheck/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-healthcheck/main.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
