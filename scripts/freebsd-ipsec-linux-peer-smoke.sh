#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu
routerd='' evidence=''
while [ "$#" -gt 0 ]; do case "$1" in --routerd) routerd=$2; shift 2;; --evidence-dir) evidence=$2; shift 2;; *) exit 2;; esac; done
[ "$(uname -s)" = FreeBSD ] && [ -x "$routerd" ] && [ -n "$evidence" ]
mkdir -p "$evidence"; work=$evidence/work; mkdir -p "$work"
peer_addr=${ROUTERD_IPSEC_PEER_ADDR:-10.0.2.2}
host_if=$(route -n get default | awk '/interface:/{print $2; exit}')
host_addr=$(ifconfig "$host_if" inet | awk '/inet /{print $2; exit}')
[ -n "$host_if" ] && [ -n "$host_addr" ]
host_ts=10.250.1.1 peer_ts=10.250.2.1 psk=routerd-native-linux-peer-disposable-psk
cleanup() { rc=$?; service strongswan onestop >>"$evidence/cleanup.log" 2>&1 || true; sysrc -x strongswan_enable >>"$evidence/cleanup.log" 2>&1 || true; ifconfig lo0 inet "$host_ts" -alias >/dev/null 2>&1 || true; rm -f /usr/local/etc/routerd/swanctl/routerd-*.conf /usr/local/etc/routerd/swanctl/routerd.conf /usr/local/etc/routerd/swanctl/.routerd-pending-load; if [ "$rc" -ne 0 ]; then for f in "$evidence"/*.log; do [ -f "$f" ] && sed "s/$psk/[REDACTED]/g" "$f" >&3; done; fi; return "$rc"; }
exec 3>&2; trap cleanup EXIT HUP INT TERM
ifconfig lo0 inet "$host_ts/32" alias
cat >"$work/router.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: native-ipsec-linux-peer}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: IPsecConnection
    metadata: {name: native-tunnel}
    spec: {localAddress: $host_addr, remoteAddress: $peer_addr, preSharedKey: $psk, leftSubnet: $host_ts/32, rightSubnet: $peer_ts/32}
EOF
"$routerd" apply --once --config "$work/router.yaml" --state-file "$work/state.db" --ledger-file "$work/ledger.db" >"$evidence/apply.log" 2>&1
/usr/local/sbin/swanctl --initiate --ike native-tunnel --child net >"$evidence/initiate.log" 2>&1
/usr/local/sbin/swanctl --list-sas --ike native-tunnel >"$evidence/sa.log"; grep -q ESTABLISHED "$evidence/sa.log"
ping -n -S "$host_ts" -c 2 "$peer_ts" >"$evidence/traffic.log" 2>&1
/usr/local/sbin/swanctl --rekey --ike native-tunnel >"$evidence/rekey.log" 2>&1
service strongswan onestop >"$evidence/restart-stop.log" 2>&1
"$routerd" apply --once --config "$work/router.yaml" --state-file "$work/state.db" --ledger-file "$work/ledger.db" >"$evidence/restart.log" 2>&1
cat >"$work/empty.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: empty}
spec: {}
EOF
"$routerd" apply --once --config "$work/empty.yaml" >"$evidence/teardown.log" 2>&1
printf 'freebsd-ipsec-linux-peer=ok\n' >"$evidence/result"
