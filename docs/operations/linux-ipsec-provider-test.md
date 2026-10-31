# Linux IPsec package and service gate

Use only an explicitly assigned disposable Ubuntu 24.04 VM with independent
management access. Unlike the private-process gate, this test installs and
removes packages and deliberately interrupts its newly installed provider.
It refuses an existing strongSwan provider/configuration, nonempty routerd
configuration, nonempty XFRM state/policies, or an unhealthy installed routerd.
The guest needs Python 3, jq, iproute2, namespace/mount tools, AppArmor with its
profile parser, and the Ubuntu archive keyring. Existing provider configuration
and installed packages must be absent; review the package simulation first.

Prepare a private, root-owned work directory and a checksum-pinned `make dist`
archive. Prepare the independent peer's matching strongSwan dependency extraction
and an `APT_CONFIG` pointing to authenticated Ubuntu repositories, private lists
and archives. On a network-isolated guest, a loopback SSH reverse forward can
carry APT HTTP requests without changing routes, NAT or firewall configuration.
Use the installed Ubuntu archive keyring; never disable signature verification.
Run `apt-get -o APT::Update::Error-Mode=any update` and retain its exit status.
Review `apt-get -s install charon-systemd strongswan-swanctl`: this fixture expects
six initially absent strongSwan packages and no existing-package upgrades/removals.
Keep that prerequisite separate from the product's actual package installation.
Run from the repository root. Create `WORK_DIR` before invoking the script and
use a fresh directory for each attempt, so a failed result is not overwritten.

```sh
sudo timeout -k 15 900 bash scripts/linux-ipsec-provider-smoke.sh \
  --disposable-vm /root/private-work /root/routerd.tar.gz EXPECTED_SHA256 \
  /root/private-peer/extracted /root/private-work/apt.conf
```

The gate records the installed unit, binary/configuration hashes, package and
kernel inventories, then stops the empty installed routerd. It backs up its
configuration and persistent state and records every state-file hash. The tested
binary is extracted privately and uses separate state, ledger and control paths;
the installed binary/configuration/DB are not replaced. Product-generated
configuration under the installation is a new, initially absent IPsec receipt.
The package-only production controller first installs derived dependencies. Before
the product acquires its connection, the fixture loads one unrelated connection
from `/etc/swanctl/` and verifies it with `--list-conns`. This location respects
Ubuntu's swanctl AppArmor profile. Do not use `--load-conns` after product
acquisition: it can unload connections absent from its input. A zero exit from
the command is insufficient proof that its configuration was read.

Acceptance requires:

- Actual PackageController installation of derived dependencies, a running
  `strongswan.service`, VICI readiness and an acknowledged scoped IPsec receipt.
- Independent peer IKE/ESP and bidirectional selector traffic.
- Reconciliation, withdrawal and reacquisition without changing provider PID or
  systemd invocation identity, while an independently loaded connection survives.
  Unchanged reconciliation also retains the active ESP SPIs and actual traffic.
- Before injecting a crash, the running owner must persist each sampled native
  SPI, installation time, kernel scope and configuration fingerprint in its
  actual receipt. The fixture samples fresh native state while waiting, records
  exact coverage in `precrash-receipt-proof.json`, and stops before the crash if
  that prerequisite is not met within 60 seconds.
- An explicitly injected provider SIGKILL followed by systemd restart and the
  running routerd's periodic configuration recovery, with no manual provider
  restart or product reconcile between failure and recovered traffic. Record
  ordinary traffic continuity separately from new-provider IKE/ESP recovery.
  Continuity may use old or immediately re-established SPIs; their identities
  and counters are recorded without requiring an old SPI to remain installed.
- The independent peer uses a 20-second CHILD rekey, a 30-second hard lifetime,
  no lifetime jitter, short retries and a trap action. After at least one such
  interval, ordinary selector traffic must establish new provider-managed SPIs
  with actual packet counters. No fixture `--initiate` or `--rekey` follows
  the crash. The new-provider assertion has a bounded 90-second observation.
- After stopping the running routerd, a one-shot declaration withdrawal must
  remove its connections, shared-key receipt and all observed old/new owned SPIs.
  The independent configuration remains under the package's normal `conf.d`
  include path across the provider restart. Its native connection block must be
  unchanged after recovery and withdrawal. The gate does not flush the SAD or
  run a global connection unload.
- Child exit acknowledgments; exact owned-effect withdrawal; removal of only
  initially absent test packages; restoration of module/link/route/rule and
  AppArmor inventories; removal of the stopped provider's own stale socket; unchanged
  installed binary/config/unit/state hashes; and the original routerd Healthy again.

Failure retains the stage, provider journal, receipt and cleanup results.
After the provider crash, a failed continuity or recovery observation remains a
final failure but does not suppress the independent withdrawal observation.
Withdrawal runs only after the owned routerd worker's exit is acknowledged and
VICI readiness is confirmed. If either is unknown, the fixture records that
withdrawal was not attempted. `recovery-result.json` records these separate
verdicts; final success also requires cleanup and restoration to pass. Investigate
the preserved failure before another run. Never restore a DB
over an unexplained concurrent writer or remove an unknown pending receipt.
This is one PSK/algorithm/peer configuration, not all IPsec modes or a release verdict.
Preserve source/archive/script hashes and the scoped verdict. After review, remove
resolved raw output, duplicate binaries and backups once restoration is verified.
Keep this procedure and regression checks as the durable deliverables.


## Crash recovery observations and local hard expiry

SIGKILL removes the provider's in-memory IKE state but need not remove the kernel
SAs. The run05 observation recorded successful bidirectional traffic over the
same pre-crash SPIs while the restarted provider reported no IKE SA. Both the
1.0 renderer and the current scoped owner configure `start_action = trap`.
This observation proves continuity; it does not by itself prove a forwarding
failure or a 1.5 regression.

IKEv2 endpoints apply their own lifetime policies; the peer's short lifetime
does not shorten the product's kernel SAs ([RFC 7296 section 2.8](https://www.rfc-editor.org/rfc/rfc7296.html#section-2.8)).
The current `IPsecConnection` API has no lifetime fields. The observed product
SAs had a 3960-second hard lifetime. The peer renewal/expiry test above is a
separate qualification from waiting for those local SAs to expire.

The fixture writes `local-expiry-plan.json` from each actual pre-crash ESP
SA's add time and configured hard lifetime, with UTC timestamps. It does not
assume 3960 seconds, reset the VM clock or rewrite kernel lifetimes. To keep the
same processes and ordinary traffic alive until five seconds after the latest
recorded local hard-expiry deadline, run a separate fresh trial:

```sh
sudo env ROUTERD_IPSEC_WAIT_LOCAL_EXPIRY=1 timeout -k 15 6000 \
  bash scripts/linux-ipsec-provider-smoke.sh \
  --disposable-vm /root/fresh-private-work /root/routerd.tar.gz EXPECTED_SHA256 \
  /root/private-peer/extracted /root/fresh-private-work/apt.conf
```

This longer trial keeps the peer namespace holder alive for 6000 seconds. It
requires the recorded old SPIs to be absent after their original deadline and
new provider-managed SAs to carry packets. `recovery-observations.jsonl` retains
the bounded sequence of old-SPI presence and packet counters. If an old SA is
retired earlier, report that observed transition rather than attributing it to
a hard-expiry event. The peer may rekey or time out according to its own normal
settings; this is not a test of lifetime negotiation between endpoints.

Retain distinct verdicts for `provider-crash-continuity`,
`peer-expiry-recovery` (or `recorded-local-expiry-recovery`), and
`post-crash-withdrawal`. A pending/empty receipt, a native SA inventory and real
packet counters are different observations. Failure of a later condition does
not erase earlier continuity, and continuity does not qualify the later ones.
The fixture continues to the safe withdrawal stage after a failed recovery
deadline so that remaining known SPIs and preserved foreign configuration can
be assessed independently. An immediate new-SPI recovery qualifies ordinary
traffic continuity without making a claim about old-SPI counter growth.
The exact-record gate covers SAs observed before the crash. It does not qualify
the asynchronous window between a new SA's creation/rekey and the owner's next
observation. On withdrawal, a remaining same-endpoint SA without exact recorded
authority is detected as uncertainty: routerd preserves its pending receipt and
returns an error instead of deleting it or reporting withdrawal complete. Endpoint
matching supplies no deletion authority. A different native generation under a
recorded lookup identity is also preserved and rejected.

These additions are prepared test conditions; only a new assigned-VM execution
can qualify them. They do not change the recorded run05 or product-dc14/fixture-2b
result. That original short trial recovered traffic and new provider SAs but
left two old SPIs after the declaration was removed.

The renderer and VICI loader continue to omit `mobike`. strongSwan 5.9 enables
[MOBIKE by default](https://docs.strongswan.org/docs/5.9/swanctl/swanctlConf.html),
so the absence of a dedicated routerd API field does not mean endpoint mobility
is disabled. This gate uses fixed configured endpoints. The native recovery
owner does not acquire new kernel deletion authority for a changed endpoint
pair merely because VICI reports the same connection name. It retains the
original receipt and returns an unknown-ownership error. Recovery withdrawal
after an authenticated MOBIKE move needs additional association proof and is
not qualified here; the protocol's existing default is unchanged.

The peer settings use the documented [swanctl CHILD lifetime and DPD controls](https://docs.strongswan.org/docs/5.9/swanctl/swanctlConf.html)
and private daemon retransmission controls. No provider-wide flush, foreign
connection removal, test-only product API or general recovery manager is added.
