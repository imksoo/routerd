#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# Destructive opt-in gate for a fresh, disposable Debian/Ubuntu systemd VM.
# The caller discards the VM after testing; retained config/state are assertions.
set -eu

if [ "$#" -ne 3 ] || [ "$1" != --isolated-vm ]; then
    echo "usage: $0 --isolated-vm PRIOR.deb CURRENT.deb" >&2
    exit 2
fi
[ "$(id -u)" -eq 0 ]
[ -d /run/systemd/system ]
prior=$(realpath "$2")
current=$(realpath "$3")
prior_version=$(dpkg-deb -f "$prior" Version)
current_version=$(dpkg-deb -f "$current" Version)
[ "$(dpkg-deb -f "$prior" Package)" = routerd ]
[ "$(dpkg-deb -f "$current" Package)" = routerd ]
dpkg --compare-versions "$current_version" gt "$prior_version"
if dpkg-query -W -f='${Status}' routerd 2>/dev/null | grep -q ' installed$'; then
    echo 'requires a clean OS without routerd' >&2
    exit 1
fi
for path in /etc/routerd/router.yaml /var/lib/routerd/routerd.db /usr/sbin/routerd; do
    [ ! -e "$path" ] || { echo "requires absent $path" >&2; exit 1; }
done
foreign=routerd-native-package-foreign.service
[ "$(systemctl show -p LoadState --value "$foreign")" = not-found ]
foreign_started=0
work=$(mktemp -d /tmp/routerd-deb-lifecycle.XXXXXX)
finish()
{
    result=$?
    if [ "$foreign_started" -eq 1 ]; then systemctl stop "$foreign" || result=1; fi
    if [ "$result" -ne 0 ]; then
        tail -80 "$work/apt.log" >&2 || true
        journalctl --no-pager -n 80 -u routerd.service -u routerd-dns-resolver@package.service >&2 || true
    fi
    rm -f "$work/apt.log"
    rmdir "$work"
    exit "$result"
}
trap finish EXIT
systemd-run --collect --unit="$foreign" --property=Type=exec /bin/sleep infinity
foreign_started=1
export DEBIAN_FRONTEND=noninteractive
apt-get -o DPkg::Lock::Timeout=300 install -y bind9-dnsutils python3 "$prior" > "$work/apt.log" 2>&1
[ "$(dpkg-query -W -f='${Version}' routerd)" = "$prior_version" ]
if systemctl is-active --quiet routerd.service; then
    echo 'fresh install unexpectedly started routerd' >&2
    exit 1
fi
[ ! -e /etc/routerd/router.yaml ]
[ ! -e /var/lib/routerd/routerd.db ]
command -v routerctl
verification=$(dpkg -V routerd)
[ -z "$verification" ]

install -d -m 0700 /etc/routerd
cat > /etc/routerd/router.yaml <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: package-lifecycle
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSResolver
      metadata: {name: package}
      spec:
        listen:
          - {name: local, addresses: [127.0.0.1], port: 53053, sources: [local]}
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSZone
      metadata: {name: package}
      spec:
        zone: routerd.test
        records:
          - {hostname: package, ipv4: 192.0.2.15}
    - apiVersion: net.routerd.net/v1alpha1
      kind: DNSForwarder
      metadata: {name: local}
      spec:
        resolver: DNSResolver/package
        match: [routerd.test]
        zoneRefs: [DNSZone/package]
EOF
configuration=$(sha256sum /etc/routerd/router.yaml)
systemctl enable --now routerd.service
wait_dns()
{
    for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
        answer=$(dig @127.0.0.1 -p 53053 package.routerd.test A +time=1 +tries=1 +short 2>/dev/null) || answer=
        if [ "$answer" = 192.0.2.15 ]; then
            [ "$(dig @127.0.0.1 -p 53053 package.routerd.test A +tcp +time=1 +tries=1 +short)" = 192.0.2.15 ]
            return 0
        fi
        sleep 1
    done
    echo 'independent DNS UDP/TCP probe failed' >&2
    return 1
}
wait_dns
main_pid=$(systemctl show -p MainPID --value routerd.service)
helper_pid=$(systemctl show -p MainPID --value routerd-dns-resolver@package.service)
[ "$(readlink "/proc/$main_pid/exe")" = /usr/sbin/routerd ]
[ "$(readlink "/proc/$helper_pid/exe")" = /usr/sbin/routerd-dns-resolver ]
old_binary=$(sha256sum /usr/sbin/routerd)
python3 - <<'PY'
import sqlite3
with sqlite3.connect('/var/lib/routerd/routerd.db') as db:
    db.execute('CREATE TABLE package_upgrade_probe (value TEXT)')
    db.execute("INSERT INTO package_upgrade_probe VALUES ('durable-before-upgrade')")
PY
apt-get -o DPkg::Lock::Timeout=300 install -y "$current" >> "$work/apt.log" 2>&1
[ "$(dpkg-query -W -f='${Version}' routerd)" = "$current_version" ]
[ "$(sha256sum /etc/routerd/router.yaml)" = "$configuration" ]
[ "$(sha256sum /usr/sbin/routerd)" != "$old_binary" ]
[ ! -d "/proc/$main_pid" ]
[ ! -d "/proc/$helper_pid" ]
wait_dns
systemctl is-active --quiet "$foreign"
verification=$(dpkg -V routerd)
[ -z "$verification" ]
python3 - <<'PY'
import sqlite3
with sqlite3.connect('/var/lib/routerd/routerd.db') as db:
    assert db.execute('SELECT value FROM package_upgrade_probe').fetchall() == [('durable-before-upgrade',)]
PY
main_pid=$(systemctl show -p MainPID --value routerd.service)
helper_pid=$(systemctl show -p MainPID --value routerd-dns-resolver@package.service)
apt-get -o DPkg::Lock::Timeout=300 remove -y routerd >> "$work/apt.log" 2>&1
systemctl is-active --quiet "$foreign"
[ ! -e /usr/sbin/routerd ]
[ ! -d "/proc/$main_pid" ]
[ ! -d "/proc/$helper_pid" ]
[ ! -e /etc/systemd/system/routerd.service ]
[ ! -e /etc/systemd/system/routerd-dns-resolver@package.service ]
[ "$(sha256sum /etc/routerd/router.yaml)" = "$configuration" ]
[ -s /var/lib/routerd/routerd.db ]
printf 'native-deb PASS: fresh install, UDP/TCP DNS, update %s -> %s, old process exit, config/database retention, removal\n' "$prior_version" "$current_version"
