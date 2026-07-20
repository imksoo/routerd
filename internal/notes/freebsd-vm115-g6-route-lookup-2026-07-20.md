# FreeBSD VM115 G6 route-lookup acceptance — 2026-07-20

Commit `bb063e1fd3ae67b7cbb83a664ba3f98cd79cbd0b` was exercised on the
isolated FreeBSD VM115 fixture.  The test used the production
`routerd-healthcheck selftest` executable, not a parser-only substitute.

## Fixture and evidence

- Host evidence root: `/tmp/vm115-stage46-route.JyDMli`
- VM owner: `/tmp/vm115-stage46-route-owner.SiDKGK`
- ISO SHA-256:
  `13022d210712d5ed19eb49076d20953ca11f17661291af8147e75b5c7f8d9e03`
- `routerd-healthcheck` SHA-256:
  `2346a11e065b5df847bb42c4127004db62a8f8b43d4b36429a80e4b4a0d3d370`

The disposable guest configured `vtnet0` with `192.0.2.1/24`; therefore the
kernel had the connected route to `192.0.2.2` on that interface.  The runner
captured `netstat -rn -f inet` immediately before and after the lookup and
required byte-for-byte equality.  This brackets lookup only: fixture setup is
outside that comparison.

`route -n get -inet 192.0.2.2` exited zero and reported `interface: vtnet0`.
The normal healthcheck selftest deliberately used an unsupported protocol so
that Probe failed before any TCP dial, while the same live context reached
EnrichEvidence.  Its JSON recorded the expected failed probe plus
`outInterface: vtnet0` and `lastEvidence.outInterface: vtnet0`.

The separate TCP selftest used `--source-interface vtnet0 --fwmark 1`.
It exited zero with the expected structured FreeBSD error,
`fwmark is not supported on FreeBSD`, and retained `outInterface: vtnet0`.
This demonstrates the existing explicit fwmark rejection without inventing a
route API fwmark parameter.

## Convergence

The full isolated v4 syntax suite also completed: `runner.rc=0` and
`result=syntax-smoke=ok`; validate was valid, plan was Healthy, render wrote
all six files, `pfctl -nf` succeeded, dnsmasq syntax succeeded, and rc.d
syntax/status checks completed.  The Stage46 controller completed with no
abort marker.  Host postflight recorded VM115 `stopped`, no `args`, exact v4
ISO attachment, `vmbr404 NO-CARRIER`, unchanged retained snapshots, and no
controller process.

Stage44 and Stage45 remain failure history only: both stopped before route
lookup because of fixture setup, not a production adapter finding.
