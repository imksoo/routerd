# ClientPolicy

[Resource configuration reference](../resources.md#clientpolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: "<name>"
  spec:
    classification: # optional; array
      - # object
        ipv4Reservation: "<string>" # optional; string
        ipv6Addresses: # optional; array
          - "<string>" # string
        match: # Required; object
          dhcpFingerprints: # optional; array
            - "<string>" # string
          hostnamePatterns: # optional; array
            - "<string>" # string
          macs: # optional; array
            - "<string>" # string
          ouiPrefixes: # optional; array
            - "<string>" # string
        mode: "trusted" # Required; string; enum=["trusted", "guest", "isolated"]
        name: "<string>" # optional; string
    guestEgressAllow: # optional; array
      - "<string>" # string
    guestEgressDeny: # optional; array
      - "<string>" # string
    guestServices: # optional; array
      - "<string>" # string
    interfaces: # optional; array
      - "<string>" # string
    isolation: # optional; object
      lanInternet: "" # optional; string; enum=["", "allow", "deny"]
      lanLAN: "" # optional; string; enum=["", "allow", "deny"]
      lanMgmt: "" # optional; string; enum=["", "allow", "deny"]
      mDNSBroadcast: "" # optional; string; enum=["", "allow", "deny"]
    macs: # optional; array
      - "<string>" # string
    mode: "include" # Required; string; enum=["include", "exclude"]
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml)
