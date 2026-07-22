#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Opt-in release-artifact qualification for the FreeBSD installer.  It uses a
# disposable VM only: the owned lifecycle exercises the real /usr/local rc.d
# service, while the foreign collision case redirects both service-manager
# commands and the canonical rc.d path into a private prefix.

set -eu

usage() {
  echo "usage: $0 --source DIR" >&2
  exit 2
}

source=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --source)
      shift
      [ "$#" -gt 0 ] || usage
      source=$1
      ;;
    *) usage ;;
  esac
  shift
done
if [ -z "$source" ] || [ ! -d "$source" ]; then
  usage
fi

[ "$(uname -s)" = FreeBSD ] || {
  echo "freebsd package lifecycle smoke must run on FreeBSD" >&2
  exit 1
}

work=$(mktemp -d /var/tmp/routerd-package-lifecycle.XXXXXX)
prior_dir=$work/prior
current_dir=$work/current
foreign_prefix=$work/foreign-prefix
foreign_rcd=$work/foreign-rc.d
fakebin=$work/fakebin
foreign_service=$foreign_rcd/routerd
foreign_before=$work/foreign-before
foreign_manager_log=$work/foreign-service-manager.log
owned_live=0

cleanup() {
  rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "freebsd-package-lifecycle failure rc=$rc" >&2
    for log in prior-install current-upgrade owned-uninstall foreign-install foreign-uninstall cleanup; do
      if [ -f "$work/$log.log" ]; then
        echo "--- $log.log" >&2
        tail -120 "$work/$log.log" >&2 || true
      fi
    done
  fi
  if [ "$owned_live" -eq 1 ]; then
    if [ -x "$current_dir/uninstall.sh" ]; then
      (cd "$current_dir" && ./uninstall.sh --prefix /usr/local --all --yes) >>"$work/cleanup.log" 2>&1 || true
    fi
  fi
  rm -rf "$work"
  exit "$rc"
}
trap cleanup EXIT HUP INT TERM

# Never mutate a pre-existing guest installation.  The isolated foreign case
# below covers the preserve-all contract without touching the real manager.
for path in /usr/local/sbin/routerd /usr/local/etc/rc.d/routerd; do
  if [ -e "$path" ] || [ -L "$path" ]; then
    echo "refusing package lifecycle smoke with pre-existing canonical path: $path" >&2
    exit 1
  fi
done

stage_release() {
  version=$1
  destination=$2
  archive=$work/routerd-${version}-freebsd-amd64.tar.gz
  (
    cd "$source"
    make dist ROUTERD_OS=freebsd GOARCH=amd64 VERSION="$version"
    cp "dist/routerd-${version}-freebsd-amd64.tar.gz" "$archive"
  )
  mkdir -p "$destination"
  tar -C "$destination" -xzf "$archive"
  [ -x "$destination/install.sh" ]
  [ -x "$destination/uninstall.sh" ]
  [ "$(sed -n '1p' "$destination/share/doc/TARGET")" = freebsd-amd64 ]
}

wait_routerd() {
  status=$1
  for _ in $(jot 30); do
    if service routerd onestatus >/dev/null 2>&1 && \
      [ -S /var/run/routerd/routerd-status.sock ] && \
      /usr/local/sbin/routerctl get status --socket /var/run/routerd/routerd-status.sock -o json >"$status" 2>/dev/null; then
      jq -e '.phase == "Healthy"' "$status" >/dev/null && return 0
    fi
    sleep 1
  done
  service routerd status >&2 || true
  if [ -f /var/log/routerd.log ]; then
    tail -100 /var/log/routerd.log >&2 || true
  fi
  return 1
}

mkdir -p /usr/local/etc/routerd
cat >/usr/local/etc/routerd/router.yaml <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: freebsd-package-lifecycle}
spec: {resources: []}
EOF

# Use distinct archives so upgrade executes the same release contract users
# receive, not a copied checkout or a direct binary replacement.
stage_release 0.0.0-package-prior "$prior_dir"
stage_release 0.0.0-package-current "$current_dir"

(
  cd "$prior_dir"
  ROUTERD_INSTALL_PACKAGE_MANAGER=none ./install.sh --prefix /usr/local --no-install-deps --no-config-update --enable-service --start-service
) >"$work/prior-install.log" 2>&1
owned_live=1
wait_routerd "$work/prior-status.json"
grep -Fqx '# routerd-managed-service: v1' /usr/local/etc/rc.d/routerd
[ -s /var/db/routerd/state.db ]
printf 'freebsd-package-prior-install-start-observe=ok\n'

(
  cd "$current_dir"
  ROUTERD_INSTALL_PACKAGE_MANAGER=none ./install.sh --prefix /usr/local --no-install-deps --no-config-update
) >"$work/current-upgrade.log" 2>&1
wait_routerd "$work/current-status.json"
[ -s /var/db/routerd/state.db ]
[ "$(/usr/local/sbin/routerd --version)" = "$(./bin/routerd --version)" ]
printf 'freebsd-package-upgrade-restart-observe=ok\n'

(
  cd "$current_dir"
  ROUTERD_UNINSTALL_FORCE_SERVICE_MANAGER=1 ./uninstall.sh --prefix /usr/local --all --yes
) >"$work/owned-uninstall.log" 2>&1
owned_live=0
for path in /usr/local/sbin/routerd /usr/local/etc/rc.d/routerd /var/run/routerd; do
  if [ -e "$path" ] || [ -L "$path" ]; then
    echo "owned uninstall left path behind: $path" >&2
    exit 1
  fi
done
printf 'freebsd-package-uninstall-owned-cleanup=ok\n'

# Exercise the production foreign-artifact fail-closed branch with a private
# canonical rc.d location and fake service manager.  The file must remain
# byte-for-byte identical and neither install nor uninstall may call service
# or sysrc.
mkdir -p "$foreign_prefix/etc/routerd" "$foreign_rcd" "$fakebin"
printf '%s\n' '# operator-owned routerd service' 'foreign=preserve' >"$foreign_service"
cp "$foreign_service" "$foreign_before"
cat >"$fakebin/service" <<EOF
#!/bin/sh
printf 'service %s\\n' "\$*" >>'$foreign_manager_log'
exit 99
EOF
cat >"$fakebin/sysrc" <<EOF
#!/bin/sh
printf 'sysrc %s\\n' "\$*" >>'$foreign_manager_log'
exit 99
EOF
chmod 0755 "$fakebin/service" "$fakebin/sysrc"
cat >"$foreign_prefix/etc/routerd/router.yaml" <<'EOF'
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: foreign-preservation}
spec: {resources: []}
EOF
(
  cd "$current_dir"
  PATH="$fakebin:$PATH" ROUTERD_INSTALL_PACKAGE_MANAGER=none ROUTERD_INSTALL_FORCE_SERVICE_MANAGER=1 ROUTERD_INSTALL_RCD_DIR="$foreign_rcd" \
    ./install.sh --prefix "$foreign_prefix" --no-install-deps --no-config-update --enable-service --start-service
) >"$work/foreign-install.log" 2>&1
cmp -s "$foreign_before" "$foreign_service"
[ ! -s "$foreign_manager_log" ]
(
  cd "$current_dir"
  PATH="$fakebin:$PATH" ROUTERD_UNINSTALL_FORCE_SERVICE_MANAGER=1 ROUTERD_UNINSTALL_RCD_DIR="$foreign_rcd" \
    ./uninstall.sh --prefix "$foreign_prefix" --all --yes
) >"$work/foreign-uninstall.log" 2>&1
cmp -s "$foreign_before" "$foreign_service"
[ ! -s "$foreign_manager_log" ]
printf 'freebsd-package-foreign-service-preservation=ok\n'
printf 'freebsd-package-lifecycle=ok\n'
