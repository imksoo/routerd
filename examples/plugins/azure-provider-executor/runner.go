// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
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
func defaultRunner() azRunner { return azureLoginEnsuringRunner(execRunner) }

func azureLoginEnsuringRunner(inner azRunner) azRunner {
	var mu sync.Mutex
	var loggedIn bool
	return func(ctx context.Context, argv ...string) ([]byte, error) {
		mu.Lock()
		if !loggedIn {
			if err := ensureManagedIdentityLogin(ctx, inner); err != nil {
				mu.Unlock()
				return nil, err
			}
			loggedIn = true
		}
		mu.Unlock()

		out, err := inner(ctx, argv...)
		if err == nil || !isAzureLoginRequiredError(err) {
			return out, err
		}
		// The token cache can expire between the bootstrap check and the real
		// mutating call. Refresh once and retry the original command.
		mu.Lock()
		refreshErr := loginWithManagedIdentity(ctx, inner)
		loggedIn = refreshErr == nil
		mu.Unlock()
		if refreshErr != nil {
			return nil, refreshErr
		}
		return inner(ctx, argv...)
	}
}

func ensureManagedIdentityLogin(ctx context.Context, runner azRunner) error {
	if _, err := runner(ctx, "account", "show"); err == nil {
		return nil
	} else if !isAzureLoginRequiredError(err) {
		return fmt.Errorf("azure managed identity bootstrap: account show failed: %w", err)
	}
	return loginWithManagedIdentity(ctx, runner)
}

func loginWithManagedIdentity(ctx context.Context, runner azRunner) error {
	if _, err := runner(ctx, "login", "--identity", "--allow-no-subscriptions"); err != nil {
		return fmt.Errorf("azure managed identity bootstrap: login --identity failed: %w", err)
	}
	return nil
}

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
		exitCode := 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		if rec := azCallObservationFromContext(runCtx); rec != nil {
			rec.ExitCode = exitCode
			rec.StderrTail = tailLines(stderr.String(), azStderrTailLines)
			rec.RateLimitRemaining = extractAzureRateLimitRemaining(stderr.String())
		}
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, &azCommandError{
				Full:     full,
				Err:      runCtx.Err(),
				Stderr:   stderr.String(),
				Timeout:  true,
				ExitCode: exitCode,
			}
		}
		return nil, &azCommandError{
			Full:     full,
			Err:      err,
			Stderr:   stderr.String(),
			ExitCode: exitCode,
		}
	}
	if rec := azCallObservationFromContext(runCtx); rec != nil {
		rec.ExitCode = 0
		rec.StderrTail = tailLines(stderr.String(), azStderrTailLines)
		rec.RateLimitRemaining = extractAzureRateLimitRemaining(stderr.String())
	}
	return stdout.Bytes(), nil
}

func azCommandArgs(argv ...string) []string {
	full := append([]string(nil), argv...)
	if azDebugEnabled() {
		full = append(full, "--debug")
	}
	return append(full, "--only-show-errors", "--output", "json")
}

func azDebugEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(azDebugEnv))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

type azCommandError struct {
	Full     []string
	Err      error
	Stderr   string
	Timeout  bool
	ExitCode int
}

func (e *azCommandError) Error() string {
	stderr := strings.TrimSpace(e.Stderr)
	if e.Timeout {
		return fmt.Sprintf("az %s timed out after %s: %s", strings.Join(e.Full, " "), commandTimeout(), stderr)
	}
	return fmt.Sprintf("az %s: %v: %s", strings.Join(e.Full, " "), e.Err, stderr)
}

func (e *azCommandError) Unwrap() error { return e.Err }

type azCallObservation struct {
	Command            string            `json:"command"`
	WallTimeMS         int64             `json:"wallTimeMs"`
	ExitCode           int               `json:"exitCode"`
	StderrTail         string            `json:"stderrTail,omitempty"`
	RateLimitRemaining map[string]string `json:"rateLimitRemaining,omitempty"`
}

type azCallRecorder struct {
	mu    sync.Mutex
	calls []azCallObservation
}

func newAzCallRecorder() *azCallRecorder { return &azCallRecorder{} }

func (r *azCallRecorder) wrap(inner azRunner) azRunner {
	return func(ctx context.Context, argv ...string) ([]byte, error) {
		call := &azCallObservation{
			Command:  strings.Join(leadingTokens(argv), " "),
			ExitCode: 0,
		}
		start := time.Now()
		out, err := inner(context.WithValue(ctx, azCallObservationKey{}, call), argv...)
		call.WallTimeMS = time.Since(start).Milliseconds()
		if err != nil {
			call.ExitCode = exitCodeFromError(err)
			if call.StderrTail == "" {
				call.StderrTail = tailLines(err.Error(), azStderrTailLines)
			}
		}
		r.mu.Lock()
		r.calls = append(r.calls, *call)
		r.mu.Unlock()
		return out, err
	}
}

func (r *azCallRecorder) attach(res *executeActionResult) {
	r.mu.Lock()
	calls := append([]azCallObservation(nil), r.calls...)
	r.mu.Unlock()
	if len(calls) == 0 {
		return
	}
	if res.Status.Observed == nil {
		res.Status.Observed = map[string]string{}
	}
	res.Status.Observed["azCallCount"] = fmt.Sprintf("%d", len(calls))
	if raw, err := json.Marshal(calls); err == nil {
		res.Status.Observed["azCalls"] = string(raw)
	}
	mergedRateLimits := map[string]string{}
	for _, call := range calls {
		for key, value := range call.RateLimitRemaining {
			mergedRateLimits[key] = value
		}
	}
	if len(mergedRateLimits) > 0 {
		if raw, err := json.Marshal(mergedRateLimits); err == nil {
			res.Status.Observed["azRateLimitRemaining"] = string(raw)
		}
	}
}

type azCallObservationKey struct{}

func azCallObservationFromContext(ctx context.Context) *azCallObservation {
	rec, _ := ctx.Value(azCallObservationKey{}).(*azCallObservation)
	return rec
}

func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	var azErr *azCommandError
	if errors.As(err, &azErr) && azErr.ExitCode != 0 {
		return azErr.ExitCode
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func tailLines(value string, maxLines int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxLines <= 0 {
		return ""
	}
	lines := strings.Split(value, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.Join(lines, "\n")
}

func extractAzureRateLimitRemaining(stderr string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		idx := strings.Index(lower, "x-ms-ratelimit-remaining-")
		if idx < 0 {
			continue
		}
		rest := line[idx:]
		keyEnd := len(rest)
		for i, r := range rest {
			if r == ':' || r == '=' || r == '\'' || r == '"' || r == ' ' || r == '\t' {
				keyEnd = i
				break
			}
		}
		key := strings.ToLower(strings.Trim(rest[:keyEnd], "'\" "))
		if key == "" {
			continue
		}
		value := ""
		if colon := strings.Index(rest, ":"); colon >= 0 {
			value = rest[colon+1:]
		} else if equals := strings.Index(rest, "="); equals >= 0 {
			value = rest[equals+1:]
		}
		value = strings.Trim(value, "'\" ,")
		if value != "" {
			out[key] = value
		}
	}
	return out
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

func isAzureLoginRequiredError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "please run 'az login'") ||
		strings.Contains(msg, "please run \"az login\"") ||
		strings.Contains(msg, "az login to setup account") ||
		strings.Contains(msg, "run az login")
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
