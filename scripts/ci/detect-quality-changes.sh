#!/usr/bin/env bash
# Emit quality-job filters only after change detection has completed successfully.
set -euo pipefail

: "${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"
changed_files=$(mktemp)
trap 'rm -f "$changed_files"' EXIT

case "${EVENT_NAME:-}" in
  pull_request)
    : "${PR_BASE_SHA:?PR_BASE_SHA is required for pull requests}"
    : "${PR_HEAD_SHA:?PR_HEAD_SHA is required for pull requests}"
    # Compare the PR's changes, not unrelated changes on its base branch or
    # the synthetic merge commit checked out by actions/checkout.
    git diff --no-renames --name-only -z "$PR_BASE_SHA...$PR_HEAD_SHA" -- > "$changed_files"
    ;;
  push)
    if [[ -n "${BEFORE_SHA:-}" && "$BEFORE_SHA" != 0000000000000000000000000000000000000000 ]]; then
      # A force push can leave the old tip outside the fetched branch history.
      if ! git cat-file -e "$BEFORE_SHA^{commit}" 2>/dev/null; then
        git fetch --no-tags --depth=1 origin "$BEFORE_SHA"
      fi
      # Pushes compare endpoint trees; no merge base is needed, even for a
      # multi-commit shallow checkout or a non-fast-forward push.
      git diff --no-renames --name-only -z "$BEFORE_SHA" HEAD -- > "$changed_files"
    else
      # New branches/tags have no prior tree. Check every tracked path.
      git ls-tree -r --name-only -z HEAD > "$changed_files"
    fi
    ;;
  *)
    # Dispatch/reusable release runs have no reliable change baseline.
    git ls-tree -r --name-only -z HEAD > "$changed_files"
    ;;
esac

docs=false
website=false
webconsole=false
while IFS= read -r -d '' path; do
  case "$path" in
    .github/workflows/quality.yaml|scripts/ci/detect-quality-changes.sh|scripts/ci/detect-quality-changes-test.sh)
      docs=true
      website=true
      webconsole=true
      ;;
    docs/releases/changelog.md|website/i18n/*/releases/changelog.md)
      ;;
    docs/*)
      docs=true
      ;;
    website/*)
      website=true
      ;;
    Makefile|webconsole/*|pkg/webconsole/static/*)
      webconsole=true
      ;;
  esac
done < "$changed_files"

{
  echo "docs=$docs"
  echo "website=$website"
  echo "webconsole=$webconsole"
} >> "$GITHUB_OUTPUT"
