# High availability

`RouterdCluster` gates renderer and applier work with a lightweight file-based
lease. It is intentionally separate from VIP ownership: keepalived or CARP still
decides which router owns a VIP, while routerd uses the lease to decide which
node may mutate host configuration.

The leader holds an exclusive lock on `spec.leasePath` and refreshes the lease
before `spec.leaseTTL` expires. Standby nodes keep the controller chain running
for observation, but mutating controllers are forced into dry-run mode. In
one-shot apply mode, a standby node plans normally, records cluster status, and
skips apply.

Example:

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: RouterdCluster
metadata:
  name: edge-ha
spec:
  peers:
    - routerd-01.lain.local
    - routerd-02.lain.local
  leaseTTL: 30s
  leasePath: /var/lib/routerd/ha-lease
```

Use a local path when only one routerd process can run on the host. Use a shared
filesystem path when multiple hosts must elect one applier. The filesystem must
provide working advisory locks; otherwise use this resource only for status and
keep the actual apply service enabled on one node.

Status fields:

| Field | Meaning |
| --- | --- |
| `phase` | `Leader` or `Standby` |
| `identity` | Local routerd identity, defaulting to hostname |
| `holder` | Current lease holder |
| `expiresAt` | Lease expiry timestamp |
| `leasePath` | Lease file path |

See `examples/ha-2-node.yaml` for a minimal two-node shape.
