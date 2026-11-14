# TailscaleNode

[Resource configuration reference](../resources.md#tailscalenode) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Manage Tailscale enrollment options through a routerd-generated systemd oneshot on Linux or rc.d script on FreeBSD, then observe tailscale status --json. Tailscale/Headscale server-side approvals remain separate.

## Conditions and references

- authKey is mutually exclusive with authKeyEnv and authKeyFile. authKeyEnv and authKeyFile may be combined: the former chooses the variable read from the latter; without authKeyEnv, a supplied file uses TS_AUTHKEY.
- authKeyFile is an environment-variable assignment file, not a raw key. Use TS_AUTHKEY=tskey-... (or the authKeyEnv variable). On FreeBSD use one unquoted assignment on its own line; missing, duplicate or empty selected values fail. On Linux it is loaded as systemd EnvironmentFile.
- A present node reserves UDP 41641 against configured listener conflicts. The generated command uses tailscale up --timeout=30s. Applied/Rendered describes service application, while Running is reported only after observing BackendState=Running; neither proves an advertised subnet is reachable.
- FreeBSD rc.d refuses mutation of a pre-existing tailscaled process whose routerd ownership marker is absent. Linux uses the shared tailscaled.service and a routerd-tailscale-<name>.service enrollment unit. state: absent removes the enrollment unit; it is not a package-uninstall or tailnet node-deletion request.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: TailscaleNode
  metadata:
    name: "<name>"
  spec:
    acceptDNS: true # optional; boolean
    acceptRoutes: true # optional; boolean
    advertiseExitNode: true # optional; boolean
    advertiseRoutes: # optional; array
      - "<string>" # string
    advertiseTags: # optional; array
      - "<string>" # string
    authKey: "<string>" # optional; string
    authKeyEnv: "<string>" # optional; string
    authKeyFile: "<string>" # optional; string
    hostname: "<string>" # optional; string
    loginServer: "<string>" # optional; string
    shieldsUp: true # optional; boolean
    ssh: true # optional; boolean
    state: "" # optional; string; enum=["", "present", "absent"]
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
| `spec.state` | present by default; absent removes the managed enrollment service. No package removal or control-server node deletion is performed by this field. |
| `spec.hostname` | Optional tailscale up --hostname value, not Router.metadata.name or an OS hostname change. Omitted leaves the flag absent. |
| `spec.loginServer` | Optional control-server URL for --login-server, for example a Headscale server. Omitted delegates to the Tailscale client configuration/default; routerd provides no URL default or resource lookup. |
| `spec.authKey` | Optional inline enrollment key; excludes authKeyEnv/authKeyFile. Omitted supplies no key flag unless another key source is configured; existing client login state or interactive enrollment requirements then apply. |
| `spec.authKeyEnv` | Optional environment variable name for the enrollment key, not the key itself. May select a variable from authKeyFile. Linux uses the generated service environment; this does not automatically copy routerd process environment into that service. |
| `spec.authKeyFile` | Optional absolute environment-file path on the router; selected variable defaults to TS_AUTHKEY. Use a simple unquoted NAME=value line for Linux/FreeBSD portability. Not a Secret resource reference. |
| `spec.advertiseExitNode` | Default false; true emits --advertise-exit-node. Does not approve exit-node use on the control server or make this router use another exit node. |
| `spec.advertiseRoutes` | Optional IPv4/IPv6 CIDR list passed as comma-separated --advertise-routes. Empty omits the flag, rather than explicitly passing an empty route list. Required control-server approval is separate. |
| `spec.advertiseTags` | Optional tag strings passed as comma-separated --advertise-tags. Validator rejects empty/whitespace entries but does not enforce a tag: prefix or verify server ACL permissions. Empty omits the flag. |
| `spec.acceptRoutes` | Optional boolean for --accept-routes. Omitted leaves the flag absent and delegates behavior to the Tailscale client; explicit false is emitted, unlike omission. |
| `spec.acceptDNS` | Optional boolean for --accept-dns. Omitted does not force either true or false; explicit values are passed to the client. This does not configure routerd DNSResolver/DNSForwarder resources. |
| `spec.ssh` | Default false omits the flag; true emits --ssh to request Tailscale SSH. This is separate from OS sshd and routerd management access. |
| `spec.shieldsUp` | Optional boolean for --shields-up; omission leaves the client flag unset, while explicit true/false is emitted. |
| `spec.when` | Optional common activation predicate; omission is unconditional. Consult common fields for state/all/any and field-reference rules. It gates the generated service rather than deleting a tailnet node. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/tailscale_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/tailscale_systemd.go) · [pkg/render/freebsd_rcd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd_rcd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/tailscale/tailscale.go](https://github.com/imksoo/routerd/blob/main/pkg/tailscale/tailscale.go) · [pkg/render/firewall_holes.go](https://github.com/imksoo/routerd/blob/main/pkg/render/firewall_holes.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `tailnetName`, `peerCount`, `advertiseRoutes`

[Example](https://github.com/imksoo/routerd/blob/main/examples/tailscale-exit-subnet.yaml)
