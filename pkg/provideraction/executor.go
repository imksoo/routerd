// SPDX-License-Identifier: BSD-3-Clause

package provideraction

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/plugin"
)

const (
	defaultExecutorTimeout = 30 * time.Second
	maxCapturedStderrLen   = 8192
	defaultExecutorPathEnv = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
)

// RunOutcome captures the executor process telemetry (distinct from the parsed
// ExecuteActionResult), mirroring pkg/plugin.RunOutcome.
type RunOutcome struct {
	ExitCode    int    `json:"exitCode,omitempty" yaml:"exitCode,omitempty"`
	HasExitCode bool   `json:"hasExitCode,omitempty" yaml:"hasExitCode,omitempty"`
	DurationMs  int64  `json:"durationMs" yaml:"durationMs"`
	Stderr      string `json:"stderr,omitempty" yaml:"stderr,omitempty"`
	Error       string `json:"error,omitempty" yaml:"error,omitempty"`
}

// ExecutorRunner is the injectable seam the engine uses to launch an executor.
// The production implementation is RunExecutor; tests substitute a fake.
type ExecutorRunner func(ctx context.Context, spec api.PluginSpec, req ExecuteActionRequest) (ExecuteActionResult, RunOutcome, error)

// RunExecutor launches an executor plugin and exchanges the wire protocol with
// it. This is the ONLY place in routerd an executor process is launched.
//
// Guards (refusals never start the process):
//   - the plugin executable must validate (pkg/plugin.ValidateExecutable);
//   - spec.Capabilities MUST include execute.providerAction;
//   - req.Spec.Mode MUST be "dry-run" or "execute".
//
// Credential isolation: the process inherits NO parent environment. Only PATH
// (so the executor can find basic tools) plus the plugin's own spec.Env are
// passed. routerd writes the request (no secrets) to stdin and reads the result
// from stdout. The executor authenticates itself with its own cloud-native
// identity; routerd hands it no credentials.
func RunExecutor(ctx context.Context, spec api.PluginSpec, req ExecuteActionRequest) (ExecuteActionResult, RunOutcome, error) {
	var result ExecuteActionResult

	if !hasCapability(spec.Capabilities, CapabilityExecuteProviderAction) {
		err := fmt.Errorf("executor refused: plugin lacks capability %q", CapabilityExecuteProviderAction)
		return result, RunOutcome{Error: err.Error()}, err
	}
	if !validMode(req.Spec.Mode) {
		err := fmt.Errorf("executor refused: mode %q must be %q or %q", req.Spec.Mode, ModeDryRun, ModeExecute)
		return result, RunOutcome{Error: err.Error()}, err
	}
	if err := plugin.ValidateExecutable(spec.Executable); err != nil {
		return result, RunOutcome{Error: err.Error()}, err
	}
	timeout, err := executorTimeout(spec.Timeout)
	if err != nil {
		return result, RunOutcome{Error: err.Error()}, err
	}

	// Ensure the protocol envelope is set even if the caller passed a bare spec.
	if req.APIVersion == "" {
		req.APIVersion = ProtocolAPIVersion
	}
	if req.Kind == "" {
		req.Kind = KindExecuteActionRequest
	}

	stdin, err := json.Marshal(req)
	if err != nil {
		return result, RunOutcome{Error: err.Error()}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	started := time.Now()
	cmd := exec.CommandContext(runCtx, spec.Executable)
	cmd.Env = executorEnvironment(spec.Env)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	duration := time.Since(started)

	outcome := RunOutcome{
		DurationMs: duration.Milliseconds(),
		Stderr:     truncateString(stderr.String(), maxCapturedStderrLen),
	}
	if cmd.ProcessState != nil {
		outcome.ExitCode = cmd.ProcessState.ExitCode()
		outcome.HasExitCode = true
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		err := fmt.Errorf("executor %s timed out after %s", spec.Executable, timeout)
		outcome.Error = err.Error()
		return result, outcome, err
	}
	if runErr != nil {
		err := fmt.Errorf("executor %s failed: %w", spec.Executable, runErr)
		outcome.Error = err.Error()
		return result, outcome, err
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		err = fmt.Errorf("decode executor %s stdout: %w", spec.Executable, err)
		outcome.Error = err.Error()
		return result, outcome, err
	}
	switch result.Status.Status {
	case ResultSucceeded, ResultFailed, ResultSkipped:
	default:
		err := fmt.Errorf("executor %s returned invalid status %q", spec.Executable, result.Status.Status)
		outcome.Error = err.Error()
		return result, outcome, err
	}
	return result, outcome, nil
}

func executorTimeout(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultExecutorTimeout, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("executor timeout must be a valid duration: %w", err)
	}
	if d <= 0 {
		return 0, errors.New("executor timeout must be greater than 0")
	}
	return d, nil
}

// executorEnvironment intentionally does not inherit the parent environment so
// routerd's own environment (which may carry secrets) never leaks to an
// executor. PATH is preserved; spec.Env is layered on top for the executor's
// own site-local identity configuration.
func executorEnvironment(extra map[string]string) []string {
	env := map[string]string{
		"PATH": firstNonEmpty(os.Getenv("PATH"), defaultExecutorPathEnv),
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		if key == "" || strings.Contains(key, "=") {
			continue
		}
		env[key] = value
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func truncateString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
