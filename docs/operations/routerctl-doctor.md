---
title: routerctl doctor
sidebar_label: routerctl doctor
---

# routerctl doctor — runtime health diagnostics

![Diagram showing routerctl doctor read-only diagnostics combining state, status socket, and optional host probes into area checks, stable JSON or YAML output, and fail-only nonzero exit behavior](/img/diagrams/operations-routerctl-doctor.png)

`routerctl doctor` runs a battery of read-only checks and reports whether
this router is currently functioning as a home gateway. It does not
change host state. It is designed to be used by operators, CI, monitoring
agents, and downstream tools (a Prometheus exporter, the Web Console, or
an LLM-assisted diagnostic).

## Usage

```sh
# Run every area (default)
routerctl doctor

# Run a single area
routerctl doctor dns

# Skip host commands (resource-status checks only)
routerctl doctor --no-host

# Machine-readable output
routerctl doctor -o json
routerctl doctor -o yaml
```

Per-call options reuse the `diagnose` flag set: `--config`, `--state-file`,
`--no-host` / `--host`, `-o` / `--output`, `--timeout`.

## Areas

| Area | Checks |
| --- | --- |
| `wan` | `EgressRoutePolicy` and `HealthCheck` resource status; IPv4 / IPv6 default route presence (`ip -4/-6 route show default`). |
| `dns` | `DNSResolver` resource status; an A-record probe via `dig @127.0.0.1`. |
| `dslite` | `DSLiteTunnel` resource status; AFTR FQDN AAAA probe; tunnel device existence (`ip link show`). |
| `dhcpv6-pd` | `DHCPv6PrefixDelegation` status (Bound, delegated prefix). PD pending is **WARN** by design (do not advertise stale IPv6 on the LAN). |
| `nat` | `NAT44Rule` resource status; `nft list table ip routerd_nat` exists. When `NAT44SessionSync` uses conntrackd mode on Linux, the runtime `net.netfilter.nf_conntrack_tcp_be_liberal` value must be `1`; unreadable or different values are FAIL. |
| `firewall` | `FirewallZone` / `FirewallPolicy` resource status; `nft list table inet routerd_filter` exists with `policy drop` on the input chain (otherwise the router is permissive); Linux host check for marked routerd-owned nft tables that are present but not expected by the current config-rendered ruleset. |
| `rollback` | At least one stored generation exists, so `routerctl rollback --to` is usable. |
| `disk` | `/var/lib/routerd` and `/run/routerd` capacity; WARN at 90% or `<256 MiB`, FAIL at 98% or `<64 MiB`. On Linux, also fails when temporary directory invariants are broken: `/tmp` and `/var/tmp` must be `root:root` sticky `1777` directories. |
| `mgmt` | Management interface presence (best-effort from `ManagementAccess` or `FirewallZone role=mgmt`); WebConsole binding (FAIL/WARN on `0.0.0.0` / `::`). |
| `reconcile` | Per-controller reconcile error history from the read-only status socket. `--since <duration>` bounds the window. WARN at ≥1 error in the window, FAIL at ≥10; up to 5 sample entries are shown in the detail. |
| `runtime` | routerd's own heap / goroutine / fd footprint from the read-only status socket: `heapAlloc`, `heapObjects`, `numGoroutine`, `numGC`, `openFds`/`maxFds`. WARN when `numGoroutine` exceeds 10000 or open fds reach ≥80% of `RLIMIT_NOFILE`. Observational — never FAILs. |
| `dynamic` | `DynamicConfigPart` freshness, masks, and override policies, including stale or masking parts that can make the effective config diverge from the intended generated state. |
| `routes` | Installed `IPv4Route` status compared with the Linux host FIB (`ip -4 route show <destination>`), including destination, type, gateway, device, preferred source when applicable, and metric. Use this as drift evidence; it does not replace dataplane probes. |
| `plugin` | Trusted local plugin executable presence, permissions, and recent run freshness from plugin status where available. |
| `hybrid` | `HybridRoute` / `OverlayPeer` references, Selective Address Mobility config references, default-route safety, MTU estimate, optional `HealthCheck` status, read-only route-table observation (`ip -4 route show <prefix>`), and Linux SAM checks for `/32` delivery routes, provider local-address absence, proxy-neighbor capture, `proxy_arp`, `ip_forward`, route lookup, warning-only `rp_filter`, and default-drop `FORWARD` policy heuristics. When the FORWARD policy table cannot be inspected, the detail distinguishes `nft` unavailable, permission denied, `routerd_filter` table absent, and other `nft list table` failures. |
| `sam` | CloudEdge SAM ownership/capture diagnostics, including provider ownership freshness, OS capture state, delivery route lookup, forwarding prerequisites, blocking reasons, and owner-table/FIB drift. When host checks are enabled, `doctor sam` compares endpoint-owned local rows from `ownershipResolverControlPlaneOwnerTable` with the Linux main FIB: those `/32` rows must resolve to a local/cloud route instead of a SAM overlay route. Provider-secondary capture-holder rows for BGP remote owners are not subject to that local-route requirement; they may legitimately stay no-local, with delivery/forwarding checks and dataplane probes proving the overlay path. Unexpected `/32` route residue inside the MobilityPool prefix is reported unless it is known provider DHCP/link state or an observed BGP return route. This is a diagnostic view only; real dataplane checks still decide CloudEdge acceptance. |
| `sam-enrollment-client` | Leaf-side `SAMEnrollmentClient` diagnostics: referenced claim existence, RR bootstrap endpoint/socket shape, controller phase, last success/backoff reason, and fetched `SAMRRSet` dynamic-state freshness. `Degraded` is FAIL; `Backoff` and missing/expired RRSet state are WARN. `directTopologyPending: true` is the safe RR-only convergence state: inspect `reason`, allow the GET-only retry to finish, and never manually add a partial direct peer group. |
| `bgp-dynamic-peer` | RR-side `BGPDynamicPeer` diagnostics: source-prefix/import-policy shape, observed dynamic-peer count, accepted/rejected route counters, and rejected-route summary. No discovered peers is WARN; connected peers with all observed routes rejected is FAIL. |

Each check returns one of `pass`, `warn`, `fail`, or `skip` (the resource
or signal is not present on this router).

## JSON output contract

`routerctl doctor -o json` is a **stable** machine-readable interface. The
shape is:

```jsonc
{
  "summary": {
    "overall": "pass",      // "pass" | "warn" | "fail" | "skip"
    "pass": 7,
    "warn": 1,
    "fail": 0,
    "skip": 2
  },
  "checks": [
    {
      "area":   "dns",                          // see Areas table above
      "name":   "DNSResolver/lan-resolver",     // human-readable subject
      "status": "warn",                         // "pass" | "warn" | "fail" | "skip"
      "detail": "phase=Degraded,waiting=...",   // optional
      "remedy": "wait for or repair dependency wan-pd" // optional
    }
    // ...
  ]
}
```

Field guarantees:

- `summary.overall` is the worst-of `checks[].status` (`fail` > `warn` > `unknown`/`skip` > `pass`).
- `summary.pass/warn/fail/skip` are integer counts and sum to `len(checks)`.
- `checks[].status` is one of `pass`, `warn`, `fail`, `skip` — no other values.
- `checks[].area` is one of the identifiers in the **Areas** table above; the set is stable.
- `checks[].name` is human-readable; do not pattern-match on its exact form.
- `detail` and `remedy` are optional, free-form text intended for operators.

For example, `routerctl doctor runtime -o json` surfaces routerd's own
process footprint from the read-only status socket:

```jsonc
{
  "summary": { "overall": "pass", "pass": 1, "warn": 0, "fail": 0, "skip": 0 },
  "checks": [
    {
      "area": "runtime",
      "name": "process",
      "status": "pass",
      "detail": "heapAlloc=11.0MiB heapObjects=84213 numGoroutine=187 numGC=14 openFds=23/1024"
    }
  ]
}
```

## Exit code

- `0` — no `fail` checks (`pass`, `warn`, and `skip` are all considered non-failure for exit purposes).
- non-zero — at least one `fail` check. Scriptable as `routerctl doctor || alert`.

`warn` does **not** fail the exit code (e.g., DHCPv6-PD not yet Bound on a
fresh boot is informational). Tighten this with an explicit area
selection if you want stricter gates (`routerctl doctor wan` exits
non-zero only if `wan` fails).

## Stability

### Daemon inspection through `--probe`

`routerctl doctor --probe egress [resource]`, `--probe dns [resource]`, and
`--probe lan-client IP` use the running daemon's control API and execute bounded
router-originated network checks. Resource names and literal LAN-client target
semantics are unchanged.

DNS/egress probes prepare the currently resolved `when` graph before choosing
questions or targets. Inactive or indeterminate sources do not authorize a
private question; observation read failures propagate. Questions also honor the
first matching selected source, rather than merely zone membership.
Before each exchange and after its wait, the daemon rereads current configuration
and activation observations. A changed question, listener, target or binding is
reported as `stale_source`/unobserved and stops remaining attempts; an old reply
cannot establish that the newly selected source works.

- DNS probes send both UDP and TCP questions to explicit configured listeners,
  including currently resolved `addressFrom` values. Wildcard listeners are
  reached through the corresponding loopback address. A selected static zone
  record supplies an exact expected A/AAAA answer. A selected root-forwarding
  source can instead answer a root NS question. Missing usable listeners or
  questions remain unobserved; private names outside a selected forwarder's
  match are never chosen. SERVFAIL/REFUSED proves a transport reply, not
  successful resolution, and is reported as a failed DNS answer.
- Egress probes execute the candidate's configured enabled HealthCheck using its
  existing protocol, target, interface and mark binding. They do not reuse an old
  healthy sample. Missing checks, unresolved dynamic targets and unavailable
  native bindings remain unobserved. These host-originated attempts do not
  establish forwarded client traffic, NAT or automatic failover.
- LAN-client probes send an actual ICMP echo with the existing per-exchange
  identity and peer checks. A response proves that router-originated exchange.
  Permission denial remains unknown; a deadline without a matching echo is a
  failed attempt and may mean that the client filters ICMP. IPv6 link-local
  addresses require an explicit native zone such as `fe80::1%eth0`.

Each active request has a four-second network budget, at most eight attempts and
at most 32 returned checks. Individual attempts have at most two seconds.
Question preparation stops after 64 source/ref/record items; listener address
sources consume their preparation limit even when unresolved. Listener address
and candidate planning have separate small bounds. Unattempted checks are explicitly reported. Four requests may own probe slots
per API handler; additional requests return an unobserved admission check without
starting network work. Cancellation closes owned DNS sockets and propagates to
other protocol helpers. Probes do not modify routing, controller Health history
or stored decisions.

Optional structured fields identify `kind` (`communication`, `reported-status`,
or `scope`), protocol, actual target, observation start, elapsed milliseconds,
failure category and configured binding interface/mark. Reported phases remain
separate diagnostic checks; their PASS does not establish current traffic.
Existing CLI exit behavior is retained: FAIL checks return nonzero, while WARN
checks may return zero. A warning or a router-originated PASS must not be used as
independent client/forwarding qualification.

These daemon inspection commands reject `--server`, `--name`, and `--no-host`
instead of silently ignoring them. The ordinary local `doctor AREA` commands
retain their documented host checks. Use actual peer/client communication and
captures for the corresponding dataplane gate.

The JSON shape, area identifiers, and status enum are part of the v1alpha1
operator contract. Future versions may add new areas and optional fields;
existing area names and status values will not be renamed or repurposed in
v1alpha1 minor builds.

## See also

- [Reconcile and rollback](./reconcile.md)
- [`routerctl ledger` maintenance](./reconcile.md#removal)
