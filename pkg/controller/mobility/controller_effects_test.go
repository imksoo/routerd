// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// staleSourceBGPPaths observes the persistence state at the exact point a
// stale path is withdrawn. It reuses the package fake so these tests exercise
// only the controller's sequencing, not a second BGP implementation.
type staleSourceBGPPaths struct {
	*fakeBGPPaths
	beforeDelete func(bgpdaemon.AppliedPath)
}

func localMobilityEffectsRouter(nodeRef string) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
		Metadata: api.ObjectMeta{Name: "cloudedge"},
		Spec:     api.EventGroupSpec{NodeName: nodeRef},
	}}}}
}

func (f *staleSourceBGPPaths) DeletePath(ctx context.Context, path bgpdaemon.AppliedPath) error {
	if f.beforeDelete != nil {
		f.beforeDelete(path)
	}
	return f.fakeBGPPaths.DeletePath(ctx, path)
}

func TestApplyPoolPlanRejectsMismatchedTypedEffectsBeforeBGPMutation(t *testing.T) {
	bgp := &fakeBGPPaths{}
	controller := Controller{BGPPaths: bgp}
	state := poolReconcileState{Runtime: PoolRuntimeSnapshot{
		Pool: NormalizedMobilityPool{Name: "pool", Source: DynamicSource("pool", "node"), SelfNode: "node"},
		Now:  time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC),
	}}
	plan := PoolPlan{
		BGPPaths: []bgpdaemon.AppliedPath{{
			Source: state.Runtime.Pool.Source,
			Prefix: "10.88.60.10/32",
			Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
		}},
		LocalDataplane: dynamicconfig.MobilityDataplanePlan{
			PoolPrefix: "10.88.60.0/24",
			Captures: []dynamicconfig.LocalCaptureIntent{{
				ID:          "capture",
				PoolRef:     "pool",
				Address:     "10.88.60.10/32",
				Disposition: dynamicconfig.CaptureDesired,
				CaptureType: "proxy-arp",
			}},
		},
		FIBVerdicts: []dynamicconfig.FIBVerdict{{
			PoolRef: "pool",
			Scope:   &dynamicconfig.FIBPoolScope{Prefix: "10.88.61.0/24"},
		}},
	}

	err := controller.applyPoolPlan(context.Background(), state, plan)
	if err == nil || !strings.Contains(err.Error(), "does not match local dataplane") {
		t.Fatalf("applyPoolPlan error = %v, want mismatched typed-effects rejection", err)
	}
	if len(bgp.upserts) != 0 || len(bgp.deletes) != 0 {
		t.Fatalf("BGP mutated before typed-effects validation: upserts=%#v deletes=%#v", bgp.upserts, bgp.deletes)
	}
}

func TestDeprovisionStaleMobilitySourcesWithdrawsBGPBeforeExpiringTypedPlan(t *testing.T) {
	now := time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	const (
		pool = "retired-pool"
		node = "retired-node"
	)
	mainSource := DynamicSource(pool, node)
	arpSource := ARPObserverDynamicSource(pool, node)
	seedMobilitySourceForDeprovision(t, store, mainSource, pool, false, now)
	seedMobilitySourceForDeprovision(t, store, arpSource, pool, true, now)

	path := bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{
		Source: mainSource,
		Prefix: "10.88.60.10/32",
		Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
	})
	baseBGP := &fakeBGPPaths{paths: map[string]bgpdaemon.AppliedPath{
		bgpdaemon.AppliedPathKey(path): path,
	}}
	observedActivePlan := false
	bgp := &staleSourceBGPPaths{
		fakeBGPPaths: baseBGP,
		beforeDelete: func(got bgpdaemon.AppliedPath) {
			if got.Source != mainSource {
				t.Errorf("withdraw source = %q, want %q", got.Source, mainSource)
			}
			current := latestPart(t, store, mainSource)
			if current.EffectiveStatus(now) != "active" {
				t.Errorf("typed plan status while withdrawing BGP = %q, want active", current.EffectiveStatus(now))
			}
			if strings.TrimSpace(current.ActionPlansJSON) == "" || strings.TrimSpace(current.MobilityDataplaneJSON) == "" {
				t.Errorf("typed plan was cleared before BGP withdrawal: %#v", current)
			}
			observedActivePlan = true
		},
	}

	controller := Controller{Router: localMobilityEffectsRouter(node), Store: store, BGPPaths: bgp}
	if err := controller.deprovisionStaleMobilitySources(context.Background(), nil, nil, now); err != nil {
		t.Fatalf("deprovisionStaleMobilitySources: %v", err)
	}
	if !observedActivePlan {
		t.Fatal("stale main BGP path was not withdrawn")
	}
	if len(baseBGP.deletes) != 1 || baseBGP.deletes[0].Source != mainSource || baseBGP.deletes[0].Prefix != path.Prefix {
		t.Fatalf("BGP deletes = %#v, want one withdrawal for %s %s", baseBGP.deletes, mainSource, path.Prefix)
	}
	if _, found := maybePathBySourcePrefix(baseBGP, mainSource, path.Prefix); found {
		t.Fatalf("stale BGP path %s %s remains after deprovision", mainSource, path.Prefix)
	}

	main := latestPart(t, store, mainSource)
	if main.EffectiveStatus(now) != "expired" || strings.TrimSpace(main.ActionPlansJSON) != "" || strings.TrimSpace(main.MobilityDataplaneJSON) != "" {
		t.Fatalf("stale main plan = %#v, want an expired empty typed record", main)
	}
	arp := latestPart(t, store, arpSource)
	if arp.EffectiveStatus(now) != "expired" || strings.TrimSpace(arp.ARPObserverIntentsJSON) != "" {
		t.Fatalf("stale ARP observer plan = %#v, want an expired empty typed record", arp)
	}
}

func TestDeprovisionStaleMobilitySourcesRetainsPendingPoolSources(t *testing.T) {
	now := time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	const (
		pool = "pending-pool"
		node = "pending-node"
	)
	mainSource := DynamicSource(pool, node)
	arpSource := ARPObserverDynamicSource(pool, node)
	seedMobilitySourceForDeprovision(t, store, mainSource, pool, false, now)
	seedMobilitySourceForDeprovision(t, store, arpSource, pool, true, now)

	path := bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{
		Source: mainSource,
		Prefix: "10.88.60.20/32",
		Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
	})
	baseBGP := &fakeBGPPaths{paths: map[string]bgpdaemon.AppliedPath{
		bgpdaemon.AppliedPathKey(path): path,
	}}
	controller := Controller{Router: localMobilityEffectsRouter(node), Store: store, BGPPaths: baseBGP}
	if err := controller.deprovisionStaleMobilitySources(context.Background(), nil, map[string]bool{pool: true}, now); err != nil {
		t.Fatalf("deprovisionStaleMobilitySources: %v", err)
	}
	if len(baseBGP.deletes) != 0 {
		t.Fatalf("BGP deletes = %#v, want none for a membersFrom-pending Pool", baseBGP.deletes)
	}
	if _, found := maybePathBySourcePrefix(baseBGP, mainSource, path.Prefix); !found {
		t.Fatalf("pending Pool BGP path %s %s was withdrawn", mainSource, path.Prefix)
	}

	main := latestPart(t, store, mainSource)
	if main.EffectiveStatus(now) != "active" || strings.TrimSpace(main.ActionPlansJSON) == "" || strings.TrimSpace(main.MobilityDataplaneJSON) == "" {
		t.Fatalf("pending Pool main plan = %#v, want active typed record", main)
	}
	arp := latestPart(t, store, arpSource)
	if arp.EffectiveStatus(now) != "active" || strings.TrimSpace(arp.ARPObserverIntentsJSON) == "" {
		t.Fatalf("pending Pool ARP observer plan = %#v, want active typed record", arp)
	}
}

func seedMobilitySourceForDeprovision(t *testing.T, store *routerstate.SQLiteStore, source, pool string, arpObserver bool, now time.Time) {
	t.Helper()
	part := dynamicconfig.NewPart(
		"deprovision-"+pool,
		source,
		nil,
		dynamicGeneration,
		now.Add(-time.Minute),
		now.Add(time.Hour),
	)
	if arpObserver {
		part.Spec.ARPObserverIntents = []dynamicconfig.ARPObserverIntent{{
			ResourceName:   "mobility-arp-test",
			PoolRef:        pool,
			Prefix:         "10.88.60.0/24",
			SourceType:     "arp-observer",
			IfName:         "lan0",
			EventInterface: "lan0",
			Observe:        true,
		}}
	} else {
		part.Spec.ActionPlans = []dynamicconfig.ActionPlan{{
			Name:     "stale-assign",
			Provider: "test",
			Action:   "assign-secondary-ip",
			Target:   map[string]string{"address": "10.88.60.10/32"},
		}}
		part.Spec.MobilityDataplane = dynamicconfig.MobilityDataplanePlan{
			PoolPrefix: "10.88.60.0/24",
			Captures: []dynamicconfig.LocalCaptureIntent{{
				ID:               "capture-test",
				PoolRef:          pool,
				Address:          "10.88.60.10/32",
				Disposition:      dynamicconfig.CaptureDesired,
				CaptureType:      "proxy-arp",
				CaptureInterface: "lan0",
			}},
		}
	}
	part.Spec.Digest = digestDynamicPart(part)
	record, err := codec.Encode(part)
	if err != nil {
		t.Fatalf("encode typed MobilityPool part %q: %v", source, err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		t.Fatalf("UpsertDynamicConfigPart(%q): %v", source, err)
	}
}
