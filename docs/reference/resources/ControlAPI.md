# ControlAPI

[Resource configuration reference](../resources.md#controlapi) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Enables the HTTP control API for reading state and changing configuration. Unix-socket control and the WebConsole are separate.

## Conditions and references

- Only one ControlAPI resource is supported. Explicit HTTP CLI flags override the matching resource-derived settings. Without a resource or enabling CLI flag, HTTP control is disabled.
- tls.certFile and tls.keyFile must be set together. clientCAFile additionally requires both and enables required, verified client certificates. tokenFrom.file and tokenFrom.env are mutually exclusive.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: ControlAPI
  metadata:
    name: "<name>"
  spec:
    [ allowCIDRs:
        - <string>
    ]
    [ enabled: <boolean> ]
    [ listenAddress: <string> ]
    [ port: <integer> ]
    [ tls:
        [ certFile: <string> ]
        [ clientCAFile: <string> ]
        [ keyFile: <string> ]
    ]
    [ tokenFrom:
        [ base64: <boolean> ]
        [ env: <string> ]
        [ file: <string> ]
    ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.enabled` | *Optional* | boolean | Default true when the resource exists; default disabled when absent. An explicit HTTP-listen CLI flag enables it. |
| `spec.listenAddress` | *Optional* | string | Bind IP, default 127.0.0.1. Nonblank values must be IP addresses; this does not itself change the source CIDR allowlist. |
| `spec.port` | *Optional* | integer; minimum=1; maximum=65535 | TCP port, default 65432. Decoded zero selects the default; other values must be 1–65535. |
| `spec.allowCIDRs` | *Optional* | array | Source-IP allowlist, default 127.0.0.1/32 and ::1/128. A nonempty resource list replaces that default. Valid CIDRs are required; 0.0.0.0/0 and ::/0 are rejected. This is separate from firewall rules. |
| `spec.tokenFrom` | *Optional* | object | Optional bearer-token source. Default absent means no bearer-token check; source CIDR filtering still applies. Set exactly one of file or env when using a token. Not a reference to a routerd resource. |
| `spec.tokenFrom.file` | **Conditionally required** | string | Token file path, default empty; mutually exclusive with env. Read by the routerd process at HTTP API setup; inaccessible files fail setup. |
| `spec.tokenFrom.env` | **Conditionally required** | string | Name of a variable in the routerd process environment, default empty; mutually exclusive with file. Missing variables fail HTTP API setup. |
| `spec.tokenFrom.base64` | *Optional* | boolean | Default false. True decodes the trimmed source using standard Base64, then trims the decoded token. A malformed or empty final token fails setup. |
| `spec.tls` | *Optional* | object | Optional server TLS settings. With no certificate/key, the HTTP listener is plaintext. |
| `spec.tls.certFile` | **Conditionally required** | string | Server certificate file, default absent; requires keyFile. Loaded as an X.509 key pair at startup; TLS minimum is 1.2. |
| `spec.tls.keyFile` | **Conditionally required** | string | Server private-key file, default absent; requires certFile. This is a local file path, not a resource reference. |
| `spec.tls.clientCAFile` | *Optional* | string | Optional PEM client-CA bundle. Requires certFile/keyFile and makes client certificate verification mandatory. Missing/invalid bundles fail startup. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [cmd/routerd/cmd_serve.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_serve.go) · [pkg/controlapi/server.go](https://github.com/imksoo/routerd/blob/main/pkg/controlapi/server.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

No fields are registered for this kind in the ordinary status-reference allowlist.

[Example](https://github.com/imksoo/routerd/blob/main/examples/pve-minimal-rr-b.yaml)
