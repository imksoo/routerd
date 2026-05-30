// SPDX-License-Identifier: BSD-3-Clause

// Command azure-address-claim is an EXAMPLE / REFERENCE routerd plugin for
// CloudEdge Event Federation (ADR 0006, Phase 4.1). It is DRY-RUN ONLY: it
// reads a routerd PluginRequest JSON from stdin (matched
// "routerd.client.ipv4.observed" events plus allowlisted, secret-redacted
// context resources) and writes a PluginResult on stdout carrying one
// RemoteAddressClaim plus two display-only Azure ActionPlans (assign-secondary-ip
// and ensure-forwarding-enabled, each with an undo).
//
// It makes NO Azure calls: it imports no Azure SDK and no os/exec, performs no
// network access, and mutates nothing. The emitted actionPlans are proposals
// only — routerd validates and persists them but never executes them. Executing
// the Azure operation (NIC ipConfiguration secondary IP, enableIPForwarding) is
// OUT OF SCOPE (Phase 5).
//
// Provider-specific bits:
//   - provider:        "azure"
//   - forwarding param: ipForwarding=true (on the ensure-forwarding-* plan)
//   - target keys:      provider, providerRef, nicRef (=NIC id), address, plus
//     optional subscriptionId, resourceGroup, region, ipConfigName when present.
//
// Where values come from:
//   - subscriptionId/resourceGroup: the CloudProviderProfile context resource
//     when present (spec.subscriptionID / spec.resourceGroup), else event payload.
//   - region/ipConfigName:          event payload; omitted when absent.
//   - nicRef (NIC id):              event payload (payload.nicRef); REQUIRED —
//     the plugin never invents a cloud resource id.
//   - providerRef:                  the CloudProviderProfile context resource name.
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
	"strings"

	"github.com/imksoo/routerd/examples/plugins/internal/addressclaim"
)

const resultName = "azure-address-claim"

// azureProfile is the Azure-specific knob set the shared builder needs.
var azureProfile = addressclaim.ProviderProfile{
	Provider:             "azure",
	ForwardingParamKey:   "ipForwarding",
	ForwardingParamValue: "true",
	TargetKeys: []addressclaim.TargetKey{
		{TargetKey: "subscriptionId", PayloadKey: "subscriptionId"},
		{TargetKey: "resourceGroup", PayloadKey: "resourceGroup"},
		{TargetKey: "region", PayloadKey: "region"},
		{TargetKey: "ipConfigName", PayloadKey: "ipConfigName"},
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

	// Azure carries subscriptionId/resourceGroup in the CloudProviderProfile spec.
	// Backfill them onto the matched event payload from the (redacted, secret-free)
	// context profile when the event omitted them, so the shared builder's
	// payload-sourced target keys pick them up. This is pure data shaping; the
	// profile spec carries no secrets.
	backfillFromProfile(&req)

	result, err := addressclaim.Build(resultName, azureProfile, req)
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

// backfillFromProfile copies subscriptionID/resourceGroup from the
// CloudProviderProfile context resource onto the matched event payloads when the
// event omitted them. The profile spec is delivered redacted and secret-free.
func backfillFromProfile(req *addressclaim.PluginRequest) {
	var sub, rg string
	for _, r := range req.Spec.Context.Resources {
		if r.Kind != addressclaim.KindCloudProviderProfile {
			continue
		}
		sub = asString(r.Spec["subscriptionID"])
		rg = asString(r.Spec["resourceGroup"])
		break
	}
	if sub == "" && rg == "" {
		return
	}
	for i := range req.Spec.Events {
		ev := &req.Spec.Events[i]
		if ev.Type != addressclaim.ObservedEventType {
			continue
		}
		if ev.Payload == nil {
			ev.Payload = map[string]string{}
		}
		if sub != "" && strings.TrimSpace(ev.Payload["subscriptionId"]) == "" {
			ev.Payload["subscriptionId"] = sub
		}
		if rg != "" && strings.TrimSpace(ev.Payload["resourceGroup"]) == "" {
			ev.Payload["resourceGroup"] = rg
		}
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
