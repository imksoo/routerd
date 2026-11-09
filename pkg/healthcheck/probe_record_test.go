// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import (
	"testing"
	"time"
)

func TestStatusMapIncludesEvidenceAndHistory(t *testing.T) {
	state := State{
		Phase:        PhaseUnhealthy,
		LastResult:   ResultFailed,
		FailureCount: 3,
		LastEvidence: ProbeEvidence{FailureKind: FailureKindTimeout, NextHop: "203.0.113.1"},
		History: []ProbeRecord{{
			Time:          time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
			OK:            false,
			Result:        ResultTimeout,
			ProbeEvidence: ProbeEvidence{FailureKind: FailureKindTimeout},
		}},
	}
	status := StatusMap(state)
	if status["failureCount"] != 3 {
		t.Errorf("failureCount = %v", status["failureCount"])
	}
	ev, ok := status["lastEvidence"].(map[string]any)
	if !ok {
		t.Fatalf("lastEvidence missing or wrong type: %T", status["lastEvidence"])
	}
	if ev["failureKind"] != FailureKindTimeout {
		t.Errorf("evidence.failureKind = %v", ev["failureKind"])
	}
	history, ok := status["history"].([]map[string]any)
	if !ok || len(history) != 1 {
		t.Fatalf("history = %#v", status["history"])
	}
	if history[0]["result"] != ResultTimeout {
		t.Errorf("history[0].result = %v", history[0]["result"])
	}
}
