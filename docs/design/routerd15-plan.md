# routerd 1.5 completion and qualification plan

Status: active proposal on `proposal/routerd15-kiss-runtime`; not released.
Plan reset: 2026-11-02 at `2d13bd03704954126d57ad8274dab8f4c589ab1a`.
routerd 1.0 baseline: `e3b49dbb69c7de97bab9595884745eb7cbe93d92`.

## Product contract

- Preserve every useful routerd 1.0 function, including resource behavior,
  protocols, commands, control API, WebConsole, packaging and supported platform
  behavior. A home/site main-router trial is the first real-use gate; it does
  not reduce the final scope.
- Linux is first. Complete the corresponding FreeBSD and NixOS behavior after
  Linux; do not report whole-project completion while any baseline scope or
  platform remains unknown or broken.
- Old configuration and database formats do not need migration or upgrade
  support. Prepare a routerd 1.5 configuration before a clean installation.
  This does not authorize deleting data on an operating router during
  development.
- Do not merge to `main`, release, or cut over a production router without the
  user's explicit decision.

## Linux replacement priority (2026-11-03 user decision)

Current checkpoint (2026-11-07): product `0e186cc0` completed the isolated
107-resource combination, final service checks, static-route/Tailscale payloads
and both empty withdrawals. See the
[executed result](routerd15-audit.md#completed-packaged-combined-lifecycle--0e186cc0).
Keep this result instead of repeating that entire gate to satisfy older
remaining-check labels below. The qualified packages and validated full target
configurations are identified in the private replacement procedure. Production
allocation, NIC mapping and cutover remain explicit operator decisions.

Continue the full preservation goal with genuinely uncovered Linux paths;
overlapping-domain IPv4 UDP transit now passes in the native controller gate
(`eb43e539`), including SQLite reopen and independent withdrawal. See the
[VRF transit result](routerd15-audit.md#linux-overlapping-vrf-client-transit).
The delegated-address/RA kernel SLAAC peer gate also passes (`f28e58fd`).
IPv6 transit/renumbering exposed and fixed tentative RA source selection
(`c7f3eb83`); controller/kernel checks now pass. The next VRF gate is the
formal package lifecycle, including startup DAD recovery and restart/reboot.
The first packaged attempt exposed missing Ubuntu VRF modules in certification
and stale withdrawn VRF health; the latter is locally fixed in `b181a6c1`.
The b181a6c1 package recovered the terminal row and passed initial forwarding
and daemon restart, but cold boot failed on old-kernel VRF ownership receipts.
Next reproduce that receipt lifecycle locally, fix reboot recovery without
adopting foreign links, then repeat the scoped package lifecycle gate.
Use the qualification ledger to retain uncovered paths. Preserve
the 53.867-second maximum HA response gap as a limitation to investigate, not a
claim of fast failover. Do not start FreeBSD work or declare whole-task completion
from this Linux combination result. Historical checkpoints below retain their
original scope and do not supersede this current milestone.

The next deliverable is a Linux candidate ready for an explicitly authorized
replacement trial of a deployed router. FreeBSD implementation, packaging,
nDPI investigation and native qualification must not delay that deliverable.
FreeBSD nDPI is excluded by the user's explicit decision; do not bundle nDPI 5
or request an SDK 6 license-profile choice. Removal of that platform's current
nDPI build/runtime integration is deferred with FreeBSD work, not claimed done.
Other baseline functions remain in scope after the Linux replacement milestone.

A read-only inspection of a deployed Linux router found 110 declared resources,
including four DS-Lite tunnels, policy-based egress, BGP, DHCPv4/v6 and prefix
lease synchronization, NAT session synchronization, DNS, NTP, virtual addresses,
firewall/management access and application classification. The earlier private
19-resource candidate is not a replacement-equivalence proof. Keep live inputs
and access details in the private lab directory, never in this public repository.

The prepared full 1.5 candidate currently validates with 105 resources. Four
unused Interface role fields were removed, an intended firewall source selector
was expressed using srcCIDRs, and the non-delivering OTLP LogSink/reference was
removed while retaining Telemetry logs/metrics/traces. Four redundant static
inner-address declarations were also removed: 1.5's DS-Lite owner programs the
standard inner endpoint, and the prepared rules explicitly SNAT to that address.
The prior 109-resource candidate retained the former separate address owners;
the full configuration is revalidated after preparation. In baseline source,
OTLPSink.Emit returns without sending; do not count its old configuration as
successful delivery or claim Telemetry proves firewall-log delivery. Source
configuration preparation is not migration compatibility or native qualification.

Use the actual deployed function combinations to select the next Linux gates:
clean apt install and update; independent DS-Lite/egress/NAT traffic and failover;
BGP route exchange; DNS/DHCP/PD/RA and peer lease/session behavior; management
isolation; restart/reboot, withdrawal and automatic recovery. Reuse already
qualified procedures, extend only the missing combinations, and fix observed
failures locally before another native run. Do not substitute another basic
single-uplink demo or a FreeBSD gate for this milestone. Production cutover still
requires a separate explicit decision.

The first integrated multi-AFTR apt gate now passes at `fc5d7816`, including
three-way source-hash routing, fourth-path fallback, recovery, actual restart and
package update, withdrawal and explicit-stop persistence. It exposed/fixed main
apt-cache capability failures and delayed self-restart resurrection. See the
[scoped result](routerd15-audit.md#linux-apt-multi-path-ds-lite-and-explicit-stop-checkpoint-2026-11-03).
The follow-up `ee50d674` / `509d0613` campaign also qualified 64 existing UDP
tuples through real DS-Lite/NAT partial/total failure and recovery, preserving
unaffected and healthy fallback traffic. It fixed failed-member mark restoration
and corrected an invalid blanket conntrack-ID continuity test. See the
[existing-flow result](routerd15-audit.md#linux-packaged-ds-lite-existing-flow-checkpoint-2026-11-03).
The shared delegated-source follow-up `197bd70f` also passed real Kea renewal,
renumbering, finite WAN /128/tunnel authority, natural expiry, peer-only recovery,
daemon restart and empty withdrawal with 64-source forwarding. It repaired
removed DS and Interface statuses that had incorrectly degraded an empty router.
See the [delegated-source result](routerd15-audit.md#linux-packaged-delegated-ds-lite-lifecycle-2026-11-03).
All four deployed AFTRs use static IPv6 addresses; DNS-based AFTR discovery is
not an additional prerequisite for this first replacement milestone.
The `83414e60` snapshot fix also qualified a first two-guest HA handover with
actual DHCP/sticky replication, BGP/FIB forwarding and the original TCP/UDP
sockets after active VM power loss. It observed a 26.902-second response gap,
not hitless operation. Return was not qualified because the fixture network did
not survive reboot; that campaign ended before environment repair. See the
[scoped result](routerd15-audit.md#linux-packaged-ha-pair-initial-handover-2026-11-03).
The recertified `b698cb1d` follow-up did not qualify the lifecycle: capture found
transient shared-WAN ownership on return and untranslated packets in the minimal
zero-rule-backup fixture. Native helper/VMAC withdrawal succeeded, while old BGP
and lease-sync statuses still polluted empty-runtime health. See the
[follow-up result](routerd15-audit.md#linux-packaged-ha-lifecycle-follow-up-2026-11-03).
Prioritize the real returned-node authority defect (`2e9b15e8` RED,
`479f24d7` initial fix) and actual deployed standby policy. Both inspected routers
retain two unconditional SNAT rules; do not make an unused zero-rule case delay
this first replacement milestone. Preserve it in the final regression scope.
The `1a96b8c0` follow-up now passes the 20-resource pair lifecycle: exclusive
node-bound unicast election, original TCP/UDP continuity, capture-correct shared
SNAT, renewed sticky replication, cold return, active main restart and complete
Healthy withdrawal. It also fixed a native receive-socket bind to a pre-existing
shared VIP. See the [scoped result](routerd15-audit.md#linux-packaged-ha-lifecycle-with-declared-unicast-source-2026-11-03).
Next combine this HA path with shared PD/four AFTRs and the full target
configuration. Place independent application/client peers outside both router
failure domains; verify translated-port collision continuity and both-direction
router power loss, rather than adding another basic single-client demo.
This is not the full deployed-router qualification. Next Linux work must cover
the real BGP/HA/when-role and lease/session synchronization combinations,
remaining session modes and reboot with those combinations alongside the already
qualified client services, without restoring a FreeBSD acceptance dependency.

The corrected integrated HA input now preserves the deployed private-destination
NAT exclusions. Product `84b5d854` passed both-direction power loss, both cold
returns, latest lease/PD synchronization and active-main restarts with the same
74 application flows, zero TCP reconnects/translated-identity changes and a
53.209-second maximum response gap. Follow-up `4cb2451b` and `6dc12c92` fixed
explicit deferred-VIP withdrawal and retirement of withdrawn Pending DS-Lite
status. A focused formal-package native withdrawal/reopen check passed on both
routers. Next assess the full 105-resource target function combinations and
remaining differences, carrying forward these executed scoped checks.

## Current inventory

- `origin/main` is `e3b49dbb69c7de97bab9595884745eb7cbe93d92`; the active proposal
  is PR #1365. Its checks pass for the submitted revision, but it is a 1,490-file
  rewrite and is not a functional-parity verdict.
- Open main-based PRs #1366–#1369 cover DHCPv4 relay source selection,
  PortForward dataplane tests, firewall dataplane tests, and FreeBSD artifact
  plans. Their open status does not establish an integration gap in this branch.
  Relay source selection is already implemented here by `1195c7fc`, including
  runtime interface observations. PR #1366 instead requires exactly one static
  address declaration, so it must not be transplanted without reconciling that
  restriction. The relay observation regression covers validation without a
  static declaration, renumbering, missing source, recovery and removal at the
  config-generation boundary. The Linux single-interface packaged-daemon and
  systemd lifecycle gate now passes at product `3443ec6f`, including independent
  lease acquisition, upstream loss/return, withdrawal/reapply, native restart,
  observed address change and cleanup. Multi-interface and FreeBSD checks remain
  open.
  Read the other current diffs before selecting integration work.
- The feature ledger maps 87 baseline resource kinds, 90 example artifacts,
  and 50 feature scopes, including 11 non-resource scopes. Every preservation
  verdict is still `unknown`; `full_feature_pass_count` is 0. This means the
  whole 1.0 function set has not been audited and qualified end to end, even
  though many useful scoped tests and VM checks already exist.
- The current branch has no open GitHub issues. Use the existing feature ledger
  and open-findings document to select work; do not create parallel plans or
  branches for the same scope.
- The Linux IPv6 static host-route readback and independent-client forwarding,
  withdrawal, reapply and recovery path passed at `4f81296b`. Its reusable
  procedure is `routerd15-ipv6-route-readback.md`; this scoped pass does not
  qualify other route kinds or overall parity.
- The latest home-router reboot run at `2d13bd03` passed clean installation,
  health, DHCP, local DNS, NAT and firewall/management checks, but one peer
  upstream-DNS probe failed after router reboot and later succeeded without a
  configuration change. The capture did not pair the first client query with
  the corresponding upstream response. Treat this as an unresolved observation,
  not a confirmed product defect or a pass. Local characterization with the
  actual resolver and an independent upstream delaying its response by 1.5
  seconds reproduced a client-only failure at the old one-second deadline;
  three- and five-second client deadlines succeeded. Commit `603126ea` changes
  the reboot probe to five seconds, allowing the configured three-second
  upstream query deadline to complete. This does not identify the cause of the
  earlier PVE observation. The independent-peer Linux campaign at `603126ea`
  subsequently passed fresh install, DNS UDP/TCP, NAT, isolation, router reboot,
  SIGKILL and interrupted apply, with unchanged peer identity and actual renewal
  after each interruption. The reboot capture paired the first upstream query
  through its reply to the client. This current scoped path is qualified;
  broader dual-stack and 1.0 preservation checks remain open.

- The independent two-VM dual-stack campaign passed at product `6977c66e`
  with fixture `96e00032`: fresh install, WAN DHCPv4 and RA discovery, PD/SLAAC,
  DNS A/AAAA UDP/TCP, NAT/IPv6 forwarding, isolation and automatic recovery after
  router reboot, SIGKILL and interrupted apply. Peer processes stayed unchanged
  and the same DHCPv4 lease renewed after each interruption. The initial native
  OFFER loss exposed discarded checksum-offload metadata; a real-kernel
  test-first regression and fix preceded the successful fresh-install run.
  LAN DHCPv6 IA_NA renewal/reboot and broader feature/platform checks remain open.

## Work sequence

### 1. Resolve the post-reboot DNS observation

Execution status, 2026-11-06: the packaged combined cold-return gate passed
at `2da31049`; the later listener-boundary and pending-upstream defects have
separate native checks at `5b25593d` and `d279d919`. The latter also verifies
DHCP-owned address withdrawal. Keep the historical failures and the scope of
each result in the feature ledger. Do not restart this phase as an untested
post-reboot DNS investigation without a new failing observation.

Read the current resolver, supervisor, dnsmasq and service-manager paths and
their tests before changing code. Reproduce the reported case locally with an
independent upstream and client; distinguish client timeout, resolver response,
upstream response and service readiness. Use the existing two-VM procedure in
`docs/operations/development.md` for the actual reboot check.

Add a focused regression only for a reproduced product failure. If the product
responds inside its configured deadline and the one-second probe is the sole
failure, correct the probe to measure the configured contract. If it does not,
fix the owning production path. Then run one clean, current-revision PVE
campaign and remove its VMs, media, logs and staging when complete. Keep the
procedure and pass/fail criteria; retain raw captures only while diagnosing the
open failure.

### 2. Qualify the first real-use router path

Execution status, 2026-11-06: the maintained ledger links clean-install and
dual-stack recovery checks (`999dfd4`, `6977c66e`), the deployed-combination
cold-return check (`2da31049`), saved-generation crash/rollback and subsequent
DNS/DHCP, logging, WireGuard, NAT and event-owner checks. Both private 105-resource
deployment candidates were last validated with formal product `db41e03e`.
These are accumulated, revision-scoped results, not a claim that the complete
combined scenario ran on the latest revision.

The earlier dnsmasq startup identity failure was reproduced as an observation
of the child's pre-exec image and fixed in `176f42fb` without relaxing identity
checks. The former DNS listener UDP timeout remains historically unattributed.
Investigation reproduced a concrete UDP response loss when the previous reply
had reached the client but its writer had not released the shared lock.
Product `7012214a` replaces immediate contention drops with waiting and sending
inside the same two-second budget. The real UDP regression and complete resolver
package tests pass; formal-package qualification is tracked in the audit.
Do not infer that a historical timeout had this cause without its packet/error
observations, or use successful reruns alone to close an unexplained failure.

Prepare the target-specific clean installation and return-to-existing-router
procedure before requesting a production cutover decision. Other baseline
feature work continues independently of that decision. Do not repeat already
qualified gates merely to clear a stale remaining-check label.

Use a clean package installation and a 1.5 configuration on isolated Linux
router and independent-client VMs. Combine the existing clean-install,
home-router, reboot and interrupted-apply procedures where their setup is
shared; do not repeat a passing scenario just to produce another report.

Require independent-client DHCP acquisition and renewal, local and upstream
DNS A/AAAA over UDP/TCP, IPv4 NAT and forwarding, IPv6 delegation/RA and client
forwarding, firewall isolation and independent management access. Exercise
upstream loss/return, lease and prefix changes/expiry, service restart, router
reboot, process crash, interrupted 1.5 apply, configuration withdrawal and
automatic recovery. Do not repair product state by hand to turn a failure into
a pass. Use the existing runbook bounds; do not add new latency or recovery
requirements without an observed operator need.

Passing this gate makes a bounded real-use trial ready for an explicit user
decision. It does not complete or waive any other 1.0 feature.

### 3. Complete Linux 1.0 function preservation

Work from each `unknown` row in `routerd15-feature-qualification.json`, using
the pre-1.5 source and its real callers to establish the contract. Prioritize
actual network effects and failure consequences:

1. Interface/address ownership, static and policy routes, forwarding, MTU and
   exact withdrawal/recovery.
2. LAN DHCPv4/DHCPv6, DNS, NAT, port forwarding, firewall, address sets and
   management access with independent peers.
3. WAN DHCP/RA, DHCPv6-PD, PPPoE, DS-Lite, WireGuard, IPsec, VRF, VXLAN and
   hybrid paths, each with the peer and lifecycle the baseline supports.
4. BGP/BFD/VRRP, lease synchronization, mobility, provider actions, plugins,
   event rules, derived events, health and policy routing.
5. CLI, API, WebConsole, diagnostics, system/service behavior, examples,
   schemas, installers, live ISO, release tools and declarative platform
   integration.

For each path, trace configuration through the actual caller to the native or
peer effect, status/readback, withdrawal and recovery. Keep small production
changes and focused regressions. Use unit/package tests for logic, netns for
isolated Linux kernel behavior, and PVE VMs for clean install, service,
reboot and independent-peer paths. A fixture, compile, or mock does not qualify
native forwarding. Reuse existing test procedures and fix real gaps; do not add
speculative constraints or test-only architecture.

PPPoE remains in the preservation inventory. The user's exercised scenario is
basic connectivity through the Tokyo SoftEther endpoint; do not invent a
carrier/authentication test matrix absent a demonstrated use case.

### 4. Finish FreeBSD and NixOS preservation

After Linux paths are complete, compare each baseline platform function against
its native implementation and caller. Run FreeBSD 14.3 and NixOS checks on
their actual service managers and kernels. Cross-compilation, portable unit
tests and Linux success do not establish native parity.

### 5. Final qualification

Re-run the complete required local gate at the final source revision, then the
remaining isolated native and independent-peer procedures for every ledger
scope. Set a feature verdict only when its baseline behavior and required
withdrawal/recovery checks are demonstrated on the stated platform. Unknown
rows prevent a whole-project completion claim. Prepare a release or production
cutover proposal only after qualification; do not execute it without approval.

## Execution and retention rules

- Start each change by checking branch, clean diff, current `origin/main`, related
  PRs, current source, tests, runbooks and recent Git history. Reuse the current
  proposal branch unless an independent PR is required; do not create another
  worktree for serial work.
- Validate locally before a lab run. Use only isolated VMs/netns for network
  mutation. After a lab run, remove its VMs, disks, media, staging and temporary
  worktree without waiting for a second cleanup request; preserve unrelated
  active work.
- Keep reusable test procedures and acceptance criteria in the existing
  runbooks. Keep only the temporary output needed to diagnose the current open
  failure, then delete it. Do not append raw logs, ping output, packet captures
  or duplicate evidence bundles as routine deliverables. Report the tested
  revision, path, verdict and remaining unverified scope concisely.
- For public changes, run the repository secret scan before pushing, push
  coherent checkpoints without waiting for review, and verify the remote SHA.
  Push does not authorize merge, release or deployment.
- Relevant repository checks include `make check-routerd15-focused`,
  `make check-routerd15`, `make check-schema`, `make validate-example`, and
  `make website-build`. Run those that cover the changed scope and report every
  required check that was not run; never claim a skipped or compile-only path
  passed.

## Native package installation baseline (2026-11-03)

The user requires apt/pkg as the routerd 1.5 installation and update model on a
clean OS. Existing 1.5 users, source/manual installs and tar-installed systems
are not compatibility targets. Do not add discovery, adoption or migration for
those states. Package-to-package updates remain required.

- Linux uses native `/usr` binaries/helpers, `/etc/routerd` configuration,
  `/usr/lib/systemd/system` vendor units, `/run/routerd` and `/var/lib/routerd`.
  FreeBSD uses the normal `/usr/local` pkg prefix and rc.d integration.
- The package database owns distributed files. Desired configuration and runtime
  generated files keep their own explicit ownership; do not let the runtime
  overwrite package-owned service/configuration files.
- Declare dependencies in package metadata. A fresh install with no operator
  configuration must not apply the bundled example or change router networking.
- Updates must preserve configuration and durable state, stop/join the old
  service/owned helpers before replacement, and recover through the actual
  service manager. Do not silently report a failed lifecycle operation as success.
- Build reproducible `.deb` and native FreeBSD `.pkg` artifacts through Makefile
  and release workflows. Test actual apt/pkg fresh installation, package update,
  configuration/state retention, failure handling and removal on clean isolated
  target OS environments. An archive/manifest or cross-build alone is not a pass.
- Implement and verify native paths first, then metadata/lifecycle/builds, then
  the isolated package-manager gates. Push coherent checkpoints with incomplete
  checks explicit. The native-package work remains incomplete until both OS
  package paths are implemented and tested; this does not reduce full 1.0
  function preservation or authorize a release, main merge or production deploy.


Native Linux checkpoint (2026-11-03): runtime paths `072e57ec`, builder and
maintainer lifecycle `2eb09193`, reusable apt gate `4e225c85`. Two packages built
from the shared inventory, versions `1.5.0~20261103021000` and
`1.5.0~20261103022000`, passed actual apt install/update/remove in a fresh Ubuntu
24.04 VM (Linux `6.8.0-124-generic`). The second daemon binary was rebuilt with a
different version; this was actual executable replacement, not metadata alone.
Independent UDP/TCP DNS, old daemon/helper exit, configuration digest, retained
value in the actual SQLite database, package file verification and generated-unit
removal passed. The guest and both disk volumes were deleted after the test.
Full Linux Go suite, schema/examples and four-locale website build passed for
this checkpoint's code; packaging tests and shell lint passed. This does not
certify all network functions after the native-path change. FreeBSD pkg lifecycle,
release asset integration, native image installation and public repository delivery
remain unfinished. No manual-install compatibility path was added.

Native base-package checkpoint (2026-11-03, supersedes the unfinished base-package
items above): file ownership/modes, lifecycle hooks and shared inventory now
produce `.deb` and native `.pkg`; release workflow `17072a01` builds both native
formats for amd64/arm64, and no longer publishes a manual base installer. That
workflow has local actionlint validation, not an executed release. Native image
installation `1ee100ee` also passed its separate clean-boot IPv4 campaign.

Final Linux package gate: Ubuntu 24.04 / Linux 6.8.0-124, versions
`1.5.0~20261103034000` -> `1.5.0~20261103035000`, lifecycle `59baa653`, fixture
`4ae4af43`. Actual apt install/update/remove passed independent UDP/TCP DNS,
old executable/process replacement, configuration and SQLite value retention,
package-file verification and generated-unit removal. An unrelated prefixed
transient service remained active through update/removal. The VM and disks were
removed after the test.

Final FreeBSD package gate: FreeBSD 14.3-RELEASE-p16, pkg 2.8.4, versions
`1.5.0.a20261103034000` -> `1.5.0.a20261103035000`. Daemon code `bbbb9e79`, native
lifecycle/rc.d `626774d7` and its maintained fixture passed fresh inactive install,
independent UDP/TCP DNS, failed-stop refusal without replacing or losing service
from the prior version, normal update with old process exit, configuration/SQLite
value retention, package-file verification and removal with retained user state.
The gate uses native `HANDLE_RC_SCRIPTS=true`; enabled rc.d services start after
pkg's hook descendant cleanup. Without that policy, automatic restoration is not
claimed. The earlier direct hook startup was actually killed by pkg and is no
longer used. Native hook unit tests passed on FreeBSD with pkg's POSIX environment;
PID-lock/signal exit races have local rc.d regression tests. The VM, disks, snippet
and downloaded image staging were removed after the gate.

The reusable commands and assertions are in `scripts/linux-deb-lifecycle-smoke.sh`,
`scripts/freebsd-pkg-lifecycle-smoke.sh` and the development runbook. Retain these
procedures and scoped results, not the successful campaign's raw logs/payloads.
These checks qualify native base-package lifecycle on the two amd64 OS guests,
not all 1.0 features or arm64 runtime. Optional libnDPI native packaging remains
unfinished; its existing separate archive must not overwrite package-owned files.
Public repository hosting/release publication, main merge and production rollout
were not performed. The complete 1.0 functional goal remains unchanged.

Final local checks for the native-package checkpoint: `git diff --check`, Go
format checks, the complete Linux `go test ./...`, native lifecycle shell lint,
release actionlint, generated-schema checks, example validation and the four-locale
website build passed. The website build still reports unresolved documentation
links; successful compilation is not a claim of link-clean documentation. No
release workflow, arm64 guest lifecycle or production deployment was executed.
Reviewed implementation/test paths include `cmd/routerd/cmd_serve.go`,
`contrib/freebsd/routerd`, `packaging/native/debian-service.sh`,
`packaging/native/freebsd-service.sh`, `scripts/build-native-package.py`,
`scripts/build-live-iso.sh`, `scripts/linux-deb-lifecycle-smoke.sh`,
`scripts/freebsd-pkg-lifecycle-smoke.sh`, `tests/nativepackage/deb_test.go`,
`tests/nativepackage/pkg_test.go`, `tests/install/freebsd_rcd_boundary_test.go`,
`tests/install/install_test.go`, `Makefile` and `.github/workflows/release.yaml`.

## Native libnDPI addon checkpoint (2026-11-03)

The earlier optional libnDPI package gap now has a native Debian addon. The
existing libnDPI payload is built by `make dist-ndpi-agent-libndpi-deb`; native
`dpkg-shlibdeps` calculates runtime library dependencies. The addon pins the
matching base package version and owns the active agent path through its own
dpkg diversion. Base updates target the diverted static file; addon removal
restores the current base's static agent. No manual-install adoption was added.
The release workflow publishes this `.deb` rather than a manual overlay archive;
the workflow was linted, not executed as a release.

Native package artifacts at `be382e44`, payloads `916b4869`, passed the maintained
`linux-ndpi-deb-lifecycle-smoke.sh` on a fresh Ubuntu 24.04 / Linux 6.8.0-124 amd64
VM with libndpi4.2t64 4.2-2.1build1. Matched base/addon versions
`1.5.0~20261103050000` -> `1.5.0~20261103051000` had different executables.
TrafficFlowLog requested the actual generated agent/classifier service path;
independent loopback UDP/TCP DNS, real libnDPI TLS/SNI detection, socket backend
status, old owned process exit and automatic recovery, native file verification,
configuration/SQLite value retention and an unrelated prefixed service passed.
Addon removal restored the updated static agent and cleared its diversion;
final base removal retained user state. The guest, disks and temporary public
key staging were deleted. This does not qualify forwarded-client traffic capture
or classification, exporter interoperability, other architectures/ABIs or all
1.0 features. Those remain in the feature ledger with unknown global verdicts.

An initial addon-only helper stop was replaced after reading
`SystemdUnitController.applySystemdUnit`: main would restart stopped helpers
while files were being exchanged. The addon now uses the known owned-service
stop/restore cycle including main, so maintenance may interrupt traffic and cause
protocol reconvergence. No masking/recovery framework was added. Follow-up
`d5cc0924` locally reproduces and fixes a nonzero selftest exit masked by a shell
pipeline; this failure gate is separate from the positive VM artifacts above.

Read/reviewed paths include `scripts/build-native-package.py`,
`packaging/native/debian-service.sh`, `packaging/native/debian-ndpi-service.sh`,
`pkg/render/dpi_systemd.go`, `pkg/controller/chain/system.go`,
`cmd/routerd-ndpi-agent/main.go`, `pkg/api/specs.go`, `pkg/api/legacy.go`,
`tests/nativepackage/ndpi_test.go`, `scripts/linux-ndpi-deb-lifecycle-smoke.sh`,
`Makefile` and `.github/workflows/release.yaml`. Debian's dpkg-divert and
shlibdeps documentation was checked before implementing the native ownership.

Next native distribution check: do not treat optional native nDPI as Linux-only
for the full preservation goal. Baseline `e3b49dbb` already compiles/runs
`CGO_ENABLED=1 -tags libndpi` in `scripts/freebsd-native-vm-smoke.sh`; its
`docs/platforms.md` explicitly identifies the FreeBSD ports ndpi 5.0 ABI, implemented
by the Linux/FreeBSD CGO path in `cmd/routerd-ndpi-agent/backend_libndpi.go`.
`backend_freebsd.go` is the explicit unavailable fallback for non-CGO/non-libndpi builds. The default 1.5 native pkg inventory
currently carries the static agent, while the new optional addon is Debian-only.
FreeBSD native nDPI therefore still needs a package-owned clean-install/update
path and native OS verification. Do not label it unsupported, drop it from the
1.0 goal, or solve it with a manual binary overlay. Inspect pkg's actual ownership
and conflict contracts before choosing a native package layout; retain the shared
inventory, existing runtime paths and operator configuration/state semantics.

## nDPI 6 dependency decision

Superseded by the user decision above: exclude FreeBSD nDPI and prioritize Linux.
The observations below remain historical; no dependency choice is pending.

On 2026-11-03 the current FreeBSD repository installs `ndpi 6.0.d20260909,1`, not the nDPI 5.0
recorded in the baseline platform qualification. Actual FreeBSD 14.3-RELEASE-p16
native format tests passed at `fdd5692c`, including root ownership, native hooks,
real ELF parsing and the exact package owners of direct shared-library
requirements. The native routerd backend build then failed: `routerd_ndpi_init`
calls `ndpi_init_detection_module(NULL)`, while SDK 6 requires a second
`enum ndpi_license_type` argument. No native routerd pkg was built or installed,
and the native-agent install/update runtime campaign has not run.

This is more than an argument-count fix. A separate SDK-only probe against the
installed library, using `NDPI_LICENSE_FOR_PROFIT_LGPL`, explicitly reported that
TLS, DHCP, QUIC and DNS dissectors were not loaded. An HTTP packet was detected;
the paired TLS packet remained Unknown with empty SNI. The same generated inputs
against native Linux libnDPI 4.2 detected HTTP and TLS/SNI. Selecting the restricted
SDK 6 mode would therefore lose existing functions. Do not make that the product
fix, silently select a not-for-profit or licensed mode, skip the native test, or
substitute the static agent to claim the native requirement passed.

The independent diagnostic is reproducible without changing a router:

```sh
# From the repository root; requires cc/pkg-config and the installed SDK.
work=$(mktemp -d)
go run tests/nativepackage/testdata/ndpi-sdk-packets.go "$work"
cc tests/nativepackage/testdata/ndpi-sdk-probe.c -o "$work/probe" $(pkg-config --cflags --libs libndpi)
"$work/probe" "$work/http.packet"
"$work/probe" "$work/tls.packet"
rm -f "$work/probe" "$work/http.packet" "$work/tls.packet"
rmdir "$work"
```

The SDK 6 probe deliberately uses only its LGPL mode; it does not assert any
operator entitlement or determine the project's commercial classification.
The [upstream license/API policy](https://github.com/ntop/nDPI/blob/dev/README.license.md)
and the actual installed headers were inspected. The native Linux tagged backend
unit test at `2264b989` now fails rather than skips a missing backend, and requires
actual library detection plus the expected TLS SNI; it passed against libnDPI 4.2.

The user subsequently excluded FreeBSD nDPI. The proposed bundled nDPI 5
runtime and SDK 6 profile adaptation are not authorized implementation work and
are not prerequisites for Linux replacement readiness. Native FreeBSD integration
removal is deferred until Linux's first replacement gate is ready. Earlier
static-agent package results and the SDK failure remain historical scoped facts.
