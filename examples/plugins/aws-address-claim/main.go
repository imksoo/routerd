// SPDX-License-Identifier: BSD-3-Clause

// Command aws-address-claim is an EXAMPLE / REFERENCE routerd plugin for
// CloudEdge Event Federation (ADR 0006, Phase 4.1). It is DRY-RUN ONLY: it
// reads a routerd PluginRequest JSON from stdin (matched
// "routerd.client.ipv4.observed" events plus allowlisted, secret-redacted
// context resources) and writes a PluginResult on stdout carrying one
// RemoteAddressClaim plus two display-only AWS ActionPlans (assign-secondary-ip
// and ensure-forwarding-enabled, each with an undo).
//
// It makes NO AWS calls: it imports no AWS SDK and no os/exec, performs no
// network access, and mutates nothing. The emitted actionPlans are proposals
// only — routerd validates and persists them but never executes them. Executing
// the AWS operation (EC2 AssignPrivateIpAddresses, ModifyNetworkInterfaceAttribute
// source/dest check) is OUT OF SCOPE (Phase 5).
//
// Provider-specific bits:
//   - provider:        "aws"
//   - forwarding param: sourceDestCheck=false (on the ensure-forwarding-* plan)
//   - target keys:      provider, providerRef, nicRef (=ENI id), address, plus
//     optional region/account/subnetRef when present in the event payload.
//
// Where values come from:
//   - region/account/subnetRef: event payload (the redacted CloudProviderProfile
//     does not carry them); omitted when absent.
//   - nicRef (ENI id):          event payload (payload.nicRef); REQUIRED — the
//     plugin never invents a cloud resource id.
//   - providerRef:              the CloudProviderProfile context resource name.
//
// Like the other examples it mirrors the routerd wire JSON via the shared
// examples/plugins/internal/addressclaim package and depends only on the Go
// standard library; it does NOT import pkg/plugin or pkg/api.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/imksoo/routerd/examples/plugins/internal/addressclaim"
)

const resultName = "aws-address-claim"

// awsProfile is the AWS-specific knob set the shared builder needs.
var awsProfile = addressclaim.ProviderProfile{
	Provider:             "aws",
	ForwardingParamKey:   "sourceDestCheck",
	ForwardingParamValue: "false",
	TargetKeys: []addressclaim.TargetKey{
		{TargetKey: "region", PayloadKey: "region"},
		{TargetKey: "account", PayloadKey: "account"},
		{TargetKey: "subnetRef", PayloadKey: "subnetRef"},
	},
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", resultName, err)
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	var req addressclaim.PluginRequest
	if len(data) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			return fmt.Errorf("parse PluginRequest: %w", err)
		}
	}

	result, err := addressclaim.Build(resultName, awsProfile, req)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		return fmt.Errorf("encode PluginResult: %w", err)
	}
	return nil
}
