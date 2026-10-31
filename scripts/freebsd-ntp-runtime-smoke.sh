#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# FreeBSD 14.3 only: owned ntpd processes, denied jail clock privileges, private VNET, no rc.d.
set -eu
PATH="$PATH:/usr/sbin:/sbin"
export PATH
LC_ALL=C
export LC_ALL

binary=${1:?usage: freebsd-ntp-runtime-smoke.sh /absolute/path/chain.test}
[ -x "$binary" ]
case "$binary" in /*) ;; *) echo 'absolute test binary is required' >&2; exit 64 ;; esac
case "$(uname -sr)" in 'FreeBSD 14.3-'*) ;; *) echo 'this fixture is qualified for FreeBSD 14.3 only' >&2; exit 65 ;; esac
[ "$(id -u)" -eq 0 ]
[ "$(sysctl -n security.jail.jailed)" -eq 0 ]
for tool in jail jexec jls ifconfig sysctl ntpd ntpq timeout sha256 kldstat kldload kldunload; do command -v "$tool" >/dev/null; done
work=$(mktemp -d /tmp/routerd15-ntpd-native.XXXXXX)
managed="routerd-ntpd-managed-$$"
reference="routerd-ntpd-reference-$$"
epair_a=
epair_b=
own_managed=0
own_reference=0
own_epair_module=0
epair_valid=0
managed_jid=
reference_jid=
module_initial=unknown

# Called by the EXIT trap cleanup chain.
# shellcheck disable=SC2317
jail_identity() {
  awk -v wanted_id="jid=$1" -v wanted_name="name=$2" '
    { id=0; name=0; for (i=1;i<=NF;i++) { if ($i==wanted_id) id=1; if ($i==wanted_name) name=1 }; if (id && name) found=1 }
    END { exit !found }' "$3"
}
# Called by the EXIT trap cleanup chain.
# shellcheck disable=SC2317
jail_remaining() {
  awk -v wanted_id="jid=$1" -v wanted_name="name=$2" '
    { for (i=1;i<=NF;i++) if ($i==wanted_id || $i==wanted_name) found=1 }
    END { exit !found }' "$3"
}
# Called by the EXIT trap cleanup chain.
# shellcheck disable=SC2317
remove_owned_jail() {
  if [ -z "$1" ]; then cleanup_bad=1; return; fi
  if ! timeout 5 jls -n jid name >"$work/cleanup-jails-$1-before.txt" 2>>"$work/cleanup.log"; then
    cleanup_bad=1
  elif jail_identity "$1" "$2" "$work/cleanup-jails-$1-before.txt"; then
    if ! timeout 10 jail -r "$1" >>"$work/cleanup.log" 2>&1; then cleanup_bad=1; fi
  elif jail_remaining "$1" "$2" "$work/cleanup-jails-$1-before.txt"; then
    echo "owned jail identity changed: $1 $2" >>"$work/cleanup.log"
    cleanup_bad=1
  fi
}
# Invoked indirectly by EXIT/HUP/INT/TERM traps.
# shellcheck disable=SC2317
cleanup() {
  body_result=$?
  trap - EXIT HUP INT TERM
  cleanup_bad=0
  if [ "$own_managed" -eq 1 ]; then remove_owned_jail "$managed_jid" "$managed"; fi
  if [ "$own_reference" -eq 1 ]; then remove_owned_jail "$reference_jid" "$reference"; fi
  if ! timeout 5 jls -n jid name >"$work/cleanup-jails-after.txt" 2>>"$work/cleanup.log"; then
    cleanup_bad=1
  else
    if [ "$own_managed" -eq 1 ] && jail_remaining "$managed_jid" "$managed" "$work/cleanup-jails-after.txt"; then cleanup_bad=1; fi
    if [ "$own_reference" -eq 1 ] && jail_remaining "$reference_jid" "$reference" "$work/cleanup-jails-after.txt"; then cleanup_bad=1; fi
  fi
  if [ "$epair_valid" -eq 1 ]; then
    for owned_iface in "$epair_a" "$epair_b"; do
      if ! timeout 5 ifconfig -l >"$work/cleanup-ifconfig-$owned_iface-before.txt" 2>>"$work/cleanup.log"; then
        cleanup_bad=1
      elif ! tr '[:space:]' '\n' <"$work/cleanup-ifconfig-$owned_iface-before.txt" | grep -Fxq lo0; then
        cleanup_bad=1
      elif tr '[:space:]' '\n' <"$work/cleanup-ifconfig-$owned_iface-before.txt" | grep -Fxq "$owned_iface"; then
        if ! timeout 10 ifconfig "$owned_iface" destroy >>"$work/cleanup.log" 2>&1; then cleanup_bad=1; fi
      fi
    done
    if ! timeout 5 ifconfig -l >"$work/cleanup-ifconfig-after.txt" 2>>"$work/cleanup.log"; then
      cleanup_bad=1
    elif ! tr '[:space:]' '\n' <"$work/cleanup-ifconfig-after.txt" | grep -Fxq lo0; then
      cleanup_bad=1
    else
      for owned_iface in "$epair_a" "$epair_b"; do
        if tr '[:space:]' '\n' <"$work/cleanup-ifconfig-after.txt" | grep -Fxq "$owned_iface"; then cleanup_bad=1; fi
      done
    fi
  fi
  if [ "$own_epair_module" -eq 1 ]; then
    if ! timeout 10 kldunload if_epair >>"$work/cleanup.log" 2>&1; then cleanup_bad=1; fi
  fi
  if [ "$module_initial" != unknown ]; then
    if ! timeout 5 kldstat -v >"$work/cleanup-modules-after.txt" 2>>"$work/cleanup.log"; then
      cleanup_bad=1
    elif ! grep -Eq '^Id[[:space:]]' "$work/cleanup-modules-after.txt"; then
      cleanup_bad=1
    elif grep -Eq '(^|[[:space:]])if_epair(\.ko)?([[:space:]]|$)' "$work/cleanup-modules-after.txt"; then
      if [ "$module_initial" != present ]; then cleanup_bad=1; fi
    elif [ "$module_initial" != absent ]; then
      cleanup_bad=1
    fi
  fi
  printf '%s\n' "$cleanup_bad" >"$work/cleanup.exit"
  final_result=$body_result
  if [ "$cleanup_bad" -ne 0 ]; then final_result=70; fi
  if [ ! -f "$work/body.exit" ] && [ "$final_result" -eq 0 ]; then final_result=70; fi
  printf '%s\n' "$final_result" >"$work/native.exit"
  echo "freebsd-ntpd-body-exit=$body_result cleanup-failed=$cleanup_bad"
  if [ "$final_result" -eq 0 ]; then echo 'freebsd-ntpd-result=PASS'; fi
  echo "ntpd-native-evidence=$work"
  exit "$final_result"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
echo "ntpd-native-evidence=$work"
uname -a >"$work/uname.txt"
sha256 "$binary" >"$work/binary.sha256"
sha256 "$0" >"$work/script.sha256"
printf 'FreeBSD 14.3 VNET; jail denies clock and NTP adjustment; packet/ACL only; no shared rc.d or clock changes\n' >"$work/scope.txt"
kldstat -v >"$work/modules-before.txt"
grep -Eq '^Id[[:space:]]' "$work/modules-before.txt"
if grep -Eq '(^|[[:space:]])if_epair(\.ko)?([[:space:]]|$)' "$work/modules-before.txt"; then
  module_initial=present
else
  module_initial=absent
  kldload if_epair
  own_epair_module=1
fi
epair_a=$(ifconfig epair create)
case "$epair_a" in
  epair*a) epair_number=${epair_a#epair}; epair_number=${epair_number%a} ;;
  *) echo 'epair creation returned an unexpected interface name' >&2; exit 65 ;;
esac
case "$epair_number" in ''|*[!0-9]*) echo 'epair creation returned a nonnumeric identity' >&2; exit 65 ;; esac
epair_valid=1
epair_b="${epair_a%a}b"
jail -c name="$managed" path=/ host.hostname="$managed" persist vnet vnet.interface="$epair_a"
own_managed=1
managed_jid=$(jls -n -j "$managed" jid)
managed_jid=${managed_jid#jid=}
case "$managed_jid" in ''|*[!0-9]*) exit 65 ;; esac
jail -c name="$reference" path=/ host.hostname="$reference" persist vnet vnet.interface="$epair_b"
own_reference=1
reference_jid=$(jls -n -j "$reference" jid)
reference_jid=${reference_jid#jid=}
case "$reference_jid" in ''|*[!0-9]*) exit 65 ;; esac
for owned_jail in "$managed" "$reference"; do
  [ "$(jexec "$owned_jail" sysctl -n security.jail.jailed)" -eq 1 ]
  jexec "$owned_jail" ifconfig lo0 inet 127.0.0.1/8 up
  jls -n -j "$owned_jail" jid name vnet >>"$work/jails.txt"
done
jexec "$managed" ifconfig "$epair_a" inet 198.18.97.1/24 up
jexec "$reference" ifconfig "$epair_b" inet 198.18.97.2/24 up
jexec "$reference" ifconfig "$epair_b" inet 198.18.97.10/32 alias
jexec "$reference" ifconfig "$epair_b" inet 198.18.97.20/32 alias
result=0
ROUTERD_RUN_NTPD_VNET=1 ROUTERD_NTPD_MANAGED_JAIL="$managed" ROUTERD_NTPD_REFERENCE_JAIL="$reference" ROUTERD_NTPD_WORK="$work" \
  timeout 280 "$binary" -test.run '^TestNTPNTPDNativeFreeBSDControllerLifecycle$' -test.v -test.count=1 -test.timeout=270s >"$work/native.log" 2>&1 || result=$?
if [ "$result" -eq 0 ]; then
  if ! grep -q '^--- PASS: TestNTPNTPDNativeFreeBSDControllerLifecycle' "$work/native.log" ||
     ! grep -q 'native-stage=WhenTrue-recovery' "$work/native.log" ||
     grep -q '^--- SKIP:' "$work/native.log"; then
    result=1
  fi
fi
printf '%s\n' "$result" >"$work/body.exit"
cat "$work/native.log"
exit "$result"
