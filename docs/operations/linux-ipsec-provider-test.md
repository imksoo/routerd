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
- An explicitly injected provider SIGKILL followed by systemd restart and the
  running routerd's periodic recovery, with no manual provider restart or product
  reconcile between failure and recovered traffic. Ordinary selector traffic
  must recover an established SA without a fixture `--initiate` after the crash.
- Child exit acknowledgments; exact owned-effect withdrawal; removal of only
  initially absent test packages; restoration of module/link/route/rule and
  AppArmor inventories; removal of the stopped provider's own stale socket; unchanged
  installed binary/config/unit/state hashes; and the original routerd Healthy again.

Failure retains the stage, provider journal, receipt and cleanup results. Stop
qualification if package installation, ownership, restoration or native traffic
fails; investigate the preserved result before another run. Never restore a DB
over an unexplained concurrent writer or remove an unknown pending receipt.
This is one PSK/algorithm/peer configuration, not all IPsec modes or a release verdict.
Preserve source/archive/script hashes and the scoped verdict. After review, remove
resolved raw output, duplicate binaries and backups once restoration is verified.
Keep this procedure and regression checks as the durable deliverables.
