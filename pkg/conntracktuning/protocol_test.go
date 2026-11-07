// SPDX-License-Identifier: BSD-3-Clause

package conntracktuning

import (
	"testing"

	"github.com/imksoo/routerd/pkg/logstore"
)

func TestICMPObservationCannotAuthorizeTCPTimeoutChange(t *testing.T) {
	entry := logstore.FirewallLogEntry{Protocol: "icmp", Action: "drop", Correlation: "orphan_return"}
	summary := Analyze(Inputs{AutoApply: true, FirewallLogs: []logstore.FirewallLogEntry{entry}, DPIFlows: []logstore.DPIFlowEntry{{Protocol: "icmp"}}, ExpiredFlows: []logstore.ExpiredFlowEntry{{Protocol: "icmp"}}})
	if len(summary.Suggestions) != 0 {
		t.Fatalf("ICMP evidence authorized unrelated timeout changes: %+v", summary.Suggestions)
	}
	if got := RecommendationForEvent(entry); got.SysctlKey != "" || got.Application != "" {
		t.Fatalf("unsupported per-event recommendation: %+v", got)
	}
}
