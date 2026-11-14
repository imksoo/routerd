# SAMTransportProfile

[Resource configuration reference](../resources.md#samtransportprofile) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMTransportProfile
  metadata:
    name: "<name>"
  spec:
    addressingMode: "" # optional; string; enum=["", "edge-index", "pair-stable"]
    bgp: # Required; object
      bfd: # optional; object
        detectMultiplier: 1 # optional; integer; minimum=1; maximum=50
        enabled: true # optional; boolean
        interface: "<string>" # optional; string
        minRx: "<string>" # optional; string
        minTx: "<string>" # optional; string
        profile: "" # optional; string; enum=["", "fast", "normal", "slow"]
      convergenceProfile: "" # optional; string; enum=["", "default", "fast", "stable"]
      directLocalPreference: 0 # optional; integer
      ebgpMultihop: 0 # optional; integer; minimum=0; maximum=255
      exportPolicy: # optional; object
        allowedPrefixes: # optional; array
          - "<string>" # string
      generatePeers: true # optional; boolean
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
      peerASN: 1 # Required; integer; minimum=1
      routeReflectorClient: true # optional; boolean
      routeReflectorClusterID: "<string>" # optional; string
      routerRef: "<string>" # Required; string
      timers: # optional; object
        profile: "" # optional; string; enum=["", "default", "fast", "slow"]
      timersPreset: "" # optional; string; enum=["", "default", "fast", "slow"]
    encapDport: 1 # optional; integer; minimum=1; maximum=65535
    encapSport: 1 # optional; integer; minimum=1; maximum=65535
    encryption: "" # optional; string; enum=["", "none", "wireguard"]
    innerPrefix: "<string>" # Required; string
    localEndpoint: "<string>" # optional; string
    localEndpointFrom: # optional; object
      field: "<string>" # optional; string
      optional: true # optional; boolean
      resource: "<string>" # Required; string
    mode: "ipip" # Required; string; enum=["ipip", "gre", "fou", "gue"]
    peersFrom: # optional; array
      - # object
        direct: true # optional; boolean
        nodeRefs: # optional; array
          - "<string>" # string
        optional: true # optional; boolean
        resource: "<string>" # Required; string
    publishPeerGroup: true # optional; boolean
    selfNodeRef: "<string>" # Required; string
    underlayInterface: "<string>" # Required; string
```

[Example](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
