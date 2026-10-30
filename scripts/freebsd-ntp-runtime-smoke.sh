#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# FreeBSD 14.3 only: owned ntpd processes, disable ntp, private VNET, no rc.d.
set -eu
PATH="$PATH:/usr/sbin:/sbin"
export PATH

binary=${1:?usage: freebsd-ntp-runtime-smoke.sh /absolute/path/chain.test}
[ -x "$binary" ]
case "$binary" in /*) ;; *) echo 'absolute test binary is required' >&2; exit 64 ;; esac
case "$(uname -sr)" in 'FreeBSD 14.3-'*) ;; *) echo 'this fixture is qualified for FreeBSD 14.3 only' >&2; exit 65 ;; esac
[ "$(id -u)" -eq 0 ]
for tool in jail jexec jls ifconfig sysctl ntpd ntpq timeout sha256 kldstat kldload kldunload; do command -v "$tool" >/dev/null; done
work=$(mktemp -d /tmp/routerd15-ntpd-native.XXXXXX)
managed="routerd-ntpd-managed-$$"
reference="routerd-ntpd-reference-$$"
epair_a=
epair_b=
own_managed=0
own_reference=0
own_epair_module=0
cleanup() {
  rc=$?
  trap - EXIT HUP INT TERM
  if [ "$own_managed" -eq 1 ]; then jail -r "$managed" >/dev/null 2>&1 || true; fi
  if [ "$own_reference" -eq 1 ]; then jail -r "$reference" >/dev/null 2>&1 || true; fi
  if [ -n "$epair_a" ] && ifconfig "$epair_a" >/dev/null 2>&1; then ifconfig "$epair_a" destroy >/dev/null 2>&1 || true; fi
  if [ -n "$epair_b" ] && ifconfig "$epair_b" >/dev/null 2>&1; then ifconfig "$epair_b" destroy >/dev/null 2>&1 || true; fi
  if [ "$own_epair_module" -eq 1 ]; then kldunload if_epair >/dev/null 2>&1 || true; fi
  echo "ntpd-native-evidence=$work"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
echo "ntpd-native-evidence=$work"
uname -a >"$work/uname.txt"
sha256 "$binary" >"$work/binary.sha256"
sha256 "$0" >"$work/script.sha256"
if ! kldstat -q -m if_epair; then
  kldload if_epair
  own_epair_module=1
fi
epair_a=$(ifconfig epair create)
epair_b="${epair_a%a}b"
jail -c name="$managed" path=/ host.hostname="$managed" persist vnet vnet.interface="$epair_a"
own_managed=1
jail -c name="$reference" path=/ host.hostname="$reference" persist vnet vnet.interface="$epair_b"
own_reference=1
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
  timeout 190 "$binary" -test.run '^TestNTPNTPDNativeFreeBSDControllerLifecycle$' -test.v -test.count=1 -test.timeout=180s >"$work/native.log" 2>&1 || result=$?
printf '%s\n' "$result" >"$work/native.exit"
cat "$work/native.log"
exit "$result"
