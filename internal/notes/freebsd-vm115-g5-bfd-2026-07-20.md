# FreeBSD VM115 G5 BFD bridge evidence — 2026-07-20

This note records the isolated VM115 acceptance run for the FreeBSD FRR/bfdd
bridge.  It is deliberately an apply-and-observe check; it does not claim a
remote BFD peer reached `Up`.

- Source: `agent/freebsd-phase4-bfd` worktree containing the production fix
  that resolves `Interface/<resource>` to its declared kernel `ifname`.
- Tested FreeBSD `routerd` SHA-256:
  `376fa8e433ad46a6819b8db5ed9c805657e38759d5a5e3827cd149e10d9d1e2a`.
- ISO SHA-256: `7ac74bc250215c5ce810dcced2fb670a8e7616f3fda5c141f8afa018e8ee07e3`.
- FRR closure: FreeBSD 14 amd64 `frr10-10.6.1` plus its pinned offline
  package closure; all package checksums passed in the guest.
- Stage62 failed before FRR started because its package checksum file retained
  build-host absolute paths. Stage63 fixed those paths but used unsupported
  `pkg add -y`. Stage64 corrected that argument and exposed a dangling
  BFD-to-BGPPeer fixture reference. Stage65 added the valid BGP reference and
  exposed the production interface-reference defect. These are retained as
  pre-acceptance failures, not successes.
- Stage66 used the corrected production code and valid fixture.  The guest
  reported `runner.rc=0` and `g5-bfdd-apply-observe=ok`; its generated FRR
  input contained `peer 198.18.250.2 interface vtnet0`, and
  `vtysh -c 'show bfd peers json'` returned the configured isolated session
  in `down` state.  `down` is expected without a remote BFD peer.
- Postflight recorded `status: stopped`, no temporary QEMU args, the retained
  `routerd-plan-v4.iso`, `vmbr404` `NO-CARRIER`, and unchanged VM snapshots.
  Stage66 deleted `frr10`; removal of the rest of the temporary package closure
  remains required in the live-peer acceptance or by snapshot rollback.

Evidence owner: `/tmp/vm115-stage66-g5.cGgzKu`; console-frame collection:
`/tmp/vm115-stage66-g5-frames.khUG0j`.

Stage68--75 were VNET-jail fixture failures retained as history: peer option
usage, jailed PID/VTY runtime paths, and then peer-zebra topology were fixed
without changing production BFD behavior.  Stage76 (`/tmp/vm115-stage76-g5.nDujNo`)
used a VNET jail with a host `g5host` epair end, a jailed `g5peer` end, and a
separate peer zebra/bfdd pair on an explicit peer ZAPI socket.  It reported
`runner.rc=0` and `g5-bfdd-apply-observe=ok`. Host FRR JSON visibly recorded
the configured BFD session `up`, then `down` with `control detection time
expired` after peer bfdd stopped, then `up` after restart. The run removed the
jail/epair and offline closure packages before halting. PVE postflight was
stopped/no args/retained v4 ISO/`vmbr404 NO-CARRIER`/unchanged snapshots.

Scope conclusion: FreeBSD can apply and observe a referenced BFD session
through installed FRR `bfdd` and `/usr/local/bin/vtysh`, including the isolated
one-hop Up/Down/recovery acceptance required by G5.
