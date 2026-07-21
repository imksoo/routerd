# FreeBSD native CI first-failure classification (2026-07-21)

PR #885 run `29801633298`, job `88543676198`, executed the unfiltered
`go test ./...` suite inside a FreeBSD 14.3 amd64 guest.  The native runner
worked; the suite failed in 13 packages before the production smoke steps.

The first-failure set is classified as follows:

| Classification | Packages | Required correction |
| --- | --- | --- |
| Linux-specific test fixture executed with the host runtime OS | `cmd/routerctl`, `cmd/routerd`, `pkg/apply`, `pkg/config`, `pkg/controller/chain`, `pkg/controller/dhcpv4client`, `pkg/controller/firewall`, `pkg/controller/nat44`, `pkg/controller/vrrp`, `pkg/hostdeps` | Inject `platform.OSLinux` or the Linux backend into only the affected test/function/fixture. Keep FreeBSD-specific tests enabled. Do not change FreeBSD production behavior to match Linux commands or artifacts. |
| Cross-platform test fixture missing an explicit input | `pkg/netconfigbackend`, `tests/golden` | Give Netplan an explicit output path and validate Linux source examples with `ValidateForOS(..., OSLinux)` before rendering both targets. |
| Missing guest dependency | `tests/install` | Install `git` in the FreeBSD VM job because the installer tests discover the repository root through Git. |
| Confirmed cross-platform production defect | none in the first run | Reclassify only if a later native run demonstrates a production failure after fixture corrections. |

The gate remains the complete `go test ./...`; package exclusions, test-name
filters, and reduced package lists are prohibited.  After that command passes,
the same job must build the current `routerd` and `routerctl`, run `routerd
serve`, and exercise validate/plan/render plus native PF, dnsmasq, and rc.d
syntax/status smoke.

## ETA from this checkpoint

These are engineering estimates, not evidence of completion:

1. Fixture/dependency corrections and Linux regression: 1–2 hours.
2. FreeBSD native rerun and any remaining fixture iteration: 1–3 hours.
3. Production daemon smoke, evidence capture, and PR closure: 30–60 minutes
   after native tests are green.
4. G10/G15 packet-delivery evidence follows G1: 2–4 hours if the isolated
   delivery fixture works as designed; a newly proven production defect will
   be issue-recorded and will revise that estimate.

