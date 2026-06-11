// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// azRunner runs one Azure helper invocation with the given CLI-compatible argv
// and returns its stdout. Unit tests substitute a fake that records argv and
// returns canned JSON, NEVER calling real Azure. The production implementation
// (execRunner) execs routerd's
// azure-routerd-helper, which resolves managed identity on its own; routerd
// passes NO credentials.
type azRunner func(ctx context.Context, argv ...string) ([]byte, error)

const (
	defaultAzureHelper = "/usr/local/libexec/routerd/plugins/azure-routerd-helper/bin/azure-routerd-helper"
	azureHelperEnv     = "ROUTERD_AZURE_HELPER"
	azCLIPathEnv       = "AZ_CLI_PATH"
)

// defaultRunner returns the production runner that execs routerd's Azure helper.
func defaultRunner() azRunner { return execRunner }

func resolveAzureHelperPath() (string, error) {
	candidate := strings.TrimSpace(os.Getenv(azureHelperEnv))
	if candidate != "" {
		return validateExecutablePath(candidate, azureHelperEnv)
	}
	if legacy := strings.TrimSpace(os.Getenv(azCLIPathEnv)); legacy != "" {
		return validateExecutablePath(legacy, azCLIPathEnv)
	}
	return validateExecutablePath(defaultAzureHelper, azureHelperEnv)
}

func validateExecutablePath(candidate, source string) (string, error) {
	if !strings.ContainsAny(candidate, `/\`) {
		return "", fmt.Errorf("Azure helper executable unavailable: %s=%q must be a concrete executable path; PATH lookup is not used", source, candidate)
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return "", fmt.Errorf("Azure helper executable unavailable: %s=%q: %w", source, candidate, err)
	}
	if info.IsDir() || info.Mode()&0111 == 0 {
		return "", fmt.Errorf("Azure helper executable unavailable: %s=%q is not executable", source, candidate)
	}
	return candidate, nil
}

// execRunner execs azure-routerd-helper with the CLI-compatible argv the
// executor already emits. The plugin runs in routerd's isolated executor
// environment (no inherited parent env beyond PATH + the plugin's own spec.Env),
// so the helper authenticates with managed identity, not from routerd.
func execRunner(ctx context.Context, argv ...string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout())
	defer cancel()
	full := azCommandArgs(argv...)
	helper, err := resolveAzureHelperPath()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(runCtx, helper, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", helper, strings.Join(full, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func runHelperPreflightCommand(ctx context.Context, helper string, argv ...string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout())
	defer cancel()
	cmd := exec.CommandContext(runCtx, helper, argv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", helper, strings.Join(argv, " "), err, strings.TrimSpace(stderr.String()))
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
	Name                string `json:"name"`
	PrivateIPAddress    string `json:"privateIPAddress"`
	PrivateIPAddressAlt string `json:"privateIpAddress"`
	Properties          struct {
		PrivateIPAddress    string `json:"privateIPAddress"`
		PrivateIPAddressAlt string `json:"privateIpAddress"`
	} `json:"properties"`
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
	return nicShow{
		EnableIPForwarding: parsed.EnableIPForwarding,
		ID:                 parsed.ID,
		Name:               parsed.Name,
		ResourceGroup:      parsed.ResourceGroup,
		IPConfigurations:   normalizeIPConfigs(parsed.IPConfigurations),
	}, nil
}

func listIPConfigs(ctx context.Context, runner azRunner, subscriptionID, resourceGroup, nicName string) ([]ipConfig, error) {
	out, err := runner(ctx, appendSubscription(subscriptionID, "network", "nic", "ip-config", "list",
		"--resource-group", resourceGroup,
		"--nic-name", nicName)...)
	if err != nil {
		return nil, err
	}
	var parsed []ipConfigOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("parse network nic ip-config list output: %w", err)
	}
	return normalizeIPConfigs(parsed), nil
}

func listNICs(ctx context.Context, runner azRunner, subscriptionID, resourceGroup string) ([]nicShow, error) {
	out, err := runner(ctx, appendSubscription(subscriptionID, "network", "nic", "list", "--resource-group", resourceGroup)...)
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
