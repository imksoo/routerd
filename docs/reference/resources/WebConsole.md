# WebConsole

[Resource configuration reference](../resources.md#webconsole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Serves the read-only router web console from routerd. Its HTTP listener is separate from the mutating ControlAPI listener.

## Conditions and references

- The runtime selects the first WebConsole resource; it does not start one listener per resource. A resolved listenAddressFrom takes precedence over listenAddress; the latter is a fallback, not a mutually exclusive option.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: WebConsole
  metadata:
    name: "<name>"
  spec:
    basePath: "<string>" # optional; string
    enabled: true # optional; boolean
    links: # optional; array
      - # object
        description: "<string>" # optional; string
        label: "<string>" # Required; string
        url: "<string>" # Required; string
    listenAddress: "<string>" # optional; string
    listenAddressFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    port: 1 # optional; integer; minimum=1; maximum=65535
    title: "<string>" # optional; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.enabled` | Default true when a WebConsole resource exists. False disables the console selected by the runtime. No resource means no console listener. |
| `spec.listenAddress` | Fallback bind IP, default 127.0.0.1. Must parse as an IP when nonblank. A resolved listenAddressFrom wins. |
| `spec.listenAddressFrom` | Value source for a bind IP; default absent. Uses status first, then supported configured-interface/address resolution. Requires resource and field. If unresolved and no listenAddress is supplied, console startup fails even if optional=true. |
| `spec.port` | TCP port, default 8080. Zero in the decoded config means use this default; explicit nonzero values must be 1–65535. |
| `spec.basePath` | HTTP path prefix, default /. A nonempty value must begin with / and contain no NUL or line breaks. |
| `spec.title` | Console page title, default routerd. It does not rename the router or any resource. |
| `spec.links` | Optional additional console links, default empty. Entries with blank label or URL are omitted at display time. |
| `spec.links[].label` | Visible link text, no default; blank values are not displayed. |
| `spec.links[].url` | Link destination, no default. This is a URL, not Kind/name. The console trims surrounding whitespace. |
| `spec.links[].description` | Optional explanatory text, default empty; trimmed for display. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [cmd/routerd/cmd_serve.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_serve.go) · [pkg/webconsole/console.go](https://github.com/imksoo/routerd/blob/main/pkg/webconsole/console.go)

[Example](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
