---
title: routerctl doctor
sidebar_label: routerctl doctor
---

# routerctl doctor — runtime health diagnostics

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
| `nat` | `NAT44Rule` resource status; `nft list table ip routerd_nat` exists. |
| `firewall` | `FirewallZone` / `FirewallPolicy` resource status; `nft list table inet routerd_filter` exists with `policy drop` on the input chain (otherwise the router is permissive); Linux host check for marked routerd-owned nft tables that are present but not expected by the current config-rendered ruleset. |
| `rollback` | At least one stored generation exists, so `routerctl rollback --to` is usable. |
| `disk` | `/var/lib/routerd` and `/run/routerd` capacity; WARN at 90% or `<256 MiB`, FAIL at 98% or `<64 MiB`. |
| `mgmt` | Management interface presence (best-effort from `ManagementAccess` or `FirewallZone role=mgmt`); WebConsole binding (FAIL/WARN on `0.0.0.0` / `::`). |
| `reconcile` | Per-controller reconcile error history from the read-only status socket. `--since <duration>` bounds the window. WARN at ≥1 error in the window, FAIL at ≥10; up to 5 sample entries are shown in the detail. |
| `runtime` | routerd's own heap / goroutine / fd footprint from the read-only status socket: `heapAlloc`, `heapObjects`, `numGoroutine`, `numGC`, `openFds`/`maxFds`. WARN when `numGoroutine` exceeds 10000 or open fds reach ≥80% of `RLIMIT_NOFILE`. Observational — never FAILs. |
| `hybrid` | `HybridRoute` / `OverlayPeer` references, Selective Address Mobility config references, default-route safety, MTU estimate, optional `HealthCheck` status, read-only route-table observation (`ip -4 route show <prefix>`), and Linux SAM checks for `/32` delivery routes, provider local-address absence, proxy-neighbor capture, `proxy_arp`, `ip_forward`, route lookup, warning-only `rp_filter`, and default-drop `FORWARD` policy heuristics. When the FORWARD policy table cannot be inspected, the detail distinguishes `nft` unavailable, permission denied, `routerd_filter` table absent, and other `nft list table` failures. |

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

The JSON shape, area identifiers, and status enum are part of the v1alpha1
operator contract. Future versions may add new areas and optional fields;
existing area names and status values will not be renamed or repurposed in
v1alpha1 minor builds.

## See also

- [Reconcile and rollback](./reconcile.md)
- [`routerctl ledger` maintenance](./reconcile.md#removal)
