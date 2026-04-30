package pdmonitor

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	routerstate "routerd/pkg/state"
)

const apiVersion = "net.routerd.net/v1alpha1"

type HungResult struct {
	Resource string
	Changed  bool
	Hung     bool
	Reason   string
}

func CheckHung(store routerstate.Store, resourceNames []string, grace time.Duration) ([]HungResult, error) {
	if grace < 0 {
		grace = 0
	}
	var results []HungResult
	for _, name := range resourceNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		lease, ok := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation."+name)
		if !ok {
			continue
		}
		result, next := evaluateLease(store.Now().UTC(), name, lease, grace)
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

func evaluateLease(now time.Time, resourceName string, lease routerstate.PDLease, grace time.Duration) (HungResult, routerstate.PDLease) {
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
		if lease.Hung != nil && lease.Hung.SuspectedAt != "" {
			return HungResult{Resource: resourceName, Hung: true, Reason: lease.Hung.Reason}, lease
		}
		lease.Hung = &routerstate.PDHungStatus{SuspectedAt: now.Format(time.RFC3339Nano), Reason: reason}
		return HungResult{Resource: resourceName, Changed: true, Hung: true, Reason: reason}, lease
	}
	if lease.Hung != nil {
		lease.Hung = nil
		return HungResult{Resource: resourceName, Changed: true, Hung: false, Reason: "hung suspicion cleared"}, lease
	}
	return HungResult{Resource: resourceName}, lease
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
