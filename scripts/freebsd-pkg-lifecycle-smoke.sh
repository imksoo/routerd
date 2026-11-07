#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# Fresh disposable FreeBSD VM only. Configuration/database retention is checked
# before the caller discards the guest; no source/manual installation migration.
set -eu
if [ "$#" -ne 3 ] || [ "$1" != --isolated-vm ]; then
    echo "usage: $0 --isolated-vm PRIOR.pkg CURRENT.pkg" >&2
    exit 2
fi
[ "$(uname -s)" = FreeBSD ]
[ "$(id -u)" -eq 0 ]
python3 -c 'import sqlite3'
command -v dig >/dev/null
prior=$(realpath "$2")
current=$(realpath "$3")
[ "$(pkg query -F "$prior" '%n')" = routerd ]
[ "$(pkg query -F "$current" '%n')" = routerd ]
prior_version=$(pkg query -F "$prior" '%v')
current_version=$(pkg query -F "$current" '%v')
[ "$(pkg version -t "$current_version" "$prior_version")" = '>' ]
if pkg info -e routerd; then echo 'requires a clean OS without routerd' >&2; exit 1; fi
for path in /usr/local/sbin/routerd /usr/local/etc/routerd/router.yaml /var/db/routerd/routerd.db; do
    [ ! -e "$path" ] || { echo "requires absent $path" >&2; exit 1; }
done
work=$(mktemp -d /tmp/routerd-pkg-lifecycle.XXXXXX)
fault_injected=0
finish()
{
    result=$?
    if [ "$fault_injected" -eq 1 ]; then
        cp -p "$work/routerd.rc" /usr/local/etc/rc.d/routerd || { echo "failed to restore injected fixture fault; backup: $work/routerd.rc" >&2; exit 1; }
    fi
    if [ "$result" -ne 0 ]; then
        tail -80 "$work/pkg.log" >&2 || true
        tail -80 /var/log/routerd.log >&2 || true
    fi
    rm -f "$work/pkg.log" "$work/processes.py" "$work/before.json" "$work/routerd.rc"
    rmdir "$work"
    exit "$result"
}
trap finish EXIT
cat > "$work/processes.py" <<'PY'
import json,os,subprocess,sys
programs={'/usr/local/sbin/routerd','/usr/local/sbin/routerd-dns-resolver'}
def processes():
    result={}
    for row in subprocess.check_output(['ps','-axww','-o','pid=','-o','args='],text=True).splitlines():
        fields=row.strip().split(None,1)
        if len(fields)==2 and fields[1].split()[0] in programs:
            result[int(fields[0])]=fields[1].split()[0]
    return result
if sys.argv[1]=='snapshot':
    result=processes()
    assert set(result.values())==programs,result
    json.dump(result,open(sys.argv[2],'w'))
else:
    for pid in json.load(open(sys.argv[2])):
        try: os.kill(int(pid),0)
        except ProcessLookupError: continue
        raise AssertionError('old routerd process remains: '+pid)
    if sys.argv[1]=='removed': assert not processes(),processes()
PY
check_native_agent()
{
    native_status=$(/usr/local/sbin/routerd-ndpi-agent selftest)
    printf '%s\n' "$native_status" | python3 -c 'import json,sys; x=json.load(sys.stdin); assert x["libndpiLoaded"] is True; assert x["libndpiVersion"]; assert x["selftest"]["reason"] == "libndpi_protocol_detection"; assert x["selftest"]["tlsSNI"] == "routerd-ndpi-selftest.example"'
    pkg query '%B' routerd | grep -E '^libndpi[.]so[.]'
}
pkg -o HANDLE_RC_SCRIPTS=true install -y "$prior" > "$work/pkg.log" 2>&1
[ "$(pkg query '%v' routerd)" = "$prior_version" ]
if service routerd onestatus >/dev/null 2>&1; then
    echo 'fresh package unexpectedly started routerd' >&2
    exit 1
fi
[ ! -e /usr/local/etc/routerd/router.yaml ]
[ ! -e /var/db/routerd/routerd.db ]
command -v routerctl
pkg check -s routerd
check_native_agent
[ "$(stat -f '%u:%g' /usr/local/sbin/routerd)" = 0:0 ]
install -d -m 0700 /usr/local/etc/routerd
cat > /usr/local/etc/routerd/router.yaml <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: package-lifecycle}
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
configuration=$(sha256 -q /usr/local/etc/routerd/router.yaml)
old_binary=$(sha256 -q /usr/local/sbin/routerd)
sysrc routerd_enable=YES
service routerd start
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
python3 "$work/processes.py" snapshot "$work/before.json"
python3 - <<'PY'
import sqlite3
with sqlite3.connect('/var/db/routerd/routerd.db') as db:
    db.execute('CREATE TABLE package_upgrade_probe (value TEXT)')
    db.execute("INSERT INTO package_upgrade_probe VALUES ('durable-before-upgrade')")
PY
# Exercise the native package manager's failure gate, not merely a shell hook.
# The fixture makes the old rc.d stop fail, then removes that injected fault.
cp -p /usr/local/etc/rc.d/routerd "$work/routerd.rc"
fault_injected=1
python3 - <<'PY'
from pathlib import Path
p=Path('/usr/local/etc/rc.d/routerd')
s=p.read_text()
needle='routerd_stop()\n{\n'
assert needle in s
p.write_text(s.replace(needle, needle+'    echo native-test-stop-failure >&2\n    return 42\n', 1))
PY
if pkg -o HANDLE_RC_SCRIPTS=true install -y "$current" >> "$work/pkg.log" 2>&1; then
    echo 'pkg replaced files despite a failed old-service stop' >&2
    exit 1
fi
grep -Fq native-test-stop-failure "$work/pkg.log"
[ "$(pkg query '%v' routerd)" = "$prior_version" ]
[ "$(sha256 -q /usr/local/sbin/routerd)" = "$old_binary" ]
wait_dns
cp -p "$work/routerd.rc" /usr/local/etc/rc.d/routerd
fault_injected=0
pkg -o HANDLE_RC_SCRIPTS=true install -y "$current" >> "$work/pkg.log" 2>&1
[ "$(pkg query '%v' routerd)" = "$current_version" ]
[ "$(sha256 -q /usr/local/etc/routerd/router.yaml)" = "$configuration" ]
[ "$(sha256 -q /usr/local/sbin/routerd)" != "$old_binary" ]
python3 "$work/processes.py" replaced "$work/before.json"
wait_dns
pkg check -s routerd
check_native_agent
python3 - <<'PY'
import sqlite3
with sqlite3.connect('/var/db/routerd/routerd.db') as db:
    assert db.execute('SELECT value FROM package_upgrade_probe').fetchall() == [('durable-before-upgrade',)]
PY
python3 "$work/processes.py" snapshot "$work/before.json"
pkg -o HANDLE_RC_SCRIPTS=true delete -y routerd >> "$work/pkg.log" 2>&1
python3 "$work/processes.py" removed "$work/before.json"
[ ! -e /usr/local/sbin/routerd ]
[ ! -e /usr/local/etc/rc.d/routerd ]
[ "$(sha256 -q /usr/local/etc/routerd/router.yaml)" = "$configuration" ]
[ -s /var/db/routerd/routerd.db ]
printf 'native-pkg PASS: fresh install, native libnDPI TLS/SNI detection, exact shared-library dependency, UDP/TCP DNS, update %s -> %s, old process exit, config/database retention, removal\n' "$prior_version" "$current_version"
