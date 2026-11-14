# WireGuardPeer

[Resource configuration reference](../resources.md#wireguardpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Define a peer on a declared WireGuardInterface. Its AllowedIPs configure WireGuard cryptographic routing; they do not automatically create ordinary IP routes in this controller.

## Conditions and references

- The peer is applied as part of its interface complete wg setconf configuration, not as an independently scheduled service. An explicit peer with the same metadata.name replaces a peersFrom-generated peer, even if its interface differs.
- Inline presharedKey takes precedence over presharedKeyFile; they are not mutually exclusive. The file is read and trimmed only when no inline key is supplied; missing, unreadable or empty files fail real application.
- Connected means a handshake timestamp was observed, with its age reported separately; it is not a guarantee that the handshake is recent or that a destination is reachable. A peer with an endpoint but no handshake is Waiting/NoHandshakeYet.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardPeer
  metadata:
    name: "<name>"
  spec:
    allowedIPs: # Required; array
      - "<string>" # string
    endpoint: "<string>" # optional; string
    interface: "<string>" # Required; string
    persistentKeepalive: 0 # optional; integer; minimum=0; maximum=65535
    presharedKey: "<string>" # optional; string
    presharedKeyFile: "<string>" # optional; string
    publicKey: "<string>" # Required; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.interface` | Required metadata.name of an existing WireGuardInterface in the same Router. An OS ifname alone is not sufficient. |
| `spec.publicKey` | Required peer public key in WireGuard base64 format. The validator checks nonempty text, not decoded key length; wg validates during application. |
| `spec.allowedIPs` | Required nonempty list of IPv4/IPv6 CIDR prefixes. Controls the peer selected for outgoing packets and accepted source addresses for incoming packets. No implicit prefix or route creation. |
| `spec.endpoint` | Optional peer host:UDP-port, using [IPv6]:port for IPv6. Empty omits Endpoint and permits learning the endpoint from received packets. Syntax/DNS reachability is not checked by routerd configuration validation; wg handles it at apply. |
| `spec.persistentKeepalive` | Seconds between keepalive packets, integer 0–65535; default 0 disables persistent keepalive. Not a handshake-success deadline. |
| `spec.presharedKey` | Optional additional symmetric key in WireGuard base64 format; default none. Overrides presharedKeyFile, without an exclusion error. |
| `spec.presharedKeyFile` | Optional router-local file containing the raw preshared key text, not an environment file. No key is generated for a missing file. Only read when presharedKey is empty. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/wireguard/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/wireguard/wireguard.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `latestEndpoint`, `latestHandshake`, `handshakeAgeSeconds`, `transferRxBytes`, `transferTxBytes`

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-wireguard.yaml)
