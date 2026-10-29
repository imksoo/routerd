# routerd 1.5 remaining production-path findings

Status: implementation pending unless explicitly closed in the audit. These
findings come from cumulative production body/caller review, with isolated
command/SQLite/loopback reproductions where stated. They do not represent native
or cloud qualification. A passing existing suite does not close them.

## Provider action and observation boundary

- Real SDK executors report ambiguous mutating-command failures as `failed`.
  Import then requeues them and clears prior facts. Add explicit wire uncertainty,
  preserve reported prior facts, and connect it to existing forward/undo unknown
  fences. Azure must not retry writes or refresh authentication and replay them;
  read-only retries can remain. Bound the three real CLI runners.
- AWS retries a failed replace as create without knowing the first outcome.
  Select the write from an authoritative pre-read instead.
- Azure route deletion ignores changed destination prefixes; ipconfig deletion
  can remove a same-name configuration for another address. OCI compares route
  destinations by host rather than exact normalized CIDR/type. Guard the exact
  resource identity; OCI full route-set updates need provider CAS/etag and strict
  observation, preserving unrelated routes.
- The distributed inventory plugin converts required AWS/Azure route-read errors
  into successful missing capture observations. Distinguish failed required
  capture collection from successful empty results and optional enrichment.
- Distributed capture generations and leases are local fences. No existing
  provider-enforced cross-node epoch authorizes automatic dead-member takeover.
  Preserve conservative ownership until an actual exclusive-authority contract
  exists. Remove unfenced transition-confirmation fallbacks coherently.

Temporary overlays reproduced core prior-fact loss, AWS fallback-after-timeout,
Azure foreign route/ipconfig deletion and OCI broader-prefix deletion through
actual production helpers, without provider calls.

## BGP ownership and current observations

- Controller gRPC policy writes race daemon dynamic-path policy refresh. The
  latter reconstructs only static-peer intent, omitting dynamic-peer policies;
  GoBGP reset can therefore remove assignments/default to accept. Establish one
  native policy owner with complete typed intent and safe assignment ordering.
- Static AddPath still lacks pending-before-native intent; failed final save can
  lose its UUID and leave an advertisement outside restart cleanup. Reuse the
  actual daemon's recovery boundary rather than a second controller cache.
- Policy statement order is semantically significant and must not be sorted away
  during drift comparison. Named standard communities accepted by config must
  restore through the same encoder as initial application.
- Mobility verdict read errors and native address-list errors become empty or
  partial FIB inputs. Capture one errorful native snapshot and honor cancellation.
- The watcher and periodic RPCs mutate shared remote connection fields without
  one lifetime owner. Accepted community send/accept/set.in settings also lack
  implementation consumers; implement or reject unsupported intent explicitly.

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
