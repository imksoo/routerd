// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// dynamicRouteSAMStore is intentionally a small store double for consumers of
// the typed dynamic-config view. Legacy claim fixtures do not belong here.
type dynamicRouteSAMStore struct {
	records   []routerstate.DynamicConfigPartRecord
	objects   map[string]map[string]any
	listCalls int
}

func (s *dynamicRouteSAMStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if s.objects != nil {
		s.objects[apiVersion+"/"+kind+"/"+name] = status
	}
	return nil
}
func (s *dynamicRouteSAMStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if s.objects != nil && s.objects[apiVersion+"/"+kind+"/"+name] != nil {
		return s.objects[apiVersion+"/"+kind+"/"+name]
	}
	return map[string]any{}
}
func (s *dynamicRouteSAMStore) ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error) {
	s.listCalls++
	return s.records, nil
}

func dynamicPartRecord(t *testing.T, source string, resources []api.Resource, expiresAt time.Time) routerstate.DynamicConfigPartRecord {
	t.Helper()
	raw, err := json.Marshal(resources)
	if err != nil {
		t.Fatalf("marshal resources: %v", err)
	}
	return routerstate.DynamicConfigPartRecord{Source: source, Generation: 1, ObservedAt: time.Now().UTC(), ExpiresAt: expiresAt, Digest: source + "-digest", ResourcesJSON: string(raw), Status: "active"}
}

func countResources(router *api.Router, apiVersion, kind string) int {
	if router == nil {
		return 0
	}
	n := 0
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind {
			n++
		}
	}
	return n
}

func resourceByName(t *testing.T, router *api.Router, apiVersion, kind, name string) api.Resource {
	t.Helper()
	for _, resource := range router.Spec.Resources {
		if router != nil && resource.APIVersion == apiVersion && resource.Kind == kind && resource.Metadata.Name == name {
			return resource
		}
	}
	t.Fatalf("resource %s/%s/%s not found", apiVersion, kind, name)
	return api.Resource{}
}

func TestBuildDynamicRouteSAMViewReadsTypedMobilityPlanFromOneSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	plan, err := json.Marshal(dynamicconfig.MobilityDataplanePlan{
		PoolPrefix: "10.77.60.0/24",
		Captures: []dynamicconfig.LocalCaptureIntent{{
			ID: "cloudedge/10.77.60.9", PoolRef: "cloudedge", Address: "10.77.60.9/32",
			Disposition: dynamicconfig.CaptureDesired, CaptureType: "proxy-arp", CaptureInterface: "ens3",
		}},
		Routes: []dynamicconfig.MobilityIPv4RouteIntent{{
			ID: "cloudedge/local-10.77.60.20", PoolRef: "cloudedge", Purpose: dynamicconfig.MobilityIPv4RoutePurposeLocalInventory,
			Destination: "10.77.60.20/32", Device: "ens3", Metric: 1,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &dynamicRouteSAMStore{records: []routerstate.DynamicConfigPartRecord{{
		Source: "MobilityPool/cloudedge/node/router-a", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(5 * time.Minute),
		Digest: "sha256:typed-plan", MobilityDataplaneJSON: string(plan), Status: "active",
	}}}
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test-router"}}
	view, err := buildDynamicRouteSAMView(router, store, now, platform.OSLinux)
	if err != nil {
		t.Fatal(err)
	}
	if store.listCalls != 1 {
		t.Fatalf("dynamic part list calls = %d, want 1", store.listCalls)
	}
	if len(view.MobilityDataplane.Captures) != 1 || len(view.MobilityDataplane.Routes) != 1 {
		t.Fatalf("mobility dataplane = %#v", view.MobilityDataplane)
	}
	if countResources(view.EffectiveRouter, api.NetAPIVersion, "IPv4Route") != 0 || countResources(view.RouteRouter, api.NetAPIVersion, "IPv4Route") != 0 {
		t.Fatalf("typed mobility effects must not be projected into generic IPv4Route resources: effective=%#v route=%#v", view.EffectiveRouter.Spec.Resources, view.RouteRouter.Spec.Resources)
	}
}

func TestBuildDynamicRouteSAMViewIgnoresStaleGenericMobilityPayload(t *testing.T) {
	now := time.Date(2026, 8, 18, 13, 0, 0, 0, time.UTC)
	legacy := dynamicPartRecord(t, "MobilityPool/cloudedge/node/router-a", []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
		Metadata: api.ObjectMeta{Name: "legacy-bgp-capture-route"},
		Spec:     api.IPv4RouteSpec{Destination: "10.77.60.9/32", Device: "ens3"},
	}}, now.Add(time.Hour))
	plan, err := json.Marshal(dynamicconfig.MobilityDataplanePlan{PoolPrefix: "10.77.60.0/24", Routes: []dynamicconfig.MobilityIPv4RouteIntent{{
		ID: "cloudedge/local-10.77.60.9", PoolRef: "cloudedge", Purpose: dynamicconfig.MobilityIPv4RoutePurposeLocalInventory,
		Destination: "10.77.60.9/32", Device: "ens3",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	legacy.ObservedAt = now
	legacy.ExpiresAt = now.Add(5 * time.Minute)
	legacy.Digest = "sha256:typed-plan-with-legacy-generic"
	legacy.MobilityDataplaneJSON = string(plan)
	store := &dynamicRouteSAMStore{records: []routerstate.DynamicConfigPartRecord{legacy}}
	view, err := buildDynamicRouteSAMView(&api.Router{}, store, now, platform.OSLinux)
	if err != nil {
		t.Fatal(err)
	}
	if countResources(view.EffectiveRouter, api.NetAPIVersion, "IPv4Route") != 0 || countResources(view.RouteRouter, api.NetAPIVersion, "IPv4Route") != 0 {
		t.Fatalf("stale generic MobilityPool payload entered the effective router: effective=%#v route=%#v", view.EffectiveRouter.Spec.Resources, view.RouteRouter.Spec.Resources)
	}
	if len(view.MobilityDataplane.Routes) != 1 || view.MobilityDataplane.Routes[0].ID != "cloudedge/local-10.77.60.9" {
		t.Fatalf("typed plan was lost while ignoring stale generic payload: %#v", view.MobilityDataplane)
	}
}

func TestBuildDynamicRouteSAMViewRejectsMalformedTypedMobilityPlan(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	store := &dynamicRouteSAMStore{records: []routerstate.DynamicConfigPartRecord{{
		Source: "MobilityPool/cloudedge/node/router-a", Generation: 1, ObservedAt: now,
		ExpiresAt: now.Add(5 * time.Minute), Digest: "sha256:malformed-json",
		MobilityDataplaneJSON: "{", Status: "active",
	}}}
	if _, err := buildDynamicRouteSAMView(&api.Router{}, store, now, platform.OSLinux); err == nil {
		t.Fatal("malformed typed mobility plan was accepted")
	}
}

func TestBuildDynamicRouteSAMViewRejectsMalformedCaptureBeforeRouteEffects(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 30, 0, 0, time.UTC)
	store := &dynamicRouteSAMStore{records: []routerstate.DynamicConfigPartRecord{{
		Source: "MobilityPool/cloudedge/node/router-a", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(5 * time.Minute), Digest: "sha256:unsupported-capture", Status: "active",
		MobilityDataplaneJSON: `{"poolPrefix":"10.77.60.0/24","captures":[{"id":"capture-a","poolRef":"cloudedge","address":"10.77.60.9/32","disposition":"desired","captureType":"unsupported","captureInterface":"ens3"}],"routes":[{"id":"route-a","poolRef":"cloudedge","purpose":"local-inventory","destination":"10.77.60.9/32","device":"ens3"}]}`,
	}}}
	if _, err := buildDynamicRouteSAMView(&api.Router{}, store, now, platform.OSLinux); err == nil {
		t.Fatal("malformed capture allowed a typed route plan to proceed")
	}
}

func TestBuildDynamicRouteSAMViewIgnoresTypedPlanFromNonMobilitySource(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	store := &dynamicRouteSAMStore{records: []routerstate.DynamicConfigPartRecord{{
		Source: "MobilityPool/cloudedge/node/router-a/not-a-plan", Generation: 1, ObservedAt: now,
		MobilityDataplaneJSON: "{", Status: "active",
	}}}
	view, err := buildDynamicRouteSAMView(&api.Router{}, store, now, platform.OSLinux)
	if err != nil {
		t.Fatalf("non-MobilityPool typed payload must be ignored: %v", err)
	}
	if !view.MobilityDataplane.IsEmpty() {
		t.Fatalf("non-MobilityPool typed payload entered mobility dataplane: %#v", view.MobilityDataplane)
	}
}

func TestBuildDynamicRouteSAMViewRejectsCrossPoolOrConflictingTypedPlan(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 30, 0, 0, time.UTC)
	for _, tt := range []struct {
		name    string
		records []routerstate.DynamicConfigPartRecord
	}{
		{
			name: "cross pool",
			records: []routerstate.DynamicConfigPartRecord{{
				Source: "MobilityPool/cloudedge/node/router-a", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(5 * time.Minute), Digest: "sha256:cross-pool", Status: "active",
				MobilityDataplaneJSON: `{"poolPrefix":"10.77.60.0/24","captures":[{"id":"capture-a","poolRef":"other","address":"10.77.60.9/32","disposition":"desired","captureType":"proxy-arp","captureInterface":"ens3"}]}`,
			}},
		},
		{
			name: "conflicting id",
			records: []routerstate.DynamicConfigPartRecord{
				{Source: "MobilityPool/cloudedge/node/router-a", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(5 * time.Minute), Digest: "sha256:conflict-a", Status: "active", MobilityDataplaneJSON: `{"poolPrefix":"10.77.60.0/24","captures":[{"id":"capture-a","poolRef":"cloudedge","address":"10.77.60.9/32","disposition":"desired","captureType":"proxy-arp","captureInterface":"ens3"}]}`},
				{Source: "MobilityPool/cloudedge/node/router-b", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(5 * time.Minute), Digest: "sha256:conflict-b", Status: "active", MobilityDataplaneJSON: `{"poolPrefix":"10.77.60.0/24","captures":[{"id":"capture-a","poolRef":"cloudedge","address":"10.77.60.10/32","disposition":"desired","captureType":"proxy-arp","captureInterface":"ens3"}]}`},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildDynamicRouteSAMView(&api.Router{}, &dynamicRouteSAMStore{records: tt.records}, now, platform.OSLinux)
			if err == nil {
				t.Fatal("invalid typed MobilityPool plan was accepted")
			}
		})
	}
}
