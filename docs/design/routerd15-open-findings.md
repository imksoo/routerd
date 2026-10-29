# routerd 1.5 remaining production-path findings

Each section distinguishes implemented paths from remaining work. These
findings come from cumulative production body/caller review, with isolated
command/SQLite/loopback reproductions where stated. They do not represent native
or cloud qualification. A passing existing suite does not close them.

## Provider action and observation boundary

Implemented in the provider/packaging follow-up checkpoint:

- SDK executors report ambiguous writes as `outcomeUnknown`, preserving known
  prior facts and fencing automatic forward/undo replay. CLI runners have bounded
  output/lifetime and process-group cleanup; mutation retries are disabled.
- AWS uses an authoritative route read to select one create/replace operation.
  Azure guards exact route/ipconfig identity, binds NIC writes to its observed
  ARM subscription/resource group/name, and rejects null required observations.
  OCI preserves complete route-rule fields, compares exact destination/type and
  uses the observed ETag for a conditional full-set update. AWS/Azure CLI writes
  still lack atomic tuple CAS against unrelated external writers; that limitation
  and actual cloud qualification remain open.
- The inventory plugin fails required route reads/incomplete responses; a complete
  alternative Azure read remains useful. Failed collection preserves the prior
  capture fact. Real CLI-process/wire/SQLite regressions and peer review passed;
  no cloud mutation was executed.

- Distributed capture generations and leases are local fences. No existing
  provider-enforced cross-node epoch authorizes automatic dead-member takeover.
  Preserve conservative ownership until an actual exclusive-authority contract
  exists. Remove unfenced transition-confirmation fallbacks coherently.

The earlier overlay reproductions for prior-fact loss, AWS fallback-after-timeout,
Azure foreign deletion and OCI broader-prefix deletion are now permanent
regressions through the production helpers, without provider calls.

## BGP ownership and current observations

Implemented in the daemon policy/path checkpoint:

- The daemon owns complete static/dynamic policy intent and local path changes.
  Both import and export use neighbor-scoped global assignments, default reject,
  and a reject guard during policy replacement. Statement order is preserved.
- Static and dynamic AddPath record pending intent before effects. Exact source
  identifiers and the shared path encoder preserve other sources and peer paths
  during lost-UUID recovery. The private journal is format 3; earlier formats
  are rejected. See [BGP ownership](../reference/bgp-ownership.md) for cutover.
- Required Mobility/FIB and native address reads return errors rather than empty
  inputs. Watch cancellation owns its RPC subscription; it cannot close a shared
  connection. Startup waits for daemon restoration before controller BGP calls.
- Community send modes have consumers. Unsupported `communities.accept` and
  `communities.set.in` are rejected; prefix-length bounds require an allowlist.
  Control requests and acknowledgements use complete, bounded typed decoding.

Loopback GoBGP export, edit/withdraw and exact-source recovery regressions pass.
These are production-server tests, not independent-peer qualification. Native
kernel FIB ownership now uses one typed SQL receipt owner, exclusive add/exact
withdrawal, current RIB/Mobility/native rechecks after persistence and explicit
unknown-present fencing for both operations. Watcher and in-flight mutation exit
are acknowledged before connection replacement. Protocol-only adoption, display
fallback and duplicate retention caches are removed. Actual kernel qualification
and automatic reacquisition after a kernel-scope change/reboot remain open; the
current journal safely fences that mismatch. FreeBSD's delete API also lacks an
atomic full-tuple CAS against unrelated external writers.

## Native effects and lifetime

Implemented in the native/runtime follow-up:

- Ordinary static IPv4 addresses and routes use exact scope/index receipts,
  exclusive acquisition, pending-before-effect and native readback. Unknown
  pending presence is not adopted or replayed. Required read failures preserve
  receipts. WhenFalse/removal and changed intent use the same actual owner.
- Delegated LAN addresses retain acknowledged deadlines and pending intent,
  including infinite lifetimes. Source revocation/removal cannot fabricate an
  owned address. Child address/route/RA operations on Segment links use actual
  journal/cookie proof, including removal of the final parent declaration.
- DS-Lite now owns the tunnel, inner address/default route and finite endpoint
  coherently, with exact native token/scope/index and no foreign GIF replacement.
  Duplicate FreeBSD boot creators and generic teardown ownership are removed.
- PF emits the SAM root forward anchor; Linux SAM sysctl paths preserve dotted
  interface names. FreeBSD managed AdminUp uses its native command.
- VXLANSegment has one native Linux/FreeBSD owner, current admission, durable
  creation/withdrawal and scope-partitioned receipts. Duplicate networkd/rc
  creators and Bridge port ownership are removed. Native VM effects are unrun.
- One RA service owns finite current/previous PIOs, solicitation handling,
  independent M/O, router lifetime and prefix/DNS withdrawal. dnsmasq no longer
  creates RAs or a DHCPv6 socket for ra-only intent. See
  [RA contracts and limits](../reference/router-advertisements.md).

Still open:

- Policy routing can delete foreign rules sharing priority; IPv6 host routes can
  be installed before any durable receipt. Use exact native identities and one
  pending owner rather than priority-only deletion or inferred ownership.
- Mobility's separate address/route pending paths still have automatic unmarked
  adoption/replace recovery; the ordinary owners' fence does not repair them.
- Existing VXLANTunnel lifecycle still needs migration to the concrete native
  owner. Engine VXLANSegment observations can default to Healthy without native
  proof; wire the actual read-only owner observation instead. Unspecified local
  source readback and child proof for other virtual parents need closure.
- RA fragmented-RS provenance is unavailable through the current ndp API; custom
  DHCPv6 SelfAddressPolicy live resolution remains incomplete. FreeBSD forward
  zone firewall parity is also unsupported. Native independent peers remain
  required even where local wire/ownership tests pass.

## Runtime graph and process owner

Implemented: errorful ARP worker admission; current prepared Router/Store in
DHCP/conntrack callbacks; joined per-worker event readers on reload; one actual
conntrack timer; and activation receipts that survive unchanged-byte retries.
System/unit/network-adoption/health stop acknowledgements are required before
replacement or unlink. Expired shutdown deadlines do not manufacture owner exit.

Native operations share a kernel-scope outer owner across serve/one-shot/rollback
before startup cleanup and bootstrap; custom paths cannot bypass it. Actual
2-process tests cover rejection and preservation of the first pending operation.

DNS main-config changes restart the owned process, hosts-only changes HUP the
acknowledged owner. A native process receipt and per-artifact activation receipt
cover startup/stop/retry, exact command and systemd fragment/input identity.
Linux signals bind to pidfds, including ancestor-procfs PID mapping. FreeBSD's
bounded birth/jail/executable/argv observation still has second-resolution birth
and a check-to-PID-signal race; it is not a pidfd-equivalent atomic guarantee.
Actual systemd/FreeBSD DHCP/RA communication remains unqualified.

## Command/control plane

Implemented: canonical rollback; current effective serve observation; runtime
IPsec ownership; reloadable HTTP exposure/auth/TLS with acknowledged listener
lifetime; early signal cancellation; strict CIDR admission; and exact Unix socket
ownership. Required inspection failures propagate and Error cannot become WARN.
CLI retention arithmetic/output admission/read-only federation/strict Mobility
trap decoding and nullable traffic counters are corrected. FreeBSD mpd secret
permissions and default-DUID failures are explicit. Counter browser/API/schema
checks pass; old missing-as-zero traffic databases are rejected untouched.

Remaining implementation: daemon --probe currently reports current communication
as unobserved rather than executing packet/DNS/neighbor/conntrack probes. SAM
legacy enrollment compatibility removal still depends on atomic current
revocation/tombstones; GET must not perform recovery writes. IPsec control tests
do not establish native IKE or packet forwarding.

## Build, release and isolated lab tools

- Installer stop/restart/rollback ordering and retained recovery backups are
  implemented with subprocess regressions. Actual service installation and
  native rollback qualification remain unexecuted.
- Live ISO builder uses a private mount namespace, exact workdir ownership,
  pre-walk mount refusal and error-preserving owned unmounts. Recursive chown and
  all-root image rewriting are removed; old mode-damaged caches are rejected.
  Firstboot validates before caching and before service generation/DHCP removal.
  Actual shell-body failure regressions pass. Native image construction, package
  modes in a built image and VM boot are not yet qualified here.
- Release QA lifecycle ownership is implemented: lock before state/auth changes,
  durable STARTING admission before child effects, native birth/session checks
  with pidfd signaling, exact consumed-input pins and acknowledged stop before
  cleanup. Standalone certification reuses the same supervisor. Token revocation
  resolves lost replies through authoritative absence; unknown recovery is
  retained. Offline subprocess/fault checks pass; boot-enabled systemd/PVE/cloud
  lifecycle qualification remains unexecuted. Proxy I/O bounds remain a separate
  review item.
- Unimplemented labctl live operations now fail and preserve manifests. Required
  protocol evidence cannot be promoted to PASS by a result override. Remaining
  collectors still need complete parse/measurement identity; capture
  uses bare PIDs; exact address/account/source identities are not always checked.
  E2E deployment ignores stop failures and can write private YAML with ambient mode.
- Native harness cleanup has unowned PID/route/config deletion and lost-baseline
  windows. Correct those actual paths; kernel-mechanism fixtures and string-anchor
  lints must not be reported as whole-product qualification.

Native Unix/raw/Netlink, Linux/FreeBSD peer traffic, PVE and real provider effects
remain separate unestablished gates in this cloud environment. No production
cutover, reset of an operator's data or live cloud mutation is authorized merely
by recording or implementing these fixes.
