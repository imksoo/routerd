#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu

cd "$(dirname "$0")/.."
go=${GO:-go}
work=$(mktemp -d "${TMPDIR:-/tmp}/routerd15-plugin-process.XXXXXX")
echo "plugin-process-evidence=$work"
git rev-parse HEAD >"$work/revision.txt"
"$go" version >"$work/toolchain.txt"
sha256sum pkg/controller/eventsubscription/process_lifecycle_test.go \
  cmd/routerctl/plugin_dynamic_process_test.go \
  pkg/plugin/runner.go pkg/subprocessio/process_unix.go \
  pkg/controller/eventsubscription/controller.go \
  cmd/routerctl/cmd_plugin.go pkg/state/plugin_publication.go \
  pkg/plugin/source_policy.go pkg/plugin/source_policy_test.go \
  pkg/controller/plugin/controller.go pkg/controller/plugin/controller_test.go \
  pkg/controller/plugin/process_unix_test.go \
  pkg/controller/plugin/publication_wait_test.go \
  pkg/controller/plugin/trigger_review_test.go \
  pkg/controller/chain/chain.go \
  pkg/controller/chain/plugin_trigger_runner_test.go \
  pkg/controller/chain/plugin_trigger_runtime_test.go \
  pkg/controller/framework/framework.go pkg/controller/framework/bootstrap_test.go \
  cmd/routerd/cmd_serve.go \
  pkg/dynamicconfig/merge.go scripts/plugin-dynamic-process-smoke.sh \
  >"$work/source.sha256"

CGO_ENABLED=0 "$go" build -p 1 -o "$work/routerctl" ./cmd/routerctl
sha256sum "$work/routerctl" >"$work/binary.sha256"
CGO_ENABLED=0 "$go" test -p 1 ./pkg/controller/eventsubscription \
  -run '^TestRealPluginStopFailureAndDurableRestart$' -count=1 -v \
  >"$work/subscription.log" 2>&1
cat "$work/subscription.log"
ROUTERD_RUN_PLUGIN_PROCESS=1 ROUTERCTL_BINARY="$work/routerctl" CGO_ENABLED=0 \
  "$go" test -p 1 ./cmd/routerctl -run '^TestPluginDynamicConfigThroughRealBinary$' \
  -count=1 -v >"$work/cli.log" 2>&1
cat "$work/cli.log"
CGO_ENABLED=0 "$go" test -p 1 ./pkg/controller/plugin ./pkg/controller/framework \
  -count=1 -v \
  >"$work/owner.log" 2>&1
cat "$work/owner.log"
CGO_ENABLED=0 "$go" test -p 1 ./pkg/controller/chain \
  -run '^TestDeclaredPlugin' -count=1 -v >"$work/declared.log" 2>&1
cat "$work/declared.log"
sha256sum -c "$work/source.sha256" >"$work/source-check.log"
cat "$work/source-check.log"
echo 'plugin-process-executed-pass=1'
