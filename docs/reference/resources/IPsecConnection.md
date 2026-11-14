# IPsecConnection

[Resource configuration reference](../resources.md#ipsecconnection) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Render a strongSwan swanctl connection and synchronize all routerd IPsec connections together. A traffic-triggered CHILD_SA trap is configured; loading the connection is not proof of an established SA.

## Conditions and references

- Exactly one of preSharedKey and certificateRef is required. certificateRef is passed directly to swanctl local.certs, not resolved as a routerd resource.
- The aggregate lives under /etc/routerd/swanctl on Linux or /usr/local/etc/routerd/swanctl on FreeBSD. swanctl --load-all --file synchronizes the whole managed set, including secrets. Certificates, trust anchors and matching private keys must be provisioned in the credential stores used by that configuration.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPsecConnection
  metadata:
    name: "<name>"
  spec:
    certificateRef: "<string>" # optional; string
    cloudProviderHint: "" # optional; string; enum=["", "aws", "azure", "gcp"]
    leftSubnet: "<string>" # Required; string
    localAddress: "<string>" # Required; string
    phase1Proposals: # optional; array
      - "<string>" # string
    phase2Proposals: # optional; array
      - "<string>" # string
    preSharedKey: "<string>" # optional; string
    remoteAddress: "<string>" # Required; string
    rightSubnet: "<string>" # Required; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.localAddress` | Required local endpoint IP address, IPv4 or IPv6; no hostname or interface reference. |
| `spec.remoteAddress` | Required remote endpoint IP address, IPv4 or IPv6. The validator does not enforce matching address families across endpoints and traffic selectors. |
| `spec.preSharedKey` | Inline IKE shared secret; required unless certificateRef is used. No default, file or environment source. Rendered into the managed swanctl secrets section. |
| `spec.certificateRef` | Certificate name/path understood by strongSwan local.certs; required unless preSharedKey is used. Selects remote auth=pubkey. Does not create certificates or select a routerd Secret resource. |
| `spec.phase1Proposals` | IKE proposal list, default [aes256-sha256-modp2048]. Nonempty lists are comma-joined verbatim; routerd does not validate algorithm support. |
| `spec.phase2Proposals` | ESP proposal list, default [aes256-sha256]. Applied to the child named net; algorithm validity is checked by strongSwan when loading. |
| `spec.leftSubnet` | Required local traffic-selector CIDR, IPv4 or IPv6; emitted as local_ts. |
| `spec.rightSubnet` | Required remote traffic-selector CIDR, IPv4 or IPv6; emitted as remote_ts. |
| `spec.cloudProviderHint` | Optional aws, azure or gcp label, default empty. Reported by observation but not used to change proposals, credentials or swanctl rendering. No cloud resources are created. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/ipsec/ipsec.go](https://github.com/imksoo/routerd/blob/main/pkg/ipsec/ipsec.go) · [cmd/routerd/ipsec_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/ipsec_apply.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)
