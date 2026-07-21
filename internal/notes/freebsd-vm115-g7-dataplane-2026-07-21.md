# FreeBSD VM115 G7 VNET dataplane acceptance — 2026-07-21

Commit: `ba2e7e84b1831aedd2818b0cdb52a71671560aa4`.

The durable runner is
`scripts/freebsd-vnet-firewall-dataplane-smoke.sh`.  It renders a production
FreeBSD `EgressRoutePolicy` fixture through `routerd validate` and `routerd
render freebsd`, then uses PF, VNET jails, and epairs to observe real packets
and PF states.  It refuses to replace an already enabled or nonempty disabled
PF instance.  Cleanup flushes and verifies its rules/states, disables PF,
removes jails/epairs, restores forwarding, and unloads `pf` only when the
runner loaded it.

## Fixture history

Stage78 (`/tmp/vm115-stage78-g7.D6jZcA`) was a harness-only failure.  The
staging builder accidentally checksummed `SHA256SUMS` into its own redirected
output, so the media checksum failed before the production runner began.  The
builder was corrected to exclude `SHA256SUMS` and the output ISO.  No Stage78
dataplane result is claimed.

## Stage79 result

Stage79 owner: `/tmp/vm115-stage79-g7.8fc91K`.

The persisted original-resolution console frames show:

- `runner.rc=0` and empty `runner.stderr`;
- `freebsd-vnet-firewall-dataplane=ok`;
- production `routerd validate`: `config is valid`;
- source 10: `sink-a=0 sink-b=6`; source 11: `sink-a=6 sink-b=0`;
- both routehosts selected; repeated three-packet flows for each source stayed
  on their chosen egress;
- `pfctl -ss -v` recorded both sources at rule 1;
- post-cleanup PF rule and state observations were empty and PF reported
  `pf disabled`.

Frame SHA256 values are recorded in
`/tmp/vm115-stage79-g7.8fc91K/frames.sha256`; key frames are runner rc
`699a94897e8064846bd3260cf27d746b43b128df07ce6f3ca8dd4f0d23b46f1e`, result
`d7ce4ccb0f9f1c31d1aa0a49c78469da70a444a536314ae19cf647e92b0f388a`, summary
`d5abcadb8e8561623f859344cf55f46b80dba2732c2de2cd68b9b3abaa6309cc`, PF states
`b08e89377915bbf126976d627d96c1e5d895daddcc8a63a60726287df32f9982`, and the
two sink captures `1def85d52e2db18ba0bc269bd86ce3c8005a58fc43933f5765157a4f2db64387`
and `1bebfbcba56700a69073fc581cf3326839d13a0a8b70d24cc64003b0d4f92b5d`.

Postflight was exact: VM115 stopped, no `args`, retained
`routerd-plan-v4.iso,size=104458K`, `vmbr404 NO-CARRIER`, and the two retained
snapshots unchanged.  The dedicated tmux owner is gone and no controller
process remained.

This closes only G7's documented FreeBSD production dataplane-acceptance
sub-AC.  It does not extend G4's documented semantic limitations or claim
general FreeBSD network-namespace parity.
