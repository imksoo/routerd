// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestFaultApplySnapshotManifestDoesNotExposePayload(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "snapshot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{Source: "fixture", Generation: 7, Digest: "sha256:fixture", ObservedAt: now.Add(-time.Minute), ExpiresAt: now, Status: "active", ResourcesJSON: `[{"password":"payload-secret"}]`}); err != nil {
		t.Fatal(err)
	}
	manifest, err := describeApplySnapshot(&api.Router{Metadata: api.ObjectMeta{Name: "candidate"}}, store, now, platform.OSLinux)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "payload-secret") || strings.Contains(string(encoded), "resourcesJson") {
		t.Fatal("manifest disclosed payload")
	}
	if manifest.CanonicalHash == "" || !manifest.EvaluatedAt.Equal(now) || manifest.TargetOS != platform.OSLinux || len(manifest.DynamicParts) != 1 || len(manifest.Inputs) != 3 {
		t.Fatalf("incomplete provenance: %+v", manifest)
	}
	if manifest.DynamicParts[0].Generation != 7 || !manifest.DynamicParts[0].ExpiresAt.Equal(now) {
		t.Fatalf("manifest refreshed expired part: %+v", manifest.DynamicParts)
	}
}

// Exercise the real SQLite WAL and real dynamic planner, at the same evaluation
// instant. A missing input must not quietly turn a rejected plan into success.
func TestFaultApplySnapshotPreservesDynamicEvaluation(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	route := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
		Metadata: api.ObjectMeta{Name: "dynamic-blackhole"},
		Spec:     api.IPv4RouteSpec{Type: "blackhole", Destination: "192.0.2.0/24", Metric: 90},
	}
	part := dynamicconfig.NewPart("routes", "test-routes", nil, 1, now.Add(-time.Minute), now.Add(time.Minute))
	part.Spec.Digest = "sha256:fixture"
	part.Spec.Resources = []api.Resource{route}
	generic, err := codec.Encode(part)
	if err != nil {
		t.Fatal(err)
	}
	typedPart := dynamicconfig.NewPart("pool-plan", "MobilityPool/pool/node/router", nil, 1, now.Add(-time.Minute), now.Add(time.Minute))
	typedPart.Spec.Digest = "sha256:typed-fixture"
	typedPart.Spec.MobilityDataplane = dynamicconfig.MobilityDataplanePlan{
		PoolPrefix: "192.0.2.0/24",
		Routes:     []dynamicconfig.MobilityIPv4RouteIntent{{ID: "pool/capture-prefix", PoolRef: "pool", Purpose: dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix, Destination: "192.0.2.0/24", Device: "lan0", Metric: 90}},
	}
	typed, err := codec.Encode(typedPart)
	if err != nil {
		t.Fatal(err)
	}
	expired := generic
	expired.ExpiresAt = now
	withdraw := generic
	withdraw.Generation = 2
	withdraw.ResourcesJSON = "[]"
	invalidScope := typed
	invalidScope.MobilityDataplaneJSON = `{"poolPrefix":"198.51.100.0/24","routes":[{"id":"pool/capture-prefix","poolRef":"pool","purpose":"capture-prefix","destination":"192.0.2.0/24","device":"lan0"}]}`
	duplicate := typed
	duplicate.Generation = 2
	legacy := typed
	legacy.Source = "MobilityPool/removed-producer"
	legacy.MobilityDataplaneJSON = ""
	legacy.ResourcesJSON = generic.ResourcesJSON
	cases := []struct {
		name       string
		records    []routerstate.DynamicConfigPartRecord
		wantRoutes int
		wantErr    bool
	}{
		{"generic-active", []routerstate.DynamicConfigPartRecord{generic}, 1, false},
		{"generic-expired", []routerstate.DynamicConfigPartRecord{expired}, 0, false},
		// Generic parts retain all unexpired generations; an empty new
		// generation does not withdraw the older lease by itself.
		{"generic-coexisting-generations", []routerstate.DynamicConfigPartRecord{generic, withdraw}, 1, false},
		{"generic-withdraw-after-expiry", []routerstate.DynamicConfigPartRecord{expired, withdraw}, 0, false},
		{"typed-active-remains-nongeneric", []routerstate.DynamicConfigPartRecord{typed}, 0, false},
		{"typed-invalid-scope", []routerstate.DynamicConfigPartRecord{invalidScope}, 0, true},
		{"typed-generation-conflict", []routerstate.DynamicConfigPartRecord{typed, duplicate}, 0, true},
		{"legacy-removed-producer-inert", []routerstate.DynamicConfigPartRecord{legacy}, 0, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source.db")
			src, err := routerstate.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			defer src.Close()
			for _, record := range tt.records {
				if err := src.UpsertDynamicConfigPart(record); err != nil {
					t.Fatal(err)
				}
			}
			if err := src.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", "old-route", map[string]any{"owner": api.NetAPIVersion + "/IPv4Route/old-route", "observedAt": now.Add(-time.Hour).Format(time.RFC3339Nano), "phase": "Installed"}); err != nil {
				t.Fatal(err)
			}
			dst, cleanup, err := openApplyChainStateStore(path, true)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			candidate := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "snapshot"}}
			before, beforeErr := controllerchain.BuildDynamicRouteSAMEffectiveRouter(candidate, src, now, platform.OSLinux)
			after, afterErr := controllerchain.BuildDynamicRouteSAMEffectiveRouter(candidate, dst, now, platform.OSLinux)
			if (beforeErr != nil) != tt.wantErr {
				t.Fatalf("source planner error = %v, want error %t", beforeErr, tt.wantErr)
			}
			if (beforeErr != nil) != (afterErr != nil) {
				t.Errorf("snapshot changed planner acceptance: source=%v snapshot=%v", beforeErr, afterErr)
			}
			if beforeErr == nil && afterErr == nil {
				if len(before.Spec.Resources) != tt.wantRoutes {
					t.Fatalf("source resources = %d, want %d", len(before.Spec.Resources), tt.wantRoutes)
				}
				if !reflect.DeepEqual(before.Spec.Resources, after.Spec.Resources) {
					t.Errorf("snapshot changed effective route intent: source=%#v snapshot=%#v", before.Spec.Resources, after.Spec.Resources)
				}
			}
			want, err := src.ListDynamicConfigParts()
			if err != nil {
				t.Fatal(err)
			}
			got, err := dst.ListDynamicConfigParts()
			if err != nil {
				t.Fatal(err)
			}
			// Record IDs are local storage identities, not ownership. Compare all
			// envelopes and payloads including withdrawal, freshness and generation.
			for i := range want {
				want[i].ID = 0
			}
			for i := range got {
				got[i].ID = 0
			}
			if !reflect.DeepEqual(want, got) {
				t.Errorf("dynamic records were not preserved: source count=%d snapshot count=%d", len(want), len(got))
			}
			if !reflect.DeepEqual(src.ObjectStatus(api.NetAPIVersion, "IPv4Route", "old-route"), dst.ObjectStatus(api.NetAPIVersion, "IPv4Route", "old-route")) {
				t.Error("snapshot changed withdrawal ownership/freshness status")
			}
		})
	}
}
