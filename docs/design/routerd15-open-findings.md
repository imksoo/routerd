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
- Standard/extended community send modes now survive the GoBGP peer/group
  API conversion and filter cloned outbound paths after export policy. The
  pinned local v4.7.0 source correction covers both extended attribute forms;
  large communities retain their independent always-advertised behavior. Raw
  TCP BGP UPDATE and withdrawal regressions cover static/dynamic peers and
  verify original local RIB attributes remain intact. Independent-device
  interoperability remains unqualified.
  Unsupported `communities.accept` and `communities.set.in` are rejected;
  prefix-length bounds require an allowlist.
  Control requests and acknowledgements use complete, bounded typed decoding.

Loopback GoBGP export, edit/withdraw and exact-source recovery regressions pass.
These are production-server tests, not independent-peer qualification. Native
kernel FIB ownership now uses one typed SQL receipt owner, exclusive add/exact
withdrawal, current RIB/Mobility/native rechecks after persistence and explicit
unknown-present fencing for both operations. Watcher and in-flight mutation exit
are acknowledged before connection replacement. Protocol-only adoption, display
fallback and duplicate retention caches are removed. Actual kernel qualification
remains unqualified. The SQL journal is now keyed by the current kernel scope,
including the actual FreeBSD FIB; old worlds and pending operations remain
untouched, while the new world can acquire only through exclusive native add.
A present foreign route is not adopted. Real SQL two-world/reopen regressions
pass, but actual reboot/forwarding qualification remains unrun. FreeBSD's delete
API also lacks an atomic full-tuple CAS against unrelated external writers.

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
- VXLANTunnel and VXLANSegment share one native Linux/FreeBSD owner, current
  admission, durable creation/withdrawal and kind-aware scope-partitioned
  receipts. Tunnel HA gates retain current-lifetime/freshness checks after
  waits, including child observation. Duplicate networkd/rc creators and Bridge
  port ownership are removed; read-only inspection requires native proof.
  Native VM effects and independent-peer forwarding are unrun.
- One RA service owns finite current/previous PIOs, solicitation handling,
  independent M/O, router lifetime and prefix/DNS withdrawal. dnsmasq no longer
  creates RAs or a DHCPv6 socket for ra-only intent. See
  [RA contracts and limits](../reference/router-advertisements.md).
- IPv4/IPv6 policy routing shares exact, family-separated native receipts,
  pending add/delete and current-source checks. Priority-only deletion, shared
  table flushes, inferred artifact ownership and post-effect IPv6 file state are
  removed. Overlapping foreign selectors cannot hide beside an owned rule.
- Mobility addresses and routes record native scope/index/virtual-parent proof,
  acknowledge exact readback explicitly and fence interrupted presence. Current
  typed source and native authority are rechecked after persistence and the last
  observation wait; pending intent alone cannot authorize adoption or deletion.
- RA SelfAddressPolicy prepares ordered candidates and selects current native
  validity or canonical PD plus acknowledged exact address receipts. Finite
  per-address RDNSS lifetimes, fallback/withdrawal, DAD and cross-interface proof
  have local regressions.

Still open:

- Child proof for virtual parents other than the shared native VXLAN owner
  remains open. VXLAN local wildcard source, nolearning and Linux outer DF
  use native readback; explicit FreeBSD outer DF remains unsupported.
- Fragmented-RS provenance remains unavailable through the current ndp API.
  Separately rendered DHCP DNS selection, FreeBSD forward-zone firewall parity
  and native independent peers remain unqualified. Native route/address APIs
  also lack an atomic full-identity CAS against unrelated external writers; a
  final userspace recheck does not establish that stronger property.

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

## Current Health and event decisions

Health samples now carry the normalized configured probe identity. Worker
threshold state resets after a changed probe or expired sample; importer,
advisory selection and policy routing require that identity and current
freshness. SQLite reads project stale or identityless reachability to Unknown
without writing display repairs. Native route/source lifetime authority and
independent two-WAN failover qualification remain open.

EventRule commits its typed correlation checkpoint, sole per-rule cursor,
public status and local output together. It resumes unchanged rules, retires
removed ones and reports missing retained history rather than inferring absence.
Bounded fair input pages prevent starvation; correlation key/byte limits evict
oldest partial state with a visible warning. Start republishes the selected
checkpoint's status; processing errors preserve the cursor and report Error
when storage remains writable.

DerivedEvent uses its existing object status as the durable latch/deadline
owner. The status/output transaction rechecks input versions and time-dependent
Health/PD values. Failed writes, lost replies, restart and removal/readdition
have actual SQLite regressions. One loop owns and joins timer work. The deadline
does not prove continuous observation across downtime. Auxiliary engine
activation follows selected runtime generation; the separate canonical
configuration/external-effect commit barrier remains open.

DNS workers reject another resource's runtime configuration and use exact zone
resource references and DNS label boundaries, including escaped dots and
separate resources sharing an apex. Reload preserves DHCP observations only
within the same zone resource, apex and declared source namespace, with the
current TTL. Named DHCP observations carry source identity and acknowledged
expiry, and journal insertion atomically updates the current source/IP lease or
withdrawal projection. Startup and reload read that current journal directly;
an unchanged consumed runtime file cannot resurrect a later withdrawn name.
Client reassignment, unrelated-client removal and IPv6 textual aliases have
live/update and SQLite-reopen regressions.

Lease POST acknowledgement is serialized with table handover. Old query/probe
owners are joined, complete requests and response writes are bounded, and staged
or retired listeners cannot serve a different selected generation. TCP wildcard
listeners use their configured binding for source/cache admission. The remaining
lease attribution gap is a tagless constructor-based IPv6 lease-file row on a
first start with no journal evidence; it cannot acquire guessed source authority.
Native dnsmasq/client interoperability remains unqualified.

## Command/control plane

Implemented: canonical rollback; current effective serve observation; runtime
IPsec ownership; reloadable HTTP exposure/auth/TLS with acknowledged listener
lifetime; early signal cancellation; strict CIDR admission; and exact Unix socket
ownership. Required inspection failures propagate and Error cannot become WARN.
CLI retention arithmetic/output admission/read-only federation/strict Mobility
trap decoding and nullable traffic counters are corrected. FreeBSD mpd secret
permissions and default-DUID failures are explicit. Counter browser/API/schema
checks pass; old missing-as-zero traffic databases are rejected untouched.

SAM admission and revocation now atomically commit claims and durable nonce
ledger entries. Topology GET uses a read-only consistent snapshot and mandatory
current digests; lazy backfill and omitted-field signature compatibility are
removed. SQL failure, reopen/retry and replay regressions pass.

Remaining implementation: daemon --probe currently reports current communication
as unobserved rather than executing packet/DNS/neighbor/conntrack probes. IPsec
control tests do not establish native IKE or packet forwarding; SAM tests do not
establish independent RR/leaf interoperability.

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
  lifecycle qualification remains unexecuted. The proxy now has one header
  deadline, bounded tunnel writes and a shared TCP-connect budget; bytes received
  with CONNECT are retained, and established tunnels never receive an injected
  HTTP error. Real socket regressions pass. Synchronous OS name resolution and
  concurrent handler admission still need lifetime/resource-bound review.
- Unimplemented labctl live operations now fail and preserve manifests. Required
  protocol evidence cannot be promoted to PASS by a result override. Remaining
  collectors still need complete parse/measurement identity. The four-point
  pcap runner now uses durable gated admission, exact Linux pidfd/birth/output
  receipts and acknowledged stop before copy; native process/fault checks pass.
  Other capture harnesses and exact address/account/source identities still
  require review; real remote capture remains unqualified.
  The isolated E2E deployment now rejects unconfirmed unit stops, and generated
  secret-bearing YAML is replaced atomically with private permissions; offline
  production-body regressions pass. Native deployment remains unqualified.
- The FreeBSD lifecycle harness now drives the actual serve/ApplyAPI and uses a
  direct-child launcher with durable exclusive intent and acknowledged Wait.
  Epair cleanup checks both endpoint identities and confirms absence; failed
  cleanup preserves evidence and cannot publish PASS. Its five offline boundary
  tests pass, but actual BPF/DHCP/PD/DNS VM execution remains unrun. Other native
  harnesses still need actual PID/route/config ownership review; mechanism
  fixtures and string-anchor lints are not whole-product qualification.
- FreeBSD 14.3 IPv4 address add is create-or-replace through both ioctl and
  Netlink; the kernel does not implement exclusive-add flags. Ordinary and
  Mobility prechecks cannot prevent an unrelated writer inserting the same
  address before our add. This is a native API limitation, not established
  foreign-safe acquisition. See [native ownership](../reference/native-ownership.md).

Native Unix/raw/Netlink, Linux/FreeBSD peer traffic, PVE and real provider effects
remain separate unestablished gates in this cloud environment. No production
cutover, reset of an operator's data or live cloud mutation is authorized merely
by recording or implementing these fixes.
