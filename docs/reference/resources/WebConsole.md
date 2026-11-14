# WebConsole

[Resource configuration reference](../resources.md#webconsole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Serves the read-only router web console from routerd. Its HTTP listener is separate from the mutating ControlAPI listener.

## Conditions and references

- The runtime selects the first WebConsole resource; it does not start one listener per resource. A resolved listenAddressFrom takes precedence over listenAddress; the latter is a fallback, not a mutually exclusive option.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: WebConsole
  metadata:
    name: "<name>"
  spec:
    [ basePath: <string> ]
    [ enabled: <boolean> ]
    [ links:
        -
          [ description: <string> ]
          label: <string>
          url: <string>
    ]
    [ listenAddress: <string> ]
    [ listenAddressFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ port: <integer> ]
    [ title: <string> ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.enabled` | *Optional* | boolean | Default true when a WebConsole resource exists. False disables the console selected by the runtime. No resource means no console listener. |
| `spec.listenAddress` | *Optional* | string | Fallback bind IP, default 127.0.0.1. Must parse as an IP when nonblank. A resolved listenAddressFrom wins. |
| `spec.listenAddressFrom` | *Optional* | object | Value source for a bind IP; default absent. Uses status first, then supported configured-interface/address resolution. Requires resource and field. If unresolved and no listenAddress is supplied, console startup fails even if optional=true. |
| `spec.port` | *Optional* | integer; minimum=1; maximum=65535 | TCP port, default 8080. Zero in the decoded config means use this default; explicit nonzero values must be 1–65535. |
| `spec.basePath` | *Optional* | string | HTTP path prefix, default /. A nonempty value must begin with / and contain no NUL or line breaks. |
| `spec.title` | *Optional* | string | Console page title, default routerd. It does not rename the router or any resource. |
| `spec.links` | *Optional* | array | Optional additional console links, default empty. Entries with blank label or URL are omitted at display time. |
| `spec.links[].label` | **Conditionally required** | string | Visible link text, no default; blank values are not displayed. |
| `spec.links[].url` | **Conditionally required** | string | Link destination, no default. This is a URL, not Kind/name. The console trims surrounding whitespace. |
| `spec.links[].description` | *Optional* | string | Optional explanatory text, default empty; trimmed for display. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [cmd/routerd/cmd_serve.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_serve.go) · [pkg/webconsole/console.go](https://github.com/imksoo/routerd/blob/main/pkg/webconsole/console.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `listenAddress`, `port`

[Example](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
