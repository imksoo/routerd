// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"encoding/json"
	"net/netip"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDiscoveryCurrentTrapAddressesUsesCurrentPendingCaptureJournalRecords(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	journalRecord := func(id int64, key, address, status string, updatedAt time.Time) routerstate.ActionExecutionRecord {
		target, err := json.Marshal(map[string]string{
			"address":     address,
			"nicRef":      "eni-a",
			"providerRef": "aws-provider",
		})
		if err != nil {
			t.Fatalf("marshal target: %v", err)
		}
		parameters, err := json.Marshal(map[string]string{captureParamHolder: "aws-router-a"})
		if err != nil {
			t.Fatalf("marshal parameters: %v", err)
		}
		return routerstate.ActionExecutionRecord{
			ID:             id,
			IdempotencyKey: key,
			Provider:       "aws",
			ProviderRef:    "aws-provider",
			Action:         actionAssignSecondaryIP,
			TargetJSON:     string(target),
			ParametersJSON: string(parameters),
			Status:         status,
			UpdatedAt:      updatedAt,
		}
	}

	history := newProviderActionHistoryWithRevision(nil, []routerstate.ActionExecutionRecord{
		// Discovery must see only the latest record for an idempotency key.
		journalRecord(1, "replaced", "10.88.60.10/32", routerstate.ActionPending, now),
		journalRecord(2, "replaced", "10.88.60.11/32", routerstate.ActionRunning, now.Add(time.Second)),
		journalRecord(3, "approved", "10.88.60.12/32", routerstate.ActionApproved, now),
		journalRecord(4, "failed", "10.88.60.13/32", routerstate.ActionFailed, now),
		journalRecord(5, "succeeded", "10.88.60.14/32", routerstate.ActionSucceeded, now),
	}, "")

	pool := NormalizedMobilityPool{
		SelfNode: "aws-router-a",
		Self: memberPlanInfo{
			NodeRef: "aws-router-a",
			Capture: api.MobilityMemberCapture{
				Type:        "provider-secondary-ip",
				ProviderRef: "aws-provider",
				NICRef:      "eni-a",
			},
		},
	}
	got := discoveryCurrentTrapAddresses(history, pool, "aws-provider", netip.MustParsePrefix("10.88.60.0/24"))
	want := map[string]bool{
		"10.88.60.11/32": true,
		"10.88.60.12/32": true,
	}
	if len(got) != len(want) {
		t.Fatalf("trap addresses = %#v, want %#v", got, want)
	}
	for address := range want {
		if !got[address] {
			t.Fatalf("trap addresses = %#v, missing %s", got, address)
		}
	}
}

func TestProviderActionHistoryReleaseSource(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	const address = "10.88.60.10/32"
	self := memberPlanInfo{
		NodeRef: "aws-router-a",
		Capture: api.MobilityMemberCapture{
			Type:        "provider-secondary-ip",
			ProviderRef: "aws-provider",
			NICRef:      "eni-a",
		},
	}
	record := func(id int64, key, action, nicRef, pathSig string, at time.Time) routerstate.ActionExecutionRecord {
		target, err := json.Marshal(map[string]string{
			"address":         address,
			"captureStrategy": captureStrategySecondaryIP,
			"nicRef":          nicRef,
			"providerRef":     "aws-provider",
		})
		if err != nil {
			t.Fatalf("marshal target: %v", err)
		}
		parameters, err := json.Marshal(map[string]string{
			bgpPathSigParam:    pathSig,
			captureParamHolder: self.NodeRef,
		})
		if err != nil {
			t.Fatalf("marshal parameters: %v", err)
		}
		return routerstate.ActionExecutionRecord{
			ID:             id,
			IdempotencyKey: key,
			Provider:       "aws",
			ProviderRef:    "aws-provider",
			Action:         action,
			TargetJSON:     string(target),
			ParametersJSON: string(parameters),
			Status:         routerstate.ActionSucceeded,
			ExecutedAt:     at,
			UpdatedAt:      at,
		}
	}

	t.Run("active plan takes precedence", func(t *testing.T) {
		active := dynamicconfig.ActionPlan{
			Action:         actionAssignSecondaryIP,
			IdempotencyKey: "active-fence",
			ProviderRef:    "aws-provider",
			Target: map[string]string{
				"address":         address,
				"captureStrategy": captureStrategySecondaryIP,
				"nicRef":          "eni-previous",
				"providerRef":     "aws-provider",
			},
			Parameters: map[string]string{bgpPathSigParam: "prefix=" + address + ":active"},
		}
		history := newProviderActionHistoryWithRevision([]dynamicconfig.ActionPlan{active}, []routerstate.ActionExecutionRecord{
			record(1, "journal-fence", actionAssignSecondaryIP, "eni-a", "prefix="+address+":journal", now),
		}, "")
		source, ok := history.releaseSourceFor(self, address)
		if !ok {
			t.Fatal("release source missing for active assignment")
		}
		if source.Capture.NICRef != "eni-previous" || source.PathSig != "prefix="+address+":active" {
			t.Fatalf("release source = %#v, want active target and path fence", source)
		}
	})

	t.Run("current succeeded assign is available", func(t *testing.T) {
		history := newProviderActionHistoryWithRevision(nil, []routerstate.ActionExecutionRecord{
			record(1, "assign-fence", actionAssignSecondaryIP, "eni-a", "prefix="+address+":assign", now),
		}, "")
		source, ok := history.releaseSourceFor(self, address)
		if !ok {
			t.Fatal("release source missing for current successful assign")
		}
		if source.Capture.NICRef != "eni-a" || source.PathSig != "prefix="+address+":assign" {
			t.Fatalf("release source = %#v, want current assign target and path fence", source)
		}
	})

	t.Run("later unassign is never a release source", func(t *testing.T) {
		history := newProviderActionHistoryWithRevision(nil, []routerstate.ActionExecutionRecord{
			record(1, "assign-fence", actionAssignSecondaryIP, "eni-a", "prefix="+address+":assign", now),
			record(2, "unassign-fence", actionUnassignSecondaryIP, "eni-a", "deprovision:"+address, now.Add(time.Second)),
		}, "")
		if source, ok := history.releaseSourceFor(self, address); ok {
			t.Fatalf("release source = %#v, want later unassign excluded", source)
		}
	})
}
