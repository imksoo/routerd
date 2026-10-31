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
 timeout -k 5 300 "$routerd" serve --once --controllers package,ipsec --config "$1" \
  --state-file "$work/state.db" --ledger-file "$work/ledger.db" --status-file "$work/status.json" \
  --socket "$work/control.sock" --status-socket "$work/status.sock"
}
stage=preparation peer='' holder='' running_router='' peer_birth='' holder_birth='' running_birth='' stopped=0 link_created=0 address_created=0
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
 stop_child "$running_router" "$running_birth" router || failures=1
 if [[ -S /run/charon.vici && -f /usr/local/etc/routerd/swanctl/routerd-ipsec.json ]]; then
  product "$work/empty.yaml" > "$work/cleanup-withdraw.log" 2>&1 || failures=1
 fi
 stop_child "$peer" "$peer_birth" peer || failures=1
 stop_child "$holder" "$holder_birth" namespace || failures=1
 if [[ -f $work/provider-started ]]; then
  systemctl stop strongswan.service || failures=1
  # Remove only this gate's initially absent packages, never autoremove.
  apt-get -y --no-auto-remove purge "${packages[@]}" > "$work/package-remove.log" 2>&1 || failures=1
 fi
 if ((link_created)); then ip link del rd-provider || failures=1; fi
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
unshare -n sleep 600 & holder=$!
holder_birth=$(birth "$holder")
for _ in {1..50}; do [[ $(readlink /proc/"$holder"/ns/net) != "$(readlink /proc/self/ns/net)" ]] && break; sleep .1; done
[[ $(readlink /proc/"$holder"/ns/net) != "$(readlink /proc/self/ns/net)" ]]
ip link add rd-provider type veth peer name rd-peer netns "$holder"
link_created=1
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
stage=fresh-package-provider
touch "$work/provider-started"
product "$work/router.yaml" > "$work/fresh-package-provider.log" 2>&1
systemctl is-active --quiet strongswan.service
systemctl show strongswan.service -p MainPID -p InvocationID -p FragmentPath > "$work/provider.first"
swanctl --stats > "$work/provider.stats" 2>&1
cp /usr/local/etc/routerd/swanctl/routerd-ipsec.json "$work/receipt.acquired.json"
mkdir "$work/peer-run"
cat > "$work/peer-strongswan.conf" <<'EOF'
charon {
 load_modular = no
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
  local_addrs = 198.18.95.2
  remote_addrs = 198.18.95.1
  proposals = aes256-sha256-modp2048
  local { auth = psk }
  remote { auth = psk }
  children { net { local_ts = 10.254.95.2/32
   remote_ts = 10.254.95.1/32
   esp_proposals = aes256-sha256
   start_action = trap
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
 timeout -k 2 25 swanctl --initiate --ike routerd-provider-native --child net --timeout 15 > "$work/$stage.initiate" 2>&1
 ping -n -I 10.254.95.1 -c 2 -W 2 10.254.95.2 > "$work/$stage.forward"
 peer_exec ping -n -I 10.254.95.2 -c 2 -W 2 10.254.95.1 > "$work/$stage.reverse"
 ip -s xfrm state > "$work/$stage.xfrm"
 grep -Eq '[1-9][0-9]*\(packets\)' "$work/$stage.xfrm"
}
stage=initial-traffic
traffic
cat > "$work/foreign.conf" <<'EOF'
connections {
 fixture-foreign {
  local_addrs = 198.18.95.1
  remote_addrs = 198.18.95.254
  local { auth = psk }
  remote { auth = psk }
 }
}
EOF
swanctl --load-conns --file "$work/foreign.conf" > "$work/foreign-load.log" 2>&1
stage=unchanged-provider
product "$work/router.yaml" > "$work/unchanged.log" 2>&1
systemctl show strongswan.service -p MainPID -p InvocationID -p FragmentPath > "$work/provider.unchanged"
cmp "$work/provider.first" "$work/provider.unchanged"
traffic
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
swanctl --unload-conn --name fixture-foreign > "$work/foreign-unload.log" 2>&1
stage=provider-crash-recovery
"$routerd" serve --controllers package,ipsec --config "$work/router.yaml" \
 --state-file "$work/state.db" --ledger-file "$work/ledger.db" --status-file "$work/status.json" \
 --socket "$work/control.sock" --status-socket "$work/status.sock" --graceful-stop-timeout 0 > "$work/router-daemon.log" 2>&1 & running_router=$!
running_birth=$(birth "$running_router")
for _ in {1..50}; do [[ -S $work/control.sock ]] && break; kill -0 "$running_router"; sleep .2; done
[[ -S $work/control.sock ]]
old_pid=$(systemctl show strongswan.service -p MainPID --value)
[[ $old_pid != 0 ]]
systemctl kill --kill-whom=main --signal=SIGKILL strongswan.service
for _ in {1..60}; do
 new_pid=$(systemctl show strongswan.service -p MainPID --value)
 if [[ $new_pid != 0 && $new_pid != "$old_pid" ]] && swanctl --list-conns > "$work/recovered.connections" 2>&1 && grep -q routerd-provider-native "$work/recovered.connections"; then break; fi
 kill -0 "$running_router"; sleep .5
done
[[ $new_pid != 0 && $new_pid != "$old_pid" ]]
grep -q routerd-provider-native "$work/recovered.connections"
traffic
stop_child "$running_router" "$running_birth" router
running_router=''
stage=completed
