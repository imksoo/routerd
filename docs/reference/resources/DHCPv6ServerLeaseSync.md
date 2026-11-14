# DHCPv6ServerLeaseSync

[Resource configuration reference](../resources.md#dhcpv6serverleasesync) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Copy one DHCPv6 server lease file to peer hosts using rsync over SSH; not a DHCP failover protocol.

## Conditions and references

- source.resource and at least one target host are required. Unlike status-value references, normal validation checks the source spelling/type but does not verify that this DHCPv6Server exists. A missing named source therefore falls back to the platform default lease path.
- Only the main lease file is copied; unlike DHCPv4ServerLeaseSync, this path does not include dhcp-sticky.db/WAL. The destination uses the same absolute path as the local source. No target path override or automatic peer service reload is provided.
- Each invocation uses rsync -a --checksum --delay-updates, a default rsync inactivity timeout of 60s and a 2m command execution limit. SSH defaults BatchMode=yes and ConnectTimeout=10, overridable via sshOptions. A Synced status records file-copy success, not peer lease reload or client continuity.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6ServerLeaseSync
  metadata:
    name: "<name>"
  spec:
    command: "<string>" # optional; string
    interval: "<string>" # optional; string
    source: # optional; object
      resource: "<string>" # Required; string
    targets: # optional; array
      - # object
        host: "<string>" # Required; string
        name: "<string>" # optional; string
        options: # optional; array
          - "<string>" # string
        sshOptions: # optional; array
          - "<string>" # string
        user: "<string>" # optional; string
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

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.source` | Required source descriptor with resource; no filesystem path override here. |
| `spec.source.resource` | Required bare DHCPv6Server name or DHCPv6Server/name. Uses its nonblank leaseFile, otherwise <StateDir>/dnsmasq/dnsmasq.leases. This is not a status-value reference and has no field/optional members. |
| `spec.command` | Optional executable name/path, default rsync; receives rsync arguments directly, not a shell command string. |
| `spec.interval` | Go duration, default 30s. Suppresses work until this long after the last successful syncedAt; controller itself is polled every 30s and also on subscriptions. Zero/negative values disable this skip, not accelerate the periodic scheduler. |
| `spec.targets` | Required nonempty peer-target list. Files are sent serially to each target; first failed transfer records Error/SyncFailed. No remote routerd API call is made. |
| `spec.targets[].name` | Optional status label, default empty; does not select the destination host or source. |
| `spec.targets[].host` | Required SSH host/address. CR/LF prohibited; must already be reachable and configured for noninteractive SSH. |
| `spec.targets[].user` | Optional SSH user; empty omits user@ and lets SSH choose its configured user. CR/LF prohibited. |
| `spec.targets[].sshOptions` | Optional SSH argument list assembled into rsync -e. Defaults add -o BatchMode=yes and -o ConnectTimeout=10 unless those keys are supplied. No CR/LF in entries. |
| `spec.targets[].options` | Optional extra rsync arguments, appended after defaults. A supplied --timeout or --timeout=... suppresses the default --timeout=60. Does not override the separate 2m process limit; no CR/LF. |
| `spec.when` | Optional common state condition, default unconditional. Use actual active-role status to prevent standby writers; this resource does not itself negotiate which peer should own the lease file. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/controller/chain/filesync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/filesync.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `command`, `sourceCount`, `targetCount`, `sources`, `targets`, `syncedAt`, `dryRun`
