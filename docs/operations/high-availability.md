# High availability

![Diagram showing high availability with RouterdCluster file lease leader election gating routerd mutation while keepalived or CARP separately owns VIP address movement](/img/diagrams/operations-high-availability.png)

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

### Linux daemon startup and persisted VRRP roles

Before startup controllers evaluate `when`, routerd refreshes Linux VRRP roles
from its owned keepalived configuration and current native observations. A saved
`master` value cannot authorize a shared address or dependent service after a
rebooted node returns as standby. An ordinary daemon restart still recognizes a
live owned gateway as master, preserving its dependent services. Role observation
happens before the first controller generation; configuration reloads retain
their existing generation ordering. This is not a claim that complete HA
qualification has passed; see the Linux pair qualification record.

On complete BGP withdrawal, public BGPRouter/BGPPeer observations retire only
after acknowledged daemon stop, owned FIB removal and managed systemd helper
removal. Failed cleanup retains its observation and retry acknowledgement.
Lease-sync declarations removed from the configuration retire their public
observations after the previous controller generation stops; source and receiver
lease files are preserved. A declaration disabled by `when` remains visible.
Normal generation cancellation does not publish a new sync failure; a transfer
timeout while the generation is running still does.

For IPv4 unicast VRRP, a unique unconditional `IPv4StaticAddress` on the election
interface supplies keepalived's explicit `unicast_src_ip` and `track_src_ip`.
If that interface also has legacy subnet addresses, routerd selects the unique
declared source whose subnet contains all configured VRRP peers.
This keeps the receive socket on the node address even if a shared VIP existed
first and is subsequently withdrawn by a BACKUP notification. A missing tracked
source is a fault, not permission to use a shared address. Multicast and an
interface without a unique declared source or peer-subnet match retain native source selection;
the static-source qualification does not cover those other cases.
