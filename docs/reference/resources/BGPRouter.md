# BGPRouter

[Resource configuration reference](../resources.md#bgprouter) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: BGPRouter
  metadata:
    name: "<name>"
  spec:
    asn: 1 # Required; integer; minimum=1
    backend: "" # optional; string; enum=["", "gobgp"]
    communities: # optional; object
      accept: # optional; array
        - "<string>" # string
      send: "" # optional; string; enum=["", "standard", "extended", "both"]
      set: # optional; object
        in: # optional; array
          - "<string>" # string
        out: # optional; array
          - "<string>" # string
    convergenceProfile: "" # optional; string; enum=["", "default", "fast", "stable"]
    exportPolicy: # optional; object
      allowedPrefixes: # optional; array
        - "<string>" # string
    gracefulRestart: # optional; object
      enabled: true # optional; boolean
      restartTime: "<string>" # optional; string
      stalePathTime: "<string>" # optional; string
    importPolicy: # optional; object
      allowedPrefixLengthMax: 0 # optional; integer; minimum=0; maximum=128
      allowedPrefixLengthMin: 0 # optional; integer; minimum=0; maximum=128
      allowedPrefixes: # optional; array
        - "<string>" # string
      forbiddenCommunities: # optional; array
        - "<string>" # string
      localPreference: 0 # optional; integer
      nextHopRewrite: "" # optional; string; enum=["", "peer-address", "unchanged"]
      requiredCommunities: # optional; array
        - "<string>" # string
    listen: # optional; object
      address: "<string>" # optional; string
      port: 1 # optional; integer; minimum=1; maximum=65535
    redistribute: # optional; object
      connected: # optional; object
        allowedPrefixes: # optional; array
          - "<string>" # string
      static: # optional; object
        allowedPrefixes: # optional; array
          - "<string>" # string
    routerID: "<string>" # Required; string
    timers: # optional; object
      profile: "" # optional; string; enum=["", "default", "fast", "slow"]
    vrf: "<string>" # optional; string
    watcher: # optional; object
      maxPrefixes: 1 # optional; integer; minimum=1; maximum=999999
      peerStateChangeThrottle: "<string>" # optional; string
      pollInterval: "<string>" # optional; string
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

[Example](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
