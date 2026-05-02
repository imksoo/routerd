package pdmonitor

import (
	"testing"
	"time"

	routerstate "routerd/pkg/state"
)

func TestCheckHungMarksLeaseAfterT1Grace(t *testing.T) {
	store := routerstate.New()
	now := store.Now().UTC()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		CurrentPrefix: "2001:db8:1200:1230::/60",
		LastReplyAt:   now.Add(-95 * time.Second).Format(time.RFC3339Nano),
		T1:            "60",
	}), "test")

	results, err := CheckHung(store, []string{"wan-pd"}, 30*time.Second)
	if err != nil {
		t.Fatalf("check hung: %v", err)
	}
	if len(results) != 1 || !results[0].Changed || !results[0].Hung {
		t.Fatalf("results = %#v", results)
	}
	lease, _ := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation.wan-pd")
	if lease.Hung == nil || lease.Hung.SuspectedAt == "" {
		t.Fatalf("lease hung status = %#v", lease.Hung)
	}
}

func TestCheckHungClearsAfterFreshReply(t *testing.T) {
	store := routerstate.New()
	now := store.Now().UTC()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		CurrentPrefix: "2001:db8:1200:1230::/60",
		LastReplyAt:   now.Add(-10 * time.Second).Format(time.RFC3339Nano),
		T1:            "60",
		Hung:          &routerstate.PDHungStatus{SuspectedAt: now.Add(-time.Hour).Format(time.RFC3339Nano), Reason: "old"},
	}), "test")

	results, err := CheckHung(store, []string{"wan-pd"}, 30*time.Second)
	if err != nil {
		t.Fatalf("check hung: %v", err)
	}
	if len(results) != 1 || !results[0].Changed || results[0].Hung {
		t.Fatalf("results = %#v", results)
	}
	lease, _ := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation.wan-pd")
	if lease.Hung != nil {
		t.Fatalf("lease hung status = %#v", lease.Hung)
	}
}

func TestCheckHungEmptyModeDefaultsToManual(t *testing.T) {
	store := routerstate.New()
	now := store.Now().UTC()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		CurrentPrefix: "2001:db8:1200:1230::/60",
		LastReplyAt:   now.Add(-95 * time.Second).Format(time.RFC3339Nano),
		T1:            "60",
	}), "test")

	// Empty RecoveryMode must default to manual after the 2026-05-01 retreat:
	// routerd-driven active Request/Renew frames in parallel to the OS DHCPv6
	// client poisoned HGW per-client state in the lab, so the conservative
	// default is to NOT inject DHCPv6 packets from routerd. The hung monitor
	// records lease.Hung but does not schedule any RecoveryAction.
	results, err := CheckHungWithPolicies(store, []HungPolicy{{Resource: "wan-pd", RecoveryMode: ""}}, 30*time.Second, 5*time.Minute, 3)
	if err != nil {
		t.Fatalf("check hung: %v", err)
	}
	if len(results) != 1 || results[0].RecoveryAction != "" || results[0].RecoveryMode != RecoveryManual {
		t.Fatalf("results = %#v", results)
	}
	if !results[0].Hung {
		t.Fatalf("expected lease.Hung to be set, results = %#v", results)
	}
}

func TestCheckHungAutoRequestSchedulesRecovery(t *testing.T) {
	store := routerstate.New()
	now := store.Now().UTC()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		CurrentPrefix: "2001:db8:1200:1230::/60",
		LastReplyAt:   now.Add(-95 * time.Second).Format(time.RFC3339Nano),
		T1:            "60",
	}), "test")

	results, err := CheckHungWithPolicies(store, []HungPolicy{{Resource: "wan-pd", RecoveryMode: RecoveryAutoRequest}}, 30*time.Second, 5*time.Minute, 3)
	if err != nil {
		t.Fatalf("check hung: %v", err)
	}
	if len(results) != 1 || results[0].RecoveryAction != "request" {
		t.Fatalf("results = %#v", results)
	}
	lease, _ := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation.wan-pd")
	if lease.Hung == nil || lease.Hung.RecoveryAttempts != 1 || lease.Hung.RecoveryMode != RecoveryAutoRequest {
		t.Fatalf("lease hung status = %#v", lease.Hung)
	}
}

func TestCheckHungRecoveryBackoff(t *testing.T) {
	store := routerstate.New()
	now := store.Now().UTC()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		CurrentPrefix: "2001:db8:1200:1230::/60",
		LastReplyAt:   now.Add(-95 * time.Second).Format(time.RFC3339Nano),
		T1:            "60",
		Hung: &routerstate.PDHungStatus{
			SuspectedAt:           now.Add(-time.Minute).Format(time.RFC3339Nano),
			Reason:                "old",
			RecoveryMode:          RecoveryAutoRebind,
			RecoveryAttempts:      1,
			RecoveryLastAttemptAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
		},
	}), "test")

	results, err := CheckHungWithPolicies(store, []HungPolicy{{Resource: "wan-pd", RecoveryMode: RecoveryAutoRebind}}, 30*time.Second, 5*time.Minute, 3)
	if err != nil {
		t.Fatalf("check hung: %v", err)
	}
	if len(results) != 1 || results[0].RecoveryAction != "" || results[0].Changed {
		t.Fatalf("results = %#v", results)
	}
	lease, _ := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation.wan-pd")
	if lease.Hung == nil || lease.Hung.RecoveryAttempts != 1 {
		t.Fatalf("lease hung status = %#v", lease.Hung)
	}
}

func TestCheckHungRecoveryExhaustsToManual(t *testing.T) {
	store := routerstate.New()
	now := store.Now().UTC()
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(routerstate.PDLease{
		CurrentPrefix: "2001:db8:1200:1230::/60",
		LastReplyAt:   now.Add(-95 * time.Second).Format(time.RFC3339Nano),
		T1:            "60",
		Hung: &routerstate.PDHungStatus{
			SuspectedAt:           now.Add(-time.Hour).Format(time.RFC3339Nano),
			Reason:                "old",
			RecoveryMode:          RecoveryAutoRequest,
			RecoveryAttempts:      3,
			RecoveryLastAttemptAt: now.Add(-10 * time.Minute).Format(time.RFC3339Nano),
		},
	}), "test")

	results, err := CheckHungWithPolicies(store, []HungPolicy{{Resource: "wan-pd", RecoveryMode: RecoveryAutoRequest}}, 30*time.Second, 5*time.Minute, 3)
	if err != nil {
		t.Fatalf("check hung: %v", err)
	}
	if len(results) != 1 || results[0].RecoveryAction != "" || !results[0].Changed {
		t.Fatalf("results = %#v", results)
	}
	lease, _ := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation.wan-pd")
	if lease.Hung == nil || lease.Hung.RecoveryMode != RecoveryManual || lease.Hung.RecoveryExhaustedAt == "" {
		t.Fatalf("lease hung status = %#v", lease.Hung)
	}
}
