#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause

# This is a real-kernel contract test for routerd-vrrp-vmac.  It is opt-in:
# it creates its complete topology in a private mount and network namespace
# and refuses to run unless ROUTERD_RUN_NETNS=1 is supplied explicitly.
set -euo pipefail

if [[ "${ROUTERD_RUN_NETNS:-}" != "1" ]]; then
  printf '%s\n' 'set ROUTERD_RUN_NETNS=1 to run this isolated netns test' >&2
  exit 2
fi
if [[ ${EUID} -ne 0 ]]; then
  printf '%s\n' "run explicitly with sudo: sudo ROUTERD_RUN_NETNS=1 $0" >&2
  exit 2
fi

helper="${ROUTERD_VRRP_VMAC_BIN:-$(cd "$(dirname "$0")/../.." && pwd)/bin/linux/routerd-vrrp-vmac}"
for command in unshare mount ip grep sha256sum; do
  command -v "$command" >/dev/null 2>&1 || {
    printf 'missing required command: %s\n' "$command" >&2
    exit 2
  }
done
[[ -x "$helper" ]] || {
  printf 'missing executable helper: %s\n' "$helper" >&2
  exit 2
}

exec unshare --mount --net --fork --kill-child -- bash -x -s -- "$helper" <<'INNER'
set -euo pipefail

helper="$1"
shared_ll='fe80::5eff:fe00:112'
wan_ll='fe80::5eff:fe00:113'
lan_global='2001:db8:1200::2/64'
wan_global='2001:db8:1200:1::23/128'

wait_global_usable() {
  local ifname="$1"
  local address="$2"
  local deadline=$((SECONDS + 5))
  local addresses
  while (( SECONDS < deadline )); do
    addresses=$(ip -o -6 addr show dev "$ifname" scope global || true)
    if grep -Fq "${address%/*}" <<<"$addresses" &&
      ! grep -Eq 'tentative|dadfailed' <<<"$addresses"; then
      return 0
    fi
    sleep 0.1
  done
  printf 'timed out waiting for usable delegated global IPv6 address on %s\n' "$ifname" >&2
  ip -o link show dev "$ifname" >&2 || true
  ip -o -6 addr show dev "$ifname" >&2 || true
  return 1
}

mount --make-rprivate /
mount -t tmpfs tmpfs /run
if [[ -d /etc/conntrackd ]]; then
  mount -t tmpfs tmpfs /etc/conntrackd
fi
ip link add lan-parent type veth peer name lan-peer
ip link add wan-parent type veth peer name wan-peer
ip link set lan-parent addrgenmode none
ip -6 addr add fe80::1/64 dev lan-parent nodad
ip link set lan-parent up
ip link set lan-peer up
ip link set wan-parent up
ip link set wan-peer up

activate() {
  "$helper" activate \
    --vmac "lan-parent,lan-vrrp,02:00:5e:00:01:12,${shared_ll},true" \
    --vmac "wan-parent,wan-vmac,02:00:5e:00:01:13,${wan_ll},false"
}
deactivate() {
  "$helper" deactivate \
    --vmac "lan-parent,lan-vrrp,02:00:5e:00:01:12,${shared_ll},true" \
    --vmac "wan-parent,wan-vmac,02:00:5e:00:01:13,${wan_ll},false"
}

# Cold BACKUP creates both child links before any address is staged.
deactivate
echo '== assert cold backup =='
for ifname in lan-vrrp wan-vmac; do
  ip -o link show dev "$ifname" | grep -q 'state DOWN'
done
for ifname in lan-vrrp wan-vmac; do
  ! ip -o -6 addr show dev "$ifname" scope link | grep -q .
done

# PD-derived addresses are staged while both VMACs remain down.
ip -6 addr add "$lan_global" dev lan-vrrp
ip -6 addr add "$wan_global" dev wan-vmac

assert_backup() {
  echo '== assert backup staged globals =='
  ip -6 addr show dev lan-vrrp scope global | grep -Fq "${lan_global%/*}"
  ip -6 addr show dev wan-vmac scope global | grep -Fq "${wan_global%/*}"
  for ifname in lan-vrrp wan-vmac; do
    ! ip -o -6 addr show dev "$ifname" scope link | grep -q .
  done
  for ifname in lan-vrrp wan-vmac; do
    ip -o link show dev "$ifname" | grep -q 'state DOWN'
  done
}

assert_master() {
  echo '== assert master retained globals =='
  wait_global_usable lan-vrrp "$lan_global"
  wait_global_usable wan-vmac "$wan_global"
  for ifname in lan-vrrp wan-vmac; do
    ip -o link show dev "$ifname" | grep -q 'state UP'
  done
  [[ $(ip -o -6 addr show dev lan-vrrp scope link | wc -l) -eq 1 ]]
  ip -o -6 addr show dev lan-vrrp scope link | grep -Fq "$shared_ll"
  [[ $(ip -o -6 addr show dev wan-vmac scope link | wc -l) -eq 1 ]]
  ip -o -6 addr show dev wan-vmac scope link | grep -Fq "$wan_ll"
}

assert_backup

activate
assert_master
deactivate
assert_backup
activate
assert_master

# Graceful VRRP keeps the client VIP outside keepalived. The controller adds
# it only after readiness; the helper must remove it before staging BACKUP and
# must not add it on the following election.
resource='lan-gw-v4'
vip='172.18.0.1/32'
ip -4 addr add 172.18.0.2/16 dev lan-parent
ip -4 addr add "$vip" dev lan-parent
"$helper" deactivate \
  --resource "$resource" --deferred-address "$vip" --deferred-interface lan-parent \
  --vmac "lan-parent,lan-vrrp,02:00:5e:00:01:12,${shared_ll},true" \
  --vmac "wan-parent,wan-vmac,02:00:5e:00:01:13,${wan_ll},false"
! ip -4 -o addr show dev lan-parent | grep -Fq "${vip%/*}"
state_hash="$(printf '%s' "$resource" | sha256sum | cut -c1-16)"
grep -Fxq backup "/run/routerd/vrrp-election-${state_hash}.role"
assert_backup

"$helper" activate \
  --resource "$resource" --deferred-address "$vip" --deferred-interface lan-parent \
  --vmac "lan-parent,lan-vrrp,02:00:5e:00:01:12,${shared_ll},true" \
  --vmac "wan-parent,wan-vmac,02:00:5e:00:01:13,${wan_ll},false"
! ip -4 -o addr show dev lan-parent | grep -Fq "${vip%/*}"
grep -Fxq master "/run/routerd/vrrp-election-${state_hash}.role"
assert_master
INNER
