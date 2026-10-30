#!/bin/sh
# SPDX-License-Identifier: BSD-3-Clause
set -eu

cd "$(dirname "$0")/.."
go=${GO:-go}
work=$(mktemp -d "${TMPDIR:-/tmp}/routerd15-plugin-process.XXXXXX")
echo "plugin-process-evidence=$work"
git rev-parse HEAD >"$work/revision.txt"
sha256sum pkg/controller/eventsubscription/process_lifecycle_test.go \
  cmd/routerctl/plugin_dynamic_process_test.go \
  pkg/plugin/runner.go pkg/subprocessio/process_unix.go \
  pkg/controller/eventsubscription/controller.go \
  cmd/routerctl/cmd_plugin.go pkg/state/plugin_publication.go \
  pkg/dynamicconfig/merge.go >"$work/source.sha256"

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
sha256sum -c "$work/source.sha256"
echo 'plugin-process-executed-pass=1'
