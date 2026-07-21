#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause

set -eu

case "$(uname -s)" in
FreeBSD) ;;
*)
  echo "native observer smoke must run in FreeBSD" >&2
  exit 1
  ;;
esac

work=$(mktemp -d /tmp/routerd-freebsd-observer-smoke.XXXXXX)
tap=""
arp_pid=""
ra_pid=""
own_tuntap_module=0

cleanup() {
  for pid in "$ra_pid" "$arp_pid"; do
    if [ -n "$pid" ]; then
      kill -TERM "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
  done
  if [ -n "$tap" ] && ifconfig "$tap" >/dev/null 2>&1; then
    ifconfig "$tap" destroy || true
  fi
  if [ "$own_tuntap_module" -eq 1 ]; then
    kldunload if_tuntap || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT HUP INT TERM

if ! kldstat -q -m if_tuntap; then
  kldload if_tuntap
  own_tuntap_module=1
fi

arp_observer="$work/routerd-arp-observer"
ra_observer="$work/routerd-ra-observer"
injector="$work/freebsd-tap-frame-injector"
arp_socket="$work/arp.sock"
ra_socket="$work/ra.sock"
arp_events="$work/arp-events.jsonl"
ra_events="$work/ra-events.jsonl"

go build -o "$arp_observer" ./cmd/routerd-arp-observer
go build -o "$ra_observer" ./cmd/routerd-ra-observer
go build -o "$injector" ./scripts/freebsd-tap-frame-injector

tap=$(ifconfig tap create)
ifconfig "$tap" inet 192.0.2.1/24 up
"$arp_observer" daemon \
  --resource native-ci-arp --interface "$tap" --event-interface native-ci \
  --socket "$arp_socket" --event-file "$arp_events" --pool native-ci-pool \
  --prefix 192.0.2.0/24 --source-type arp-observer --observe \
  >"$work/arp.log" 2>&1 &
arp_pid=$!

"$ra_observer" daemon \
  --resource native-ci-ra --interface "$tap" \
  --socket "$ra_socket" --event-file "$ra_events" \
  >"$work/ra.log" 2>&1 &
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

# FreeBSD tap(4) defines each control-device write as one Ethernet frame
# received by the kernel. The production BPF readers, parsers, status, and
# event files remain the acceptance oracle.
"$injector" "$tap"

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
  ifconfig "$tap" >&2
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
echo "freebsd-native-observers=ok"
