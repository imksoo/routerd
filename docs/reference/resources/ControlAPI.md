# ControlAPI

[Resource configuration reference](../resources.md#controlapi) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Enables the HTTP control API for reading state and changing configuration. Unix-socket control and the WebConsole are separate.

## Conditions and references

- Only one ControlAPI resource is supported. Explicit HTTP CLI flags override the matching resource-derived settings. Without a resource or enabling CLI flag, HTTP control is disabled.
- tls.certFile and tls.keyFile must be set together. clientCAFile additionally requires both and enables required, verified client certificates. tokenFrom.file and tokenFrom.env are mutually exclusive.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ControlAPI
  metadata:
    name: "<name>"
  spec:
    allowCIDRs: # optional; array
      - "<string>" # string
    enabled: true # optional; boolean
    listenAddress: "<string>" # optional; string
    port: 1 # optional; integer; minimum=1; maximum=65535
    tls: # optional; object
      certFile: "<string>" # optional; string
      clientCAFile: "<string>" # optional; string
      keyFile: "<string>" # optional; string
    tokenFrom: # optional; object
      base64: true # optional; boolean
      env: "<string>" # optional; string
      file: "<string>" # optional; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.enabled` | Default true when the resource exists; default disabled when absent. An explicit HTTP-listen CLI flag enables it. |
| `spec.listenAddress` | Bind IP, default 127.0.0.1. Nonblank values must be IP addresses; this does not itself change the source CIDR allowlist. |
| `spec.port` | TCP port, default 65432. Decoded zero selects the default; other values must be 1–65535. |
| `spec.allowCIDRs` | Source-IP allowlist, default 127.0.0.1/32 and ::1/128. A nonempty resource list replaces that default. Valid CIDRs are required; 0.0.0.0/0 and ::/0 are rejected. This is separate from firewall rules. |
| `spec.tokenFrom` | Optional bearer-token source. Default absent means no bearer-token check; source CIDR filtering still applies. Set exactly one of file or env when using a token. Not a reference to a routerd resource. |
| `spec.tokenFrom.file` | Token file path, default empty; mutually exclusive with env. Read by the routerd process at HTTP API setup; inaccessible files fail setup. |
| `spec.tokenFrom.env` | Name of a variable in the routerd process environment, default empty; mutually exclusive with file. Missing variables fail HTTP API setup. |
| `spec.tokenFrom.base64` | Default false. True decodes the trimmed source using standard Base64, then trims the decoded token. A malformed or empty final token fails setup. |
| `spec.tls` | Optional server TLS settings. With no certificate/key, the HTTP listener is plaintext. |
| `spec.tls.certFile` | Server certificate file, default absent; requires keyFile. Loaded as an X.509 key pair at startup; TLS minimum is 1.2. |
| `spec.tls.keyFile` | Server private-key file, default absent; requires certFile. This is a local file path, not a resource reference. |
| `spec.tls.clientCAFile` | Optional PEM client-CA bundle. Requires certFile/keyFile and makes client certificate verification mandatory. Missing/invalid bundles fail startup. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [cmd/routerd/cmd_serve.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_serve.go) · [pkg/controlapi/server.go](https://github.com/imksoo/routerd/blob/main/pkg/controlapi/server.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

No fields are registered for this kind in the ordinary status-reference allowlist.

[Example](https://github.com/imksoo/routerd/blob/main/examples/pve-minimal-rr-b.yaml)
