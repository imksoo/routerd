// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// azRunner runs one `az` CLI invocation with the given argv (without the leading
// "az") and returns its stdout. It is the single injectable seam so unit tests
// substitute a fake that records argv and returns canned JSON, NEVER calling real
// Azure. The production implementation (execRunner) execs the real `az` binary,
// which resolves the managed identity on its own; routerd passes NO credentials.
type azRunner func(ctx context.Context, argv ...string) ([]byte, error)

// defaultRunner returns the production runner that execs the real `az` binary.
// This is the ONLY use of os/exec in the executor, and it runs only `az`.
func defaultRunner() azRunner { return execRunner }

// execRunner execs `az <argv...> --output json`. The plugin runs in routerd's
// isolated executor environment (no inherited parent env beyond PATH + the
// plugin's own spec.Env), so `az` authenticates with the managed identity, not
// from routerd. --output json forces machine-readable output.
func execRunner(ctx context.Context, argv ...string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout())
	defer cancel()
	full := append([]string(nil), argv...)
	full = append(full, "--output", "json")
	cmd := exec.CommandContext(runCtx, "az", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("az %s: %w: %s", strings.Join(full, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// guardedRunner wraps a runner so that ONLY read-only show/list verbs may be
// issued. It is applied to every dry-run dispatch so a coding mistake cannot
// mutate Azure during a non-destructive preview: any non-read-only verb is
// refused before the underlying runner is invoked. The az argv shape is
// "<group...> <verb> ...", so the verb is the last leading non-flag token.
func guardedRunner(inner azRunner) azRunner {
	return func(ctx context.Context, argv ...string) ([]byte, error) {
		if !isReadOnlyVerb(argv) {
			return nil, fmt.Errorf("dry-run guard: refusing non-read-only az command %q (only *show/*list permitted in dry-run)", strings.Join(leadingTokens(argv), " "))
		}
		return inner(ctx, argv...)
	}
}

// leadingTokens returns the non-flag tokens at the front of argv (the command
// group/verb words before any "--flag").
func leadingTokens(argv []string) []string {
	var out []string
	for _, a := range argv {
		if strings.HasPrefix(a, "-") {
			break
		}
		out = append(out, a)
	}
	return out
}

// isReadOnlyVerb reports whether the az command is a read-only show/list. The
// verb is the last leading non-flag token (e.g. ["network","nic","show"] ->
// show).
func isReadOnlyVerb(argv []string) bool {
	toks := leadingTokens(argv)
	if len(toks) == 0 {
		return false
	}
	verb := toks[len(toks)-1]
	return verb == "show" || verb == "list" ||
		strings.HasSuffix(verb, "-show") || strings.HasSuffix(verb, "-list")
}

// nicShow is the subset of `az network nic show` output the executor reads.
type nicShow struct {
	EnableIPForwarding bool
}

// nicShowOutput mirrors the JSON shape of `az network nic show`.
type nicShowOutput struct {
	EnableIPForwarding bool `json:"enableIPForwarding"`
}

// showNIC runs the read-only `az network nic show --ids <nic>` call and parses
// it. This is the read-only verb used for dry-run preview AND for the
// execute-time prior capture.
func showNIC(ctx context.Context, runner azRunner, nicID string) (nicShow, error) {
	out, err := runner(ctx, "network", "nic", "show", "--ids", nicID)
	if err != nil {
		return nicShow{}, err
	}
	var parsed nicShowOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nicShow{}, fmt.Errorf("parse network nic show output: %w", err)
	}
	return nicShow{EnableIPForwarding: parsed.EnableIPForwarding}, nil
}
