# WireGuardPeer

[Resource configuration reference](../resources.md#wireguardpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Define a peer on a declared WireGuardInterface. Its AllowedIPs configure WireGuard cryptographic routing; they do not automatically create ordinary IP routes in this controller.

## Conditions and references

- The peer is applied as part of its interface complete wg setconf configuration, not as an independently scheduled service. An explicit peer with the same metadata.name replaces a peersFrom-generated peer, even if its interface differs.
- Inline presharedKey takes precedence over presharedKeyFile; they are not mutually exclusive. The file is read and trimmed only when no inline key is supplied; missing, unreadable or empty files fail real application.
- Connected means a handshake timestamp was observed, with its age reported separately; it is not a guarantee that the handshake is recent or that a destination is reachable. A peer with an endpoint but no handshake is Waiting/NoHandshakeYet.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardPeer
  metadata:
    name: "<name>"
  spec:
    allowedIPs:
      - <string>
    [ endpoint: <string> ]
    interface: <string>
    [ persistentKeepalive: <integer> ]
    [ presharedKey: <string> ]
    [ presharedKeyFile: <string> ]
    publicKey: <string>
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.interface` | **Required** | string | Required metadata.name of an existing WireGuardInterface in the same Router. An OS ifname alone is not sufficient. |
| `spec.publicKey` | **Required** | string | Required peer public key in WireGuard base64 format. The validator checks nonempty text, not decoded key length; wg validates during application. |
| `spec.allowedIPs` | **Required** | array | Required nonempty list of IPv4/IPv6 CIDR prefixes. Controls the peer selected for outgoing packets and accepted source addresses for incoming packets. No implicit prefix or route creation. |
| `spec.endpoint` | *Optional* | string | Optional peer host:UDP-port, using [IPv6]:port for IPv6. Empty omits Endpoint and permits learning the endpoint from received packets. Syntax/DNS reachability is not checked by routerd configuration validation; wg handles it at apply. |
| `spec.persistentKeepalive` | *Optional* | integer; minimum=0; maximum=65535 | Seconds between keepalive packets, integer 0–65535; default 0 disables persistent keepalive. Not a handshake-success deadline. |
| `spec.presharedKey` | *Optional* | string | Optional additional symmetric key in WireGuard base64 format; default none. Overrides presharedKeyFile, without an exclusion error. |
| `spec.presharedKeyFile` | *Optional* | string | Optional router-local file containing the raw preshared key text, not an environment file. No key is generated for a missing file. Only read when presharedKey is empty. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/wireguard/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/wireguard/wireguard.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `latestEndpoint`, `latestHandshake`, `handshakeAgeSeconds`, `transferRxBytes`, `transferTxBytes`

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-wireguard.yaml)
