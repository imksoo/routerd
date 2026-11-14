# PPPoESession

[Resource configuration reference](../resources.md#pppoesession) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata:
    name: "<name>"
  spec:
    acName: "<string>" # optional; string
    authMethod: "chap" # optional; string; enum=["chap", "pap", "both"]
    defaultRoute: true # optional; boolean
    enabled: true # optional; boolean
    ifname: "<string>" # optional; string
    interface: "<string>" # Required; string
    ipv6: true # optional; boolean
    lcpEchoFailure: 0 # optional; integer; minimum=0
    lcpEchoInterval: 0 # optional; integer; minimum=0
    lcpFailure: 0 # optional; integer; minimum=0
    lcpInterval: 0 # optional; integer; minimum=0
    managed: true # optional; boolean
    mru: 576 # optional; integer; minimum=576; maximum=1500
    mtu: 576 # optional; integer; minimum=576; maximum=1500
    password: "<string>" # optional; string
    passwordFile: "<string>" # optional; string
    persist: true # optional; boolean
    secretEncoding: "plain" # optional; string; enum=["plain"]
    serviceName: "<string>" # optional; string
    usePeerDNS: true # optional; boolean
    username: "<string>" # Required; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/example-pppoe-ipv4-nat.yaml)
