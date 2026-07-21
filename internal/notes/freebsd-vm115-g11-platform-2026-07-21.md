# FreeBSD VM115 G11/G13/G14/G16 platform acceptance — 2026-07-21

Commit: `d22d057cedacbef2e4e8cd8c8e6340abe233e694`  
ISO SHA-256: `c82dafcecfa597d39a40a1322ac764f014d77e329d9b6ac5a0fa12391623a514`  
Owner: `/tmp/vm115-g11-platform.eEap8H`

The disposable VM115 runner completed with `runner.rc=0` and wrote
`g11-g13-g14-g16=ok`. The retained console frames show:

- `routerd validate` accepted a router with `BGPRouter` and
  `SysctlProfile(profile: router-freebsd)`.
- `routerd render freebsd` wrote `rc.d-routerd_bgp`; the rendered service uses
  `/var/run/routerd/bgp/gobgp.sock` and
  `/var/db/routerd/bgp/applied.json`.
- `routerd-dns-resolver daemon` loaded the runtime JSON then rejected the
  non-local `198.51.100.9:5053` bind with the explicit FreeBSD
  `IP_FREEBIND` diagnostic (exit `1`).
- `kill -0` and `procstat -b` observed the temporary live `sleep` process;
  the record contains no `/proc` dependency.

Frame SHA-256 values:

- runner rc: `b223acf18a0cab292a6a9534bb32d43a894d77a2e19ed660f33f19d0c6dbd524`
- runner/result: `cc1130138b822ad751bdfe19be5c35651e3474deea724a15b8e8cbcf0e7a2c8c`
- non-local bind: `d10c1c3450ad005431cd08a3de9ec887f67270d5d03b2298a363d032e0cc71a2`
- `procstat`: `a4eaea4ca71a38a82995736508c76b30cab4dcb6beb5bb278da51718a303b7c`

After presentation, VM115 was converged to `stopped`; `args` is absent,
`ide2` is the exact `routerd-plan-v4.iso`, `vmbr404` remains `NO-CARRIER`,
and the retained snapshot chain is unchanged.

G12 is intentionally not a VM runtime feature: `NetworkAdoption` is rejected
for every OS as a removed legacy kind by `pkg/api/legacy.go`; existing
Interface/service intent derives the internal adoption resources.
