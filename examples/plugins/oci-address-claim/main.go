// SPDX-License-Identifier: BSD-3-Clause

// Command oci-address-claim is an EXAMPLE / REFERENCE routerd plugin for
// CloudEdge Event Federation (ADR 0006, Phase 4.1). It is DRY-RUN ONLY: it
// reads a routerd PluginRequest JSON from stdin (matched
// "routerd.client.ipv4.observed" events plus allowlisted, secret-redacted
// context resources) and writes a PluginResult on stdout carrying one
// RemoteAddressClaim plus two display-only OCI ActionPlans (assign-secondary-ip
// and ensure-forwarding-enabled, each with an undo).
//
// It makes NO OCI calls: it imports no OCI SDK and no os/exec, performs no
// network access, and mutates nothing. The emitted actionPlans are proposals
// only — routerd validates and persists them but never executes them. Executing
// the OCI operation (VNIC secondary private IP, skipSourceDestCheck=true) is
// OUT OF SCOPE (Phase 5).
//
// Provider-specific bits:
//   - provider:        "oci"
//   - forwarding param: skipSourceDestCheck=true (on the ensure-forwarding-* plan)
//   - target keys:      provider, providerRef, nicRef (=VNIC OCID), address, plus
//     optional compartmentId, region when present in the event payload.
//
// Where values come from:
//   - compartmentId/region: event payload (the redacted CloudProviderProfile
//     does not carry them); omitted when absent.
//   - nicRef (VNIC OCID):   event payload (payload.nicRef); REQUIRED — the plugin
//     never invents a cloud resource id.
//   - providerRef:          the CloudProviderProfile context resource name.
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

const resultName = "oci-address-claim"

// ociProfile is the OCI-specific knob set the shared builder needs.
var ociProfile = addressclaim.ProviderProfile{
	Provider:             "oci",
	ForwardingParamKey:   "skipSourceDestCheck",
	ForwardingParamValue: "true",
	TargetKeys: []addressclaim.TargetKey{
		{TargetKey: "compartmentId", PayloadKey: "compartmentId"},
		{TargetKey: "region", PayloadKey: "region"},
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

	result, err := addressclaim.Build(resultName, ociProfile, req)
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
