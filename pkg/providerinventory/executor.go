// SPDX-License-Identifier: BSD-3-Clause

package providerinventory

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
	defaultInventoryTimeout = 30 * time.Second
	maxCapturedStderrLen    = 8192
	defaultInventoryPathEnv = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
)

type RunOutcome struct {
	ExitCode    int    `json:"exitCode,omitempty" yaml:"exitCode,omitempty"`
	HasExitCode bool   `json:"hasExitCode,omitempty" yaml:"hasExitCode,omitempty"`
	DurationMs  int64  `json:"durationMs" yaml:"durationMs"`
	Stderr      string `json:"stderr,omitempty" yaml:"stderr,omitempty"`
	Error       string `json:"error,omitempty" yaml:"error,omitempty"`
}

type Runner func(ctx context.Context, spec api.PluginSpec, req ObservePrivateIPsRequest) (ObservePrivateIPsResult, RunOutcome, error)

func RunInventory(ctx context.Context, spec api.PluginSpec, req ObservePrivateIPsRequest) (ObservePrivateIPsResult, RunOutcome, error) {
	var result ObservePrivateIPsResult
	if !hasCapability(spec.Capabilities, CapabilityObserveProviderPrivateIPs) {
		err := fmt.Errorf("provider inventory refused: plugin lacks capability %q", CapabilityObserveProviderPrivateIPs)
		return result, RunOutcome{Error: err.Error()}, err
	}
	if err := plugin.ValidateExecutable(spec.Executable); err != nil {
		return result, RunOutcome{Error: err.Error()}, err
	}
	timeout, err := inventoryTimeout(spec.Timeout)
	if err != nil {
		return result, RunOutcome{Error: err.Error()}, err
	}
	if req.APIVersion == "" {
		req.APIVersion = ProtocolAPIVersion
	}
	if req.Kind == "" {
		req.Kind = KindObservePrivateIPsRequest
	}
	stdin, err := json.Marshal(req)
	if err != nil {
		return result, RunOutcome{Error: err.Error()}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	started := time.Now()
	cmd := exec.CommandContext(runCtx, spec.Executable)
	cmd.Env = inventoryEnvironment(spec.Env)
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
		err := fmt.Errorf("provider inventory %s timed out after %s", spec.Executable, timeout)
		outcome.Error = err.Error()
		return result, outcome, err
	}
	if runErr != nil {
		err := fmt.Errorf("provider inventory %s failed: %w", spec.Executable, runErr)
		outcome.Error = err.Error()
		return result, outcome, err
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		err = fmt.Errorf("decode provider inventory %s stdout: %w", spec.Executable, err)
		outcome.Error = err.Error()
		return result, outcome, err
	}
	switch result.Status.Status {
	case ResultSucceeded, ResultFailed, ResultSkipped:
	default:
		err := fmt.Errorf("provider inventory %s returned invalid status %q", spec.Executable, result.Status.Status)
		outcome.Error = err.Error()
		return result, outcome, err
	}
	return result, outcome, nil
}

func inventoryTimeout(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultInventoryTimeout, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("provider inventory timeout must be a valid duration: %w", err)
	}
	if d <= 0 {
		return 0, errors.New("provider inventory timeout must be greater than 0")
	}
	return d, nil
}

func inventoryEnvironment(extra map[string]string) []string {
	env := map[string]string{
		"PATH": firstNonEmpty(os.Getenv("PATH"), defaultInventoryPathEnv),
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func truncateString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}
