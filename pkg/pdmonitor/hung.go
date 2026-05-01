package pdmonitor

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"routerd/pkg/pdstrategy"
	routerstate "routerd/pkg/state"
)

const apiVersion = "net.routerd.net/v1alpha1"

type HungResult struct {
	Resource       string
	Changed        bool
	Hung           bool
	Reason         string
	RecoveryAction string
	RecoveryMode   string
}

type HungPolicy struct {
	Resource     string
	RecoveryMode string
}

const (
	RecoveryManual      = "manual"
	RecoveryAutoRequest = "auto-request"
	RecoveryAutoRebind  = "auto-rebind"
)

func CheckHung(store routerstate.Store, resourceNames []string, grace time.Duration) ([]HungResult, error) {
	policies := make([]HungPolicy, 0, len(resourceNames))
	for _, name := range resourceNames {
		policies = append(policies, HungPolicy{Resource: name, RecoveryMode: RecoveryManual})
	}
	return CheckHungWithPolicies(store, policies, grace, 5*time.Minute, 3)
}

func CheckHungWithPolicies(store routerstate.Store, policies []HungPolicy, grace, recoveryBackoff time.Duration, recoveryMaxAttempts int) ([]HungResult, error) {
	if grace < 0 {
		grace = 0
	}
	if recoveryBackoff < 0 {
		recoveryBackoff = 0
	}
	if recoveryMaxAttempts < 0 {
		recoveryMaxAttempts = 0
	}
	var results []HungResult
	for _, policy := range policies {
		name := strings.TrimSpace(policy.Resource)
		if name == "" {
			continue
		}
		lease, ok := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation."+name)
		if !ok {
			continue
		}
		result, next := evaluateLease(store.Now().UTC(), policy, lease, grace, recoveryBackoff, recoveryMaxAttempts)
		if result.Changed {
			store.Set("ipv6PrefixDelegation."+name+".lease", routerstate.EncodePDLease(next), "PDHungCheck")
			if recorder, ok := store.(routerstate.EventRecorder); ok {
				eventType := "Warning"
				reason := "HGWHungSuspected"
				message := result.Reason
				if !result.Hung {
					eventType = "Normal"
					reason = "HGWHungCleared"
					message = "DHCPv6-PD reply observed after hung suspicion"
				}
				_ = recorder.RecordEvent(apiVersion, "IPv6PrefixDelegation", name, eventType, reason, message)
			}
		}
		results = append(results, result)
	}
	return results, nil
}

func evaluateLease(now time.Time, policy HungPolicy, lease routerstate.PDLease, grace, recoveryBackoff time.Duration, recoveryMaxAttempts int) (HungResult, routerstate.PDLease) {
	resourceName := strings.TrimSpace(policy.Resource)
	lastReply, err := time.Parse(time.RFC3339Nano, lease.LastReplyAt)
	if err != nil || lastReply.IsZero() {
		return HungResult{Resource: resourceName}, lease
	}
	t1, ok := parseSeconds(lease.T1)
	if !ok || t1 <= 0 {
		return HungResult{Resource: resourceName}, lease
	}
	deadline := lastReply.Add(time.Duration(t1) * time.Second).Add(grace)
	if now.After(deadline) {
		reason := fmt.Sprintf("no DHCPv6-PD Reply observed since %s; expected renewal by %s", lastReply.Format(time.RFC3339), deadline.Format(time.RFC3339))
		changed := false
		if lease.Hung == nil || lease.Hung.SuspectedAt == "" {
			lease.Hung = &routerstate.PDHungStatus{SuspectedAt: now.Format(time.RFC3339Nano), Reason: reason}
			strategy := ""
			if lease.Acquisition != nil {
				strategy = lease.Acquisition.Strategy
			}
			lease = pdstrategy.UpdateNextAction(lease, strategy, 3)
			changed = true
		} else if lease.Hung.Reason == "" {
			lease.Hung.Reason = reason
			changed = true
		}
		action, recoveredLease, recoveryChanged := scheduleRecovery(now, policy.RecoveryMode, lease, recoveryBackoff, recoveryMaxAttempts)
		if recoveryChanged {
			lease = recoveredLease
			changed = true
		}
		if lease.Hung != nil && lease.Hung.Reason != "" {
			reason = lease.Hung.Reason
		}
		return HungResult{Resource: resourceName, Changed: changed, Hung: true, Reason: reason, RecoveryAction: action, RecoveryMode: normalizedRecoveryMode(policy.RecoveryMode)}, lease
	}
	if lease.Hung != nil {
		lease.Hung = nil
		return HungResult{Resource: resourceName, Changed: true, Hung: false, Reason: "hung suspicion cleared"}, lease
	}
	return HungResult{Resource: resourceName}, lease
}

func scheduleRecovery(now time.Time, mode string, lease routerstate.PDLease, backoff time.Duration, maxAttempts int) (string, routerstate.PDLease, bool) {
	mode = normalizedRecoveryMode(mode)
	if mode == RecoveryManual || lease.Hung == nil {
		return "", lease, false
	}
	if maxAttempts <= 0 {
		if lease.Hung.RecoveryExhaustedAt == "" {
			lease.Hung.RecoveryMode = RecoveryManual
			lease.Hung.RecoveryExhaustedAt = now.Format(time.RFC3339Nano)
			return "", lease, true
		}
		return "", lease, false
	}
	if lease.Hung.RecoveryExhaustedAt != "" {
		return "", lease, false
	}
	if lease.Hung.RecoveryAttempts >= maxAttempts {
		lease.Hung.RecoveryMode = RecoveryManual
		lease.Hung.RecoveryExhaustedAt = now.Format(time.RFC3339Nano)
		return "", lease, true
	}
	if lease.Hung.RecoveryLastAttemptAt != "" && backoff > 0 {
		lastAttempt, err := time.Parse(time.RFC3339Nano, lease.Hung.RecoveryLastAttemptAt)
		if err == nil && now.Before(lastAttempt.Add(backoff)) {
			return "", lease, false
		}
	}
	lease.Hung.RecoveryMode = mode
	lease.Hung.RecoveryAttempts++
	lease.Hung.RecoveryLastAttemptAt = now.Format(time.RFC3339Nano)
	if backoff > 0 {
		lease.Hung.RecoveryNextAttemptAt = now.Add(backoff).Format(time.RFC3339Nano)
	} else {
		lease.Hung.RecoveryNextAttemptAt = ""
	}
	switch mode {
	case RecoveryAutoRequest:
		return "request", lease, true
	case RecoveryAutoRebind:
		return "rebind", lease, true
	default:
		return "", lease, true
	}
}

func normalizedRecoveryMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case RecoveryAutoRequest:
		return RecoveryAutoRequest
	case RecoveryAutoRebind:
		return RecoveryAutoRebind
	default:
		return RecoveryManual
	}
}

func parseSeconds(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}
