// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"sort"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	mobilitycontroller "github.com/imksoo/routerd/pkg/controller/mobility"
)

type gracefulStopFakeBGP struct {
	paths map[string]bgpdaemon.AppliedPath
}

func (f *gracefulStopFakeBGP) ListPaths(_ context.Context, source string) ([]bgpdaemon.AppliedPath, error) {
	var out []bgpdaemon.AppliedPath
	for _, path := range f.paths {
		if source == "" || path.Source == source {
			out = append(out, path)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source == out[j].Source {
			return out[i].Prefix < out[j].Prefix
		}
		return out[i].Source < out[j].Source
	})
	return out, nil
}

func (f *gracefulStopFakeBGP) UpsertPath(_ context.Context, path bgpdaemon.AppliedPath) (bgpdaemon.AppliedPath, error) {
	if f.paths == nil {
		f.paths = map[string]bgpdaemon.AppliedPath{}
	}
	path = bgpdaemon.NormalizeAppliedPath(path)
	f.paths[bgpdaemon.AppliedPathKey(path)] = path
	return path, nil
}

func (f *gracefulStopFakeBGP) DeletePath(_ context.Context, path bgpdaemon.AppliedPath) error {
	delete(f.paths, bgpdaemon.AppliedPathKey(bgpdaemon.NormalizeAppliedPath(path)))
	return nil
}

func TestRouterWithGracefulStopDrainMarksSelfPlacementMember(t *testing.T) {
	router := gracefulStopRouter()
	drained, ok := routerWithGracefulStopDrain(router)
	if !ok {
		t.Fatal("routerWithGracefulStopDrain returned ok=false")
	}
	spec, err := drained.Spec.Resources[1].MobilityPoolSpec()
	if err != nil {
		t.Fatalf("MobilityPoolSpec: %v", err)
	}
	if !spec.Members[0].Maintenance.Drain {
		t.Fatalf("self member drain = false, want true")
	}
	if spec.Members[1].Maintenance.Drain {
		t.Fatalf("peer member drain = true, want unchanged false")
	}
}

func TestGracefulStopTakeoverCompleteRequiresPeerActivePath(t *testing.T) {
	sourceA := mobilitycontroller.DynamicSource("cloudedge", "aws-router-a")
	sourceB := mobilitycontroller.DynamicSource("cloudedge", "aws-router-b")
	targets := []gracefulStopTarget{{PoolName: "cloudedge", SelfNode: "aws-router-a", Source: sourceA, Prefixes: []string{"10.88.60.11/32"}}}
	bgp := &gracefulStopFakeBGP{paths: map[string]bgpdaemon.AppliedPath{
		"a": bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{Source: sourceA, Prefix: "10.88.60.11/32", Family: bgpdaemon.AppliedPathFamilyIPv4Unicast, Attrs: bgpdaemon.AppliedPathAttrs{LocalPref: 200}}),
	}}
	complete, err := gracefulStopTakeoverComplete(context.Background(), bgp, targets)
	if err != nil {
		t.Fatalf("gracefulStopTakeoverComplete: %v", err)
	}
	if complete {
		t.Fatal("takeover complete with only self low-preference path")
	}
	bgp.paths["b"] = bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{Source: sourceB, Prefix: "10.88.60.11/32", Family: bgpdaemon.AppliedPathFamilyIPv4Unicast, Attrs: bgpdaemon.AppliedPathAttrs{LocalPref: 201}})
	complete, err = gracefulStopTakeoverComplete(context.Background(), bgp, targets)
	if err != nil {
		t.Fatalf("gracefulStopTakeoverComplete after peer path: %v", err)
	}
	if !complete {
		t.Fatal("takeover incomplete after peer active path")
	}
}

func gracefulStopRouter() *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec:     api.EventGroupSpec{NodeName: "aws-router-a"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec: api.MobilityPoolSpec{
					Prefix:   "10.88.60.0/24",
					GroupRef: "cloudedge",
					DeliveryPolicy: api.MobilityDeliveryPolicy{
						Mode: "bgp",
					},
					Members: []api.MobilityPoolMember{
						{NodeRef: "aws-router-a", Site: "aws", Role: "cloud", Placement: api.MobilityMemberPlacement{Group: "aws-edge", Priority: 10}},
						{NodeRef: "aws-router-b", Site: "aws", Role: "cloud", Placement: api.MobilityMemberPlacement{Group: "aws-edge", Priority: 20}},
					},
				},
			},
		}},
	}
}
