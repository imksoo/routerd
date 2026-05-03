package daemonapi

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDaemonStatusJSONContract(t *testing.T) {
	status := NewStatus(DaemonRef{Name: "routerd-dhcpv6-client-wan-pd", Kind: "routerd-dhcpv6-client"})
	status.Phase = PhaseRunning
	status.Health = HealthOK
	status.Since = time.Date(2026, 5, 2, 6, 0, 0, 0, time.UTC)
	status.Resources = []ResourceStatus{{
		Resource: ResourceRef{APIVersion: "net.routerd.net/v1alpha1", Kind: "DHCPv6PrefixDelegation", Name: "wan-pd"},
		Phase:    ResourcePhaseBound,
		Health:   HealthOK,
		Conditions: []Condition{{
			Type:    "LeaseReady",
			Status:  ConditionTrue,
			Reason:  "Bound",
			Message: "delegated prefix is active",
		}},
		Observed: map[string]string{"currentPrefix": "2001:db8:1200:1240::/60"},
	}}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var decoded DaemonStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.APIVersion != APIVersion || decoded.Kind != KindDaemonStatus {
		t.Fatalf("type meta = %s/%s", decoded.APIVersion, decoded.Kind)
	}
	if decoded.Resources[0].Observed["currentPrefix"] != "2001:db8:1200:1240::/60" {
		t.Fatalf("observed prefix not preserved: %+v", decoded.Resources[0].Observed)
	}
	if len(decoded.Resources[0].Conditions) != 1 || decoded.Resources[0].Conditions[0].Status != ConditionTrue {
		t.Fatalf("conditions not preserved: %+v", decoded.Resources[0].Conditions)
	}
}

func TestDaemonEventJSONContract(t *testing.T) {
	event := NewEvent(DaemonRef{Name: "routerd-dhcpv6-client-wan-pd", Kind: "routerd-dhcpv6-client"}, EventDHCPv6PrefixBound, SeverityInfo)
	event.Time = time.Date(2026, 5, 2, 6, 0, 1, 0, time.UTC)
	event.Resource = &ResourceRef{APIVersion: "net.routerd.net/v1alpha1", Kind: "DHCPv6PrefixDelegation", Name: "wan-pd"}
	event.Attributes = map[string]string{"prefix": "2001:db8:1200:1240::/60"}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var decoded DaemonEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != EventDHCPv6PrefixBound {
		t.Fatalf("event type = %q", decoded.Type)
	}
	if decoded.Resource == nil || decoded.Resource.Name != "wan-pd" {
		t.Fatalf("resource not preserved: %+v", decoded.Resource)
	}
}

func TestCommandVocabulary(t *testing.T) {
	got := []string{CommandRenew, CommandRebind, CommandRelease, CommandReload, CommandStop, CommandStart, CommandFlush}
	want := []string{"renew", "rebind", "release", "reload", "stop", "start", "flush"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("command[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
