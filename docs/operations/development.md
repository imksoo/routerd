---
title: Development checks
---

# Development checks

![Diagram showing development checks split between local pre-commit tests, CI pull request validation, and release workflow archive publishing](/img/diagrams/operations-development.png)

routerd uses two separate automation paths.

- The CI workflow checks normal pushes and pull requests.
- The release workflow builds signed release archives after a release tag is pushed.

The release workflow is intentionally separate because it builds multiple
operating system and architecture archives and publishes GitHub Release assets.


## Mandatory test and harness design rules

These rules apply to unit tests, offline fixtures, runtime smoke, release qualification and reusable old helpers, templates and copy sources, including code without a current caller. Every test change must identify its product requirement, required direct evidence and observation limits.

- Preserve observations and diagnostic logs; separate them from product acceptance. Journal text, progress counters, latency statistics and whole-host diagnostics must not become acceptance conditions without a concrete product requirement.
- Judge PASS or product FAIL using completed, attributable direct evidence of that requirement. API/SSH instability, acquisition failure, missing or malformed input, incomplete guest commands and timeouts must remain observation or infrastructure outcomes. Never convert them into zero, no violation, successful rejection or measured product failure.
- Reduce arbitrary speed assertions, repeated probes and duplicate gates. Keep contractual deadlines and a bounded watchdog to stop hung work; distinguish watchdogs from product performance measurements.
- Before deleting temporary data, retain raw failed attempts, source identity, exits, timestamps and cleanup outcomes. Continue cleanup only for resources this invocation owns. Keep cleanup failure visible and preserve product evidence.
- Do not repeat a failed real trial without investigating its cause. Stop at the existing retry, time or cost budget; report failure, uncertainty, remaining budget and proposed fix before further trials. Reacquire only justified failed or missing observations, with bounds and retained attempts.
- Audit the actual entry, selected source paths and hashes, dependencies and final result propagation. Candidate and mock validation proves that scope; it cannot prove that a running entry selects the candidate or that real forwarding works.
- Distinguish implementation, prepared candidate, deployed code, saved-original replay, deliberately modified fixture and unverified behavior. Preserve original negative evidence; never rewrite a historical failure as PASS.
- Apply these rules before reusing old or unconnected code. Keep sealed historical evidence unchanged; repair reusable sources. An absent current caller does not excuse a defect.

These rules do not weaken real functional requirements, safety or ownership checks, or spending limits. Missing necessary evidence cannot establish PASS. Keep proof of normal communication and direct violations; remove only unjustified coupling to diagnostics.

Good: curl exit 0 and HTTP 200 with required body/path evidence can PASS when optional latency is missing; record the omission. Bad: a failed WireGuard read proves no forbidden AllowedIP; a forced invalid-apply timeout counts as successful rejection; a same-stack ping proves tunnel forwarding.

## CI workflow

`.github/workflows/ci.yaml` runs on branch pushes and pull requests.
It uses an Ubuntu runner and checks the development surface that should stay
green before review:

```sh
go test ./...
make check-schema
make validate-example
make website-build
```

When a change touches `webconsole/`, its checked-in static assets, the shared
quality workflow, or the Makefile, CI also runs the Web Console-specific gate:
`npm ci`, high-severity audits for all and production dependencies, TypeScript
type-checking, a production build, and a generated-asset drift check.

The CI workflow does not publish release artifacts.
Release archives are created only by the `Release` workflow on date-based tags.

## Native socket checks

Normal `go test ./...` is unprivileged. The actual ICMPv6 receiver test uses the
`routerd_native` build tag and must run separately with raw-socket privileges.
CI runs it in an isolated Linux network namespace; the FreeBSD native VM gate
also runs it explicitly. Enabling the tag retains permission failures as failures.

In a disposable Linux VM, compile as the normal user and execute only that test
with privilege:

```sh
go test -tags routerd_native -c -o /tmp/routeradvertisement-native.test ./pkg/routeradvertisement
sudo unshare --net sh -ec 'ip link set lo up; exec /tmp/routeradvertisement-native.test -test.run="^TestSolicitationWildcardReceiverOnActualSocket$" -test.v'
```

On a disposable FreeBSD VM, build with the same tag and execute the test binary
as root. This checks actual kernel reception and packet metadata; it does not
prove forwarding or peer interoperability. Remove the binary when finished.

## Native helper ownership and service capabilities

The Linux service needs `CAP_SYS_PTRACE` to read the exact procfs identity of
its dnsmasq child after dnsmasq drops UID, and `CAP_KILL` to signal that verified
child through a pidfd. The packaged unit and runtime renderer carry the same
capabilities. Do not weaken native identity checks or keep dnsmasq running as
root to hide a permission failure. The procfs access checks are described in
[ptrace(2)](https://man7.org/linux/man-pages/man2/ptrace.2.html).

Compile without privilege, then run the explicit native test in new PID/mount
namespaces. It drops the observer to the actual service bounding set, observes
a child under another UID and confirms its termination through the production
owner. Failure to observe or signal is a failed gate. CI runs the same test.

```sh
go test -tags routerd_native -c -o /tmp/chain-native.test ./pkg/controller/chain
sudo unshare --mount --pid --fork --mount-proc /tmp/chain-native.test -test.run="^TestDnsmasqOwnerWithServiceCapabilities$" -test.v
```

This proves the native privilege boundary. Actual DHCP acquisition, renewal,
DNS requests and service restart still require the isolated VM/client tests.
Remove the test binary after use.

## Pre-commit hook

The repository includes an optional pre-commit hook script:

```sh
ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
chmod +x scripts/pre-commit.sh
```

After enabling it, `git commit` runs:

```sh
go test ./...
make check-schema
```

If either command fails, the commit is stopped.
This catches schema drift and test failures before they reach CI.

For an emergency local commit, set this environment variable:

```sh
ROUTERD_SKIP_PRE_COMMIT=1 git commit
```

Use that only when the follow-up fix is already clear.
CI still runs after the branch is pushed.

## Linux PD/RA lifecycle VM gate

`scripts/linux-pd-ra-lifecycle-smoke.sh` exercises an installed `make dist`
package through its actual systemd service. Use a dedicated, disposable Linux VM
with an empty router configuration, independent management access, and no
production traffic. The explicit `--disposable-vm` argument is required. It
replaces that guest's configuration and changes its test interfaces and IPv6
forwarding; it is not part of ordinary unit tests.

Certify the guest before running: systemd must be healthy; install iproute2,
Python 3, curl, jq, dig and dnsmasq. Prepare Kea DHCPv6 and ndisc6 from packages
matching the guest, with their shared libraries. Either use an installed package
root or extract the packages as root into a private directory. In particular,
`rdisc6` must remain root-owned: extracting its setuid executable as an ordinary
user changes its privilege behavior. Check Kea's shared libraries with `ldd`.
The tested peer was Kea 2.4.1 on Ubuntu 24.04.4. Its transient unit owns `/run/kea`;
an existing Kea service/runtime directory is a collision, not permission to stop it.

```sh
sudo scripts/linux-pd-ra-lifecycle-smoke.sh --disposable-vm \
  "$PWD/tests/fixtures/linux-pd-ra-lifecycle.json" /opt/routerd-pd-peer
```

The package root above contains `usr/sbin/kea-dhcp6`, `usr/bin/rdisc6` and
`usr/lib/x86_64-linux-gnu`; use `/` only when the prerequisites are already
installed and no Kea instance owns its runtime directory. See the
[Kea DHCPv6 configuration reference](https://kea.readthedocs.io/en/kea-2.4.1/arm/dhcp6-srv.html)
for the independent PD pool and lease timer semantics.

The test uses separate upstream and client namespaces. Kea delegates a /60 with
20/40-second T1/T2 and 60/90-second preferred/valid lifetimes. Linux processes
real RA packets to create its SLAAC address and default route; `rdisc6` separately
checks the PIO. Pass requires local and forwarded AAAA answers over UDP/TCP,
an HTTP peer observing the client's delegated source, actual Renew/Reply with
an advanced lease timestamp, service restart, and upstream-stop expiry removing
the router address and client default route. Traffic must then fail while the
product service remains active. Final empty apply, peer shutdown, namespace
removal and restoration of the original forwarding setting are also required.

DAD is asynchronous: a newly installed delegated address holds RA in Pending
without failing configuration apply. Positive advertisement resumes only after
address readiness; failed DAD and unknown ownership remain errors. DNSForwarder
rules are evaluated in configuration order, so the fixture's local zone precedes
the default upstream rule.

Successful raw output is deleted automatically; retain the revision and verdict,
this procedure, its fixture and the regression tests. Failures stop the product
and peers and retain the exact fixture path for diagnosis. Reproduce a product
failure locally before another native run, and remove retained output after the
failure is resolved. WAN transit and its return route are fixture scaffolding:
this gate does not qualify WAN RA discovery, DHCPv6 IA_NA, prefix renumbering,
reboot/crash recovery, firewall isolation, FreeBSD or all routerd 1.0 functions.
