package pdstrategy

import (
	"strings"
	"time"

	"routerd/pkg/api"
	routerstate "routerd/pkg/state"
)

const (
	ActionSolicit      = "solicit"
	ActionRequestClaim = "request-claim"
	ActionRenew        = "renew"
	ActionRebind       = "rebind"
	ActionRelease      = "release"
	PhaseAcquired      = "acquired"
	PhaseSoliciting    = "soliciting"
	PhaseRequesting    = "requesting"
	PhaseRenewing      = "renewing"
	PhaseRebinding     = "rebinding"
	PhaseReleased      = "released"
)

func EffectiveStrategy(profile, configured string) string {
	if trimmed := strings.TrimSpace(configured); trimmed != "" {
		return trimmed
	}
	if api.IsNTTIPv6PDProfile(profile) {
		return "hybrid"
	}
	return "solicit-only"
}

func RecordReply(lease routerstate.PDLease, strategy string) routerstate.PDLease {
	status := ensure(lease.Acquisition)
	status.Strategy = firstNonEmpty(strategy, status.Strategy)
	status.Phase = PhaseAcquired
	status.AttemptsSinceReply = 0
	status.NextAction = ""
	lease.Acquisition = status
	return lease
}

func RecordAttempt(lease routerstate.PDLease, strategy, action string, now time.Time) routerstate.PDLease {
	status := ensure(lease.Acquisition)
	status.Strategy = firstNonEmpty(strategy, status.Strategy)
	status.LastAttemptAt = now.UTC().Format(time.RFC3339Nano)
	status.AttemptsSinceReply++
	status.NextAction = ""
	switch action {
	case ActionRequestClaim:
		status.Phase = PhaseRequesting
	case ActionRenew:
		status.Phase = PhaseRenewing
	case ActionRebind:
		status.Phase = PhaseRebinding
	case ActionRelease:
		status.Phase = PhaseReleased
	default:
		status.Phase = PhaseSoliciting
	}
	lease.Acquisition = status
	return lease
}

func DecideNextAction(lease routerstate.PDLease, strategy string, retryBudget int) string {
	if retryBudget <= 0 {
		retryBudget = 3
	}
	switch strategy {
	case "request-claim-only":
		return ActionRequestClaim
	case "solicit-only":
		return ActionSolicit
	case "hybrid", "":
		if lease.Acquisition != nil && lease.Acquisition.AttemptsSinceReply >= retryBudget {
			return ActionRequestClaim
		}
		return ActionSolicit
	default:
		return ActionSolicit
	}
}

func UpdateNextAction(lease routerstate.PDLease, strategy string, retryBudget int) routerstate.PDLease {
	status := ensure(lease.Acquisition)
	status.Strategy = firstNonEmpty(strategy, status.Strategy)
	status.NextAction = DecideNextAction(lease, status.Strategy, retryBudget)
	lease.Acquisition = status
	return lease
}

func ensure(status *routerstate.PDAcquisitionStatus) *routerstate.PDAcquisitionStatus {
	if status != nil {
		return status
	}
	return &routerstate.PDAcquisitionStatus{}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
