package nat44

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/bus"
)

type testStore struct {
	status map[string]map[string]any
}

func (s *testStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if s.status == nil {
		s.status = map[string]map[string]any{}
	}
	s.status[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s *testStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if s.status == nil {
		return map[string]any{}
	}
	if status := s.status[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func TestControllerRendersDryRunNAT44FromEgressRoutePolicy(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "ipv4-default"}, Spec: api.EgressRoutePolicySpec{}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-wan"}, Spec: api.NAT44RuleSpec{
			Type:            "masquerade",
			EgressPolicyRef: "ipv4-default",
			SourceRanges:    []string{"192.168.0.0/16"},
		}},
	}}}
	store := &testStore{}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default", map[string]any{"phase": "Applied", "selectedDevice": "ds-lite"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "nat44.nft")
	controller := Controller{Router: router, Bus: bus.New(), Store: store, DryRun: true, NftablesPath: path}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ruleset: %v", err)
	}
	if !strings.Contains(string(data), `oifname "ds-lite" ip saddr 192.168.0.0/16 masquerade`) {
		t.Fatalf("ruleset missing masquerade:\n%s", string(data))
	}
	status := store.ObjectStatus(api.NetAPIVersion, "NAT44Rule", "lan-to-wan")
	if status["phase"] != "Active" || status["activeEgressInterface"] != "ds-lite" {
		t.Fatalf("status = %#v", status)
	}
}
