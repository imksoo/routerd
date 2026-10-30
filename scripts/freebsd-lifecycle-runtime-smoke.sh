#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Exercise actual BPF/PD peers, supervised helpers and the serve-owned dnsmasq
# process in an isolated native FreeBSD guest. No generated rc.d service owns
# dnsmasq. Foreground fixtures are stopped through their actual child launcher.
set -eu
ipc_curl() {
  curl --connect-timeout 1 --max-time 1 --fail --silent --show-error "$@" || {
    echo 'INCONCLUSIVE: IPC request failed or exceeded its deadline' >&2
    return 3
  }
}

dhcpv4_client=
dhcpv6_client=
dns_resolver=
routerd=
lifecycle_helper=
evidence_dir=

while [ "$#" -gt 0 ]; do
  case "$1" in
  --dhcpv4-client) dhcpv4_client=$2; shift 2 ;;
  --dhcpv6-client) dhcpv6_client=$2; shift 2 ;;
  --dns-resolver) dns_resolver=$2; shift 2 ;;
  --routerd) routerd=$2; shift 2 ;;
  --lifecycle-helper) lifecycle_helper=$2; shift 2 ;;
  --evidence-dir) evidence_dir=$2; shift 2 ;;
  *) echo "usage: $0 --dhcpv4-client PATH --dhcpv6-client PATH --dns-resolver PATH --routerd PATH --lifecycle-helper PATH --evidence-dir DIR" >&2; exit 2 ;;
  esac
done

[ -x "$dhcpv4_client" ]
[ -x "$dhcpv6_client" ]
[ -x "$dns_resolver" ]
[ -x "$routerd" ]
[ -x "$lifecycle_helper" ]
[ -n "$evidence_dir" ]

PATH=$(dirname "$dhcpv4_client"):$PATH
export PATH

mkdir -p "$evidence_dir"
(set -C; printf 'freebsd-lifecycle-runtime=not-run\n' >"$evidence_dir/result") || {
  echo 'evidence result already exists; use a fresh evidence directory' >&2
  exit 2
}
case "$(uname -s)" in
  FreeBSD) ;;
  *) echo 'INCONCLUSIVE: native FreeBSD is required' >&2; exit 3 ;;
esac
printf 'freebsd-lifecycle-runtime=in-progress\n' >"$evidence_dir/result"
work=$(mktemp -d /var/tmp/routerd-lifecycle-runtime.XXXXXX)
dnsmasq_pid=
resolver_pid=
dhcpv6_pid=
kea_pid=
routerd_pid=
foreign_dhcpv4_pid=
owned_control=
process_launches=
process_closed=
epair_a=
epair_b=
managed_dnsmasq=0
managed_dnsmasq_pid=/var/run/routerd/dnsmasq.pid
managed_dnsmasq_owner=$managed_dnsmasq_pid.routerd-owner.json
supervised_touched=0
foreign_socket_identity=
supervised_resource=lifecycle-$(basename "$work" | tr '[:upper:]' '[:lower:]')
supervised_socket=/var/run/routerd/dhcpv4-client/$supervised_resource.sock
supervised_marker_dir=/var/run/routerd/supervised-daemons

# Every signal is sent by the launcher to the direct child it actually started.
# These handles are private control sockets, not PIDs loaded from a file.
process_request() {
  process_control=$1
  process_operation=$2
  curl --connect-timeout 1 --max-time 12 --fail --silent --show-error \
    --unix-socket "$process_control" -X POST "http://child/$process_operation"
}

process_alive() {
  curl --connect-timeout 1 --max-time 1 --fail --silent --show-error \
    --unix-socket "$1" http://child/status | jq -e '.running == true and (.error // "") == ""' >/dev/null
}

start_owned_process() {
  process_label=$1
  process_stdout=$2
  process_stderr=$3
  shift 3
  owned_control=$work/child-$process_label.sock
  [ ! -e "$owned_control" ] && [ ! -e "$owned_control.intent.json" ]
  "$lifecycle_helper" --socket "$owned_control" -- "$@" >"$process_stdout" 2>"$process_stderr" &
  owned_wrapper=$!
  process_launches="$process_launches $owned_wrapper:$owned_control"
  for _ in $(jot 30); do
    if curl --connect-timeout 1 --max-time 1 --fail --silent --show-error \
      --unix-socket "$owned_control" http://child/status >"$owned_control.started.json" && \
      jq -e '.pid > 0 and (.error // "") == ""' "$owned_control.started.json" >/dev/null; then
      cp "$owned_control.intent.json" "$evidence_dir/child-$process_label.intent.json" || return 1
      cp "$owned_control.started.json" "$evidence_dir/child-$process_label.started.json" || return 1
      return 0
    fi
    sleep 1
  done
  echo "INCONCLUSIVE: fixture child failed to acknowledge startup: $process_label" >&2
  return 3
}

wrapper_for_control() {
  for process_launch in $process_launches; do
    case "$process_launch" in
      *:"$1") printf '%s\n' "${process_launch%%:*}"; return 0 ;;
    esac
  done
  return 1
}

stop_owned_process() {
  process_stop_control=$1
  [ -n "$process_stop_control" ] || return 0
  case " $process_closed " in *" $process_stop_control "*) return 0 ;; esac
  process_request "$process_stop_control" term >"$process_stop_control.stopped.json" || return 1
  jq -e '.running == false and (.error // "") == ""' "$process_stop_control.stopped.json" >/dev/null || return 1
  cp "$process_stop_control.stopped.json" "$evidence_dir/$(basename "$process_stop_control").stopped.json" || return 1
  process_request "$process_stop_control" close >/dev/null || return 1
  process_wrapper=$(wrapper_for_control "$process_stop_control") || return 1
  # wait only reaps this shell's launcher child; it never sends a signal.
  wait "$process_wrapper" || return 1
  process_closed="$process_closed $process_stop_control"
}

crash_owned_process() {
  process_crash_control=$1
  process_request "$process_crash_control" kill >"$process_crash_control.stopped.json"
  jq -e '.running == false and (.error // "") == ""' "$process_crash_control.stopped.json" >/dev/null
  cp "$process_crash_control.stopped.json" "$evidence_dir/$(basename "$process_crash_control").crashed.json"
  process_request "$process_crash_control" close >/dev/null
  process_wrapper=$(wrapper_for_control "$process_crash_control")
  wait "$process_wrapper"
  process_closed="$process_closed $process_crash_control"
}

empty_router() {
  cat >"$1" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: lifecycle-empty
spec:
  resources: []
EOF
}

marker_file() {
  [ -d "$supervised_marker_dir" ] || return 1
  for candidate in "$supervised_marker_dir"/*.json; do
    [ -f "$candidate" ] || continue
    if jq -e --arg resource "$supervised_resource" \
      '.binary == "routerd-dhcpv4-client" and .resource == $resource' "$candidate" >/dev/null; then
      printf '%s\n' "$candidate"
      return 0
    else
      marker_rc=$?
      [ "$marker_rc" -eq 1 ] || return 2
    fi
  done
  return 1
}

wait_marker() {
  output=$1
  for _ in $(jot 30); do
    if marker=$(marker_file); then
      if jq -e 'select(.pid > 0 and (.ownerToken | type == "string" and length > 0))' "$marker" >"$output"; then
        return 0
      fi
    fi
    sleep 1
  done
  return 1
}

wait_no_marker() {
  for _ in $(jot 30); do
    if marker_file >/dev/null; then
      :
    else
      marker_rc=$?
      [ "$marker_rc" -eq 1 ] || return 2
      return 0
    fi
    sleep 1
  done
  return 1
}

wait_socket() {
  socket=$1
  for _ in $(jot 30); do
    [ -S "$socket" ] && return 0
    sleep 1
  done
  return 1
}

wait_supervised_gone() {
  for _ in $(jot 30); do
    if marker_file >/dev/null; then
      :
    else
      marker_rc=$?
      [ "$marker_rc" -eq 1 ] || return 2
      [ -S "$supervised_socket" ] || return 0
    fi
    sleep 1
  done
  return 1
}

start_supervised_routerd() {
  label=$1
  start_owned_process "routerd-$label" "$evidence_dir/supervised-$label.log" "$evidence_dir/supervised-$label.stderr.log" \
    "$routerd" serve --config "$work/supervised-router.yaml" \
    --controllers daemon-supervisor \
    --state-file "$work/supervised-state.db" --ledger-file "$work/supervised-ledger.json" \
    --status-file "$evidence_dir/supervised-$label.status.json" \
    --socket "$work/supervised-$label.sock" --status-socket "$work/supervised-$label-status.sock" \
    --apply-interval 1s || return 1
  routerd_pid=$owned_control
}

wait_resolver_healthy() {
  pid=$1
  socket=$2
  status_file=$3
  status_deadline=$(( $(date +%s) + 30 ))
  while [ "$(date +%s)" -lt "$status_deadline" ]; do
    process_alive "$pid" || break
    if ipc_curl --unix-socket "$socket" \
      http://localhost/v1/status >"$status_file" 2>/dev/null && \
      jq -e '.health == "ok" and .phase == "Running"' "$status_file" >/dev/null; then
      return 0
    fi
    sleep 1
  done
  echo 'INCONCLUSIVE: resolver did not acknowledge current health before its deadline' >&2
  return 3
}

wait_linklocal_ready() {
  iface=$1
  dump=$2
  for _ in $(jot 30); do
    ifconfig "$iface" >"$dump" 2>&1 || return 1
    if awk '/inet6 fe80:/{ if ($0 !~ /tentative/) ready=1 } END { exit !ready }' "$dump"; then
      return 0
    fi
    sleep 1
  done
  return 1
}

cleanup_started=0
cleanup() {
  test_rc=$?
  [ "$cleanup_started" -eq 0 ] || return "$test_rc"
  cleanup_started=1
  trap - EXIT HUP INT TERM
  set +e
  cleanup_rc=0
  if [ "$managed_dnsmasq" -eq 1 ]; then
    retire_managed_dnsmasq || cleanup_rc=1
  fi
  for process_handle in "$resolver_pid" "$routerd_pid" "$foreign_dhcpv4_pid" "$dhcpv6_pid" "$kea_pid" "$dnsmasq_pid" "$owned_control"; do
    [ -z "$process_handle" ] || stop_owned_process "$process_handle" || cleanup_rc=1
  done
  if [ -n "$foreign_socket_identity" ] && [ -S "$supervised_socket" ]; then
    current_socket_identity=$(stat -f '%d:%i' "$supervised_socket")
    if [ "$current_socket_identity" = "$foreign_socket_identity" ]; then
      rm "$supervised_socket" || cleanup_rc=1
    else
      cleanup_rc=1
    fi
  fi
  if [ "$supervised_touched" -eq 1 ]; then
    empty_router "$work/supervised-router.yaml"
    start_supervised_routerd cleanup
    supervisor_cleanup_start=$?
    if [ "$supervisor_cleanup_start" -eq 0 ]; then
      wait_supervised_gone || cleanup_rc=1
      stop_owned_process "$routerd_pid" || cleanup_rc=1
      routerd_pid=
    else
      cleanup_rc=1
    fi
  fi
  if [ -n "$epair_a" ]; then
    if [ "$cleanup_rc" -eq 0 ] && [ -s "$work/epair-owner.json" ]; then
      "$lifecycle_helper" interface destroy "$work/epair-owner.json" >>"$evidence_dir/cleanup.log" 2>&1 || cleanup_rc=1
    else
      echo 'epair retained: owner/effect cleanup is unconfirmed' >>"$evidence_dir/cleanup.log"
      cleanup_rc=1
    fi
  fi
  printf 'test_exit=%s\ncleanup_exit=%s\n' "$test_rc" "$cleanup_rc" >>"$evidence_dir/cleanup.log"
  if [ "$test_rc" -eq 0 ] && [ "$cleanup_rc" -eq 0 ]; then
    rm -rf "$work" || cleanup_rc=1
  fi
  if [ "$test_rc" -eq 0 ] && [ "$cleanup_rc" -eq 0 ]; then
    echo 'owned-epair-and-process-cleanup=ok' >>"$evidence_dir/summary.log"
    printf 'freebsd-lifecycle-runtime=ok\n' >"$evidence_dir/result"
  else
    printf 'freebsd-lifecycle-runtime=failed-or-inconclusive\n' >"$evidence_dir/result"
    echo "native work retained at $work" >&2
    for log in "$evidence_dir"/*.log; do
      [ -f "$log" ] || continue
      echo "--- $log" >&2
      cat "$log" >&2
    done
    [ "$test_rc" -ne 0 ] || test_rc=1
  fi
  exit "$test_rc"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# The production runtime namespaces are deliberately fixed. An isolated native
# guest must have no prior helper owner; never erase/adopt a collision to test.
[ ! -e "$managed_dnsmasq_pid" ] && [ ! -L "$managed_dnsmasq_pid" ]
[ ! -e "$managed_dnsmasq_owner" ] && [ ! -L "$managed_dnsmasq_owner" ]
[ ! -e /usr/local/etc/rc.d/routerd_dnsmasq ] && [ ! -L /usr/local/etc/rc.d/routerd_dnsmasq ]
[ ! -e "$supervised_socket" ] && [ ! -L "$supervised_socket" ]
if [ -d "$supervised_marker_dir" ]; then
  for existing_marker in "$supervised_marker_dir"/*.json; do
    if [ -e "$existing_marker" ] || [ -L "$existing_marker" ]; then
      echo 'existing supervisor owner; isolated guest required' >&2
      exit 1
    fi
  done
fi
if pgrep -x dnsmasq >"$evidence_dir/dnsmasq-preflight.log"; then
  echo 'existing dnsmasq peer; isolated guest required' >&2
  exit 1
else
  preflight_rc=$?
  [ "$preflight_rc" -eq 1 ] || exit 3
fi

epair_a=$(ifconfig epair create)
case "$epair_a" in epair*a) ;; *) echo "unexpected epair name: $epair_a" >&2; exit 1 ;; esac
epair_b=${epair_a%a}b
"$lifecycle_helper" interface capture "$epair_a" "routerd-lifecycle-$(basename "$work")" "$work/epair-owner.json"
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
start_owned_process dnsmasq-peer "$evidence_dir/dnsmasq.log" "$evidence_dir/dnsmasq.stderr.log" \
  dnsmasq --keep-in-foreground --conf-file="$work/dnsmasq.conf"
dnsmasq_pid=$owned_control

sleep 1
process_alive "$dnsmasq_pid"

"$dhcpv4_client" once --resource lifecycle-dhcpv4 --interface "$epair_a" \
  --timeout 25s --socket "$work/dhcpv4.sock" --lease-file "$evidence_dir/dhcpv4-lease.json" \
  --event-file "$evidence_dir/dhcpv4-events.jsonl" >"$evidence_dir/dhcpv4-result.json"
# dnsmasq legitimately chooses any address in the configured disposable pool;
# require a real Bound lease in that pool rather than a non-protocol-specific
# first-address assumption.
jq -e '.state == "Bound" and (.currentAddress | test("^192\\.0\\.2\\.(1[0-9]|20)$"))' "$evidence_dir/dhcpv4-result.json" >/dev/null
# The current daemon publishes lease-state changes, rather than a separate
# packet-send event. Check the complete stream, not only its last record.
jq -se 'any(.[]; .reason == "Discovering" and .attributes.state == "Discovering")' "$evidence_dir/dhcpv4-events.jsonl" >/dev/null
jq -se 'any(.[]; .reason == "Bound" and .attributes.state == "Bound")' "$evidence_dir/dhcpv4-events.jsonl" >/dev/null

# #959: routerd owns this long-running DHCPv4 daemon through a runtime marker.
# A controller SIGKILL must neither duplicate the exact owned child on the same
# desired config nor strand it when the desired resource is removed. A ready
# markerless socket is foreign and must remain untouched.
supervised_touched=1
cat >"$work/supervised-router.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: lifecycle-supervised-dhcpv4
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lifecycle-wan
      spec:
        ifname: $epair_a
        managed: false
        owner: external
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Client
      metadata:
        name: $supervised_resource
      spec:
        interface: lifecycle-wan
        useRoutes: false
        useDNS: false
EOF
start_supervised_routerd initial
wait_marker "$evidence_dir/supervised-initial-marker.json"
marker=$(marker_file)
owned_pid=$(jq -r '.pid' "$marker")
owned_token=$(jq -r '.ownerToken' "$marker")
ipc_curl --unix-socket "$supervised_socket" http://localhost/v1/status >/dev/null
crash_owned_process "$routerd_pid"
routerd_pid=
ipc_curl --unix-socket "$supervised_socket" http://localhost/v1/status >/dev/null
start_supervised_routerd restart-same
wait_marker "$evidence_dir/supervised-restart-marker.json"
jq -e --argjson pid "$owned_pid" --arg token "$owned_token" \
  '.pid == $pid and .ownerToken == $token' "$evidence_dir/supervised-restart-marker.json" >/dev/null
ps -axww -o pid= -o command= >"$evidence_dir/supervised-restart-ps.log"
owned_count=$(grep -F -- "--resource $supervised_resource" "$evidence_dir/supervised-restart-ps.log" | grep -F -c -- "--supervisor-owner $owned_token" || true)
[ "$owned_count" -eq 1 ]
crash_owned_process "$routerd_pid"
routerd_pid=
cat >"$work/supervised-router.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: lifecycle-supervised-dhcpv4-deleted
spec:
  resources: []
EOF
start_supervised_routerd delete
wait_supervised_gone
wait_no_marker
echo 'supervised-dhcpv4-crash-restart-delete=ok' >>"$evidence_dir/summary.log"
stop_owned_process "$routerd_pid"
routerd_pid=

start_owned_process dhcpv4-foreign "$evidence_dir/supervised-foreign.log" "$evidence_dir/supervised-foreign.stderr.log" \
  "$dhcpv4_client" daemon --resource "$supervised_resource" --interface "$epair_a" \
  --socket "$supervised_socket" --lease-file "$work/foreign-dhcpv4-lease.json" \
  --event-file "$work/foreign-dhcpv4-events.jsonl"
foreign_dhcpv4_pid=$owned_control
wait_socket "$supervised_socket"
foreign_socket_identity=$(stat -f '%d:%i' "$supervised_socket")
process_alive "$foreign_dhcpv4_pid"
cat >"$work/supervised-router.yaml" <<EOF
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: lifecycle-supervised-dhcpv4-foreign
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lifecycle-wan
      spec:
        ifname: $epair_a
        managed: false
        owner: external
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Client
      metadata:
        name: $supervised_resource
      spec:
        interface: lifecycle-wan
        useRoutes: false
        useDNS: false
EOF
start_supervised_routerd foreign
sleep 3
process_alive "$foreign_dhcpv4_pid"
if marker_file >/dev/null; then
  exit 1
else
  marker_rc=$?
  [ "$marker_rc" -eq 1 ] || exit 3
fi
echo 'supervised-dhcpv4-markerless-foreign-preservation=ok' >>"$evidence_dir/summary.log"
stop_owned_process "$routerd_pid"
routerd_pid=
stop_owned_process "$foreign_dhcpv4_pid"
foreign_dhcpv4_pid=
if [ -S "$supervised_socket" ]; then
  [ "$(stat -f '%d:%i' "$supervised_socket")" = "$foreign_socket_identity" ]
  rm "$supervised_socket"
fi
foreign_socket_identity=

# The sole native dnsmasq owner is the actual dhcpv6-server runtime actor.
# Its process receipt stays at the production namespace; all configuration,
# lease, state and API files used by this isolated guest are private.
stop_owned_process "$dnsmasq_pid"
dnsmasq_pid=
managed_config=$work/managed-dnsmasq.conf
managed_router=$work/managed-router.yaml
managed_socket=$work/managed-router.sock

start_managed_routerd() {
  managed_label=$1
  start_owned_process "managed-$managed_label" "$evidence_dir/dnsmasq-$managed_label.log" "$evidence_dir/dnsmasq-$managed_label.stderr.log" \
    "$routerd" serve --config "$managed_router" --controllers link,dhcpv6-server \
    --dnsmasq-file "$managed_config" --state-file "$work/managed-state.db" \
    --ledger-file "$work/managed-ledger.json" --status-file "$evidence_dir/managed-status.json" \
    --socket "$managed_socket" --status-socket "$work/managed-status.sock" --apply-interval 0 || return 1
  routerd_pid=$owned_control
  wait_socket "$managed_socket"
}

apply_managed_config() {
  jq -n --rawfile candidate "$1" \
    '{apiVersion:"control.routerd.net/v1alpha1",kind:"ApplyRequest",candidateYaml:$candidate,replace:true}' >"$work/managed-apply.json"
  curl --connect-timeout 1 --max-time 60 --fail --silent --show-error \
    --unix-socket "$managed_socket" -H 'Content-Type: application/json' \
    --data-binary "@$work/managed-apply.json" \
    http://localhost/api/control.routerd.net/v1alpha1/apply >"$evidence_dir/managed-apply-result.json"
}

wait_managed_owner() {
  managed_output=$1
  managed_previous=${2:-0}
  for _ in $(jot 30); do
    if [ -f "$managed_dnsmasq_owner" ] && jq -e --arg config "$managed_config" --argjson previous "$managed_previous" \
      'select(.state == "running" and .spec.Config == $config and .process.PID > 0 and .process.PID != $previous and (.process.Birth | length > 0) and .scope == .process.Scope)' \
      "$managed_dnsmasq_owner" >"$managed_output"; then
      return 0
    fi
    process_alive "$routerd_pid" || return 1
    sleep 1
  done
  return 1
}

retire_managed_dnsmasq() {
  empty_router "$work/managed-empty.yaml" || return 1
  if [ -n "$routerd_pid" ] && process_alive "$routerd_pid"; then
    apply_managed_config "$work/managed-empty.yaml" || return 1
  else
    [ -z "$routerd_pid" ] || stop_owned_process "$routerd_pid" || return 1
    cp "$work/managed-empty.yaml" "$managed_router" || return 1
    start_managed_routerd recovery || return 1
  fi
  managed_retired=0
  for _ in $(jot 30); do
    if [ ! -e "$managed_dnsmasq_owner" ] && [ ! -e "$managed_dnsmasq_pid" ]; then
      managed_retired=1
      break
    fi
    sleep 1
  done
  [ "$managed_retired" -eq 1 ] || return 1
  # The guest was checked to contain no pre-existing dnsmasq. A removed PID
  # receipt alone cannot acknowledge helper descendants or their DHCP sockets.
  if pgrep -x dnsmasq >"$evidence_dir/dnsmasq-after-withdrawal.log"; then
    ps -axww -o pid= -o ppid= -o state= -o command= >"$evidence_dir/dnsmasq-withdrawal-processes.log"
    sockstat -46 -l >"$evidence_dir/dnsmasq-withdrawal-sockets.log"
    echo 'managed dnsmasq processes remain after withdrawal' >&2
    return 1
  else
    remaining_rc=$?
    [ "$remaining_rc" -eq 1 ] || return 1
  fi
  stop_owned_process "$routerd_pid" || return 1
  routerd_pid=
  managed_dnsmasq=0
}

cat >"$managed_router" <<EOF
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
        managed: false
        owner: external
    - apiVersion: net.routerd.net/v1alpha1
      kind: DHCPv4Server
      metadata:
        name: lan-dhcp
      spec:
        interface: lan
        routerSource: static
        router: 192.0.2.1
        dnsSource: static
        dnsServers: [192.0.2.1]
        leaseFile: $work/managed-dnsmasq.leases
        addressPool:
          start: 192.0.2.10
          end: 192.0.2.20
          leaseTime: 1h
EOF
cp "$managed_router" "$work/managed-original.yaml"
managed_dnsmasq=1
start_managed_routerd initial
wait_managed_owner "$evidence_dir/dnsmasq-initial-owner.json"
managed_first_pid=$(jq -r '.process.PID' "$evidence_dir/dnsmasq-initial-owner.json")
"$dhcpv4_client" once --resource lifecycle-native-dnsmasq --interface "$epair_a" \
  --timeout 25s --socket "$work/dnsmasq-peer-client.sock" --lease-file "$evidence_dir/dnsmasq-native-lease.json" \
  --event-file "$evidence_dir/dnsmasq-native-events.jsonl" >"$evidence_dir/dnsmasq-native-result.json"
jq -e '.state == "Bound" and (.currentAddress | test("^192\\.0\\.2\\.(1[0-9]|20)$"))' "$evidence_dir/dnsmasq-native-result.json" >/dev/null
sed 's/leaseTime: 1h/leaseTime: 2h/' "$work/managed-original.yaml" >"$work/managed-renewed.yaml"
apply_managed_config "$work/managed-renewed.yaml"
wait_managed_owner "$evidence_dir/dnsmasq-renewed-owner.json" "$managed_first_pid"
managed_next_pid=$(jq -r '.process.PID' "$evidence_dir/dnsmasq-renewed-owner.json")
[ "$managed_first_pid" -ne "$managed_next_pid" ]
apply_managed_config "$work/managed-renewed.yaml"
wait_managed_owner "$evidence_dir/dnsmasq-unchanged-owner.json"
[ "$(jq -r '.process.PID' "$evidence_dir/dnsmasq-unchanged-owner.json")" -eq "$managed_next_pid" ]
retire_managed_dnsmasq
echo 'dnsmasq-serve-native-start-peer-config-restart-idempotence-withdraw=ok' >>"$evidence_dir/summary.log"

# A markerless private configuration/hosts pair must remain byte-for-byte
# unchanged. No obsolete service export, rc.d creator or generic PID cleanup.
mkdir "$work/foreign"
foreign_config=$work/foreign/dnsmasq.conf
foreign_hosts=$work/foreign/dnsmasq-hosts.hosts
printf '%s\n' '# fixture foreign dnsmasq config' 'port=53' >"$foreign_config"
printf '%s\n' '# fixture foreign dnsmasq hosts sidecar' '192.0.2.55 foreign.example' >"$foreign_hosts"
cp "$foreign_config" "$evidence_dir/dnsmasq-foreign-config.before"
cp "$foreign_hosts" "$evidence_dir/dnsmasq-foreign-hosts.before"
set +e
/usr/bin/timeout -k 2 45 "$routerd" serve --once --controllers link,dhcpv6-server \
  --config "$work/managed-original.yaml" --dnsmasq-file "$foreign_config" \
  --state-file "$work/dnsmasq-foreign-state.db" --ledger-file "$work/dnsmasq-foreign-ledger.json" \
  --status-file "$evidence_dir/dnsmasq-foreign-serve.status.json" \
  --socket "$work/dnsmasq-foreign.sock" --status-socket "$work/dnsmasq-foreign-status.sock" \
  >"$evidence_dir/dnsmasq-foreign-serve.log" 2>&1
foreign_serve_rc=$?
set -e
case "$foreign_serve_rc" in
  0) echo 'foreign dnsmasq artifact accepted' >&2; exit 1 ;;
  124|137|143) echo 'INCONCLUSIVE: foreign-artifact probe did not acknowledge completion' >&2; exit 3 ;;
esac
cmp "$evidence_dir/dnsmasq-foreign-config.before" "$foreign_config"
cmp "$evidence_dir/dnsmasq-foreign-hosts.before" "$foreign_hosts"
[ ! -e "$managed_dnsmasq_pid" ] && [ ! -e "$managed_dnsmasq_owner" ]
sqlite3 "$work/dnsmasq-foreign-state.db" \
  "SELECT status FROM objects WHERE api_version='net.routerd.net/v1alpha1' AND kind='DHCPv4Server' AND name='lan-dhcp';" \
  >"$evidence_dir/dnsmasq-foreign-status.json"
jq -e '.phase == "Error" and .reason == "ForeignDnsmasqArtifact"' "$evidence_dir/dnsmasq-foreign-status.json" >/dev/null
echo 'dnsmasq-foreign-live-chain-preservation=ok' >>"$evidence_dir/summary.log"

# DHCPv6-PD uses the real FreeBSD Kea server on the peer half of the disposable
# epair.  This is an actual multicast Solicit/Advertise/Request/Reply exchange,
# not the in-process selftest or a synthetic lease injection.
command -v kea-dhcp6 >/dev/null
ifconfig "$epair_a" inet6 2001:db8:927::10/64 up
ifconfig "$epair_b" inet6 2001:db8:927::1/64 up
# Kea replies to the client link-local address.  FreeBSD creates that address
# asynchronously after interface configuration, so make DAD completion a
# protocol precondition rather than depending on a timing sleep.
wait_linklocal_ready "$epair_a" "$evidence_dir/dhcpv6-client-linklocal.log" || {
  ifconfig "$epair_a" >"$evidence_dir/dhcpv6-client-linklocal.failure.log" 2>&1 || true
  ifconfig "$epair_b" >"$evidence_dir/dhcpv6-server-linklocal.failure.log" 2>&1 || true
  exit 1
}
wait_linklocal_ready "$epair_b" "$evidence_dir/dhcpv6-server-linklocal.log" || {
  ifconfig "$epair_a" >"$evidence_dir/dhcpv6-client-linklocal.failure.log" 2>&1 || true
  ifconfig "$epair_b" >"$evidence_dir/dhcpv6-server-linklocal.failure.log" 2>&1 || true
  exit 1
}
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
start_owned_process kea "$evidence_dir/kea-dhcp6.log" "$evidence_dir/kea-dhcp6.stderr.log" \
  kea-dhcp6 -d -c "$work/kea-dhcp6.json"
kea_pid=$owned_control
for _ in $(jot 30); do
  process_alive "$kea_pid" 2>/dev/null || break
  sockstat -46 -l | grep -E '[.:]547[[:space:]]' >"$evidence_dir/kea-dhcp6-listener.log" && break
  sleep 1
done
process_alive "$kea_pid"
test -s "$evidence_dir/kea-dhcp6-listener.log"

start_owned_process dhcpv6-initial "$evidence_dir/dhcpv6-initial.stdout.log" "$evidence_dir/dhcpv6-initial.stderr.log" \
  "$dhcpv6_client" daemon --resource lifecycle-pd --interface "$epair_a" \
  --socket "$work/dhcpv6.sock" --lease-file "$evidence_dir/dhcpv6-lease.json" \
  --event-file "$evidence_dir/dhcpv6-events.jsonl"
dhcpv6_pid=$owned_control
for _ in $(jot 30); do
  [ -S "$work/dhcpv6.sock" ] && break
  process_alive "$dhcpv6_pid" 2>/dev/null || break
  sleep 1
done
[ -S "$work/dhcpv6.sock" ]
status_deadline=$(( $(date +%s) + 30 ))
while [ "$(date +%s)" -lt "$status_deadline" ]; do
  process_alive "$dhcpv6_pid" 2>/dev/null || break
  if ipc_curl --unix-socket "$work/dhcpv6.sock" \
    http://localhost/v1/status >"$evidence_dir/dhcpv6-status-before.json" 2>/dev/null && \
    jq -e '.phase == "Running" and .resources[0].phase == "Bound" and .resources[0].conditions[0].reason == "bound" and (.resources[0].observed.currentPrefix | startswith("2001:db8:928:"))' "$evidence_dir/dhcpv6-status-before.json" >/dev/null; then
    break
  fi
  sleep 1
done
jq -e 'type == "object"' "$evidence_dir/dhcpv6-status-before.json" >/dev/null 2>&1 || { echo 'INCONCLUSIVE: IPC status unavailable' >&2; exit 3; }
jq -e '.phase == "Running" and .resources[0].phase == "Bound" and (.resources[0].observed.currentPrefix | startswith("2001:db8:928:"))' "$evidence_dir/dhcpv6-status-before.json" >/dev/null
jq -se '[.[] | select(.reason == "PrefixBound")] | first // empty' "$evidence_dir/dhcpv6-events.jsonl" >"$evidence_dir/dhcpv6-bound-event.json"
test -s "$evidence_dir/dhcpv6-lease.json"
stop_owned_process "$dhcpv6_pid"
dhcpv6_pid=
# The daemon's owned Unix socket can remain after an external SIGTERM.  Its
# production startup deliberately removes only that configured socket before
# binding, so the restart below is the real stale-socket recovery assertion.
# A restart must acquire again from Kea rather than merely restore its first
# lease snapshot from disk.
rm -f "$evidence_dir/dhcpv6-lease.json"
start_owned_process dhcpv6-restart "$evidence_dir/dhcpv6-restart.stdout.log" "$evidence_dir/dhcpv6-restart.stderr.log" \
  "$dhcpv6_client" daemon --resource lifecycle-pd --interface "$epair_a" \
  --socket "$work/dhcpv6.sock" --lease-file "$evidence_dir/dhcpv6-lease.json" \
  --event-file "$evidence_dir/dhcpv6-events.jsonl"
dhcpv6_pid=$owned_control
for _ in $(jot 30); do
  [ -S "$work/dhcpv6.sock" ] && break
  process_alive "$dhcpv6_pid" 2>/dev/null || break
  sleep 1
done
[ -S "$work/dhcpv6.sock" ]
status_deadline=$(( $(date +%s) + 30 ))
while [ "$(date +%s)" -lt "$status_deadline" ]; do
  process_alive "$dhcpv6_pid" 2>/dev/null || break
  if ipc_curl --unix-socket "$work/dhcpv6.sock" \
    http://localhost/v1/status >"$evidence_dir/dhcpv6-status-restart.json" 2>/dev/null && \
    jq -e '.phase == "Running" and .resources[0].phase == "Bound" and (.resources[0].observed.currentPrefix | startswith("2001:db8:928:"))' "$evidence_dir/dhcpv6-status-restart.json" >/dev/null; then
    break
  fi
  sleep 1
done
jq -e 'type == "object"' "$evidence_dir/dhcpv6-status-restart.json" >/dev/null 2>&1 || { echo 'INCONCLUSIVE: IPC status unavailable' >&2; exit 3; }
jq -e '.phase == "Running" and .resources[0].phase == "Bound" and (.resources[0].observed.currentPrefix | startswith("2001:db8:928:"))' "$evidence_dir/dhcpv6-status-restart.json" >/dev/null
stop_owned_process "$dhcpv6_pid"
dhcpv6_pid=
stop_owned_process "$kea_pid"
kea_pid=

cat >"$work/resolver.json" <<'EOF'
{"resource":"lifecycle-resolver","spec":{"listen":[{"addresses":["127.0.0.1"],"port":10553}],"sources":[{"name":"fixture-upstream","kind":"upstream","match":["."],"upstreams":["udp://127.0.0.1:5300"]}]}}
EOF
start_owned_process resolver-initial "$evidence_dir/resolver-initial.stdout.log" "$evidence_dir/resolver-initial.stderr.log" \
  "$dns_resolver" daemon --resource lifecycle-resolver --config-file "$work/resolver.json" \
  --socket "$work/resolver.sock" --state-file "$evidence_dir/resolver-state.json" \
  --event-file "$evidence_dir/resolver-events.jsonl"
resolver_pid=$owned_control
for _ in $(jot 30); do
  [ -S "$work/resolver.sock" ] && break
  process_alive "$resolver_pid" 2>/dev/null || break
  sleep 1
done
[ -S "$work/resolver.sock" ]
wait_resolver_healthy "$resolver_pid" "$work/resolver.sock" "$evidence_dir/resolver-status-before.json"
ipc_curl --unix-socket "$work/resolver.sock" -X POST \
  http://localhost/v1/reload >"$evidence_dir/resolver-reload.json"
jq -e '.reloaded == true and .listeners == 1' "$evidence_dir/resolver-reload.json" >/dev/null
stop_owned_process "$resolver_pid"
resolver_pid=
# Like the DHCPv6 daemon, resolver startup owns and clears its configured
# private socket before binding.  The restart/status check below is the
# meaningful recovery oracle; immediate unlink after external SIGTERM is not.

start_owned_process resolver-restart "$evidence_dir/resolver-restart.stdout.log" "$evidence_dir/resolver-restart.stderr.log" \
  "$dns_resolver" daemon --resource lifecycle-resolver --config-file "$work/resolver.json" \
  --socket "$work/resolver.sock" --state-file "$evidence_dir/resolver-state.json" \
  --event-file "$evidence_dir/resolver-events.jsonl"
resolver_pid=$owned_control
for _ in $(jot 30); do
  [ -S "$work/resolver.sock" ] && break
  process_alive "$resolver_pid" 2>/dev/null || break
  sleep 1
done
[ -S "$work/resolver.sock" ]
wait_resolver_healthy "$resolver_pid" "$work/resolver.sock" "$evidence_dir/resolver-status-restart.json"
stop_owned_process "$resolver_pid"
resolver_pid=

printf '%s\n' \
  'dhcpv4-bpf-lease=Bound' \
  'dhcpv6-pd-kea-delegated-prefix-Bound-restart-stop=ok' \
  'dns-resolver-start-observe-reload-restart-stop=ok' \
  'runtime-assertions=complete-cleanup-required' >>"$evidence_dir/summary.log"
