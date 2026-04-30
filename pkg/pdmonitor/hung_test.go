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
