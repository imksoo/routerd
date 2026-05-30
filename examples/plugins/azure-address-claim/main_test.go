// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/imksoo/routerd/examples/plugins/internal/addressclaim"
)

// TestRunBackfillsProfileSubscription exercises the azure main end-to-end via
// stdin->stdout and asserts that subscriptionId/resourceGroup are backfilled
// from the CloudProviderProfile context (not the event payload) into the
// assign-secondary-ip plan target.
func TestRunBackfillsProfileSubscription(t *testing.T) {
	req := `{
      "spec": {
        "events": [
          {"id":"e1","type":"routerd.client.ipv4.observed","subject":"10.88.60.9/32",
           "payload":{"domain":"d","ownerSide":"onprem","nicRef":"nic-1"}}
        ],
        "context": {
          "resources": [
            {"apiVersion":"hybrid.routerd.net/v1alpha1","kind":"CloudProviderProfile","name":"az-tokyo","spec":{"provider":"azure","subscriptionID":"sub-123","resourceGroup":"rg-edge"}},
            {"apiVersion":"hybrid.routerd.net/v1alpha1","kind":"AddressMobilityDomain","name":"d","spec":{}},
            {"apiVersion":"hybrid.routerd.net/v1alpha1","kind":"OverlayPeer","name":"onprem-main","spec":{}}
          ]
        }
      }
    }`

	var out bytes.Buffer
	if err := run(strings.NewReader(req), &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	var res addressclaim.PluginResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("decode result: %v\n%s", err, out.String())
	}

	var assign addressclaim.ActionPlan
	for _, p := range res.Status.ActionPlans {
		if p.Action == addressclaim.ActionAssignSecondaryIP {
			assign = p
		}
	}
	if assign.Target["subscriptionId"] != "sub-123" {
		t.Errorf("target.subscriptionId = %q, want sub-123 (from profile)", assign.Target["subscriptionId"])
	}
	if assign.Target["resourceGroup"] != "rg-edge" {
		t.Errorf("target.resourceGroup = %q, want rg-edge (from profile)", assign.Target["resourceGroup"])
	}
	if assign.Provider != "azure" {
		t.Errorf("provider = %q, want azure", assign.Provider)
	}
}
