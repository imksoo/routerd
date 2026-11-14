# IPsecConnection

[Resource configuration reference](../resources.md#ipsecconnection) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Render a strongSwan swanctl connection and synchronize all routerd IPsec connections together. A traffic-triggered CHILD_SA trap is configured; loading the connection is not proof of an established SA.

## Conditions and references

- Exactly one of preSharedKey and certificateRef is required. certificateRef is passed directly to swanctl local.certs, not resolved as a routerd resource.
- The aggregate lives under /etc/routerd/swanctl on Linux or /usr/local/etc/routerd/swanctl on FreeBSD. swanctl --load-all --file synchronizes the whole managed set, including secrets. Certificates, trust anchors and matching private keys must be provisioned in the credential stores used by that configuration.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPsecConnection
  metadata:
    name: "<name>"
  spec:
    [ certificateRef: <string> ]
    [ cloudProviderHint: ("" | "aws" | "azure" | "gcp") ]
    leftSubnet: <string>
    localAddress: <string>
    [ phase1Proposals:
        - <string>
    ]
    [ phase2Proposals:
        - <string>
    ]
    [ preSharedKey: <string> ]
    remoteAddress: <string>
    rightSubnet: <string>
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.localAddress` | **Required** | string | Required local endpoint IP address, IPv4 or IPv6; no hostname or interface reference. |
| `spec.remoteAddress` | **Required** | string | Required remote endpoint IP address, IPv4 or IPv6. The validator does not enforce matching address families across endpoints and traffic selectors. |
| `spec.preSharedKey` | **Conditionally required** | string | Inline IKE shared secret; required unless certificateRef is used. No default, file or environment source. Rendered into the managed swanctl secrets section. |
| `spec.certificateRef` | **Conditionally required** | string | Certificate name/path understood by strongSwan local.certs; required unless preSharedKey is used. Selects remote auth=pubkey. Does not create certificates or select a routerd Secret resource. |
| `spec.phase1Proposals` | *Optional* | array | IKE proposal list, default [aes256-sha256-modp2048]. Nonempty lists are comma-joined verbatim; routerd does not validate algorithm support. |
| `spec.phase2Proposals` | *Optional* | array | ESP proposal list, default [aes256-sha256]. Applied to the child named net; algorithm validity is checked by strongSwan when loading. |
| `spec.leftSubnet` | **Required** | string | Required local traffic-selector CIDR, IPv4 or IPv6; emitted as local_ts. |
| `spec.rightSubnet` | **Required** | string | Required remote traffic-selector CIDR, IPv4 or IPv6; emitted as remote_ts. |
| `spec.cloudProviderHint` | *Optional* | string; enum=["", "aws", "azure", "gcp"] | Optional aws, azure or gcp label, default empty. Reported by observation but not used to change proposals, credentials or swanctl rendering. No cloud resources are created. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/ipsec/ipsec.go](https://github.com/imksoo/routerd/blob/main/pkg/ipsec/ipsec.go) · [cmd/routerd/ipsec_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/ipsec_apply.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`
