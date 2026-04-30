package pdstrategy

import (
	"testing"
	"time"

	routerstate "routerd/pkg/state"
)

func TestDecideNextActionHybridFallsBackAfterBudget(t *testing.T) {
	lease := routerstate.PDLease{Acquisition: &routerstate.PDAcquisitionStatus{AttemptsSinceReply: 2}}
	if got := DecideNextAction(lease, "hybrid", 3); got != ActionSolicit {
		t.Fatalf("next action = %q, want solicit", got)
	}
	lease.Acquisition.AttemptsSinceReply = 3
	if got := DecideNextAction(lease, "hybrid", 3); got != ActionRequestClaim {
		t.Fatalf("next action = %q, want request-claim", got)
	}
}

func TestRecordAttemptAndReply(t *testing.T) {
	now := time.Date(2026, 4, 30, 1, 2, 3, 0, time.UTC)
	lease := RecordAttempt(routerstate.PDLease{}, "hybrid", ActionRequestClaim, now)
	if lease.Acquisition == nil || lease.Acquisition.AttemptsSinceReply != 1 || lease.Acquisition.Phase != PhaseRequesting {
		t.Fatalf("after attempt = %#v", lease.Acquisition)
	}
	lease = RecordReply(lease, "hybrid")
	if lease.Acquisition.AttemptsSinceReply != 0 || lease.Acquisition.Phase != PhaseAcquired {
		t.Fatalf("after reply = %#v", lease.Acquisition)
	}
}
