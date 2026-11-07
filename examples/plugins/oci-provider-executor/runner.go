// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/imksoo/routerd/pkg/subprocessio"
)

// ociRunner runs one `oci` CLI invocation with the given argv (without the
// leading "oci") and returns its stdout. It is the single injectable seam so
// unit tests substitute a fake that records argv and returns canned JSON, NEVER
// calling real OCI. The production implementation (execRunner) execs the real
// `oci` binary, which resolves the instance principal on its own; routerd passes
// NO credentials.
type ociRunner func(ctx context.Context, argv ...string) ([]byte, error)

var errPrivateIPNotFound = errors.New("private-ip not found")

func isPrivateIPNotFoundError(err error) bool {
	return errors.Is(err, errPrivateIPNotFound)
}

// defaultRunner returns the production runner that execs the real `oci` binary.
// This is the ONLY use of os/exec in the executor, and it runs only `oci`.
func defaultRunner() ociRunner { return execRunner }

// execRunner execs `oci <argv...> --auth instance_principal`. The plugin runs in
// routerd's isolated executor environment (no inherited parent env beyond PATH +
// the plugin's own spec.Env), so `oci` authenticates with the instance
// principal, not from routerd. --output json forces machine-readable output.
func execRunner(ctx context.Context, argv ...string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout())
	defer cancel()
	full := append([]string(nil), argv...)
	full = append(full, "--auth", "instance_principal", "--output", "json", "--no-retry")
	oci, err := resolveOCICommand()
	if err != nil {
		return nil, &commandNotStartedError{err}
	}
	cmd := exec.CommandContext(runCtx, oci, full...)
	stdout, stderr := subprocessio.NewCapture(subprocessio.StdoutLimit), subprocessio.NewCapture(subprocessio.StderrLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := subprocessio.Run(cmd)
	if cmd.Process == nil && runErr != nil {
		return nil, &commandNotStartedError{runErr}
	}
	if runCtx.Err() != nil {
		runErr = runCtx.Err()
	}
	if limitErr := subprocessio.LimitError(stdout, stderr); limitErr != nil {
		runErr = limitErr
	}
	if runErr != nil {
		return nil, fmt.Errorf("oci %s: %w: %s", strings.Join(full, " "), runErr, strings.TrimSpace(stderr.String()))
	}

	return stdout.Bytes(), nil
}

func resolveOCICommand() (string, error) {
	if override := strings.TrimSpace(os.Getenv("OCI_CLI_PATH")); override != "" {
		if err := executableFile(override); err != nil {
			return "", fmt.Errorf("OCI_CLI_PATH=%s is not executable: %w", override, err)
		}
		return override, nil
	}
	if path, err := exec.LookPath("oci"); err == nil && executableFile(path) == nil {
		return path, nil
	}
	for _, dir := range []string{"/usr/local/bin", "/usr/bin", "/bin", "/opt/oci-cli/bin"} {
		path := filepath.Join(dir, "oci")
		if executableFile(path) == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("OCI CLI \"oci\" was not found as an executable on PATH=%q; set OCI_CLI_PATH to an absolute executable path", os.Getenv("PATH"))
}

func executableFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("is a directory")
	}
	if info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("mode %s has no execute bit", info.Mode().Perm())
	}
	return nil
}

// guardedRunner wraps a runner so that ONLY read-only get/list verbs may be
// issued. It is applied to every dry-run dispatch so a coding mistake cannot
// mutate OCI during a non-destructive preview: any non-read-only verb is refused
// before the underlying runner is invoked. The oci argv shape is
// "<service> <resource> <verb> ...", so the verb is the last non-flag token
// before flags begin. We scan argv for the first read-only verb token (get/list)
// and refuse otherwise.
func guardedRunner(inner ociRunner) ociRunner {
	return func(ctx context.Context, argv ...string) ([]byte, error) {
		if !isReadOnlyVerb(argv) {
			return nil, fmt.Errorf("dry-run guard: refusing non-read-only oci command %q (only *get/*list permitted in dry-run)", strings.Join(leadingTokens(argv), " "))
		}
		return inner(ctx, argv...)
	}
}

// leadingTokens returns the non-flag tokens at the front of argv (the
// service/resource/verb words before any "--flag").
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

// isReadOnlyVerb reports whether the oci command is a read-only get/list. The
// verb is the last leading non-flag token (e.g. ["network","vnic","get"] -> get,
// ["network","private-ip","list"] -> list).
func isReadOnlyVerb(argv []string) bool {
	toks := leadingTokens(argv)
	if len(toks) == 0 {
		return false
	}
	verb := toks[len(toks)-1]
	return verb == "get" || verb == "list" ||
		strings.HasSuffix(verb, "-get") || strings.HasSuffix(verb, "-list")
}

// vnic is the subset of `oci network vnic get` output the executor reads.
type vnic struct {
	SkipSourceDestCheck bool
}

// vnicGetOutput mirrors the JSON shape of `oci network vnic get`, whose payload
// is wrapped in a top-level "data" object.
type vnicGetOutput struct {
	Data struct {
		ID                  string `json:"id"`
		SkipSourceDestCheck *bool  `json:"skip-source-dest-check"`
	} `json:"data"`
}

// getVNIC runs the read-only `oci network vnic get` call and parses it. This is
// the read-only verb used for dry-run preview AND for the execute-time prior
// capture.
func getVNIC(ctx context.Context, runner ociRunner, vnicID string) (vnic, error) {
	out, err := runner(ctx, "network", "vnic", "get", "--vnic-id", vnicID)
	if err != nil {
		return vnic{}, err
	}
	var parsed vnicGetOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return vnic{}, fmt.Errorf("parse network vnic get output: %w", err)
	}
	if parsed.Data.ID != vnicID || parsed.Data.SkipSourceDestCheck == nil {
		return vnic{}, fmt.Errorf("incomplete VNIC observation for %q", vnicID)
	}
	return vnic{SkipSourceDestCheck: *parsed.Data.SkipSourceDestCheck}, nil
}

// privateIPListOutput mirrors the JSON shape of `oci network private-ip list`,
// whose payload is a top-level "data" array of private-ip objects.
type privateIPListOutput struct {
	Data []struct {
		ID        string `json:"id"`
		IPAddress string `json:"ip-address"`
	} `json:"data"`
}

// findPrivateIPOCID lists the private IPs on the VNIC (read-only) and returns the
// OCID of the one whose ip-address matches address. address may be a bare IP or
// a CIDR (e.g. "10.88.60.9/32"); the host part is compared. It errors if no
// match is found.
func findPrivateIPOCID(ctx context.Context, runner ociRunner, vnicID, address string) (string, error) {
	out, err := runner(ctx, "network", "private-ip", "list", "--vnic-id", vnicID)
	if err != nil {
		return "", err
	}
	var parsed privateIPListOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return "", fmt.Errorf("parse network private-ip list output: %w", err)
	}
	if parsed.Data == nil {
		return "", fmt.Errorf("private-ip list observation has no data array")
	}
	want := hostPart(address)
	for _, p := range parsed.Data {
		if hostPart(p.IPAddress) == want {
			if strings.TrimSpace(p.ID) == "" {
				return "", fmt.Errorf("matching private-ip observation has no OCID")
			}
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("%w: address %q on vnic %q", errPrivateIPNotFound, address, vnicID)
}

// hostPart strips a trailing "/<prefix>" so a captured /32 ("10.88.60.9/32")
// compares equal to OCI's bare "10.88.60.9".
func hostPart(addr string) string {
	if i := strings.IndexByte(addr, '/'); i >= 0 {
		return addr[:i]
	}
	return addr
}

// ociRouteRule is the camelCase shape OCI's `route-table update --route-rules`
// complex input accepts. The read path converts the kebab-case `route-table get`
// output into this form, so the same struct round-trips through an update. OCI
// replaces the ENTIRE rule set on update (there is no per-rule add/delete like
// AWS create/delete-route or an Azure per-route create), so route-table capture
// is a read-modify-write of the full set.
type ociRouteRule struct {
	Destination     string `json:"destination,omitempty"`
	DestinationType string `json:"destinationType,omitempty"`
	NetworkEntityID string `json:"networkEntityId,omitempty"`
	Description     string `json:"description,omitempty"`
	RouteType       string `json:"routeType,omitempty"`
}

// routeTableGetOutput mirrors `oci network route-table get`, whose payload wraps
// the route rules (kebab-case keys) under a top-level "data" object.
type routeTableGetOutput struct {
	ETag string `json:"etag"`
	Data struct {
		ID         string `json:"id"`
		RouteRules []struct {
			Destination     string `json:"destination"`
			DestinationType string `json:"destination-type"`
			CidrBlock       string `json:"cidr-block"`
			NetworkEntityID string `json:"network-entity-id"`
			Description     string `json:"description"`
			RouteType       string `json:"route-type"`
		} `json:"route-rules"`
	} `json:"data"`
}

// getRouteTableRules runs the read-only `oci network route-table get` and returns
// the existing rules normalized into the camelCase update shape, preserving every
// unrelated rule (including service-gateway rules) so a read-modify-write update
// never drops routes the executor did not author.
type ociRouteTable struct {
	Rules []ociRouteRule
	ETag  string
}

func getRouteTableRules(ctx context.Context, runner ociRunner, rtID string) (ociRouteTable, error) {
	out, err := runner(ctx, "network", "route-table", "get", "--rt-id", rtID)
	if err != nil {
		return ociRouteTable{}, err
	}
	var parsed routeTableGetOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return ociRouteTable{}, fmt.Errorf("parse network route-table get output: %w", err)
	}
	if parsed.Data.ID != rtID || parsed.ETag == "" || parsed.Data.RouteRules == nil {
		return ociRouteTable{}, fmt.Errorf("incomplete route-table/etag observation for %q", rtID)
	}
	rules := make([]ociRouteRule, 0, len(parsed.Data.RouteRules))
	for _, r := range parsed.Data.RouteRules {
		dest := strings.TrimSpace(r.Destination)
		if dest == "" {
			dest = strings.TrimSpace(r.CidrBlock)
		}
		dtype := strings.TrimSpace(r.DestinationType)
		if dtype == "" {
			dtype = "CIDR_BLOCK"
		}
		rules = append(rules, ociRouteRule{
			Destination:     dest,
			DestinationType: dtype,
			NetworkEntityID: strings.TrimSpace(r.NetworkEntityID),
			Description:     r.Description,
			RouteType:       r.RouteType,
		})
	}
	return ociRouteTable{Rules: rules, ETag: parsed.ETag}, nil
}

// updateRouteTableRules writes the full rule set back with `route-table update`.
// Callers MUST pass the merged set (existing rules plus/minus the one mobility
// rule) returned from getRouteTableRules, because OCI replaces the entire set.
func updateRouteTableRules(ctx context.Context, runner ociRunner, rtID, etag string, rules []ociRouteRule) error {
	if etag == "" {
		return fmt.Errorf("route-table write requires observed etag")
	}
	if rules == nil {
		rules = []ociRouteRule{}
	}
	payload, err := json.Marshal(rules)
	if err != nil {
		return fmt.Errorf("marshal route-rules: %w", err)
	}
	_, err = runner(ctx, "network", "route-table", "update",
		"--rt-id", rtID,
		"--if-match", etag,
		"--route-rules", string(payload),
		"--force")
	return err
}

// routeRuleIndexForDest returns the index of the rule whose destination host
// matches the captured /32 (compared by host so "10.88.60.9/32" matches), or -1.
func routeRuleIndexForDest(rules []ociRouteRule, dest string) int {
	wanted, err := netip.ParsePrefix(dest)
	if err != nil || !wanted.Addr().Is4() || wanted.Bits() != 32 {
		return -1
	}
	for i, r := range rules {
		actual, err := netip.ParsePrefix(r.Destination)
		if err == nil && r.DestinationType == "CIDR_BLOCK" && actual.Masked() == wanted.Masked() {
			return i
		}
	}
	return -1
}
