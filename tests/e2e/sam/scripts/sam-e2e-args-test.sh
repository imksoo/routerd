#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
harness="$script_dir/sam-e2e.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

tofu_output="$tmp/tofu-output.json"
artifact="$tmp/routerd.tar.gz"
ssh_key="$tmp/lab.key"
mkdir -p "$tmp/configs" "$tmp/secrets"
: >"$artifact"
ssh-keygen -q -t ed25519 -N "" -f "$ssh_key"
cat >"$tofu_output" <<'JSON'
{
  "nodes": {
    "value": {
      "aws-rr-a": {
        "role": "rr",
        "site": "aws",
        "public_ip": "127.0.0.1"
      },
      "aws-leaf-a": {
        "role": "leaf",
        "site": "aws",
        "public_ip": "127.0.0.1"
      },
      "aws-client-a": {
        "role": "client",
        "site": "aws",
        "public_ip": "127.0.0.1"
      },
      "azure-client-a": {
        "role": "client",
        "site": "azure",
        "public_ip": "127.0.0.1"
      }
    }
  },
  "fabric": { "value": {} }
}
JSON

assert_help_mentions_secrets_dir() {
  "$harness" --help | grep -q -- '--secrets-dir DIR'
}

assert_missing_dir_fails() {
  local stderr="$tmp/missing-dir.stderr"
  if "$harness" \
    --tofu-output "$tofu_output" \
    --artifact "$artifact" \
    --evidence-dir "$tmp/evidence" \
    --ssh-key "$ssh_key" \
    --configs-dir "$tmp/missing-configs" \
    --skip-deploy >"$tmp/missing-dir.stdout" 2>"$stderr"; then
    echo "expected missing --configs-dir to fail" >&2
    exit 1
  fi
  grep -q 'configs dir not found:' "$stderr"
}

assert_existing_dirs_are_accepted_until_preflight() {
  local stdout="$tmp/existing-dirs.stdout"
  local stderr="$tmp/existing-dirs.stderr"
  if "$harness" \
    --tofu-output "$tofu_output" \
    --artifact "$artifact" \
    --evidence-dir "$tmp/evidence-existing" \
    --ssh-key "$ssh_key" \
    --configs-dir "$tmp/configs" \
    --secrets-dir "$tmp/secrets" \
    --skip-deploy >"$stdout" 2>"$stderr"; then
    echo "expected offline harness run to stop before successful SSH preflight" >&2
    exit 1
  fi
  if grep -q 'configs dir not found:\\|secrets dir not found:' "$stderr"; then
    echo "existing config/secrets dirs were rejected unexpectedly" >&2
    cat "$stderr" >&2
    exit 1
  fi
  grep -q 'configs_dir=' "$tmp/evidence-existing/run-note.txt"
  grep -q 'secrets_dir=' "$tmp/evidence-existing/run-note.txt"
}

assert_help_mentions_secrets_dir
assert_missing_dir_fails
assert_existing_dirs_are_accepted_until_preflight

echo "sam-e2e-args-test PASS"
