#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Exercise two production-managed FreeBSD lifecycle seams without relying on
# rendered files as the oracle: routerd-dhcpv4-client binds a real BPF device
# and obtains a lease from a disposable dnsmasq peer, while routerd-dns-resolver
# starts, reports health over its Unix API, reloads, and stops cleanly.
set -eu

dhcpv4_client=
dhcpv6_client=
dns_resolver=
routerd=
evidence_dir=

while [ "$#" -gt 0 ]; do
  case "$1" in
  --dhcpv4-client) dhcpv4_client=$2; shift 2 ;;
  --dhcpv6-client) dhcpv6_client=$2; shift 2 ;;
  --dns-resolver) dns_resolver=$2; shift 2 ;;
  --routerd) routerd=$2; shift 2 ;;
  --evidence-dir) evidence_dir=$2; shift 2 ;;
  *) echo "usage: $0 --dhcpv4-client PATH --dhcpv6-client PATH --dns-resolver PATH --routerd PATH --evidence-dir DIR" >&2; exit 2 ;;
  esac
done

[ -x "$dhcpv4_client" ]
[ -x "$dhcpv6_client" ]
[ -x "$dns_resolver" ]
[ -x "$routerd" ]
[ -n "$evidence_dir" ]

case "$(uname -s)" in FreeBSD) ;; *) exit 1 ;; esac
mkdir -p "$evidence_dir"
work=$(mktemp -d /var/tmp/routerd-lifecycle-runtime.XXXXXX)
dnsmasq_pid=
resolver_pid=
dhcpv6_pid=
kea_pid=
epair_a=
epair_b=
rcd_script=/usr/local/etc/rc.d/routerd_dnsmasq
rcd_config=/usr/local/etc/routerd/dnsmasq.conf
rcd_installed=0

# Each of these is a fixture-owned foreground daemon.  FreeBSD reports the
# deliberate SIGTERM through wait(1) as 128+SIGTERM; after its live status was
# already asserted, that is cleanup bookkeeping rather than a lifecycle
# failure.  Keep the caller's assertion result while reaping it.
stop_owned_pid() {
  pid=$1
  [ -n "$pid" ] || return 0
  if kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null || true
  fi
  wait "$pid" 2>/dev/null || true
}

wait_resolver_healthy() {
  pid=$1
  socket=$2
  status_file=$3
  for _ in $(jot 30); do
    kill -0 "$pid" 2>/dev/null || break
    if curl --fail --silent --show-error --unix-socket "$socket" \
      http://localhost/v1/status >"$status_file" 2>/dev/null && \
      jq -e '.health == "ok" and .phase == "Running"' "$status_file" >/dev/null; then
      return 0
    fi
    sleep 1
  done
  jq -e '.health == "ok" and .phase == "Running"' "$status_file" >/dev/null
}

cleanup() {
  rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "freebsd-lifecycle-runtime failure evidence:" >&2
    for log in "$evidence_dir"/*; do
      [ -f "$log" ] || continue
      echo "--- $log" >&2
      cat "$log" >&2 || true
    done
  fi
  if [ -n "$resolver_pid" ]; then
    stop_owned_pid "$resolver_pid"
  fi
  if [ -n "$dhcpv6_pid" ]; then
    stop_owned_pid "$dhcpv6_pid"
  fi
  if [ -n "$kea_pid" ]; then
    stop_owned_pid "$kea_pid"
  fi
  if [ -n "$dnsmasq_pid" ]; then
    stop_owned_pid "$dnsmasq_pid"
  fi
  if [ "$rcd_installed" -eq 1 ]; then
    env routerd_dnsmasq_enable=YES "$rcd_script" onestop >>"$evidence_dir/dnsmasq-rcd-stop.log" 2>&1 || rc=1
    rm -f "$rcd_script" "$rcd_config"
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
# dnsmasq legitimately chooses any address in the configured disposable pool;
# require a real Bound lease in that pool rather than a non-protocol-specific
# first-address assumption.
jq -e '.state == "Bound" and (.currentAddress | test("^192\\.0\\.2\\.(1[0-9]|20)$"))' "$evidence_dir/dhcpv4-result.json" >/dev/null
jq -e 'select(.reason == "DiscoverSent")' "$evidence_dir/dhcpv4-events.jsonl" >/dev/null
jq -e 'select(.reason == "LeaseBound")' "$evidence_dir/dhcpv4-events.jsonl" >/dev/null

# Exercise the actual current routerd FreeBSD render artifact.  The native
# guest must not overwrite an operator-owned rc.d/config collision.
if [ -e "$rcd_script" ] || [ -e "$rcd_config" ]; then
  echo "routerd dnsmasq rc.d fixture collision" >&2
  exit 1
fi
# The DHCPv4 client has already completed. If the disposable peer exited in
# the interval, its absence is not a production failure and must not trip
# `set -e` before the generated rc.d lifecycle is reached.
stop_owned_pid "$dnsmasq_pid"
dnsmasq_pid=
cat >"$work/routerd-dnsmasq.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: lifecycle-dnsmasq
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: $epair_b
        adminUp: true
        managed: false
        owner: external
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata:
        name: lan-address
      spec:
        interface: lan
        address: 192.0.2.1/24
        exclusive: false
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Server
      metadata:
        name: lan-dhcp
      spec:
        interface: lan
        addressPool:
          start: 192.0.2.10
          end: 192.0.2.20
          leaseTime: 1h
        gatewayFrom:
          resource: IPv4StaticAddress/lan-address
          field: address
        dnsServerFrom:
          - resource: IPv4StaticAddress/lan-address
            field: address
EOF
"$routerd" render freebsd --config "$work/routerd-dnsmasq.yaml" --out-dir "$work/rendered" >"$evidence_dir/dnsmasq-render.log"
test -s "$work/rendered/dnsmasq.conf"
test -x "$work/rendered/rc.d-routerd_dnsmasq"
install -d -m 0755 /usr/local/etc/routerd /usr/local/etc/rc.d
install -m 0644 "$work/rendered/dnsmasq.conf" "$rcd_config"
install -m 0555 "$work/rendered/rc.d-routerd_dnsmasq" "$rcd_script"
rcd_installed=1
env routerd_dnsmasq_enable=YES "$rcd_script" onestart >"$evidence_dir/dnsmasq-rcd-start.log" 2>&1
env routerd_dnsmasq_enable=YES "$rcd_script" onestatus >"$evidence_dir/dnsmasq-rcd-status.log" 2>&1
env routerd_dnsmasq_enable=YES "$rcd_script" onerestart >"$evidence_dir/dnsmasq-rcd-restart.log" 2>&1
env routerd_dnsmasq_enable=YES "$rcd_script" onestatus >>"$evidence_dir/dnsmasq-rcd-status.log" 2>&1
env routerd_dnsmasq_enable=YES "$rcd_script" onestop >"$evidence_dir/dnsmasq-rcd-stop.log" 2>&1
rcd_installed=0
rm -f "$rcd_script" "$rcd_config"

# DHCPv6-PD uses the real FreeBSD Kea server on the peer half of the disposable
# epair.  This is an actual multicast Solicit/Advertise/Request/Reply exchange,
# not the in-process selftest or a synthetic lease injection.
command -v kea-dhcp6 >/dev/null
ifconfig "$epair_a" inet6 2001:db8:927::10/64 up
ifconfig "$epair_b" inet6 2001:db8:927::1/64 up
cat >"$work/kea-dhcp6.json" <<EOF
{
  "Dhcp6": {
    "interfaces-config": { "interfaces": [ "$epair_b" ] },
    "lease-database": { "type": "memfile", "persist": false, "name": "$work/kea-leases.csv" },
    "renew-timer": 900,
    "rebind-timer": 1800,
    "valid-lifetime": 3600,
    "subnet6": [
      {
        "id": 927,
        "subnet": "2001:db8:927::/64",
        "interface": "$epair_b",
        "pools": [],
        "pd-pools": [
          { "prefix": "2001:db8:928::", "prefix-len": 56, "delegated-len": 60 }
        ]
      }
    ]
  }
}
EOF
kea-dhcp6 -t "$work/kea-dhcp6.json" >"$evidence_dir/kea-dhcp6-configtest.log" 2>&1
kea-dhcp6 -d -c "$work/kea-dhcp6.json" >"$evidence_dir/kea-dhcp6.log" 2>&1 &
kea_pid=$!
for _ in $(jot 30); do
  kill -0 "$kea_pid" 2>/dev/null || break
  sockstat -46 -l | grep -E '[.:]547[[:space:]]' >"$evidence_dir/kea-dhcp6-listener.log" && break
  sleep 1
done
kill -0 "$kea_pid"
test -s "$evidence_dir/kea-dhcp6-listener.log"

"$dhcpv6_client" daemon --resource lifecycle-pd --interface "$epair_a" \
  --socket "$work/dhcpv6.sock" --lease-file "$evidence_dir/dhcpv6-lease.json" \
  --event-file "$evidence_dir/dhcpv6-events.jsonl" >"$evidence_dir/dhcpv6.stdout.log" 2>"$evidence_dir/dhcpv6.stderr.log" &
dhcpv6_pid=$!
for _ in $(jot 30); do
  [ -S "$work/dhcpv6.sock" ] && break
  kill -0 "$dhcpv6_pid" 2>/dev/null || break
  sleep 1
done
[ -S "$work/dhcpv6.sock" ]
for _ in $(jot 30); do
  kill -0 "$dhcpv6_pid" 2>/dev/null || break
  if curl --fail --silent --show-error --unix-socket "$work/dhcpv6.sock" \
    http://localhost/v1/status >"$evidence_dir/dhcpv6-status-before.json" 2>/dev/null && \
    jq -e '.phase == "Running" and .resources[0].phase == "Bound" and .resources[0].conditions[0].reason == "Bound" and (.resources[0].observed.currentPrefix | startswith("2001:db8:928:"))' "$evidence_dir/dhcpv6-status-before.json" >/dev/null; then
    break
  fi
  sleep 1
done
jq -e '.phase == "Running" and .resources[0].phase == "Bound" and (.resources[0].observed.currentPrefix | startswith("2001:db8:928:"))' "$evidence_dir/dhcpv6-status-before.json" >/dev/null
jq -e 'select(.reason == "PrefixBound")' "$evidence_dir/dhcpv6-events.jsonl" >"$evidence_dir/dhcpv6-bound-event.json"
test -s "$evidence_dir/dhcpv6-lease.json"
stop_owned_pid "$dhcpv6_pid"
dhcpv6_pid=
# The daemon's owned Unix socket can remain after an external SIGTERM.  Its
# production startup deliberately removes only that configured socket before
# binding, so the restart below is the real stale-socket recovery assertion.
# A restart must acquire again from Kea rather than merely restore its first
# lease snapshot from disk.
rm -f "$evidence_dir/dhcpv6-lease.json"
"$dhcpv6_client" daemon --resource lifecycle-pd --interface "$epair_a" \
  --socket "$work/dhcpv6.sock" --lease-file "$evidence_dir/dhcpv6-lease.json" \
  --event-file "$evidence_dir/dhcpv6-events.jsonl" >>"$evidence_dir/dhcpv6.stdout.log" 2>>"$evidence_dir/dhcpv6.stderr.log" &
dhcpv6_pid=$!
for _ in $(jot 30); do
  [ -S "$work/dhcpv6.sock" ] && break
  kill -0 "$dhcpv6_pid" 2>/dev/null || break
  sleep 1
done
[ -S "$work/dhcpv6.sock" ]
for _ in $(jot 30); do
  kill -0 "$dhcpv6_pid" 2>/dev/null || break
  if curl --fail --silent --show-error --unix-socket "$work/dhcpv6.sock" \
    http://localhost/v1/status >"$evidence_dir/dhcpv6-status-restart.json" 2>/dev/null && \
    jq -e '.phase == "Running" and .resources[0].phase == "Bound" and (.resources[0].observed.currentPrefix | startswith("2001:db8:928:"))' "$evidence_dir/dhcpv6-status-restart.json" >/dev/null; then
    break
  fi
  sleep 1
done
jq -e '.phase == "Running" and .resources[0].phase == "Bound" and (.resources[0].observed.currentPrefix | startswith("2001:db8:928:"))' "$evidence_dir/dhcpv6-status-restart.json" >/dev/null
stop_owned_pid "$dhcpv6_pid"
dhcpv6_pid=
stop_owned_pid "$kea_pid"
kea_pid=

cat >"$work/resolver.json" <<'EOF'
{"resource":"lifecycle-resolver","spec":{"listen":[{"addresses":["127.0.0.1"],"port":10553}],"sources":[{"name":"fixture-upstream","kind":"upstream","match":["."],"upstreams":["udp://127.0.0.1:5300"]}]}}
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
wait_resolver_healthy "$resolver_pid" "$work/resolver.sock" "$evidence_dir/resolver-status-before.json"
curl --fail --silent --show-error --unix-socket "$work/resolver.sock" -X POST \
  http://localhost/v1/reload >"$evidence_dir/resolver-reload.json"
jq -e '.reloaded == true and .listeners == 1' "$evidence_dir/resolver-reload.json" >/dev/null
stop_owned_pid "$resolver_pid"
resolver_pid=
# Like the DHCPv6 daemon, resolver startup owns and clears its configured
# private socket before binding.  The restart/status check below is the
# meaningful recovery oracle; immediate unlink after external SIGTERM is not.

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
wait_resolver_healthy "$resolver_pid" "$work/resolver.sock" "$evidence_dir/resolver-status-restart.json"
stop_owned_pid "$resolver_pid"
resolver_pid=

printf '%s\n' \
  'dhcpv4-bpf-lease=Bound' \
  'dnsmasq-rcd-render-start-observe-restart-stop=ok' \
  'dhcpv6-pd-kea-delegated-prefix-Bound-restart-stop=ok' \
  'dns-resolver-start-observe-reload-restart-stop=ok' \
  'owned-epair-cleanup=pending-exit-trap' >"$evidence_dir/summary.log"
printf 'freebsd-lifecycle-runtime=ok\n' >"$evidence_dir/result"
