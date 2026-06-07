# ADR 0014: Config Source of Truth and CLI Verbs

## Status

Proposed -- 2026-06-07.

Defines the config persistence model, the candidate/commit lifecycle, and the
`routerd` / `routerctl` command surface. Supersedes the ad-hoc verb sprawl on
`routerd` and aligns deletion, history, and rollback with the existing SQLite
generations.

## Context

routerd treats the on-disk `router.yaml` as both the operator input and the
state reconciled on boot. This conflation produced a concrete defect: removing a
resource at runtime does not survive a restart.

- `routerd delete` removes host artifacts, the ownership ledger entry, and the
  object status, but it does **not** edit `router.yaml`.
- `routerd serve` loads `router.yaml` on startup and reconciles it as the
  desired state.
- The apply/serve orphan GC compares against the resources declared in
  `router.yaml`, so anything still in the file is "desired" and is recreated.

Therefore a `delete` of a resource that is still present in the startup config is
undone on the next boot or apply.

Two industry models were considered:

- **DB as source of truth (Cisco running-config, Kubernetes etcd).** Mutations
  go to a store; files are inputs. This makes imperative delete durable, but for
  routerd it sacrifices the plaintext, comment-bearing, version-controllable,
  portable config that is central to the product (audit by `cat`, disaster
  recovery by copying one file, schema-reauthor on upgrade, diskless USB
  persistence). It also forces a startup-config/running-config split.
- **Files as source of truth, candidate/commit (VyOS/Junos).** A human-readable
  config is the persisted truth; `set`/`delete`/`commit` build a candidate,
  `commit` validates and activates atomically, history/rollback are built in.

Plain GitOps was rejected as a target: Git is nominally the truth, but a file
that fails to apply still lives in Git as the declared state, so the truth of
record and reality silently diverge. The accepted model fixes this by making the
truth "the last successfully applied config", gated by a transactional commit.

The CLI surface had also grown by implementation rather than intent:

- `routerd` carried 11 verbs (validate / check / observe / plan / adopt /
  render / apply / rollback / delete / serve / run), with five overlapping
  "look without applying" verbs, a not-implemented `run` stub, and a mandatory
  `--once` on `apply` that reads as optional.
- `routerctl` carried ~28 verbs, with four overlapping inspection verbs
  (get / status / show / describe) that differ only by data source
  (config file / status socket / state store), six top-level runtime data-table
  dumps, and two diagnostic verbs (doctor / diagnose).

## Decision

### 1. Source of truth

The single source of truth is one human-readable canonical `router.yaml` file.
routerd does not move the truth into an opaque database.

- The truth is the **last successfully applied** config. A config that fails
  validation or reconcile never becomes the truth.
- Comments and ordering are preserved across machine mutations using a
  comment-preserving YAML round-trip (yaml.v3 `Node`).
- Each successful apply writes the canonical file atomically (temp + fsync +
  rename) and snapshots a generation. History and rollback reuse the existing
  SQLite generations; no new history mechanism is introduced.
- On boot, `serve` loads the canonical config; if it fails validation, serve
  reconciles the last-good committed generation and warns loudly rather than
  refusing to start or enshrining a broken file.

### 2. Binary split

- **`routerd` is the daemon/engine.** The systemd unit runs `routerd serve` and
  nothing else. `serve --once` performs a single converge-and-exit (boot tests,
  CI, drift repair). Bootstrap and recovery seed the canonical via
  `routerd serve --config <initial.yaml>`.
- **`routerctl` is the operator CLI** (kubectl-equivalent). It owns the config
  lifecycle and inspection verbs. Mutating verbs talk to the running daemon over
  the control socket; the daemon performs the privileged canonical write,
  reconcile, and generation snapshot.

### 3. Config lifecycle verbs (on `routerctl`)

- `validate [-f <file>]` -- static schema validity. No host change.
- `plan [-f <file>]` -- preview the diff. No host change.
- `apply -f <file>` -- mutate the canonical and reconcile. **Input required.**
  - Default is **partial upsert** (add-or-update the resources in the input;
    other resources untouched), symmetric with partial `delete`.
  - `--replace` makes the canonical exactly equal to the input (absent resources
    are pruned).
  - There is **no `add` verb**: addition needs a body, so it is `apply` of a
    fragment. Only `delete` warrants its own verb because absence cannot be
    expressed as a document.
  - When `serve` is running, apply reconciles immediately by default;
    `--no-reconcile` writes only. When serve is not running, `routerctl apply`
    errors and points to `routerd serve`.
- `delete <kind>/<name>` -- atomic partial removal from the canonical, then
  reconcile.

Input conventions: `-f <file>` reads a file, `-f -` reads stdin, and omitting
`-f` targets the current canonical (so `validate`/`plan` operate on the live
truth). `apply` requires explicit input. `validate` and `plan` are unprivileged
(read); `apply` and `delete` are privileged, gated by control-socket access.

### 4. Inspection and runtime verbs (on `routerctl`)

- Consolidate `get` / `status` / `show` / `describe` into two:
  - `get [kind[/name]] [-o yaml|json|table]` -- machine-readable, merging spec
    and status by subject.
  - `describe <kind>/<name>` -- human-readable detail (spec, status,
    conditions, recent events, related runtime).
  - `status` and `show` are removed; their views fold into `get`/`describe`.
  - All inspection queries the running daemon's control API and stops switching
    data source per verb (the root of the old confusion).
- Collapse the six runtime data-table dumps (`events`, `ledger`,
  `dns-queries`, `connections`, `traffic-flows`, `firewall-logs`) into
  `get <subject>`.
- Collapse diagnostics into `doctor`; active probes move under
  `doctor --probe <subject>` (absorbing `diagnose`).
- Domain subtrees stay (`firewall`, `dynamic`, `mobility`, `plugin`, `action`,
  `federation`) and use `get`/`describe`-style sub-verbs. `wireguard` and
  `tailscale` move under a `vpn` subtree. `firewall-logs` becomes
  `get firewall-logs`.
- Runtime control: `drain`/`undrain` move under `ingress`,
  `restart-dns-resolver` generalizes to `restart <daemon>`, `set-log-level`
  becomes `log-level`.
- `version` and `help` are unchanged.

### 5. Removed or relocated from `routerd`

`check`, `observe`, `render`, `adopt`, and the not-implemented `run` are removed
or folded (`check`/`observe`/`render` into `plan`; `adopt` into `routerctl`).
`apply` loses its mandatory `--once`. `rollback` moves to `routerctl`.

### 6. Permissions

The canonical `router.yaml` is world-readable but writable only by
root/`routerd` (secrets live outside it via `SecretValueSource`). The control
socket is `0660 root:routerd`, so read verbs work for any user and mutating
verbs are gated by socket membership, performed by the privileged daemon.

## Consequences

- `delete` and `apply` become durable across reboot by construction, because the
  commit rewrites the canonical truth.
- A config that fails to apply cannot become the running truth; boot falls back
  to last-good.
- The verb surface shrinks and stops overlapping by data source.
- The control API must gain apply/plan/delete/validate mutations -- the main
  implementation cost.
- Breaking changes are acceptable (single user, no back-compat shim per project
  policy); configs are reauthored to the new model.

## Implementation plan (goals)

- **Phase 1 -- Commit core.** Canonical writer in the daemon: yaml.v3 round-trip
  (comment/order preserving), atomic write, generation snapshot on successful
  apply, and last-good boot fallback in `serve`.
- **Phase 2 -- Control API mutations.** Add apply/plan/delete/validate to the
  control socket API with the socket permission model.
- **Phase 3 -- Verb move.** `routerctl` gains validate/plan/apply/delete (via the
  daemon) with upsert-default/`--replace`/input-required; `serve --once`; trim
  `routerd` to serve-only (remove/relocate check/observe/render/adopt/run, drop
  mandatory `--once`, move rollback to routerctl).
- **Phase 4 -- Inspection consolidation.** Merge get/status/show/describe into
  `get`+`describe` over the control API; fold the six data-table dumps into
  `get <subject>`; absorb `diagnose` into `doctor --probe`.
- **Phase 5 -- Domain and control tidy.** `vpn` subtree for wireguard/tailscale,
  `restart <daemon>`, `ingress drain/undrain`, `log-level`.
- **Phase 6 -- Docs and migration.** Update tutorials/how-to/reference and
  example configs to the new surface; remove deprecated verbs.
