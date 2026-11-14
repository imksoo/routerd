# WireGuardPeer

[Resource configuration reference](../resources.md#wireguardpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

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

[Example](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-wireguard.yaml)
