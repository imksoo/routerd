# Linux IPsec isolated process gate

This gate is for an explicitly assigned, disposable Linux VM. Do not run charon
on a shared development machine merely because network namespaces are available:
kernel module loading is global. strongSwan 5.9.13 kernel-netlink initialization
[creates and removes an XFRM test interface](https://github.com/strongswan/strongswan/blob/5.9.13/src/libcharon/plugins/kernel_netlink/kernel_netlink_xfrmi.c#L386)
before any IKE request. The wrapper requires the relevant modules to be loaded or
built in before entering its namespaces; it never installs packages, starts a
shared service, invokes modprobe or changes shared configuration.

## Inputs and preparation

Use the Go toolchain from go.mod and a CGO-disabled Linux routerd binary built
from the tested revision. Keep its SHA-256 and source manifest.

The dependency directory is a private extraction of these matching Ubuntu 24.04
amd64 packages, not an installation into the running OS:

- `strongswan-charon`: `usr/lib/ipsec/charon`.
- `strongswan-libcharon`: libcharon and socket-default.
- `strongswan-swanctl`: swanctl, VICI and kernel-netlink.
- `libstrongswan` and `libstrongswan-standard-plugins`: shared library and
  cryptographic plugins.

Check each downloaded package hash against the retained apt metadata and record
the extracted daemon, client, libraries and plugin hashes. Verify dynamic
dependencies with ldd. Only the private directory is supplied through
LD_LIBRARY_PATH and the private plugin mount. The supplied STRONGSWAN_CONF
contains an explicit plugin list and no shared include.

The assigned VM operator prepares the VM's kernel prerequisites outside the gate
and records the module list before and after that preparation. For the default
AES/SHA-256 ESP test this includes XFRM user/interface support, IPv4 ESP, authenc
and CBC when they are modules. The gate deliberately fails when a prerequisite
is unknown or missing.

## Execute

From a checkout containing the matching script:

```sh
ROUTERD_RUN_IPSEC_DEDICATED_VM=1 scripts/linux-ipsec-netns-smoke.sh \
  --routerd /absolute/private/path/routerd \
  --dependency-root /absolute/private/path/extracted \
  --evidence-dir /absolute/private/path/evidence \
  --mode esp
```

Use `--mode ike` only to measure IKE negotiation and process recovery without a
child SA. It does not qualify ESP or encrypted traffic.
[strongSwan documents this distinction](https://docs.strongswan.org/docs/5.9/swanctl/swanctlInitiate.html):
`--initiate --ike` establishes only the IKE SA; adding `--child` requests the
child SA.

The wrapper creates private user, network and mount namespaces, mounts new
sysfs, and gives the router and independent peer separate network stacks and
private runtime directories. The platform's /usr/local configuration hierarchy
is masked in that private mount namespace. No shared /etc file is changed.
Only direct child processes with the recorded birth identity are signaled and
waited for. Preserve the evidence directory on failure.

## Acceptance and qualification

The gate calls the actual `routerd serve --once --controllers ipsec` command
with a private SQLite database and ownership ledger. The production scoped VICI
owner must load and acknowledge its receipt. The peer is configured separately
with its own swanctl file and daemon. The stages require acquisition, unchanged
reconciliation, declaration withdrawal, reacquisition, router-side charon stop
and restart, and final withdrawal. Process cleanup records each child being
reaped.

ESP mode also requires bidirectional selector-address pings, established IKE
observations on both endpoints and nonzero packet counters in both native XFRM
inventories. IKE mode requires the IKE observations but does not send selector
traffic. Keep stage output/exit, binary and source hashes, receipt/database
snapshots, module inventories and outer interface inventories together.

This fixture starts its two private charon processes explicitly. It proves
neither automatic OS dependency installation nor the production service manager's
provider startup. Those are separate acceptance conditions. In particular,
strongswan-swanctl does not depend on a charon daemon package. The alternative
charon-systemd package includes a systemd unit whose ExecStartPost and reload use
swanctl --load-all; that service behavior must not be confused with routerd's
scoped connection ownership.
