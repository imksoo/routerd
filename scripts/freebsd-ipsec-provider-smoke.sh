#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# Verify routerd's FreeBSD strongSwan provider lifecycle without a peer fixture.
set -eu

usage() {
  echo 'usage: freebsd-ipsec-provider-smoke.sh --routerd /absolute/routerd --evidence-dir /absolute/dir' >&2
}

routerd=
evidence=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --routerd) routerd=${2:?missing routerd path}; shift 2 ;;
    --evidence-dir) evidence=${2:?missing evidence directory}; shift 2 ;;
    *) usage; exit 2 ;;
  esac
done

[ "$(uname -s)" = FreeBSD ] || { echo 'FreeBSD is required' >&2; exit 2; }
[ -x "$routerd" ] || { echo 'an executable --routerd is required' >&2; exit 2; }
[ -n "$evidence" ] || { echo '--evidence-dir is required' >&2; exit 2; }
pkg info -e strongswan || { echo 'strongswan package is required' >&2; exit 2; }

mkdir -p "$evidence"
work="$evidence/work"
mkdir -p "$work"
state="$work/state.db"
ledger="$work/ledger.db"
connection='routerd-provider-lifecycle'
psk='routerd-provider-lifecycle-disposable-psk'
service_started=0
cleanup_started=0
enable_known=0

cleanup() {
  rc=$?
  [ "$cleanup_started" -eq 0 ] || return "$rc"
  cleanup_started=1
  trap - EXIT HUP INT TERM
  if [ "$service_started" -eq 1 ]; then
    service strongswan onestop >>"$evidence/cleanup.log" 2>&1 || true
  fi
  if [ "$enable_known" -eq 1 ]; then
    sysrc "strongswan_enable=$(cat "$work/strongswan-enable.before")" >>"$evidence/cleanup.log" 2>&1 || true
  else
    sysrc -x strongswan_enable >>"$evidence/cleanup.log" 2>&1 || true
  fi
  rm -f /usr/local/etc/routerd/swanctl/routerd-*.conf \
    /usr/local/etc/routerd/swanctl/routerd.conf \
    /usr/local/etc/routerd/swanctl/.routerd-pending-load >>"$evidence/cleanup.log" 2>&1 || true
  return "$rc"
}
trap cleanup EXIT HUP INT TERM

if service strongswan status >"$evidence/service.before.log" 2>&1; then
  echo 'provider lifecycle smoke requires a fresh FreeBSD guest without a running strongswan service' >&2
  exit 1
fi
for endpoint in /var/run/charon.vici /var/run/charon.pid /var/run/daemon-charon.pid; do
  if [ -e "$endpoint" ]; then
    echo "provider lifecycle smoke requires no existing provider endpoint: $endpoint" >&2
    exit 1
  fi
done
if sysrc -n strongswan_enable >"$work/strongswan-enable.before" 2>"$evidence/strongswan-enable.before.log"; then
  enable_known=1
fi

cat >"$work/router.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-ipsec-provider-lifecycle}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPsecConnection
    metadata: {name: $connection}
    spec:
      localAddress: 198.18.200.10
      remoteAddress: 198.18.200.1
      preSharedKey: $psk
      leftSubnet: 10.250.1.1/32
      rightSubnet: 10.250.2.1/32
EOF
cat >"$work/empty.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-ipsec-provider-lifecycle-empty}
spec: {}
EOF

"$routerd" apply --once --config "$work/router.yaml" --state-file "$state" --ledger-file "$ledger" \
  --status-file "$evidence/apply.status.json" >"$evidence/apply.log" 2>&1
service_started=1
/usr/local/sbin/swanctl --stats >"$evidence/provider.stats.log" 2>&1
/usr/local/sbin/swanctl --list-conns >"$evidence/provider.connections.log" 2>&1
grep -F "$connection" "$evidence/provider.connections.log" >/dev/null

"$routerd" apply --once --config "$work/empty.yaml" --state-file "$state" --ledger-file "$ledger" \
  --status-file "$evidence/teardown.status.json" >"$evidence/teardown.log" 2>&1
/usr/local/sbin/swanctl --list-conns >"$evidence/teardown.connections.log" 2>&1
if grep -F "$connection" "$evidence/teardown.connections.log" >/dev/null; then
  echo 'routerd-owned IPsec connection remained after empty desired state' >&2
  exit 1
fi

printf '%s\n' \
  'ipsec-provider-vici=ok' \
  'ipsec-provider-definition-persists-after-routerd-exit=ok' \
  'ipsec-provider-empty-desired-state-withdraws-definition=ok' >"$evidence/summary.log"
printf 'freebsd-ipsec-provider-lifecycle=ok\n' >"$evidence/result"
