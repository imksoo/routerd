#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause

# Run only the opt-in IPv6 production FIB acceptance against an isolated
# connected epair/VNET peer. The host VM's vtnet0 is intentionally untouched.
set -eu

[ "$(uname -s)" = FreeBSD ] || {
  echo "FreeBSD IPv6 FIB smoke must run in FreeBSD" >&2
  exit 1
}

jail_name="routerd-bgp-ipv6-$$"
epair_host=""
own_epair_module=0
prefix_owned="2001:db8:77::/64"
prefix_foreign="2001:db8:78::/64"
first_hop="2001:db8:1::2"
second_hop="2001:db8:1::3"

cleanup_route() {
  owned=$1
  prefix=$2
  hop=$3
  if [ "$owned" = 1 ]; then
    route -n delete -inet6 -proto1 -net "$prefix" "$hop" >/dev/null 2>&1 || true
  else
    route -n delete -inet6 -net "$prefix" "$hop" >/dev/null 2>&1 || true
  fi
}

cleanup() {
  cleanup_route 1 "$prefix_owned" "$first_hop"
  cleanup_route 1 "$prefix_owned" "$second_hop"
  cleanup_route 0 "$prefix_foreign" "$first_hop"
  if jls -j "$jail_name" >/dev/null 2>&1; then
    jail -r "$jail_name" >/dev/null 2>&1 || true
  fi
  if [ -n "$epair_host" ] && ifconfig "$epair_host" >/dev/null 2>&1; then
    ifconfig "$epair_host" destroy >/dev/null 2>&1 || true
  fi
  if [ "$own_epair_module" -eq 1 ]; then
    kldunload if_epair >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT HUP INT TERM

if ! kldstat -q -m if_epair; then
  kldload if_epair
  own_epair_module=1
fi

epair_host=$(ifconfig epair create)
epair_peer="${epair_host%a}b"
ifconfig "$epair_host" inet6 2001:db8:1::1/64 up
jail -c name="$jail_name" path=/ host.hostname="$jail_name" persist vnet \
  vnet.interface="$epair_peer"
jexec "$jail_name" ifconfig lo0 up
jexec "$jail_name" ifconfig "$epair_peer" inet6 2001:db8:1::2/64 up
jexec "$jail_name" ifconfig "$epair_peer" inet6 2001:db8:1::3/64 alias

# Verify neighbor discovery before route(8) installs production-owned routes
# through the two connected peer aliases.
ping6 -n -c 1 2001:db8:1::2 >/dev/null

ROUTERD_FREEBSD_FIB_VM=1 go test -count=1 -run '^TestFreeBSDIPv6FIBVMAcceptance$' ./pkg/controller/bgp
echo "freebsd-ipv6-fib-vnet=ok"
