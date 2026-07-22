#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
#
# Run ce296e68^ only inside an isolated native fixture.  This does not alter
# the production binary: it exists solely to classify the pre/post first IPv6
# packet console boundary that caused the original native SSH loss.
set -eu

source=
evidence=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --source) source=${2:?missing source}; shift 2 ;;
    --evidence-dir) evidence=${2:?missing evidence directory}; shift 2 ;;
    *) echo 'usage: --source DIR --evidence-dir DIR' >&2; exit 2 ;;
  esac
done

[ "$(uname -s)" = FreeBSD ] || exit 2
[ "$(id -u)" -eq 0 ] || exit 2
[ -d "$source/.git" ] || { echo 'source checkout is required' >&2; exit 2; }
[ -n "$evidence" ] || exit 2
mkdir -p "$evidence"

work=$(mktemp -d /tmp/routerd-ipv6-route-to-candidate.XXXXXX)
candidate="$work/source"
shim="$work/shim"
marker="$evidence/before-first-ipv6-packet.marker"
cleanup() {
  rc=$?
  if [ -d "$candidate" ]; then
    git -C "$source" worktree remove --force "$candidate" >"$evidence/worktree-cleanup.log" 2>&1 || true
  fi
  rm -rf "$work"
  exit "$rc"
}
trap cleanup EXIT HUP INT TERM

git -C "$source" worktree add --detach "$candidate" ce296e68^ >"$evidence/worktree.log" 2>&1
(cd "$candidate" && GOFLAGS="${GOFLAGS:+$GOFLAGS }-buildvcs=false" go build -o "$work/routerd-ipv6-candidate" ./cmd/routerd) >"$evidence/build.log" 2>&1
git -C "$candidate" show ce296e68^:scripts/freebsd-vnet-policyroute-smoke.sh >"$work/candidate-smoke.sh"
chmod 0700 "$work/candidate-smoke.sh"
mkdir -p "$shim"
cat >"$shim/jexec" <<'EOF'
#!/bin/sh
marker=${ROUTERD_IPV6_CANDIDATE_MARKER:?}
evidence=${ROUTERD_IPV6_CANDIDATE_EVIDENCE:?}
if [ "$#" -ge 2 ] && [ "$2" = ping6 ] && [ ! -e "$marker" ]; then
  printf 'routerd-ipv6-candidate marker=before-first-ipv6-packet\n' | tee "$marker" >/dev/console
  /usr/sbin/jexec "$@"
  rc=$?
  printf 'routerd-ipv6-candidate marker=after-first-ipv6-packet rc=%s\n' "$rc" | tee "$evidence/after-first-ipv6-packet.marker" >/dev/console
  exit "$rc"
fi
exec /usr/sbin/jexec "$@"
EOF
chmod 0700 "$shim/jexec"
cat >"$shim/pfctl" <<'EOF'
#!/bin/sh
# The historical candidate predates the anyvm control interface.  Exclude only
# the anyvm control NIC from PF so the candidate's generated route-to rules
# remain evaluated on its disposable epair topology.
evidence=${ROUTERD_IPV6_CANDIDATE_EVIDENCE:?}
if [ "$#" -eq 2 ] && [ "$1" = -f ] && [ "${2##*/}" = pf.conf ]; then
  guarded="$2.console-control"
  {
    printf '%s\n' 'set skip on vtnet0'
    cat "$2"
  } >"$guarded"
  printf 'routerd-ipv6-candidate fixture=control-interface-skipped\n' >"$evidence/control-rule.log"
  exec /sbin/pfctl -f "$guarded"
fi
exec /sbin/pfctl "$@"
EOF
chmod 0700 "$shim/pfctl"
printf 'routerd-ipv6-candidate marker=fixture-ready\n' | tee "$evidence/fixture-ready.marker" >/dev/console
PATH="$shim:$PATH" ROUTERD_IPV6_CANDIDATE_MARKER="$marker" ROUTERD_IPV6_CANDIDATE_EVIDENCE="$evidence" \
  "$work/candidate-smoke.sh" --routerd "$work/routerd-ipv6-candidate" --evidence-dir "$evidence"
test -s "$marker"
test -s "$evidence/after-first-ipv6-packet.marker"
printf 'ipv6-route-to-candidate=completed-with-console-markers\n' >"$evidence/summary.log"
printf 'freebsd-ipv6-route-to-candidate=completed\n' >"$evidence/result"
