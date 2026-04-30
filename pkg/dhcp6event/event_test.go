package dhcp6event

import (
	"strings"
	"testing"

	routerstate "routerd/pkg/state"
)

func TestApplyStoresLeaseFromExplicitFields(t *testing.T) {
	store := routerstate.New()
	lease, err := Apply(store, Event{
		Resource: "wan-pd",
		Reason:   "RENEW6",
		Prefix:   "2001:db8:1200:1230::/60",
		IAID:     "1",
		T1:       "7200",
		T2:       "12600",
		PLTime:   "14400",
		VLTime:   "14400",
		ServerID: "00:03:00:01:02:00:00:00:00:01",
		ClientID: "00:03:00:01:02:00:00:00:00:02",
	})
	if err != nil {
		t.Fatalf("apply event: %v", err)
	}
	if lease.CurrentPrefix != "2001:db8:1200:1230::/60" {
		t.Fatalf("current prefix = %q", lease.CurrentPrefix)
	}
	if lease.LastReplyAt == "" {
		t.Fatal("LastReplyAt is empty")
	}
	if lease.ServerID != "00030001020000000001" {
		t.Fatalf("server ID = %q", lease.ServerID)
	}
}

func TestApplyExtractsLeaseFromEnv(t *testing.T) {
	store := routerstate.New()
	lease, err := Apply(store, Event{
		Resource: "wan-pd",
		Env: map[string]string{
			"reason":            "BOUND6",
			"new_ia_pd_prefix":  "2001:db8:1200:1240::/60",
			"new_ia_pd_iaid":    "7",
			"new_ia_pd_t1":      "7200",
			"new_ia_pd_t2":      "12600",
			"new_ia_pd_pltime":  "14400",
			"new_ia_pd_vltime":  "14400",
			"new_server_id":     "00030001020000000001",
			"dhcp6_client_id":   "00030001020000000002",
			"server_link_local": "fe80::1",
			"source_mac":        "02:00:00:00:00:01",
		},
	})
	if err != nil {
		t.Fatalf("apply event: %v", err)
	}
	if lease.CurrentPrefix != "2001:db8:1200:1240::/60" || lease.IAID != "7" {
		t.Fatalf("lease = %#v", lease)
	}
	if !strings.Contains(store.Get("ipv6PrefixDelegation.wan-pd.lease").Value, `"lastReplyAt"`) {
		t.Fatalf("stored lease = %s", store.Get("ipv6PrefixDelegation.wan-pd.lease").Value)
	}
}
