#!/usr/bin/env bash
# Local Git fixtures only: no network or host-network mutations.
set -euo pipefail
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
detector="$script_dir/detect-quality-changes.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_AUTHOR_NAME='Quality test' GIT_COMMITTER_NAME='Quality test'
export GIT_AUTHOR_EMAIL='quality@example.invalid' GIT_COMMITTER_EMAIL='quality@example.invalid'
export GIT_TERMINAL_PROMPT=0
export GITHUB_OUTPUT="$work/output"

commit() {
  git add -A
  git commit -qm "$1"
}
check() {
  local name=$1 expected=$2
  shift 2
  : > "$GITHUB_OUTPUT"
  env "$@" bash "$detector"
  local actual
  actual=$(cat "$GITHUB_OUTPUT")
  if [[ "$actual" != "$expected" ]]; then
    printf 'FAIL: %s\nExpected:\n%s\nActual:\n%s\n' "$name" "$expected" "$actual" >&2
    exit 1
  fi
  printf 'PASS: %s\n' "$name"
}
check_failure() {
  local name=$1
  shift
  : > "$GITHUB_OUTPUT"
  if env "$@" bash "$detector" > "$work/stdout" 2> "$work/stderr"; then
    echo "FAIL: $name unexpectedly succeeded" >&2
    exit 1
  fi
  if [[ -s "$GITHUB_OUTPUT" ]]; then
    echo "FAIL: $name published filter outputs after failure" >&2
    exit 1
  fi
  printf 'PASS: %s\n' "$name"
}
none=$'docs=false\nwebsite=false\nwebconsole=false'
all=$'docs=true\nwebsite=true\nwebconsole=true'
docs_only=$'docs=true\nwebsite=false\nwebconsole=false'
console_only=$'docs=false\nwebsite=false\nwebconsole=true'

mkdir "$work/source"
cd "$work/source"
git init -q -b main
mkdir -p docs website webconsole
printf 'initial\n' > docs/guide.md
printf 'initial\n' > website/index.html
printf 'initial\n' > webconsole/app.ts
commit initial
initial=$(git rev-parse HEAD)
check 'new branch includes root-commit paths' "$all" EVENT_NAME=push BEFORE_SHA=0000000000000000000000000000000000000000
check 'dispatch checks the complete tree' "$all" EVENT_NAME=workflow_dispatch

printf 'changed\n' >> webconsole/app.ts
commit console
printf 'unrelated\n' > README.md
commit readme
printf 'last commit\n' >> README.md
commit readme-again
tip=$(git rev-parse HEAD)
# file:// makes --depth effective, unlike a plain local-path clone.
git clone -q --depth=2 "file://$work/source" "$work/shallow"
cd "$work/shallow"
[[ $(git rev-parse --is-shallow-repository) == true ]]
check 'shallow multi-commit push includes earlier console edit' "$console_only" EVENT_NAME=push BEFORE_SHA="$initial"
# Prove this fixture reproduces the old three-dot failure.
if git merge-base "$initial" HEAD > /dev/null; then
  echo 'FAIL: shallow fixture unexpectedly has a merge base' >&2
  exit 1
fi
check 'unchanged push emits false filters' "$none" EVENT_NAME=push BEFORE_SHA="$tip"
check_failure 'missing push baseline fails closed' EVENT_NAME=push BEFORE_SHA=1111111111111111111111111111111111111111

cd "$work/source"
git checkout -qb topic "$initial"
printf 'PR docs\n' >> docs/guide.md
commit pr-docs
pr_head=$(git rev-parse HEAD)
# The main branch has unrelated console edits. Its synthetic merge must not
# make those edits part of the pull request's changed-file set.
git checkout -q main
git merge -q --no-ff topic -m 'synthetic PR merge'
check 'PR uses merge base and actual head, not synthetic checkout' "$docs_only" EVENT_NAME=pull_request PR_BASE_SHA="$tip" PR_HEAD_SHA="$pr_head"
git checkout -q topic
check 'non-fast-forward push compares trees' $'docs=true\nwebsite=false\nwebconsole=true' EVENT_NAME=push BEFORE_SHA="$tip"
git checkout -q main
check_failure 'missing PR head fails closed' EVENT_NAME=pull_request PR_BASE_SHA="$tip" PR_HEAD_SHA=1111111111111111111111111111111111111111
check_failure 'missing PR event metadata fails closed' EVENT_NAME=pull_request PR_BASE_SHA="$tip" PR_HEAD_SHA=

cd "$work/shallow"
# Both endpoints exist, but the shallow graph has no common ancestor.
check_failure 'PR without merge base fails closed' EVENT_NAME=pull_request PR_BASE_SHA="$initial" PR_HEAD_SHA="$tip"

cd "$work/source"
before=$(git rev-parse HEAD)
mkdir -p docs/releases website/i18n/ja/releases
printf 'release\n' > docs/releases/changelog.md
printf 'release\n' > website/i18n/ja/releases/changelog.md
commit changelogs
check 'changelog-only push preserves exclusions' "$none" EVENT_NAME=push BEFORE_SHA="$before"

before=$(git rev-parse HEAD)
git mv webconsole/app.ts app.ts
git rm -q website/index.html
# NUL-delimited paths must still match the docs prefix.
printf 'newline path\n' > $'docs/guide\nextra.md'
commit moved-deleted
check 'renames, deletions and newline paths retain coverage' "$all" EVENT_NAME=push BEFORE_SHA="$before"

before=$(git rev-parse HEAD)
mkdir -p scripts/ci
printf '# changed detector\n' > scripts/ci/detect-quality-changes.sh
commit detector
check 'detector changes enable all filtered jobs' "$all" EVENT_NAME=push BEFORE_SHA="$before"

echo 'All quality change detection tests passed.'
