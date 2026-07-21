# FreeBSD VM115 G9 KernelModule acceptance

G9 exercised the production `routerd serve --once --controllers
kernel-module` path on VM115 using an ordinary firewall fixture. The fixture
derives the runtime-only FreeBSD `pf` dependency; it does not use the removed
user-facing `KernelModule` YAML kind.

The owner was `/tmp/vm115-g9-kernelmodule.M4NhC5`. Persisted console frames
show `runner.rc=0`, `freebsd-kernelmodule=ok`, `pf-before=absent`, and an
after-reconcile `kldstat -m pf` row (`501 1 pf`). The runner runs the controller
twice: it requires the first stored status to be `phase=Applied`,
`loaded=[pf]`, `changed=true`, and the second to have `changed=false` before it
writes the result. The runner's EXIT trap unloads `pf` only when this fixture
observed it absent beforehand; a pre-existing module is never unloaded.

The post-run presentation was interrupted while old display filenames were
still in the guest script (`serve.log` and `kernel-status.log`). Those stale
display commands failed, but they ran after the runner had completed. The
corrected filenames are `serve-1.log`, `serve-2.log`,
`kernel-status-1.json`, and `kernel-status-2.json`. No cleanup-specific PPM
was produced; successful `runner.rc=0` includes successful trap cleanup
(`kldunload pf` would have changed that status to nonzero). VM115 was then
converged through the existing abort path and postflight is stopped, args
absent, v4 ISO attached, vmbr404 NO-CARRIER, with the two retained snapshots
unchanged.

Relevant original PPM SHA-256 values:

- `show-runner-rc.ppm`:
  `d88ef599a3d10f554314e1fd214ad37fde7deecf3c5055c640cf1cc700c1e750`
- `show-result.ppm`:
  `8047c6d31881c3fb1b7a18d2191355d6de5cf112e6fab0fba99d18db5aca2b54`
- `show-kernel-status-log.ppm` (the stale filename failure):
  `d131a7ba39b6b11d2900aea9d85f819c081c334691baa503f9be57720e815549`
- `show-runner-stderr.ppm`:
  `bb5bf599da5d4765233b67990a8b427bc8a6fb4df9b1e140d43801206a59b8d2`
