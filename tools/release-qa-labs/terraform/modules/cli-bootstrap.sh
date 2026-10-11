#!/usr/bin/env bash
# Use existing provider installers; retain each local attempt.
set -euo pipefail
provider="${1:?provider required}"
log_dir="${ROUTERD_LAB_BOOTSTRAP_LOG_DIR:-/var/log/routerd-lab-cli-bootstrap}"
mkdir -p "$log_dir"
work_dir="$(mktemp -d /tmp/routerd-lab-cli-bootstrap.XXXXXXXX)"
cleanup() { rm -rf -- "$work_dir"; }
trap cleanup EXIT
# Upstream installers also invoke apt; retain the existing lock wait there.
printf 'DPkg::Lock::Timeout "300";\n' >"$work_dir/apt.conf"
transient_error() {
  local rc="$1" stdout="$2" stderr="$3"
  case "$rc" in 126|127) return 1 ;; esac
  if grep -Eiq 'Permission denied|No space left|Unsupported (Python|platform)|requires Python|Unable to locate package|NO_PUBKEY|certificate verify failed|unrecognized (argument|option)|404 Not Found|returned error: (400|401|403|404|410)|No module named' "$stdout" "$stderr"; then
    return 1
  fi
  grep -Eiq 'Temporary failure resolving|Temporary failure in name resolution|Could not resolve host|Connection (timed out|refused|reset)|Read timed out|ReadTimeout|Network is unreachable|Could not get lock|Unable to acquire.*lock|Hash Sum mismatch|Failed to fetch|HTTP[^[:digit:]]*(408|429|500|502|503|504)|returned error: (408|429|500|502|503|504)' "$stdout" "$stderr"
}
run_step() {
  local name="$1" maximum="$2"; shift 2
  local attempt rc started finished stdout stderr
  for ((attempt=1; attempt<=maximum; attempt++)); do
    stdout="$log_dir/$name.attempt-$attempt.stdout"
    stderr="$log_dir/$name.attempt-$attempt.stderr"
    started="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    rc=0; "$@" >"$stdout" 2>"$stderr" || rc=$?
    finished="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    cat "$stdout"; cat "$stderr" >&2
    printf 'step=%s attempt=%s started_at=%s finished_at=%s exit=%s\n' "$name" "$attempt" "$started" "$finished" "$rc" | tee "$log_dir/$name.attempt-$attempt.result.txt"
    [ "$rc" -eq 0 ] && return 0
    if [ "$attempt" -eq "$maximum" ] || ! transient_error "$rc" "$stdout" "$stderr"; then return "$rc"; fi
    echo "retrying transient installation failure: step=$name attempt=$attempt" >&2
    sleep "$((attempt * 2))"
  done
}
download() {
  # Native curl policy retries transient responses, not every 4xx.
  run_step "$1-download" 1 curl --retry 5 --retry-delay 2 --retry-connrefused -fsSL "$2" -o "$3" || return "$?"
  sha256sum "$3" | tee "$log_dir/$1-downloaded.sha256"
}
case "$provider" in
  aws) packages=(ca-certificates curl unzip) ;;
  azure) packages=(ca-certificates curl) ;;
  oci) packages=(ca-certificates curl python3 python3-venv) ;;
  *) echo "unsupported provider: $provider" >&2; exit 2 ;;
esac
run_step prerequisites-update 3 env LC_ALL=C apt-get -o DPkg::Lock::Timeout=300 update
run_step prerequisites-install 3 env LC_ALL=C apt-get -o DPkg::Lock::Timeout=300 install -y --no-install-recommends "${packages[@]}"
case "$provider" in
  aws)
    download aws https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip "$work_dir/awscli.zip"
    run_step aws-unzip 1 unzip -q "$work_dir/awscli.zip" -d "$work_dir"
    run_step aws-install 1 "$work_dir/aws/install" --bin-dir /usr/local/bin --install-dir /usr/local/aws-cli --update
    run_step aws-version 1 env PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin aws --version ;;
  azure)
    download azure 'https://azurecliprod.blob.core.windows.net/$root/deb_install.sh' "$work_dir/azure.sh"
    run_step azure-install 3 env LC_ALL=C APT_CONFIG="$work_dir/apt.conf" bash -o pipefail "$work_dir/azure.sh"
    run_step azure-version 1 env PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin az version --output json ;;
  oci)
    download oci https://raw.githubusercontent.com/oracle/oci-cli/master/scripts/install/install.sh "$work_dir/oci.sh"
    run_step oci-install 3 env LC_ALL=C APT_CONFIG="$work_dir/apt.conf" bash "$work_dir/oci.sh" --accept-all-defaults --install-dir /opt/oci-cli --exec-dir /usr/local/bin --script-dir /usr/local/bin
    run_step oci-version 1 env PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin oci --version ;;
esac
