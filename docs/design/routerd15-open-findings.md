# routerd 1.5 remaining production-path findings

Status: implementation pending unless explicitly closed in the audit. These
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
kernel FIB ownership remains open: remove installed/source-known replicas,
serialize watch/reconcile/teardown through one owner, journal exact native
operations, and retain uncertainty where the kernel cannot prove ownership.
The BGP controller watcher must also acknowledge exit before owner replacement.

## Native effects and lifetime

- Ordinary IPv4StaticAddress uses after-effect generic status as ownership, can
  adopt/delete a foreign address, and drops receipts on failed observation.
  Its WhenFalse path can delete a declared address never acquired. Replace all
  acquisition/withdrawal paths together with exact pending ownership.
- Delegated LAN address acquisition lifetimes are bounded, but separate WhenFalse
  cleanup can derive an unowned address, and declaration removal lacks receipt
  enumeration. Keep one exact receipt owner for both paths, including infinity.
- DS-Lite endpoint ownership does not cover the tunnel itself. Linux adopts
  existing/matching foreign tunnels; FreeBSD can destroy a mismatching GIF and
  uses a Linux deletion command on a withdrawal path. Missing/disabled branches
  can leave prior tunnels live. Close the complete native tunnel lifecycle.
- Policy routing can delete foreign rules sharing priority; IPv6 host routes can
  be installed before any durable receipt. Normal policy acquisition does not
  acquire the explicit-adoption ledger. Use exact native identities and one
  pending owner instead of priority-only deletion or inferred ownership.
- Managed PF lacks the SAM root forward anchor expected by the native SAM actor.
  SAM dotted-interface sysctl paths are decoded incorrectly. FreeBSD forward-zone
  firewall parity remains unsupported, and must not silently become an input rule.
- Linux VXLANSegment has no confirmed live owner; FreeBSD VXLAN receipts do not
  protect against a same-name foreign replacement. Bridge and WG now have
  concrete owners, but this does not repair those separate VXLAN paths.
- Dynamic dnsmasq RA construction can advertise unrelated native prefixes;
  DHCPv6Server ranges can contribute further PIOs. Static exact /64 emission is
  fixed, but finite delegated prefix selection, RS handling and M/O/lifetime
  semantics still need one actual RA owner and independent-peer validation.

Temporary native-command overlays reproduced foreign policy-rule deletion,
unjournaled IPv6 host routes and destructive foreign FreeBSD GIF handling.

## Runtime graph and process owner

- ARP observer spec collection turns a failed dynamic-part read into empty desired
  workers. Fail before supervision so unknown observations cannot stop owners.
- DHCP information/server and conntrack callbacks retain old Router/Store inputs;
  reservation/RA/TrafficFlowLog changes can remain unapplied. Daemon event streams
  also start only once, missing workers added on reload. Bind each lifetime to the
  actual generation and one fresh prepared observation.
- Network adoption/systemd configuration can lose activation retry after writing
  the desired bytes. Stop failures are swallowed in unit/Health teardown;
  conntrackd can report Running without consuming a changed configuration.
- dnsmasq non-systemd startup/reload trusts a bare pidfile, with inconsistent
  FreeBSD managers. Give its real process one identity and shutdown owner.
- Actual dnsmasq interface aliases and BGP custom helper socket checks differ from
  their producer contracts. FreeBSD managed AdminUp still invokes Linux ip.

## Command/control plane

- Direct routerd rollback commits to a temporary path then removes it, leaving
  canonical startup YAML unchanged. Current serve observation also omits effective
  when/dynamic filtering; fresh serve lacks the one-shot IPsec load path.
- HTTP ControlAPI/WebConsole capture startup exposure/auth/TLS configuration with
  no reload owner. Signal cancellation starts too late during initial cleanup.
- CLI allow-CIDR parsing accepts host-bit /0 then masks it to universal allow;
  Unix socket setup can unlink a regular/live foreign path and chmod its directory.
- Actual status/inspection readers suppress SQLite errors and can preserve Healthy
  or return fake probe success. Bound diagnostics, execute real probes or report
  unsupported/unavailable, and preserve failed status over later warnings.
- Ledger retention duration arithmetic overflows; federation queries open writable
  databases; some mutations validate output format afterward; Mobility traps hide
  decode failure. Traffic/connection output invents zero/stateless observations.
- FreeBSD mpd configuration overwrite does not tighten an existing file's mode.
  PD default-DUID read failure can incorrectly reset a valid observed identity.
- Remove SAM enrollment compatibility fallbacks only after making current revoke
  and its identity tombstone atomic; GET must not perform recovery writes.

## Build, release and isolated lab tools

- Installer restart/rollback ordering can restore binaries under a partially
  restarted process. Required failed stops must retain exact recovery state.
- Live ISO recursive cleanup can traverse leftover host bind mounts and chown
  strips package setuid bits. Use a private mount namespace, explicit ownership,
  pre-walk mount refusal and failure-preserving cleanup. Firstboot must validate
  before caching config or removing bootstrap management DHCP.
- Release QA mutates state/auth before its lock, starts mutation before durable
  ownership, trusts reused PGIDs, admits originals while effects use pinned
  inputs, and has incompatible precheck/cleanup/systemd stop deadlines. Reuse one
  actual process/service owner, durable launch admission and existing token-removal
  state; preserve unresolved cleanup. Bound proxy writes as well as reads.
- Existing labctl live TODOs return success and can remove manifests without
  cleanup. Protocol collectors accept parse failures/unmeasured transfers; capture
  uses bare PIDs; exact address/account/source identities are not always checked.
  E2E deployment ignores stop failures and can write private YAML with ambient mode.
- Native harness cleanup has unowned PID/route/config deletion and lost-baseline
  windows. Correct those actual paths; kernel-mechanism fixtures and string-anchor
  lints must not be reported as whole-product qualification.

Native Unix/raw/Netlink, Linux/FreeBSD peer traffic, PVE and real provider effects
remain separate unestablished gates in this cloud environment. No production
cutover, reset of an operator's data or live cloud mutation is authorized merely
by recording or implementing these fixes.
