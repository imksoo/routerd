#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause

set -eu

[ "$(uname -s)" = FreeBSD ] || {
  echo "native observer smoke must run in FreeBSD" >&2
  exit 1
}

work=$(mktemp -d /tmp/routerd-freebsd-observer-smoke.XXXXXX)
jail_name="routerd-observer-$$"
epair_host=""
arp_pid=""
ra_pid=""
rtadvd_pid=""
tcpdump_pid=""
own_epair_module=0
restart_devd=0

cleanup() {
  for pid in "$rtadvd_pid" "$tcpdump_pid" "$ra_pid" "$arp_pid"; do
    if [ -n "$pid" ]; then
      kill -TERM "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
  done
  if jls -j "$jail_name" >/dev/null 2>&1; then
    jail -r "$jail_name" || true
  fi
  if [ -n "$epair_host" ] && ifconfig "$epair_host" >/dev/null 2>&1; then
    ifconfig "$epair_host" destroy || true
  fi
  if [ "$own_epair_module" -eq 1 ]; then
    kldunload if_epair || true
  fi
  if [ "$restart_devd" -eq 1 ]; then
    service devd start >/dev/null 2>&1 || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT HUP INT TERM

if ! kldstat -q -m if_epair; then
  kldload if_epair
  own_epair_module=1
fi
if service devd onestatus >/dev/null 2>&1; then
  service devd stop >/dev/null
  restart_devd=1
fi

arp_observer="$work/routerd-arp-observer"
ra_observer="$work/routerd-ra-observer"
arp_socket="$work/arp.sock"
ra_socket="$work/ra.sock"
arp_events="$work/arp-events.jsonl"
ra_events="$work/ra-events.jsonl"

go build -o "$arp_observer" ./cmd/routerd-arp-observer
go build -o "$ra_observer" ./cmd/routerd-ra-observer

epair_host=$(ifconfig epair create)
epair_peer="${epair_host%a}b"
ifconfig "$epair_host" inet 192.0.2.1/24 up
ifconfig "$epair_host" inet6 2001:db8:846::1/64
jail -c name="$jail_name" path=/ host.hostname="$jail_name" \
  persist vnet vnet.interface="$epair_peer" allow.raw_sockets
jexec "$jail_name" ifconfig lo0 up
jexec "$jail_name" ifconfig "$epair_peer" inet 192.0.2.2/24 up
jexec "$jail_name" ifconfig "$epair_peer" inet6 2001:db8:846::2/64 up
jexec "$jail_name" sysctl net.inet6.ip6.forwarding=1 >/dev/null

# Run each production observer in the sender's VNET and bind it to the exact
# transmitting epair endpoint where FreeBSD invokes BPF_MTAP.
jexec "$jail_name" "$arp_observer" daemon \
  --resource native-ci-arp --interface "$epair_peer" --event-interface native-ci \
  --socket "$arp_socket" --event-file "$arp_events" --pool native-ci-pool \
  --prefix 192.0.2.0/24 --source-type arp-observer --observe \
  --self-mac 02:00:00:00:00:99 >"$work/arp.log" 2>&1 &
arp_pid=$!

jexec "$jail_name" "$ra_observer" daemon \
  --resource native-ci-ra --interface "$epair_peer" \
  --socket "$ra_socket" --event-file "$ra_events" \
  --self-mac 02:00:00:00:00:99 >"$work/ra.log" 2>&1 &
ra_pid=$!

ready=0
for _ in $(jot 20); do
  if [ -S "$arp_socket" ] && [ -S "$ra_socket" ] && \
     kill -0 "$arp_pid" 2>/dev/null && kill -0 "$ra_pid" 2>/dev/null; then
    curl --fail --silent --unix-socket "$arp_socket" \
      http://localhost/v1/status >"$work/arp-status.json"
    curl --fail --silent --unix-socket "$ra_socket" \
      http://localhost/v1/status >"$work/ra-status.json"
    if jq -e '.health == "ok"' "$work/arp-status.json" >/dev/null && \
       jq -e '.health == "ok"' "$work/ra-status.json" >/dev/null; then
      ready=1
      break
    fi
  fi
  sleep 1
done
[ "$ready" -eq 1 ] || {
  cat "$work/arp.log" >&2
  cat "$work/ra.log" >&2
  exit 1
}

# One independent base-system capture distinguishes fixture delivery from the
# production BPF reader without changing the production acceptance oracle.
jexec "$jail_name" tcpdump -ln -e -i "$epair_peer" -c 20 \
  >"$work/tcpdump.log" 2>&1 &
tcpdump_pid=$!
sleep 1

# Generate both protocols from the same kernel interface the observers capture.
for _ in $(jot 4); do
  jexec "$jail_name" arp -d 192.0.2.1 >/dev/null 2>&1 || true
  jexec "$jail_name" ping -n -c 1 192.0.2.1 >/dev/null
done

rtadvd_conf="$work/rtadvd.conf"
{
  printf '%s:\\\n' "$epair_peer"
  printf '\t:addr="2001:db8:846::":prefixlen#64:rltime#180:maxinterval#4:mininterval#3:\n'
} >"$rtadvd_conf"
jexec "$jail_name" rtadvd -d -f -s -c "$rtadvd_conf" \
  -p "$work/rtadvd.pid" "$epair_peer" \
  >"$work/rtadvd.log" 2>&1 &
rtadvd_pid=$!

observed=0
for _ in $(jot 20); do
  curl --fail --silent --unix-socket "$arp_socket" \
    http://localhost/v1/status >"$work/arp-status.json"
  curl --fail --silent --unix-socket "$ra_socket" \
    http://localhost/v1/status >"$work/ra-status.json"
  if jq -e '.observed.packetsSeen | tonumber > 0' "$work/arp-status.json" >/dev/null && \
     jq -e '.observed.packetsSeen | tonumber > 0' "$work/ra-status.json" >/dev/null && \
     grep -q 'routerd.mobility.arp.observed' "$arp_events" 2>/dev/null && \
     grep -q '192.0.2.2' "$arp_events" 2>/dev/null && \
     grep -q 'routerd.ipv6.ra.rogue_detected' "$ra_events" 2>/dev/null; then
    observed=1
    break
  fi
  sleep 1
done

if [ "$observed" -ne 1 ]; then
  cat "$work/arp-status.json" >&2
  cat "$work/ra-status.json" >&2
  cat "$work/arp.log" >&2
  cat "$work/ra.log" >&2
  cat "$work/rtadvd.log" >&2
  cat "$work/tcpdump.log" >&2
  jexec "$jail_name" ifconfig "$epair_peer" >&2
  procstat -f "$arp_pid" >&2 || true
  procstat -f "$ra_pid" >&2 || true
  netstat -B >&2 || true
  [ ! -f "$arp_events" ] || cat "$arp_events" >&2
  [ ! -f "$ra_events" ] || cat "$ra_events" >&2
  exit 1
fi

jq -e '.health == "ok" and (.observed.packetsSeen | tonumber > 0)' \
  "$work/arp-status.json" >/dev/null
jq -e '.health == "ok" and (.observed.packetsSeen | tonumber > 0)' \
  "$work/ra-status.json" >/dev/null
cat "$arp_events"
cat "$ra_events"
echo "freebsd-native-observers=ok"
