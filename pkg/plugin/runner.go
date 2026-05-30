// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/imksoo/routerd/pkg/api"
)

const (
	defaultTimeout       = 10 * time.Second
	maxCapturedStderrLen = 8192
	defaultPathEnv       = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
)

type RunOptions struct {
	Now                 time.Time
	PreviousGeneration  int64
	StartupConfigHash   string
	EffectiveGeneration int64
	Trigger             TriggerRef
	// Events are the matched federation events delivered to the plugin on
	// stdin (EventSubscriptionController). Empty for non-subscription triggers.
	Events []PluginMatchedEvent
	// Context is the least-privilege, secret-redacted config the plugin may
	// read (Phase 4.0). The caller builds it via BuildPluginContext from the
	// Plugin's spec.context allowlist; empty = default-deny (no config passed).
	Context PluginContext
}

type RunOutcome struct {
	ExitCode     int    `json:"exitCode,omitempty" yaml:"exitCode,omitempty"`
	HasExitCode  bool   `json:"hasExitCode,omitempty" yaml:"hasExitCode,omitempty"`
	DurationMs   int64  `json:"durationMs" yaml:"durationMs"`
	StdoutDigest string `json:"stdoutDigest" yaml:"stdoutDigest"`
	Stderr       string `json:"stderr,omitempty" yaml:"stderr,omitempty"`
	Error        string `json:"error,omitempty" yaml:"error,omitempty"`
}

func Run(ctx context.Context, spec api.PluginSpec, name string, opts RunOptions) (PluginResult, RunOutcome, error) {
	var result PluginResult
	if err := ValidateExecutable(spec.Executable); err != nil {
		outcome := RunOutcome{Error: err.Error()}
		return result, outcome, err
	}
	timeout, err := pluginTimeout(spec.Timeout)
	if err != nil {
		outcome := RunOutcome{Error: err.Error()}
		return result, outcome, err
	}
	now := opts.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	request := PluginRequest{
		TypeMeta: api.TypeMeta{APIVersion: PluginAPIVersion, Kind: "PluginRequest"},
		Metadata: api.ObjectMeta{
			Name: name,
		},
		Spec: PluginRequestSpec{
			Trigger:                   opts.Trigger,
			StartupConfigHash:         opts.StartupConfigHash,
			EffectiveGeneration:       opts.EffectiveGeneration,
			PreviousDynamicGeneration: opts.PreviousGeneration,
			Now:                       now,
			Events:                    opts.Events,
			Context:                   opts.Context,
		},
	}
	stdin, err := json.Marshal(request)
	if err != nil {
		outcome := RunOutcome{Error: err.Error()}
		return result, outcome, err
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	started := time.Now()
	cmd := exec.CommandContext(runCtx, spec.Executable)
	cmd.Env = pluginEnvironment(spec.Env)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	duration := time.Since(started)

	outcome := RunOutcome{
		DurationMs:   duration.Milliseconds(),
		StdoutDigest: sha256Hex(stdout.Bytes()),
		Stderr:       truncateString(stderr.String(), maxCapturedStderrLen),
	}
	if cmd.ProcessState != nil {
		outcome.ExitCode = cmd.ProcessState.ExitCode()
		outcome.HasExitCode = true
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		err := fmt.Errorf("plugin %s timed out after %s", name, timeout)
		outcome.Error = err.Error()
		return result, outcome, err
	}
	if runErr != nil {
		err := fmt.Errorf("plugin %s failed: %w", name, runErr)
		outcome.Error = err.Error()
		return result, outcome, err
	}
	if err := yaml.Unmarshal(stdout.Bytes(), &result); err != nil {
		err = fmt.Errorf("decode plugin %s stdout: %w", name, err)
		outcome.Error = err.Error()
		return result, outcome, err
	}
	return result, outcome, nil
}

func ValidateExecutable(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("plugin executable is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("plugin executable %q is not accessible: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("plugin executable %q is not a regular file", path)
	}
	if info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("plugin executable %q is not executable", path)
	}
	return nil
}

func validateExecutable(path string) error {
	return ValidateExecutable(path)
}

func pluginTimeout(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultTimeout, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("plugin timeout must be a valid duration: %w", err)
	}
	if timeout <= 0 {
		return 0, errors.New("plugin timeout must be greater than 0")
	}
	return timeout, nil
}

// pluginEnvironment intentionally does not inherit the parent environment. PATH
// is preserved so plugin scripts can find basic commands; spec.env is layered on
// top for explicit site-local configuration.
func pluginEnvironment(extra map[string]string) []string {
	env := map[string]string{
		"PATH": firstNonEmpty(os.Getenv("PATH"), defaultPathEnv),
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

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
