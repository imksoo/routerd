// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strings"
	"time"
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

// execRunner execs `az <argv...> --only-show-errors --output json`. The plugin
// runs in routerd's isolated executor environment (no inherited parent env
// beyond PATH + the plugin's own spec.Env), so `az` authenticates with the
// managed identity, not from routerd. --output json forces machine-readable
// stdout, while --only-show-errors keeps command noise out of stderr. The
// current NIC ip-config commands do not expose a confirmation flag; if a future
// mutating command does, add its explicit non-interactive flag at that call site.
func execRunner(ctx context.Context, argv ...string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout())
	defer cancel()
	full := azCommandArgs(argv...)
	cmd := exec.CommandContext(runCtx, "az", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("az %s timed out after %s: %s", strings.Join(full, " "), commandTimeout(), strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("az %s: %w: %s", strings.Join(full, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func azCommandArgs(argv ...string) []string {
	full := append([]string(nil), argv...)
	return append(full, "--only-show-errors", "--output", "json")
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
	ID                 string
	Name               string
	ResourceGroup      string
	IPConfigurations   []ipConfig
}

type ipConfig struct {
	Name             string
	PrivateIPAddress string
}

// nicShowOutput mirrors the JSON shape of `az network nic show`.
type nicShowOutput struct {
	ID                 string           `json:"id"`
	Name               string           `json:"name"`
	ResourceGroup      string           `json:"resourceGroup"`
	EnableIPForwarding bool             `json:"enableIPForwarding"`
	IPConfigurations   []ipConfigOutput `json:"ipConfigurations"`
}

type ipConfigOutput struct {
	Name                           string `json:"name"`
	PrivateIPAddress               string `json:"privateIPAddress"`
	PrivateIPAddressAlt            string `json:"privateIpAddress"`
	PrivateIpAddressFromProperties string `json:"privateIpAddressFromProperties"`
	PrivateIPAddressFromProperties string `json:"privateIPAddressFromProperties"`
	Properties                     struct {
		PrivateIPAddress    string `json:"privateIPAddress"`
		PrivateIPAddressAlt string `json:"privateIpAddress"`
	} `json:"properties"`
}

// showNIC runs the read-only `az network nic show --ids <nic>` call and parses
// it. This is the read-only verb used for dry-run preview AND for the
// execute-time prior capture.
func showNIC(ctx context.Context, runner azRunner, nicID string) (nicShow, error) {
	out, err := callAzReadWithRetry(ctx, runner, "network", "nic", "show", "--ids", nicID)
	if err != nil {
		return nicShow{}, err
	}
	var parsed nicShowOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nicShow{}, fmt.Errorf("parse network nic show output: %w", err)
	}
	return nicShow{
		EnableIPForwarding: parsed.EnableIPForwarding,
		ID:                 parsed.ID,
		Name:               parsed.Name,
		ResourceGroup:      parsed.ResourceGroup,
		IPConfigurations:   normalizeIPConfigs(parsed.IPConfigurations),
	}, nil
}

func listIPConfigs(ctx context.Context, runner azRunner, resourceGroup, nicName string) ([]ipConfig, error) {
	out, err := callAzReadWithRetry(ctx, runner, "network", "nic", "ip-config", "list",
		"--resource-group", resourceGroup,
		"--nic-name", nicName,
		"--query", "[].{name:name, privateIPAddress: privateIPAddress, privateIpAddress: privateIpAddress, privateIpAddressFromProperties: properties.privateIpAddress, privateIPAddressFromProperties: properties.privateIPAddress}")
	if err != nil {
		return nil, err
	}
	var parsed []ipConfigOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("parse network nic ip-config list output: %w", err)
	}
	return normalizeIPConfigs(parsed), nil
}

func listNICs(ctx context.Context, runner azRunner, resourceGroup string) ([]nicShow, error) {
	out, err := callAzReadWithRetry(ctx, runner, "network", "nic", "list", "--resource-group", resourceGroup)
	if err != nil {
		return nil, err
	}
	var parsed []nicShowOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("parse network nic list output: %w", err)
	}
	nics := make([]nicShow, 0, len(parsed))
	for _, nic := range parsed {
		nics = append(nics, nicShow{
			EnableIPForwarding: nic.EnableIPForwarding,
			ID:                 nic.ID,
			Name:               nic.Name,
			ResourceGroup:      nic.ResourceGroup,
			IPConfigurations:   normalizeIPConfigs(nic.IPConfigurations),
		})
	}
	return nics, nil
}

func normalizeIPConfigs(in []ipConfigOutput) []ipConfig {
	out := make([]ipConfig, 0, len(in))
	for _, cfg := range in {
		address := strings.TrimSpace(cfg.PrivateIPAddress)
		if address == "" {
			address = strings.TrimSpace(cfg.PrivateIPAddressAlt)
		}
		if address == "" {
			address = strings.TrimSpace(cfg.PrivateIPAddressFromProperties)
		}
		if address == "" {
			address = strings.TrimSpace(cfg.PrivateIpAddressFromProperties)
		}
		if address == "" {
			address = strings.TrimSpace(cfg.Properties.PrivateIPAddress)
		}
		if address == "" {
			address = strings.TrimSpace(cfg.Properties.PrivateIPAddressAlt)
		}
		out = append(out, ipConfig{
			Name:             strings.TrimSpace(cfg.Name),
			PrivateIPAddress: bareIP(address),
		})
	}
	return out
}

func isRetryableCommandError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "signal: killed") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "temporary") ||
		strings.Contains(msg, "throttle") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "service unavailable") ||
		strings.Contains(msg, "internal server error") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "econnreset") ||
		strings.Contains(msg, "econnrefused")
}

func callTimeoutForAttempt(ctx context.Context, attemptsLeft int) time.Duration {
	timeout := commandTimeout()
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		return timeout
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0
	}
	if attemptsLeft <= 0 {
		attemptsLeft = 1
	}
	perAttempt := time.Duration(math.Ceil(float64(remaining) / float64(attemptsLeft)))
	if timeout > 0 && perAttempt > timeout {
		return timeout
	}
	return perAttempt
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	if ctx.Err() != nil {
		return false
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func callAzWithRetry(ctx context.Context, runner azRunner, argv ...string) ([]byte, error) {
	delay := azCommandRetryDelay
	var lastErr error
	for attempt := 0; attempt < azCommandRetryAttempts; attempt++ {
		attemptTimeout := callTimeoutForAttempt(ctx, azCommandRetryAttempts-attempt)
		if attemptTimeout <= 0 {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("az %s: no remaining timeout budget", strings.Join(argv, " "))
		}
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		out, err := runner(attemptCtx, argv...)
		cancel()
		if err == nil {
			return out, nil
		}
		lastErr = err
		if !isRetryableCommandError(err) || attempt+1 >= azCommandRetryAttempts {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !sleepWithContext(ctx, delay) {
			return nil, ctx.Err()
		}
		delay *= 2
	}
	return nil, lastErr
}

func callAzReadWithRetry(ctx context.Context, runner azRunner, argv ...string) ([]byte, error) {
	delay := azCommandRetryDelay
	var lastErr error
	for attempt := 0; attempt < azCommandRetryAttempts; attempt++ {
		attemptTimeout := callTimeoutForAttempt(ctx, azCommandRetryAttempts-attempt)
		if attemptTimeout <= 0 {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("az %s: no remaining timeout budget", strings.Join(argv, " "))
		}
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
		out, err := runner(attemptCtx, argv...)
		cancel()
		if err == nil {
			return out, nil
		}
		lastErr = err
		if !isRetryableCommandError(err) || attempt+1 >= azCommandRetryAttempts {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !sleepWithContext(ctx, delay) {
			return nil, ctx.Err()
		}
		delay *= 2
	}
	return nil, lastErr
}
