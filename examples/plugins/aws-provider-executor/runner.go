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

// awsRunner runs one `aws` CLI invocation with the given argv (without the
// leading "aws") and returns its stdout. It is the single injectable seam so
// unit tests substitute a fake that records argv and returns canned JSON,
// NEVER calling real AWS. The production implementation (execRunner) execs the
// real `aws` binary, which resolves the EC2 instance IAM role on its own;
// routerd passes NO credentials.
type awsRunner func(ctx context.Context, argv ...string) ([]byte, error)

const awsCLIPathEnv = "AWS_CLI_PATH"

// defaultRunner returns the production runner that execs the real `aws` binary.
// This is the ONLY use of os/exec in the executor, and it runs only `aws`.
func defaultRunner() awsRunner { return execRunner }

func resolveAWSCLIPath() (string, error) {
	candidate := strings.TrimSpace(os.Getenv(awsCLIPathEnv))
	if candidate == "" {
		candidate = "aws"
	}
	if strings.ContainsAny(candidate, `/\`) {
		info, err := os.Stat(candidate)
		if err != nil {
			return "", fmt.Errorf("aws CLI executable unavailable: %s=%q: %w", awsCLIPathEnv, candidate, err)
		}
		if info.IsDir() || info.Mode()&0111 == 0 {
			return "", fmt.Errorf("aws CLI executable unavailable: %s=%q is not executable", awsCLIPathEnv, candidate)
		}
		return candidate, nil
	}
	path, err := exec.LookPath(candidate)
	if err != nil {
		return "", fmt.Errorf("aws CLI executable unavailable: install aws in PATH or set %s: %w", awsCLIPathEnv, err)
	}
	return path, nil
}

// execRunner execs `aws <argv...>`. The plugin runs in routerd's isolated
// executor environment (no inherited parent env beyond PATH + the plugin's own
// spec.Env), so `aws` resolves credentials from the instance profile, not from
// routerd.
func execRunner(ctx context.Context, argv ...string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout())
	defer cancel()
	awsPath, err := resolveAWSCLIPath()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(runCtx, awsPath, argv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("aws %s: %w: %s", strings.Join(argv, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// guardedRunner wraps a runner so that ONLY describe-* verbs may be issued. It
// is applied to every dry-run dispatch so a coding mistake cannot mutate AWS
// during a non-destructive preview: any non-describe verb is refused before the
// underlying runner is invoked. The aws argv shape is "<service> <verb> ...",
// so argv[1] is the verb.
func guardedRunner(inner awsRunner) awsRunner {
	return func(ctx context.Context, argv ...string) ([]byte, error) {
		verb := ""
		if len(argv) >= 2 {
			verb = argv[1]
		}
		if !strings.HasPrefix(verb, "describe-") {
			return nil, fmt.Errorf("dry-run guard: refusing non-describe aws verb %q (only describe-* permitted in dry-run)", verb)
		}
		return inner(ctx, argv...)
	}
}

// networkInterface is the subset of describe-network-interfaces output the
// executor reads: the source/dest check and the secondary (non-primary) private
// IPs.
type networkInterface struct {
	SourceDestCheck     bool
	secondaryPrivateIPs []string
}

// secondaryIPsCSV renders the secondary (non-primary) private IPs as a stable,
// comma-separated string for the Observed map. Empty when there are none.
func (n networkInterface) secondaryIPsCSV() string {
	return strings.Join(n.secondaryPrivateIPs, ",")
}

// describeNetworkInterfacesOutput mirrors the JSON shape of
// `aws ec2 describe-network-interfaces`.
type describeNetworkInterfacesOutput struct {
	NetworkInterfaces []struct {
		NetworkInterfaceID string `json:"NetworkInterfaceId"`
		SourceDestCheck    bool   `json:"SourceDestCheck"`
		PrivateIPAddresses []struct {
			PrivateIPAddress string `json:"PrivateIpAddress"`
			Primary          bool   `json:"Primary"`
		} `json:"PrivateIpAddresses"`
	} `json:"NetworkInterfaces"`
}

// describeInterface runs the read-only describe-network-interfaces call and
// parses the first interface. This is the read-only verb used for dry-run
// preview AND for the execute-time prior-state capture.
func describeInterface(ctx context.Context, runner awsRunner, eni, region string) (networkInterface, error) {
	out, err := runner(ctx, "ec2", "describe-network-interfaces",
		"--network-interface-ids", eni,
		"--region", region)
	if err != nil {
		return networkInterface{}, err
	}
	var parsed describeNetworkInterfacesOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return networkInterface{}, fmt.Errorf("parse describe-network-interfaces output: %w", err)
	}
	if len(parsed.NetworkInterfaces) == 0 {
		return networkInterface{}, fmt.Errorf("describe-network-interfaces returned no interface for %q", eni)
	}
	ni := parsed.NetworkInterfaces[0]
	iface := networkInterface{SourceDestCheck: ni.SourceDestCheck}
	for _, p := range ni.PrivateIPAddresses {
		if !p.Primary {
			iface.secondaryPrivateIPs = append(iface.secondaryPrivateIPs, p.PrivateIPAddress)
		}
	}
	return iface, nil
}
