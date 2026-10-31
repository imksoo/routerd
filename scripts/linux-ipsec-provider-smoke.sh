#!/bin/bash
# SPDX-License-Identifier: BSD-3-Clause
# Destructive-to-the-fixture package test: explicitly assigned disposable VM only.
set -Eeuo pipefail
if [[ ${1:-} != --disposable-vm || $# != 6 ]]; then
 echo "usage: sudo $0 --disposable-vm WORK_DIR MAKE_DIST_ARCHIVE SHA256 PRIVATE_PEER_ROOT APT_CONFIG" >&2
 exit 64
fi
[[ $(id -u) == 0 ]]
systemd-detect-virt --vm --quiet
work=$(realpath "$2") archive=$(realpath "$3") peer_root=$(realpath "$5")
export APT_CONFIG
APT_CONFIG=$(realpath "$6")
export DEBIAN_FRONTEND=noninteractive
export TZ=UTC
wait_local_expiry=${ROUTERD_IPSEC_WAIT_LOCAL_EXPIRY:-0}
[[ $wait_local_expiry == 0 || $wait_local_expiry == 1 ]]
[[ -d $work && -f $APT_CONFIG && -x $peer_root/usr/lib/ipsec/charon ]]
[[ $(sha256sum "$archive" | cut -d ' ' -f1) == "$4" ]]
[[ ! -e $work/provider-started ]]
for path in /usr/local/etc/routerd/swanctl /etc/swanctl /etc/strongswan.conf /run/charon.vici; do
 [[ ! -e $path ]] || { echo "fresh-provider prerequisite occupied: $path" >&2; exit 69; }
done
packages=(charon-systemd strongswan-swanctl strongswan-libcharon libstrongswan libstrongswan-standard-plugins libcharon-extauth-plugins)
for package in "${packages[@]}"; do
 if dpkg-query -W -f='${Status}' "$package" 2>/dev/null | grep -q ' installed$'; then
  echo "fresh-provider prerequisite occupied: $package" >&2; exit 69
 fi
done
[[ $(systemctl is-active routerd) == active ]]
routerctl get status -o json > "$work/service.before.json"
jq -e '.status.phase=="Healthy" and ([.status.controllers[]|select(.currentError==true)]|length)==0' "$work/service.before.json" >/dev/null
jq -e '(.spec.resources // [] | length)==0' /usr/local/etc/routerd/router.yaml >/dev/null
ip xfrm state count | grep -q 'SAD count 0'
ip xfrm policy count | grep -Eq 'SPD IN +0 OUT 0 FWD 0'
if ip link show rd-provider >/dev/null 2>&1; then exit 69; fi
cat /proc/modules > "$work/modules.before"
ip -j link show > "$work/links.before.json"
ip -4 -j route show table all > "$work/routes4.before.json"
ip -6 -j route show table all > "$work/routes6.before.json"
ip -4 rule show > "$work/rules4.before"
ip -6 rule show > "$work/rules6.before"
cat /sys/kernel/security/apparmor/profiles > "$work/apparmor.before"
dpkg-query -W > "$work/packages.before"
systemctl cat routerd > "$work/unit.before.txt"
sha256sum /usr/local/sbin/routerd /usr/local/etc/routerd/router.yaml /etc/systemd/system/routerd.service > "$work/installed.sha256"
tar -xzf "$archive" -C "$work" ./bin
routerd=$work/bin/routerd
"$routerd" version
cat > "$work/router.yaml" <<'YAML'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: ipsec-provider-native}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPsecConnection
      metadata: {name: provider-native}
      spec: {localAddress: 198.18.95.1, remoteAddress: 198.18.95.2, preSharedKey: disposable-ipsec-provider-native-key, leftSubnet: 10.254.95.1/32, rightSubnet: 10.254.95.2/32}
YAML
cat > "$work/empty.yaml" <<'YAML'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: ipsec-provider-native}
spec: {resources: []}
YAML
product() {
 timeout -k 5 300 "$routerd" serve --once --controllers "${2:-package,ipsec}" --config "$1" \
  --state-file "$work/state.db" --ledger-file "$work/ledger.db" --status-file "$work/status.json" \
  --socket "$work/control.sock" --status-socket "$work/status.sock"
}
stage=preparation peer='' holder='' running_router='' peer_birth='' holder_birth='' running_birth='' stopped=0 link_created=0 link_index='' address_created=0 foreign_created=0
birth() { awk '{print $22}' "/proc/$1/stat" 2>/dev/null; }
stop_child() {
 local pid=$1 expected=$2 label=$3 rc=0
 [[ -n $pid ]] || return 0
 if [[ -e /proc/$pid/stat && $(birth "$pid") == "$expected" ]]; then
  kill -TERM "$pid" || return 1
  for _ in {1..50}; do
   [[ ! -e /proc/$pid/stat || $(awk '{print $3}' "/proc/$pid/stat" 2>/dev/null) == Z ]] && break
   sleep .1
  done
  if [[ -e /proc/$pid/stat && $(birth "$pid") == "$expected" && $(awk '{print $3}' "/proc/$pid/stat" 2>/dev/null) != Z ]]; then
   kill -KILL "$pid"; wait "$pid" || true; return 1
  fi
 fi
 wait "$pid" || rc=$?
 printf 'reaped=%s waitExit=%s\n' "$label" "$rc" >> "$work/process-cleanup.log"
 [[ $rc == 0 || ( $label == namespace && $rc == 143 ) ]]
}
finish() {
 rc=$? failures=0
 trap - EXIT
 set +e
 printf 'stage=%s exit=%s\n' "$stage" "$rc" > "$work/result-stage.txt"
 cp -a /usr/local/etc/routerd/swanctl "$work/receipt-before-cleanup" 2>/dev/null
 journalctl -u strongswan.service -n 120 --no-pager > "$work/provider-journal.log"
 cleanup_worker_stopped=0
 if stop_child "$running_router" "$running_birth" router; then cleanup_worker_stopped=1; else failures=1; fi
 if ((cleanup_worker_stopped)) && [[ -S /run/charon.vici && -f /usr/local/etc/routerd/swanctl/routerd-ipsec.json ]]; then
  product "$work/empty.yaml" > "$work/cleanup-withdraw.log" 2>&1 || failures=1
 fi
 stop_child "$peer" "$peer_birth" peer || failures=1
 stop_child "$holder" "$holder_birth" namespace || failures=1
 if [[ -f $work/provider-started ]]; then
  vici_inode=$(stat -c '%d:%i' /run/charon.vici 2>/dev/null)
  systemctl stop strongswan.service || failures=1
  # Some providers leave their socket inode after a confirmed stop.
  # Delete only this run's unchanged socket with no accepting listener.
  if [[ -S /run/charon.vici && -n $vici_inode ]]; then
   if ! python3 - "$vici_inode" <<'PY'
import errno,os,socket,sys
p='/run/charon.vici'
st=os.stat(p)
assert f'{st.st_dev}:{st.st_ino}'==sys.argv[1]
with socket.socket(socket.AF_UNIX) as s:
    try: s.connect(p)
    except OSError as e:
        if e.errno!=errno.ECONNREFUSED: raise
    else: raise RuntimeError('provider still accepts VICI connections')
st=os.stat(p)
assert f'{st.st_dev}:{st.st_ino}'==sys.argv[1]
os.unlink(p)
PY
   then failures=1; fi
  fi
  if ((foreign_created)); then rm /etc/swanctl/conf.d/routerd-fixture-foreign.conf || failures=1; fi
  for executable in swanctl charon-systemd; do
   if ! grep -q "^/usr/sbin/$executable " "$work/apparmor.before"; then
    if grep -q "^/usr/sbin/$executable " /sys/kernel/security/apparmor/profiles; then
     apparmor_parser -R "/etc/apparmor.d/usr.sbin.$executable" || failures=1
    fi
   fi
  done
  # Remove only this gate's initially absent packages, never autoremove.
  apt-get -y --no-auto-remove purge "${packages[@]}" > "$work/package-remove.log" 2>&1 || failures=1
 fi
 if ((link_created)) && [[ -e /sys/class/net/rd-provider/ifindex ]]; then
  if [[ $(cat /sys/class/net/rd-provider/ifindex) == "$link_index" ]]; then
   ip link del rd-provider || failures=1
  else failures=1; fi
 fi
 if ((address_created)); then ip addr del 10.254.95.1/32 dev lo || failures=1; fi
 if [[ -f /usr/local/etc/routerd/swanctl/routerd-ipsec.json ]]; then
  if jq -e '.pending==false and (.connections|length)==0 and (.sharedKeys|length)==0' /usr/local/etc/routerd/swanctl/routerd-ipsec.json >/dev/null; then
   rm /usr/local/etc/routerd/swanctl/routerd-ipsec.json /usr/local/etc/routerd/swanctl/.routerd-ipsec.lock
   rmdir /usr/local/etc/routerd/swanctl || failures=1
  else failures=1; fi
 fi
 # No force unload: leave any unexpectedly held module for investigation.
 if ! python3 - "$work" <<'PY'
import pathlib,subprocess,sys,json
p=pathlib.Path(sys.argv[1])
names=lambda f:{line.split()[0] for line in f.read_text().splitlines()}
initial=names(p/'modules.before')
for _ in range(12):
    added=names(pathlib.Path('/proc/modules'))-initial
    if not added: break
    progress=False
    for name in sorted(added):
        if subprocess.run(['rmmod',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:progress=True
    if not progress: break
assert names(pathlib.Path('/proc/modules'))==initial
assert json.loads((p/'links.before.json').read_text())==json.loads(subprocess.check_output(['ip','-j','link','show']))
for family in ['4','6']:
    assert json.loads((p/f'routes{family}.before.json').read_text())==json.loads(subprocess.check_output(['ip','-'+family,'-j','route','show','table','all']))
    assert (p/f'rules{family}.before').read_bytes()==subprocess.check_output(['ip','-'+family,'rule','show'])
PY
 then failures=1; fi
 sha256sum -c "$work/installed.sha256" > "$work/installed-unchanged.log" 2>&1 || failures=1
 if ((stopped)); then
  sha256sum -c "$work/state.sha256" > "$work/state-unchanged.log" 2>&1 || failures=1
  systemctl start routerd || failures=1
  for _ in {1..40}; do
   if routerctl get status -o json > "$work/service.after.json" 2>/dev/null && jq -e '.status.phase=="Healthy" and ([.status.controllers[]|select(.currentError==true)]|length)==0' "$work/service.after.json" >/dev/null; then break; fi
   sleep .5
  done
  jq -e '.status.phase=="Healthy"' "$work/service.after.json" >/dev/null || failures=1
 fi
 ip xfrm state count > "$work/xfrm.after"
 ip xfrm policy count >> "$work/xfrm.after"
 grep -q 'SAD count 0' "$work/xfrm.after" || failures=1
 grep -Eq 'SPD IN +0 OUT 0 FWD 0' "$work/xfrm.after" || failures=1
 dpkg-query -W > "$work/packages.after"
 cmp "$work/packages.before" "$work/packages.after" || failures=1
 sort /sys/kernel/security/apparmor/profiles > "$work/apparmor.after.sorted"
 sort "$work/apparmor.before" | cmp - "$work/apparmor.after.sorted" || failures=1
 if ((failures)); then rc=1; fi
 printf 'runtime_exit=%s cleanup_errors=%s stage=%s\n' "$rc" "$failures" "$stage" | tee "$work/result.txt"
 exit "$rc"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
stopped=1
systemctl stop routerd
tar --acls --xattrs -cpf "$work/installed-backup.tar" /usr/local/etc/routerd /var/lib/routerd /etc/systemd/system/routerd.service
find /var/lib/routerd -type f -print0 | sort -z | xargs -0 sha256sum > "$work/state.sha256"
holder_seconds=600
if ((wait_local_expiry)); then holder_seconds=6000; fi
unshare -n sleep "$holder_seconds" & holder=$!
holder_birth=$(birth "$holder")
for _ in {1..50}; do [[ $(readlink /proc/"$holder"/ns/net) != "$(readlink /proc/self/ns/net)" ]] && break; sleep .1; done
[[ $(readlink /proc/"$holder"/ns/net) != "$(readlink /proc/self/ns/net)" ]]
ip link add rd-provider type veth peer name rd-peer netns "$holder"
link_created=1
link_index=$(cat /sys/class/net/rd-provider/ifindex)
ip addr add 198.18.95.1/30 dev rd-provider
ip link set rd-provider up
ip addr add 10.254.95.1/32 dev lo
address_created=1
ip route add 10.254.95.2/32 via 198.18.95.2
peer_exec() { nsenter -t "$holder" -n -- "$@"; }
peer_exec ip link set lo up
peer_exec ip link set rd-peer up
peer_exec ip addr add 198.18.95.2/30 dev rd-peer
peer_exec ip addr add 10.254.95.2/32 dev lo
peer_exec ip route add 10.254.95.1/32 via 198.18.95.1
stage='fresh-package-provider'
touch "$work/provider-started"
product "$work/router.yaml" package > "$work/fresh-package-provider.log" 2>&1
systemctl is-active --quiet strongswan.service
systemctl show strongswan.service -p MainPID -p InvocationID -p FragmentPath > "$work/provider.first"
swanctl --stats > "$work/provider.stats" 2>&1
mkdir -p /etc/swanctl/conf.d
cat > /etc/swanctl/conf.d/routerd-fixture-foreign.conf <<'EOF'
connections {
 fixture-foreign {
  local_addrs = 198.18.95.1
  remote_addrs = 198.18.95.254
  local { auth = psk }
  remote { auth = psk }
 }
}
EOF
foreign_created=1
swanctl --load-conns --file /etc/swanctl/conf.d/routerd-fixture-foreign.conf > "$work/foreign-load.log" 2>&1
swanctl --list-conns > "$work/foreign-loaded.connections" 2>&1
grep -q fixture-foreign "$work/foreign-loaded.connections"
product "$work/router.yaml" > "$work/fresh-ipsec.log" 2>&1
cp /usr/local/etc/routerd/swanctl/routerd-ipsec.json "$work/receipt.acquired.json"
mkdir "$work/peer-run"
cat > "$work/peer-strongswan.conf" <<'EOF'
charon {
 load_modular = no
 retransmit_timeout = 1.0
 retransmit_base = 1.0
 retransmit_tries = 1
 load = aes sha1 sha2 random nonce gmp hmac xcbc kdf kernel-netlink socket-default vici
 plugins { vici { socket = unix:///run/charon.vici } }
}
swanctl { load = random nonce vici }
EOF
# The peer has independent runtime/configuration; the router uses the real unit.
# shellcheck disable=SC2016 # Expanded in the peer mount namespace.
nsenter -t "$holder" -n -- unshare -m -- bash -c '
 set -e
 mount --make-rprivate /
 mount -t sysfs sysfs /sys
 mount --bind "$1" /run
 mount --bind "$2/usr/lib/ipsec/plugins" /usr/lib/ipsec/plugins
 export LD_LIBRARY_PATH=$2/usr/lib/ipsec STRONGSWAN_CONF=$3
 exec "$2/usr/lib/ipsec/charon"
' peer "$work/peer-run" "$peer_root" "$work/peer-strongswan.conf" > "$work/peer.log" 2>&1 & peer=$!
peer_birth=$(birth "$peer")
peer_control() { timeout -k 2 20 nsenter -t "$peer" -n -m -- env LD_LIBRARY_PATH="$peer_root/usr/lib/ipsec" STRONGSWAN_CONF="$work/peer-strongswan.conf" "$peer_root/usr/sbin/swanctl" "$@"; }
for _ in {1..50}; do if peer_control --stats > "$work/peer.stats" 2>&1; then break; fi; kill -0 "$peer"; sleep .1; done
peer_control --stats > "$work/peer.stats" 2>&1
cat > "$work/peer.conf" <<'EOF'
connections {
 provider-peer {
  version = 2
  dpd_delay = 2s
  rekey_time = 0s
  reauth_time = 0s
  local_addrs = 198.18.95.2
  remote_addrs = 198.18.95.1
  proposals = aes256-sha256-modp2048
  local { auth = psk }
  remote { auth = psk }
  children { net { local_ts = 10.254.95.2/32
   remote_ts = 10.254.95.1/32
   esp_proposals = aes256-sha256
   start_action = trap
   rekey_time = 20s
   life_time = 30s
   rand_time = 0s
   dpd_action = trap
  } }
 }
}
secrets { ike-peer { id-1 = 198.18.95.1
 id-2 = 198.18.95.2
 secret = "disposable-ipsec-provider-native-key"
} }
EOF
mkdir -p "$work"/{x509,x509ca,x509ocsp,x509aa,x509ac,x509crl,pubkey,private,rsa,ecdsa,pkcs8,pkcs12}
peer_control --load-all --file "$work/peer.conf" > "$work/peer-load.log" 2>&1
traffic() {
 if [[ ${1:-new} == new ]]; then
  timeout -k 2 25 swanctl --initiate --ike routerd-provider-native --child net --timeout 15 > "$work/$stage.initiate" 2>&1
 fi
 ping -n -I 10.254.95.1 -c 2 -W 2 10.254.95.2 > "$work/$stage.forward" || return 1
 peer_exec ping -n -I 10.254.95.2 -c 2 -W 2 10.254.95.1 > "$work/$stage.reverse" || return 1
 ip -s xfrm state > "$work/$stage.xfrm" || return 1
 grep -Eq '[1-9][0-9]*\(packets\)' "$work/$stage.xfrm"
}
stage=initial-traffic
traffic
stage=unchanged-provider
product "$work/router.yaml" > "$work/unchanged.log" 2>&1
systemctl show strongswan.service -p MainPID -p InvocationID -p FragmentPath > "$work/provider.unchanged"
cmp "$work/provider.first" "$work/provider.unchanged"
traffic existing
awk '/proto esp/ {print $4}' "$work/initial-traffic.xfrm" | sort > "$work/initial.spis"
awk '/proto esp/ {print $4}' "$work/unchanged-provider.xfrm" | sort > "$work/unchanged.spis"
cmp "$work/initial.spis" "$work/unchanged.spis"
stage=withdrawal
product "$work/empty.yaml" > "$work/withdraw.log" 2>&1
jq -e '.pending==false and (.connections|length)==0 and (.sharedKeys|length)==0' /usr/local/etc/routerd/swanctl/routerd-ipsec.json >/dev/null
swanctl --list-sas > "$work/withdraw.sas" 2>&1
if grep -q ESTABLISHED "$work/withdraw.sas"; then exit 1; fi
swanctl --list-conns > "$work/withdraw.connections" 2>&1
grep -q fixture-foreign "$work/withdraw.connections"
stage=reacquired-traffic
product "$work/router.yaml" > "$work/reacquire.log" 2>&1
traffic
systemctl show strongswan.service -p MainPID -p InvocationID -p FragmentPath > "$work/provider.reacquired"
cmp "$work/provider.first" "$work/provider.reacquired"
swanctl --list-conns > "$work/reacquired.connections" 2>&1
grep -q fixture-foreign "$work/reacquired.connections"
# Keep the independent configuration under the package's normal include path.
# The unit may reload it after the crash; routerd must preserve that connection.
awk '/^fixture-foreign:/ {keep=1} keep && /^[^[:space:]]/ && !/^fixture-foreign:/ {keep=0} keep' "$work/reacquired.connections" > "$work/foreign.expected"
[[ -s $work/foreign.expected ]]
capture_precrash_receipt() {
 ip -s xfrm state > "$work/owned-precrash.xfrm" || return 1
 if ! python3 - "$work/owned-precrash.xfrm" "$work/local-expiry-plan.json" <<'PY'
import datetime,json,pathlib,re,sys
text=pathlib.Path(sys.argv[1]).read_text()
states=[]
for block in re.split(r'(?=^src )',text,flags=re.M):
    if block.split()[:4]not in [['src','198.18.95.1','dst','198.18.95.2'],['src','198.18.95.2','dst','198.18.95.1']]:continue
    spi=re.search(r'proto esp spi (0x[0-9a-f]+)',block)
    hard=re.search(r'expire add: soft [^\n]*hard (\d+)\(sec\)',block)
    added=re.search(r'^\s+add (\d{4}-\d\d-\d\d \d\d:\d\d:\d\d) use ',block,re.M)
    if not (spi and hard and added) or int(hard[1])<=0:raise ValueError('native local lifetime is unknown')
    packet=re.search(r'^\s+\d+\(bytes\), (\d+)\(packets\)',block,re.M)
    if not packet:raise ValueError('native packet counter is unknown')
    stamp=datetime.datetime.strptime(added[1],'%Y-%m-%d %H:%M:%S').replace(tzinfo=datetime.timezone.utc)
    states.append({'spi':hex(int(spi[1],16)),'addTimeUTC':stamp.isoformat(),'packetCountBeforeCrash':int(packet[1]),'hardLifetimeSeconds':int(hard[1]),'hardExpiryUnix':int(stamp.timestamp())+int(hard[1])})
assert len(states)==2,states
plan={'states':states,'latestRecordedHardExpiryUnix':max(s['hardExpiryUnix']for s in states),'peerRekeySeconds':20,'peerHardLifetimeSeconds':30,'productLifetimeModified':False}
pathlib.Path(sys.argv[2]).write_text(json.dumps(plan,indent=2))
PY
 then return 1; fi
 cp /usr/local/etc/routerd/swanctl/routerd-ipsec.json "$work/precrash-receipt.json" || return 1
 python3 - "$work/local-expiry-plan.json" "$work/precrash-receipt.json" "$work/precrash-receipt-proof.json" <<'PY'
import datetime,json,pathlib,re,sys
plan=json.loads(pathlib.Path(sys.argv[1]).read_text())
receipt=json.loads(pathlib.Path(sys.argv[2]).read_text())
connections=[c for c in receipt['connections']if c['name']=='routerd-provider-native']
assert not receipt['pending']and len(connections)==1
connection=connections[0]
recorded={hex(s['id']['spi']):s for s in connection.get('sas',[])}
coverage={}
for state in plan['states']:
    native=recorded.get(state['spi'])
    coverage[state['spi']]=bool(native and native['scope']==connection.get('scope')and native['scope']and native['addTime']==int(datetime.datetime.fromisoformat(state['addTimeUTC']).timestamp())and re.fullmatch('[0-9a-f]{64}',native['fingerprint'])and native['id']['protocol']=='ESP'and {native['id']['source'],native['id']['destination']}=={'198.18.95.1','198.18.95.2'})
report={'precondition':'exact pre-crash native SAs are already observed and journaled','coverage':coverage,'allObservedSAsRecorded':all(coverage.values()),'unobservedCreationCrashWindowQualified':False}
pathlib.Path(sys.argv[3]).write_text(json.dumps(report,indent=2))
assert report['allObservedSAsRecorded'],report
PY
}
stage=provider-crash-recovery
"$routerd" serve --controllers package,ipsec --config "$work/router.yaml" \
 --state-file "$work/state.db" --ledger-file "$work/ledger.db" --status-file "$work/status.json" \
 --socket "$work/control.sock" --status-socket "$work/status.sock" --graceful-stop-timeout 0 > "$work/router-daemon.log" 2>&1 & running_router=$!
running_birth=$(birth "$running_router")
for _ in {1..50}; do [[ -S $work/control.sock ]] && break; kill -0 "$running_router"; sleep .2; done
[[ -S $work/control.sock ]]
# This gate qualifies recovery of durably observed SAs. It deliberately does not
# claim recovery of an SA created and lost before any owner observation.
precrash_recorded=0
for _ in {1..120}; do
 if capture_precrash_receipt > "$work/precrash-receipt-observation.log" 2>&1; then
  precrash_recorded=1; break
 fi
 kill -0 "$running_router"
 sleep .5
done
[[ $precrash_recorded == 1 ]]
old_pid=$(systemctl show strongswan.service -p MainPID --value)
[[ $old_pid != 0 ]]
crash_epoch=$(date +%s)
systemctl kill --kill-whom=main --signal=SIGKILL strongswan.service
for _ in {1..60}; do
 new_pid=$(systemctl show strongswan.service -p MainPID --value)
 if [[ $new_pid != 0 && $new_pid != "$old_pid" ]] && swanctl --list-conns > "$work/recovered.connections" 2>&1 && grep -q routerd-provider-native "$work/recovered.connections"; then break; fi
 kill -0 "$running_router"; sleep .5
done
[[ $new_pid != 0 && $new_pid != "$old_pid" ]]
grep -q routerd-provider-native "$work/recovered.connections"
systemctl show strongswan.service -p MainPID -p InvocationID -p NRestarts > "$work/provider.recovered"
# Continuity is distinct from recovery of IKE control over new SAs.
gate_failed=0
continuity_pass=0 continuity_native=0 continuity_vici=0
stage=provider-crash-continuity
if traffic existing; then continuity_pass=1; else gate_failed=1; fi
if ip -s xfrm state > "$work/$stage.xfrm"; then
 cp "$work/$stage.xfrm" "$work/continuity.xfrm"
 continuity_native=1
else gate_failed=1; fi
if timeout -k 2 5 swanctl --list-sas > "$work/continuity.sas" 2>&1; then continuity_vici=1; else gate_failed=1; fi
stage='peer-expiry-recovery'
earliest_success=$((crash_epoch + 32))
deadline=$((crash_epoch + 90))
if ((wait_local_expiry)); then
 earliest_success=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["latestRecordedHardExpiryUnix"]+5)' "$work/local-expiry-plan.json")
 deadline=$((earliest_success + 90))
 stage=recorded-local-expiry-recovery
fi
check_recovered_state() {
 python3 - "$work/local-expiry-plan.json" "$work/$stage.xfrm" "$work/recovered.sas" "$work/recovery-state.json" <<'PY'
import datetime,json,pathlib,re,sys
plan=json.loads(pathlib.Path(sys.argv[1]).read_text())
native=pathlib.Path(sys.argv[2]).read_text()
vici=pathlib.Path(sys.argv[3]).read_text()
old={s['spi']for s in plan['states']}
kernel={}
if native.strip()and not re.search(r'^src ',native,re.M):raise ValueError('unexpected native SA output')
for block in re.split(r'(?=^src )',native,flags=re.M):
    if block.split()[:4]not in [['src','198.18.95.1','dst','198.18.95.2'],['src','198.18.95.2','dst','198.18.95.1']]:continue
    spi=re.search(r'proto esp spi (0x[0-9a-f]+)',block)
    count=re.search(r'^\s+\d+\(bytes\), (\d+)\(packets\)',block,re.M)
    if spi:
        if not count:raise ValueError('native SA packet observation is unknown')
        kernel[hex(int(spi[1],16))]=int(count[1])
active=False
child={}
for line in vici.splitlines():
    if line and not line[0].isspace():
        active=line.startswith('routerd-provider-native:')and'ESTABLISHED'in line
    if active:
        m=re.match(r'\s+(in|out)\s+([0-9a-f]+),\s+\d+ bytes,\s+(\d+) packets',line)
        if m:child[hex(int(m[2],16))]=int(m[3])
fresh=len(child)==2 and not old.intersection(child) and all(n>0 and kernel.get(spi,0)>0 for spi,n in child.items())
report={'observedAtUTC':datetime.datetime.now(datetime.timezone.utc).isoformat(),'newProviderSAWithPackets':fresh,'oldSPIs':sorted(old),'oldPacketsBeforeCrash':{x['spi']:x['packetCountBeforeCrash']for x in plan['states']},'oldPacketsNow':{spi:kernel.get(spi)for spi in old},'oldSPIsStillInstalled':sorted(old.intersection(kernel)),'currentProviderSPIs':sorted(child),'nativePackets':kernel,'productLifetimeModified':False}
pathlib.Path(sys.argv[4]).write_text(json.dumps(report,indent=2))
print(json.dumps(report,separators=(',',':')))
sys.exit(0 if fresh else 1)
PY
}
recovery_stage=$stage
stage=provider-crash-continuity
if ((continuity_native && continuity_vici)); then
 cp "$work/continuity.sas" "$work/recovered.sas"
 if check_recovered_state > "$work/continuity-state.json"; then :; fi
else
 printf '%s\n' '{"observation":"unknown","newProviderRecoveryNotInferred":true}' > "$work/continuity-state.json"
fi
stage=$recovery_stage
recovered=0
while (( $(date +%s) <= deadline )); do
 traffic_ok=0
 if traffic existing; then traffic_ok=1; fi
 native_ok=0
 if ip -s xfrm state > "$work/$stage.xfrm"; then native_ok=1; fi
 query_ok=0
 if timeout -k 2 5 swanctl --list-sas > "$work/recovered.sas" 2>&1; then query_ok=1; fi
 fresh=0
 if ((native_ok && query_ok)) && check_recovered_state >> "$work/recovery-observations.jsonl"; then fresh=1; fi
 if ((traffic_ok && fresh && $(date +%s) >= earliest_success)); then
  if ((wait_local_expiry)) && ! python3 -c 'import json,sys; assert not json.load(open(sys.argv[1]))["oldSPIsStillInstalled"]' "$work/recovery-state.json"; then
   sleep 1; continue
  fi
  recovered=1; break
 fi
 sleep 1
done
if ((recovered == 0)); then gate_failed=1; fi
peer_observed=0
if peer_control --list-sas > "$work/recovered.peer-sas" 2>&1 && grep -q 'provider-peer:.*ESTABLISHED' "$work/recovered.peer-sas"; then
 peer_observed=1
else gate_failed=1; fi
if ! peer_exec ip -s xfrm state > "$work/recovered.peer-xfrm"; then gate_failed=1; fi
foreign_recovered=0
if timeout -k 2 5 swanctl --list-conns > "$work/recovered.connections" 2>&1; then
 awk '/^fixture-foreign:/ {keep=1} keep && /^[^[:space:]]/ && !/^fixture-foreign:/ {keep=0} keep' "$work/recovered.connections" > "$work/foreign.recovered"
 if cmp "$work/foreign.expected" "$work/foreign.recovered"; then foreign_recovered=1; else gate_failed=1; fi
else gate_failed=1; fi

# Failed continuity/recovery observations remain failures, but do not suppress
# independent withdrawal evidence while our worker has exited and VICI is ready.
worker_stopped=0
if stop_child "$running_router" "$running_birth" router; then
 worker_stopped=1
 running_router=''
else gate_failed=1; fi
stage=post-crash-withdrawal
withdraw_attempted=0 withdraw_ack=0 receipt_empty=0 connection_absent=0 foreign_withdrawn=0 native_withdrawal=0
if ((worker_stopped)) && timeout -k 2 5 swanctl --stats > "$work/pre-withdraw.stats" 2>&1; then
 withdraw_attempted=1
 if product "$work/empty.yaml" > "$work/post-crash-withdraw.log" 2>&1; then withdraw_ack=1; else gate_failed=1; fi
else
 printf '%s\n' 'not attempted: owned worker exit or VICI readiness is unconfirmed' > "$work/post-crash-withdraw.log"
 gate_failed=1
fi
if jq -e '.pending==false and (.connections|length)==0 and (.sharedKeys|length)==0' /usr/local/etc/routerd/swanctl/routerd-ipsec.json >/dev/null; then receipt_empty=1; else gate_failed=1; fi
if timeout -k 2 5 swanctl --list-conns > "$work/post-crash-withdraw.connections" 2>&1; then
 if grep -q routerd-provider-native "$work/post-crash-withdraw.connections"; then gate_failed=1; else connection_absent=1; fi
 awk '/^fixture-foreign:/ {keep=1} keep && /^[^[:space:]]/ && !/^fixture-foreign:/ {keep=0} keep' "$work/post-crash-withdraw.connections" > "$work/foreign.withdrawn"
 if cmp "$work/foreign.expected" "$work/foreign.withdrawn"; then foreign_withdrawn=1; else gate_failed=1; fi
else gate_failed=1; fi
if timeout -k 2 5 swanctl --list-sas > "$work/post-crash-withdraw.sas" 2>&1; then
 if grep -q 'routerd-provider-native:.*ESTABLISHED' "$work/post-crash-withdraw.sas"; then gate_failed=1; fi
else gate_failed=1; fi
if ip -s xfrm state > "$work/post-crash-withdraw.xfrm"; then
 if python3 - "$work/local-expiry-plan.json" "$work/recovery-state.json" "$work/post-crash-withdraw.xfrm" "$work/withdrawal-state.json" "$foreign_withdrawn" <<'PY'
import json,pathlib,re,sys
plan=json.loads(pathlib.Path(sys.argv[1]).read_text())
record=pathlib.Path(sys.argv[2])
recovery=json.loads(record.read_text())if record.exists()else{}
known={x['spi']for x in plan['states']}|set(recovery.get('currentProviderSPIs',[]))
native=pathlib.Path(sys.argv[3]).read_text()
actual=set()
if native.strip()and not re.search(r'^src ',native,re.M):raise ValueError('unexpected native SA output')
for block in re.split(r'(?=^src )',native,flags=re.M):
    if block.split()[:4]not in [['src','198.18.95.1','dst','198.18.95.2'],['src','198.18.95.2','dst','198.18.95.1']]:continue
    spi=re.search(r'proto esp spi (0x[0-9a-f]+)',block)
    if spi:actual.add(hex(int(spi[1],16)))
report={'knownOwnedSPIs':sorted(known),'knownOwnedSPIsStillInstalled':sorted(known.intersection(actual)),'unclassifiedSameEndpointSPIs':sorted(actual-known),'foreignConnectionPreserved':sys.argv[5]=='1'}
pathlib.Path(sys.argv[4]).write_text(json.dumps(report,indent=2))
assert not actual and report['foreignConnectionPreserved'],report
PY
 then native_withdrawal=1; else gate_failed=1; fi
else gate_failed=1; fi
python3 - "$work/recovery-result.json" "$continuity_pass" "$recovered" "$peer_observed" "$foreign_recovered" "$wait_local_expiry" "$worker_stopped" "$withdraw_attempted" "$withdraw_ack" "$receipt_empty" "$connection_absent" "$foreign_withdrawn" "$native_withdrawal" "$gate_failed" <<'PY'
import json,pathlib,sys
names=['ordinaryTrafficContinued','newProviderSPIsWithPackets','peerIKEObserved','foreignPreservedAfterRestart','waitedForRecordedLocalExpiry','workerExitAcknowledged','withdrawalAttempted','withdrawalAcknowledged','receiptEmpty','ownedConnectionAbsent','foreignPreservedAfterWithdrawal','knownAndUnclassifiedNativeSAsAbsent','anyGateFailed']
report={name:value=='1'for name,value in zip(names,sys.argv[2:])}
pathlib.Path(sys.argv[1]).write_text(json.dumps(report,indent=2))
PY
if ((gate_failed)); then stage=completed-with-failures; exit 1; fi
stage=completed
