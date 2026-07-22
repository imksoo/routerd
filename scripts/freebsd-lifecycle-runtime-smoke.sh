#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Exercise two production-managed FreeBSD lifecycle seams without relying on
# rendered files as the oracle: routerd-dhcpv4-client binds a real BPF device
# and obtains a lease from a disposable dnsmasq peer, while routerd-dns-resolver
# starts, reports health over its Unix API, reloads, and stops cleanly.
set -eu

dhcpv4_client=
dns_resolver=
evidence_dir=

while [ "$#" -gt 0 ]; do
  case "$1" in
  --dhcpv4-client) dhcpv4_client=$2; shift 2 ;;
  --dns-resolver) dns_resolver=$2; shift 2 ;;
  --evidence-dir) evidence_dir=$2; shift 2 ;;
  *) echo "usage: $0 --dhcpv4-client PATH --dns-resolver PATH --evidence-dir DIR" >&2; exit 2 ;;
  esac
done

[ -x "$dhcpv4_client" ]
[ -x "$dns_resolver" ]
[ -n "$evidence_dir" ]

case "$(uname -s)" in FreeBSD) ;; *) exit 1 ;; esac
mkdir -p "$evidence_dir"
work=$(mktemp -d /var/tmp/routerd-lifecycle-runtime.XXXXXX)
dnsmasq_pid=
resolver_pid=
epair_a=
epair_b=

cleanup() {
  rc=$?
  if [ -n "$resolver_pid" ]; then
    kill -TERM "$resolver_pid" 2>/dev/null || true
    wait "$resolver_pid" 2>/dev/null || true
  fi
  if [ -n "$dnsmasq_pid" ]; then
    kill -TERM "$dnsmasq_pid" 2>/dev/null || true
    wait "$dnsmasq_pid" 2>/dev/null || true
  fi
  if [ -n "$epair_a" ] && ifconfig "$epair_a" >/dev/null 2>&1; then
    ifconfig "$epair_a" destroy >>"$evidence_dir/cleanup.log" 2>&1 || rc=1
  fi
  rm -rf "$work"
  exit "$rc"
}
trap cleanup EXIT HUP INT TERM

epair_a=$(ifconfig epair create)
case "$epair_a" in epair*a) ;; *) echo "unexpected epair name: $epair_a" >&2; exit 1 ;; esac
epair_b=${epair_a%a}b
ifconfig "$epair_a" up
ifconfig "$epair_b" inet 192.0.2.1/24 up

cat >"$work/dnsmasq.conf" <<EOF
interface=$epair_b
bind-interfaces
except-interface=lo0
dhcp-range=192.0.2.10,192.0.2.20,255.255.255.0,1h
dhcp-option=3,192.0.2.1
dhcp-option=6,192.0.2.1
log-dhcp
EOF
dnsmasq --keep-in-foreground --conf-file="$work/dnsmasq.conf" >"$evidence_dir/dnsmasq.log" 2>&1 &
dnsmasq_pid=$!

sleep 1
kill -0 "$dnsmasq_pid"

"$dhcpv4_client" once --resource lifecycle-dhcpv4 --interface "$epair_a" \
  --timeout 25s --socket "$work/dhcpv4.sock" --lease-file "$evidence_dir/dhcpv4-lease.json" \
  --event-file "$evidence_dir/dhcpv4-events.jsonl" >"$evidence_dir/dhcpv4-result.json"
jq -e '.state == "Bound" and .currentAddress == "192.0.2.10"' "$evidence_dir/dhcpv4-result.json" >/dev/null
jq -e 'select(.reason == "DiscoverSent")' "$evidence_dir/dhcpv4-events.jsonl" >/dev/null
jq -e 'select(.reason == "LeaseBound")' "$evidence_dir/dhcpv4-events.jsonl" >/dev/null

cat >"$work/resolver.json" <<'EOF'
{"resource":"lifecycle-resolver","spec":{"listen":[{"addresses":["127.0.0.1"],"port":10553}]}}
EOF
"$dns_resolver" daemon --resource lifecycle-resolver --config-file "$work/resolver.json" \
  --socket "$work/resolver.sock" --state-file "$evidence_dir/resolver-state.json" \
  --event-file "$evidence_dir/resolver-events.jsonl" >"$evidence_dir/resolver.stdout.log" 2>"$evidence_dir/resolver.stderr.log" &
resolver_pid=$!
for _ in $(jot 30); do
  [ -S "$work/resolver.sock" ] && break
  kill -0 "$resolver_pid" 2>/dev/null || break
  sleep 1
done
[ -S "$work/resolver.sock" ]
curl --fail --silent --show-error --unix-socket "$work/resolver.sock" \
  http://localhost/v1/status >"$evidence_dir/resolver-status-before.json"
jq -e '.health == "Healthy" and .phase == "Running"' "$evidence_dir/resolver-status-before.json" >/dev/null
curl --fail --silent --show-error --unix-socket "$work/resolver.sock" -X POST \
  http://localhost/v1/reload >"$evidence_dir/resolver-reload.json"
jq -e '.reloaded == true and .listeners == 1' "$evidence_dir/resolver-reload.json" >/dev/null
kill -TERM "$resolver_pid"
wait "$resolver_pid"
resolver_pid=
[ ! -S "$work/resolver.sock" ]

"$dns_resolver" daemon --resource lifecycle-resolver --config-file "$work/resolver.json" \
  --socket "$work/resolver.sock" --state-file "$evidence_dir/resolver-state.json" \
  --event-file "$evidence_dir/resolver-events.jsonl" >>"$evidence_dir/resolver.stdout.log" 2>>"$evidence_dir/resolver.stderr.log" &
resolver_pid=$!
for _ in $(jot 30); do
  [ -S "$work/resolver.sock" ] && break
  kill -0 "$resolver_pid" 2>/dev/null || break
  sleep 1
done
[ -S "$work/resolver.sock" ]
curl --fail --silent --show-error --unix-socket "$work/resolver.sock" \
  http://localhost/v1/status >"$evidence_dir/resolver-status-restart.json"
jq -e '.health == "Healthy" and .phase == "Running"' "$evidence_dir/resolver-status-restart.json" >/dev/null
kill -TERM "$resolver_pid"
wait "$resolver_pid"
resolver_pid=
[ ! -S "$work/resolver.sock" ]

printf '%s\n' \
  'dhcpv4-bpf-lease=Bound' \
  'dns-resolver-start-observe-reload-restart-stop=ok' \
  'owned-epair-cleanup=pending-exit-trap' >"$evidence_dir/summary.log"
printf 'freebsd-lifecycle-runtime=ok\n' >"$evidence_dir/result"
