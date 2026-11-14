# WireGuardInterface

[Resource configuration reference](../resources.md#wireguardinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Create or configure a WireGuard device and apply its complete peer configuration with wg setconf. Address assignment and ordinary routing are separate resources.

## Conditions and references

- Supply privateKey or privateKeyFile for runtime application; otherwise the interface remains Pending/PrivateKeyMissing. They are not mutually exclusive: inline privateKey wins, but a supplied privateKeyFile is still checked/generated during apply.
- Static WireGuardPeer.interface must reference this resource by metadata.name, not ifname. Imported peers are merged by Kind/name; later sources replace earlier generated peers and explicit WireGuardPeer resources replace generated peers with the same name.
- For enrollment direct SAMPeerGroup use, the direct group must be the final peersFrom entry and a nonoptional SAMRRSet must precede it. Runtime also requires usable RR peers for the same enrollment policy; unavailable or incompatible direct peers do not suppress the RR fallback.
- listenPort reserves that UDP port against other configured listeners. Linux additionally inserts an INPUT UDP accept rule when a positive port is configured. Up means interface configuration, not a successful peer handshake.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardInterface
  metadata:
    name: "<name>"
  spec:
    ifname: "<string>" # optional; string
    listenPort: 1 # optional; integer; minimum=1; maximum=65535
    mtu: 576 # optional; integer; minimum=576; maximum=9216
    peersFrom: # optional; array
      - # object
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    privateKey: "<string>" # optional; string
    privateKeyFile: "<string>" # optional; string
    selfNodeRef: "<string>" # optional; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.ifname` | OS device name; defaults to metadata.name. The validator currently checks metadata.name for whitespace, slash and a 15-character limit even when ifname is supplied; it does not apply the same checks to ifname. |
| `spec.selfNodeRef` | Local SAM node identity to exclude from imported peers; defaults to Router.metadata.name. This is a nodeRef value, not a Kind/name status reference. |
| `spec.privateKey` | Inline WireGuard private key, default empty; takes precedence over privateKeyFile. Use the WireGuard base64 key format. Configuration validation does not prove key validity; wg or file-key validation can fail during application. |
| `spec.privateKeyFile` | Optional key-file path on the router. On real apply, a missing file is generated with mode 0600 (new parent directories 0700); existing files must contain a valid 32-byte base64 private key. Empty/unreadable files fail rather than being overwritten. Not an environment file. |
| `spec.listenPort` | Optional UDP listening port 1–65535. Omitted/0 does not emit ListenPort in wg configuration, leaving WireGuard to choose/retain its port; routerd adds no explicit port exception for 0. |
| `spec.mtu` | Optional MTU 576–9216. Omitted/0 leaves the device MTU unchanged or at the OS creation default; no wg-quick MTU calculation is performed here. |
| `spec.peersFrom` | Optional ordered list of SAM peer sources, default empty. Missing nonoptional sources leave the interface Pending/PeersFromPending; static peers do not bypass that gate. Empty resolved lists are distinct from missing sources. |
| `spec.peersFrom[].resource` | Required source as SAMNodeSet/name, SAMPeerGroup/name, SAMEnrollmentPolicy/name or SAMRRSet/name; no field selector. Syntax is validated here; source lookup happens during peer resolution. Self nodes and nodes without WireGuard data are skipped. |
| `spec.peersFrom[].optional` | Default false. True tolerates a missing source without making the interface pending; it does not suppress malformed source errors. Enrollment direct groups have additional opportunistic fallback rules described above. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go) · [pkg/wireguard/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/wireguard/wireguard.go) · [pkg/render/firewall_holes.go](https://github.com/imksoo/routerd/blob/main/pkg/render/firewall_holes.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `publicKey`, `selfNodeRef`, `listenPort`, `fwmark`, `peerCount`, `hostFirewall`, `peersFrom`, `pendingSources`

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
