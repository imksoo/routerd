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

// ociRunner runs one `oci` CLI invocation with the given argv (without the
// leading "oci") and returns its stdout. It is the single injectable seam so
// unit tests substitute a fake that records argv and returns canned JSON, NEVER
// calling real OCI. The production implementation (execRunner) execs the real
// `oci` binary, which resolves the instance principal on its own; routerd passes
// NO credentials.
type ociRunner func(ctx context.Context, argv ...string) ([]byte, error)

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
	full = append(full, "--auth", "instance_principal", "--output", "json")
	cmd := exec.CommandContext(runCtx, "oci", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("oci %s: %w: %s", strings.Join(full, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
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
		SkipSourceDestCheck bool `json:"skip-source-dest-check"`
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
	return vnic{SkipSourceDestCheck: parsed.Data.SkipSourceDestCheck}, nil
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
	want := hostPart(address)
	for _, p := range parsed.Data {
		if hostPart(p.IPAddress) == want {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("no private-ip found for address %q on vnic %q", address, vnicID)
}

// hostPart strips a trailing "/<prefix>" so a captured /32 ("10.88.60.9/32")
// compares equal to OCI's bare "10.88.60.9".
func hostPart(addr string) string {
	if i := strings.IndexByte(addr, '/'); i >= 0 {
		return addr[:i]
	}
	return addr
}
