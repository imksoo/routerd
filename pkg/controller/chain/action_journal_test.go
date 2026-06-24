// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/sam"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestLatestAssignedAddressesBasic(t *testing.T) {
	actions := []routerstate.ActionExecutionRecord{
		{ID: 1, ProviderRef: "p1", Action: "assign-secondary-ip", Status: routerstate.ActionSucceeded, TargetJSON: actionTargetJSON("10.0.0.1/32")},
		{ID: 2, ProviderRef: "p1", Action: "ensure-forwarding-enabled", Status: routerstate.ActionSucceeded, TargetJSON: actionTargetJSON("10.0.0.1/32")},
		{ID: 3, ProviderRef: "p2", Action: "assign-secondary-ip", Status: routerstate.ActionSucceeded, TargetJSON: actionTargetJSON("10.0.0.2")},
	}
	got := latestAssignedAddresses(actions)
	if !got["10.0.0.1"] {
		t.Fatalf("10.0.0.1 should be assigned; /32 suffix should be stripped")
	}
	if !got["10.0.0.2"] {
		t.Fatalf("10.0.0.2 should be assigned")
	}
	if len(got) != 2 {
		t.Fatalf("got %d addresses, want 2", len(got))
	}
}

func TestLatestAssignedAddressesUnassignRemoves(t *testing.T) {
	actions := []routerstate.ActionExecutionRecord{
		{ID: 1, ProviderRef: "p1", Action: "assign-secondary-ip", Status: routerstate.ActionSucceeded, TargetJSON: actionTargetJSON("10.0.0.1")},
		{ID: 2, ProviderRef: "p1", Action: "unassign-secondary-ip", Status: routerstate.ActionSucceeded, TargetJSON: actionTargetJSON("10.0.0.1")},
	}
	got := latestAssignedAddresses(actions)
	if got["10.0.0.1"] {
		t.Fatalf("10.0.0.1 should not be assigned after unassign")
	}
}

func TestLatestAssignedAddressesFailedAssignNotProtected(t *testing.T) {
	actions := []routerstate.ActionExecutionRecord{
		{ID: 1, ProviderRef: "p1", Action: "assign-secondary-ip", Status: routerstate.ActionFailed, TargetJSON: actionTargetJSON("10.0.0.1")},
	}
	got := latestAssignedAddresses(actions)
	if got["10.0.0.1"] {
		t.Fatalf("failed assign should not protect the address")
	}
}

func TestLatestAssignedAddressesDifferentProviderIndependent(t *testing.T) {
	actions := []routerstate.ActionExecutionRecord{
		{ID: 1, ProviderRef: "p1", Action: "assign-secondary-ip", Status: routerstate.ActionSucceeded, TargetJSON: actionTargetJSON("10.0.0.1")},
		{ID: 2, ProviderRef: "p2", Action: "unassign-secondary-ip", Status: routerstate.ActionSucceeded, TargetJSON: actionTargetJSON("10.0.0.1")},
	}
	got := latestAssignedAddresses(actions)
	if !got["10.0.0.1"] {
		t.Fatalf("p1 assign should survive p2 unassign (different providerRef)")
	}
}

func TestChainCleanupSkipsAssignedAddress(t *testing.T) {
	deleted := []string{}
	c := IPv4StaticAddressController{
		Router: chainCleanupRouter("10.0.0.0/24"),
		Store: &fakeChainStore{statuses: map[string]map[string]any{
			"mobility.routerd.net/v1alpha1/MobilityPool/pool1": {
				"discoverySelfPrivateIPs":       []any{},
				"discoverySelfCapturedAddresses": []any{},
			},
		}},
		AddressList: func(_ context.Context, _ string) ([]string, error) {
			return []string{"10.0.0.5/32", "10.0.0.6/32"}, nil
		},
		ListActions: func(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error) {
			return []routerstate.ActionExecutionRecord{
				{ID: 1, ProviderRef: "p1", Action: "assign-secondary-ip", Status: routerstate.ActionSucceeded, TargetJSON: actionTargetJSON("10.0.0.5/32")},
			}, nil
		},
		Command: func(_ context.Context, _ string, args ...string) error {
			for _, a := range args {
				if a == "10.0.0.5/32" || a == "10.0.0.6/32" {
					deleted = append(deleted, a)
				}
			}
			return nil
		},
	}
	if err := c.cleanupStaleMobilityProviderOSAddresses(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, d := range deleted {
		if d == "10.0.0.5/32" {
			t.Fatalf("10.0.0.5/32 was deleted but should be protected by action journal assign")
		}
	}
	found6 := false
	for _, d := range deleted {
		if d == "10.0.0.6/32" {
			found6 = true
		}
	}
	if !found6 {
		t.Fatalf("10.0.0.6/32 should be deleted (not protected)")
	}
}

func TestChainCleanupDeletesAfterUnassign(t *testing.T) {
	deleted := []string{}
	c := IPv4StaticAddressController{
		Router: chainCleanupRouter("10.0.0.0/24"),
		Store: &fakeChainStore{statuses: map[string]map[string]any{
			"mobility.routerd.net/v1alpha1/MobilityPool/pool1": {
				"discoverySelfPrivateIPs":       []any{},
				"discoverySelfCapturedAddresses": []any{},
			},
		}},
		AddressList: func(_ context.Context, _ string) ([]string, error) {
			return []string{"10.0.0.5/32"}, nil
		},
		ListActions: func(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error) {
			return []routerstate.ActionExecutionRecord{
				{ID: 1, ProviderRef: "p1", Action: "assign-secondary-ip", Status: routerstate.ActionSucceeded, TargetJSON: actionTargetJSON("10.0.0.5/32")},
				{ID: 2, ProviderRef: "p1", Action: "unassign-secondary-ip", Status: routerstate.ActionSucceeded, TargetJSON: actionTargetJSON("10.0.0.5/32")},
			}, nil
		},
		Command: func(_ context.Context, _ string, args ...string) error {
			for _, a := range args {
				if a == "10.0.0.5/32" {
					deleted = append(deleted, a)
				}
			}
			return nil
		},
	}
	if err := c.cleanupStaleMobilityProviderOSAddresses(context.Background()); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range deleted {
		if d == "10.0.0.5/32" {
			found = true
		}
	}
	if !found {
		t.Fatalf("10.0.0.5/32 should be deleted after unassign")
	}
}

func TestChainCleanupIgnoresOutOfPoolAddress(t *testing.T) {
	deleted := []string{}
	c := IPv4StaticAddressController{
		Router: chainCleanupRouter("10.0.0.0/24"),
		Store: &fakeChainStore{statuses: map[string]map[string]any{
			"mobility.routerd.net/v1alpha1/MobilityPool/pool1": {
				"discoverySelfPrivateIPs":       []any{},
				"discoverySelfCapturedAddresses": []any{},
			},
		}},
		AddressList: func(_ context.Context, _ string) ([]string, error) {
			return []string{"10.0.0.5/32"}, nil
		},
		ListActions: func(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error) {
			return []routerstate.ActionExecutionRecord{
				{ID: 1, ProviderRef: "p1", Action: "assign-secondary-ip", Status: routerstate.ActionSucceeded, TargetJSON: actionTargetJSON("10.99.0.5/32")},
			}, nil
		},
		Command: func(_ context.Context, _ string, args ...string) error {
			for _, a := range args {
				if a == "10.0.0.5/32" {
					deleted = append(deleted, a)
				}
			}
			return nil
		},
	}
	if err := c.cleanupStaleMobilityProviderOSAddresses(context.Background()); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range deleted {
		if d == "10.0.0.5/32" {
			found = true
		}
	}
	if !found {
		t.Fatalf("10.0.0.5/32 should be deleted; 10.99.0.5 assign is out of pool")
	}
}

func TestSAMCleanupSkipsAssignedCapture(t *testing.T) {
	store := &samStore{
		objects: map[string]map[string]any{},
		statuses: []routerstate.ObjectStatus{
			{
				APIVersion: api.HybridAPIVersion,
				Kind:       "RemoteAddressClaim",
				Name:       "claim-10.0.0.5",
				Status: map[string]any{
					"captureProxyNeighbor": map[string]any{
						"address":   "10.0.0.5/32",
						"interface": "eth0",
					},
				},
			},
		},
		deleted: map[string]bool{},
	}
	c := SAMController{
		Router: &api.Router{Spec: api.RouterSpec{}},
		Store:  store,
		ListActions: func(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error) {
			return []routerstate.ActionExecutionRecord{
				{ID: 1, ProviderRef: "p1", Action: "assign-secondary-ip", Status: routerstate.ActionSucceeded, TargetJSON: actionTargetJSON("10.0.0.5/32")},
			}, nil
		},
		Applier: &noopSAMApplier{},
	}
	if err := c.cleanupRemovedCaptures(context.Background(), store.statuses); err != nil {
		t.Fatal(err)
	}
	key := api.HybridAPIVersion + "/RemoteAddressClaim/claim-10.0.0.5"
	if store.deleted[key] {
		t.Fatalf("claim-10.0.0.5 should not be deleted; address is protected by action journal assign")
	}
}

func TestSAMCleanupDeletesAfterUnassign(t *testing.T) {
	store := &samStore{
		objects: map[string]map[string]any{},
		statuses: []routerstate.ObjectStatus{
			{
				APIVersion: api.HybridAPIVersion,
				Kind:       "RemoteAddressClaim",
				Name:       "claim-10.0.0.5",
				Status: map[string]any{
					"captureProxyNeighbor": map[string]any{
						"address":   "10.0.0.5/32",
						"interface": "eth0",
					},
				},
			},
		},
		deleted: map[string]bool{},
	}
	c := SAMController{
		Router: &api.Router{Spec: api.RouterSpec{}},
		Store:  store,
		ListActions: func(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error) {
			return []routerstate.ActionExecutionRecord{
				{ID: 1, ProviderRef: "p1", Action: "assign-secondary-ip", Status: routerstate.ActionSucceeded, TargetJSON: actionTargetJSON("10.0.0.5/32")},
				{ID: 2, ProviderRef: "p1", Action: "unassign-secondary-ip", Status: routerstate.ActionSucceeded, TargetJSON: actionTargetJSON("10.0.0.5/32")},
			}, nil
		},
		Applier: &noopSAMApplier{},
	}
	if err := c.cleanupRemovedCaptures(context.Background(), store.statuses); err != nil {
		t.Fatal(err)
	}
	key := api.HybridAPIVersion + "/RemoteAddressClaim/claim-10.0.0.5"
	if !store.deleted[key] {
		t.Fatalf("claim-10.0.0.5 should be deleted after unassign")
	}
}

// --- test helpers ---

type fakeChainStore struct {
	statuses map[string]map[string]any
}

func (s *fakeChainStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if s.statuses != nil {
		s.statuses[apiVersion+"/"+kind+"/"+name] = status
	}
	return nil
}

func (s *fakeChainStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if s.statuses != nil {
		if st := s.statuses[apiVersion+"/"+kind+"/"+name]; st != nil {
			return st
		}
	}
	return map[string]any{}
}

type noopSAMApplier struct{}

func (a *noopSAMApplier) SetProxyARP(_ context.Context, _ string, _ bool) error { return nil }
func (a *noopSAMApplier) EnsureProxyNeighbor(_ context.Context, _, _ string) error {
	return nil
}
func (a *noopSAMApplier) DeleteProxyNeighbor(_ context.Context, _, _ string) error { return nil }
func (a *noopSAMApplier) EnsureOSAddressAbsent(_ context.Context, _ string) (samOSAddressDeassignResult, error) {
	return samOSAddressDeassignResult{}, nil
}
func (a *noopSAMApplier) ReconcileForwardPaths(_ context.Context, _ []sam.CaptureAction) error {
	return nil
}

func chainCleanupRouter(prefix string) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
			Metadata: api.ObjectMeta{Name: "group1"},
			Spec:     api.EventGroupSpec{NodeName: "self"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
			Metadata: api.ObjectMeta{Name: "pool1"},
			Spec: api.MobilityPoolSpec{
				Prefix:   prefix,
				GroupRef: "group1",
				Members: []api.MobilityPoolMember{{
					NodeRef: "self",
					Site:    "site1",
					Role:    "cloud",
					Capture: api.MobilityMemberCapture{
						Type:               "provider-secondary-ip",
						Interface:          "eth0",
						ConfigureOSAddress: true,
					},
				}},
			},
		},
	}}}
}
