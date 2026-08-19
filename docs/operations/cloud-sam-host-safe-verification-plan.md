# Cloud SAM host-safe verification plan

Status: the named capability smoke passed on 2026-08-18 against the current
working tree: the two Mobility tests used their ephemeral `127.0.0.1:0`
listener and the policy test remained in memory. No routerd component, DHCP,
RA, provider, PVE, SSH, BGP, or host-network mutation was run. This remains a
safety boundary, not authorization to widen the commands below or to run a
paid topology; a clean canonical release-QA run root is still required.

This is the only permitted host-capability check after the pre-host legacy
removal audit has passed. It is intentionally not a routerd integration run:
it does not start `routerd`, invoke `routerctl`, apply configuration, create a
network namespace, use `sudo`, query a provider, or touch a real BGP session.

The plan is limited to two ephemeral IPv4 loopback HTTP tests and one pure
in-memory policy test. It starts no DHCP client/server, emits no IPv6 RA, and
changes no link, address, route, firewall, sysctl, service, or routerd/host
persistent state. The mobility tests may create SQLite DB/WAL/SHM files, but
only below the private temporary directory created by this plan. Its sole
listener is the Go test server bound to `127.0.0.1:0`; it is closed by the test
before the process exits and is not reachable from another host. The policy
test does not execute `ip`, `ifconfig`, or any other host network query.

## Preconditions

- Every source-level item in the **Pre-host legacy-removal audit** in
  [`cloud-sam-rearchitecture-goal.md`](cloud-sam-rearchitecture-goal.md) is
  checked, including the typed-plan wake-up audit.
- The exact source revision, dirty-worktree state, and manifest of the named
  test/source files are recorded with the test evidence. A commit ID alone is
  insufficient for a dirty worktree.
- A reviewer has read this document and confirmed that no command has been
  widened, replaced with `make test`, or combined with another test target.
- Stop immediately on any unexpected subprocess, socket endpoint other than
  `127.0.0.1`, privilege request, DHCP/RA log line, or network mutation
  attempt. Do not retry with `sudo` or a broader test selector.

## Isolated command sequence

Run the following in one shell only. It creates a private directory under
`/tmp`, uses it for all Go cache and temporary files, and deletes only that
validated directory on exit.

```sh
set -euo pipefail
umask 077
sam_host_tmp=$(mktemp -d /tmp/routerd-sam-host-verify.XXXXXX)
case "$sam_host_tmp" in
  /tmp/routerd-sam-host-verify.*) ;;
  *) printf 'unexpected temporary path: %s\n' "$sam_host_tmp" >&2; exit 1 ;;
esac
cleanup() {
  case "$sam_host_tmp" in
    /tmp/routerd-sam-host-verify.*) rm -rf -- "$sam_host_tmp" ;;
    *) printf 'refusing to remove unexpected path: %s\n' "$sam_host_tmp" >&2; return 1 ;;
  esac
}
trap cleanup EXIT
mkdir -p "$sam_host_tmp/cache" "$sam_host_tmp/tmp"
export GOCACHE="$sam_host_tmp/cache"
export GOTMPDIR="$sam_host_tmp/tmp"
export TMPDIR="$sam_host_tmp/tmp"
export GOTOOLCHAIN=go1.25.9
export GOPROXY=off GONOSUMDB='*' GOTELEMETRY=off
export GOFLAGS='-mod=readonly -buildvcs=false'
unset HTTP_PROXY HTTPS_PROXY ALL_PROXY http_proxy https_proxy all_proxy
export NO_PROXY=127.0.0.1,localhost
export no_proxy=127.0.0.1,localhost

git rev-parse HEAD
git status --short
sha256sum go.mod go.sum \
  pkg/controller/mobility/peergroupsync.go \
  pkg/controller/mobility/peergroupsync_test.go \
  pkg/controller/mobility/transport.go \
  pkg/controller/chain/policyroute.go \
  pkg/controller/chain/policyroute_test.go
! rg -n -U '^func (TestMain|init)\(' pkg/controller/mobility pkg/controller/chain --glob '*.go'

# The two selected tests use net.Listen("tcp4", "127.0.0.1:0") only.
go test ./pkg/controller/mobility \
  -run '^(TestPeerGroupSyncClientFetchesAndStoresGroup|TestSAMTransportProfilePeersFromSyncResolvesMissingGroup)$' \
  -count=1 -timeout=30s

# Pure in-memory policy fixture: it neither applies nor probes host network
# state, and it does not start any protocol daemon.
go test ./pkg/controller/chain \
  -run '^TestIPv4PolicyRouteRejectsDSLiteTargetUntilTunnelIsUp$' \
  -count=1 -timeout=30s
```

No selected test is allowed to be swapped for
`TestEffectivePolicyRouteExcludesWhenFalseDSLiteTargetWithoutMutatingSpec`:
that test reads host netlink state and is deliberately outside this plan even
though it does not mutate it.

## Explicit exclusions

The following commands remain prohibited in this phase:

- `routerd`, `routerctl apply`, `routerctl daemon`, or any apply/serve mode;
- all DHCPv4/DHCPv6 client or server tests, all RA tests, and all
  `tests/netns` targets;
- `sudo`, `ip link/address/route` mutation, `sysctl -w`, nftables/pf changes,
  provider plugins, cloud actions, SSH to a lab, and real BGP/FIB probes;
- `make test`, `go test ./...`, or any unreviewed selector.
- `make validate-wizard-fixtures`, release-QA Python suites/drivers, or any
  command that starts a daemon, Unix socket service, `sudo systemd-run`, or a
  namespace even if it describes itself as a local check.

Passing this plan is a narrow host-capability result only. It does not
authorize cloud qualification; the separately reviewed full-topology profile
and its cleanup contract still apply.
