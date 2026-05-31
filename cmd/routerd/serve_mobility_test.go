// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	mobilitycontroller "github.com/imksoo/routerd/pkg/controller/mobility"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
	_ "modernc.org/sqlite"
)

func TestServeChainMobilityRetainsProviderClaimUntilDrainDeprovisions(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}

	if err := store.RecordFederationEvent(mobilityObservedEvent("evt-10", "10.88.60.10/32", now)); err != nil {
		t.Fatalf("RecordFederationEvent .10: %v", err)
	}
	stop := startMobilityServeChain(t, mobilityServeRouter(false), store)
	waitForMobilityPart(t, store, func(part routerstate.DynamicConfigPartRecord) bool {
		return countServeKind(t, part.ResourcesJSON, "RemoteAddressClaim") == 1 &&
			serveHasAction(t, part.ActionPlansJSON, "assign-secondary-ip", "10.88.60.10/32")
	})
	stop()
	if err := store.Close(); err != nil {
		t.Fatalf("close store after active start: %v", err)
	}

	deleteMobilityServeAddress(t, path, "10.88.60.10/32")
	store, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen sqlite after delete: %v", err)
	}
	if err := store.RecordFederationEvent(mobilityObservedEvent("evt-11", "10.88.60.11/32", now.Add(time.Second))); err != nil {
		t.Fatalf("RecordFederationEvent .11: %v", err)
	}
	stop = startMobilityServeChain(t, mobilityServeRouter(false), store)
	waitForMobilityPart(t, store, func(part routerstate.DynamicConfigPartRecord) bool {
		return countServeKind(t, part.ResourcesJSON, "RemoteAddressClaim") == 2 &&
			!serveHasAction(t, part.ActionPlansJSON, "unassign-secondary-ip", "10.88.60.10/32")
	})
	stop()
	if err := store.Close(); err != nil {
		t.Fatalf("close store after active restart: %v", err)
	}

	store, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen sqlite before drain: %v", err)
	}
	defer store.Close()
	stop = startMobilityServeChain(t, mobilityServeRouter(true), store)
	waitForMobilityPart(t, store, func(part routerstate.DynamicConfigPartRecord) bool {
		return countServeKind(t, part.ResourcesJSON, "RemoteAddressClaim") == 0 &&
			serveHasAction(t, part.ActionPlansJSON, "unassign-secondary-ip", "10.88.60.10/32")
	})
	stop()
}

func TestServeChainMobilityReemitsMarkerBackedUnassignUntilExecuted(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}

	if err := store.RecordFederationEvent(mobilityObservedEvent("evt-10", "10.88.60.10/32", now)); err != nil {
		t.Fatalf("RecordFederationEvent .10: %v", err)
	}
	stop := startMobilityServeChain(t, mobilityServeRouter(false), store)
	initial := waitForMobilityPart(t, store, func(part routerstate.DynamicConfigPartRecord) bool {
		return countServeKind(t, part.ResourcesJSON, "RemoteAddressClaim") == 1 &&
			serveHasAction(t, part.ActionPlansJSON, "assign-secondary-ip", "10.88.60.10/32")
	})
	importServeActions(t, store)
	succeedServeAction(t, store, "assign-secondary-ip", "10.88.60.10/32", now.Add(time.Second))
	succeedServeAction(t, store, "ensure-forwarding-enabled", "10.88.60.10/32", now.Add(2*time.Second))
	stop()

	stop, eventBus := startMobilityServeChainWithBus(t, mobilityServeRouter(true), store)
	drained := waitForMobilityPart(t, store, func(part routerstate.DynamicConfigPartRecord) bool {
		return part.UpdatedAt.After(initial.UpdatedAt) &&
			countServeKind(t, part.ResourcesJSON, "RemoteAddressClaim") == 0 &&
			serveActionCount(t, part.ActionPlansJSON, "unassign-secondary-ip", "10.88.60.10/32") == 1
	})
	if got := countServeMarkers(t, store); got != 2 {
		t.Fatalf("first drain marker count = %d, want 2", got)
	}
	for i := 0; i < 3; i++ {
		triggerMobilityReconcile(t, eventBus)
		drained = waitForMobilityPartUpdatedAfter(t, store, drained.UpdatedAt, func(part routerstate.DynamicConfigPartRecord) bool {
			return countServeKind(t, part.ResourcesJSON, "RemoteAddressClaim") == 0 &&
				serveActionCount(t, part.ActionPlansJSON, "unassign-secondary-ip", "10.88.60.10/32") == 1
		})
	}
	stop()
	if err := store.Close(); err != nil {
		t.Fatalf("close store before marker restart: %v", err)
	}

	store, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen sqlite after marker persist: %v", err)
	}
	stop, eventBus = startMobilityServeChainWithBus(t, mobilityServeRouter(true), store)
	drained = waitForMobilityPartUpdatedAfter(t, store, drained.UpdatedAt, func(part routerstate.DynamicConfigPartRecord) bool {
		return countServeKind(t, part.ResourcesJSON, "RemoteAddressClaim") == 0 &&
			serveActionCount(t, part.ActionPlansJSON, "unassign-secondary-ip", "10.88.60.10/32") == 1
	})
	importServeActions(t, store)
	succeedServeAction(t, store, "unassign-secondary-ip", "10.88.60.10/32", now.Add(3*time.Second))
	succeedServeAction(t, store, "ensure-forwarding-disabled", "10.88.60.10/32", now.Add(4*time.Second))
	triggerMobilityReconcile(t, eventBus)
	waitForMobilityPartUpdatedAfter(t, store, drained.UpdatedAt, func(part routerstate.DynamicConfigPartRecord) bool {
		return countServeKind(t, part.ResourcesJSON, "RemoteAddressClaim") == 0 &&
			!serveHasAction(t, part.ActionPlansJSON, "unassign-secondary-ip", "10.88.60.10/32")
	})
	if got := countServeMarkers(t, store); got != 0 {
		t.Fatalf("completed marker count = %d, want 0", got)
	}
	stop()
	if err := store.Close(); err != nil {
		t.Fatalf("close store after marker test: %v", err)
	}
}

func startMobilityServeChain(t *testing.T, router *api.Router, store *routerstate.SQLiteStore) func() {
	stop, _ := startMobilityServeChainWithBus(t, router, store)
	return stop
}

func startMobilityServeChainWithBus(t *testing.T, router *api.Router, store *routerstate.SQLiteStore) (func(), *bus.Bus) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	eventBus := bus.NewWithStore(store)
	runner := controllerchain.Runner{
		Router: router,
		Bus:    eventBus,
		Store:  store,
		Opts: controllerchain.Options{
			DryRunAddress:     true,
			DryRunRoute:       true,
			DryRunServiceUnit: true,
			EnabledControllers: []string{
				"mobility",
				"sam",
				"ipv4-route",
			},
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
	}
	if err := runner.Start(ctx); err != nil {
		cancel()
		t.Fatalf("chain Start: %v", err)
	}
	return func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
	}, eventBus
}

func waitForMobilityPart(t *testing.T, store *routerstate.SQLiteStore, ok func(routerstate.DynamicConfigPartRecord) bool) routerstate.DynamicConfigPartRecord {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	source := mobilitycontroller.DynamicSource("cloudedge", "azure-router-a")
	var last routerstate.DynamicConfigPartRecord
	for time.Now().Before(deadline) {
		parts, err := store.GetDynamicConfigPartsBySource(source)
		if err != nil {
			t.Fatalf("GetDynamicConfigPartsBySource: %v", err)
		}
		if len(parts) > 0 {
			last = parts[0]
			if ok(last) {
				return last
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for mobility part; last resources=%s actionPlans=%s", last.ResourcesJSON, last.ActionPlansJSON)
	return routerstate.DynamicConfigPartRecord{}
}

func waitForMobilityPartUpdatedAfter(t *testing.T, store *routerstate.SQLiteStore, after time.Time, ok func(routerstate.DynamicConfigPartRecord) bool) routerstate.DynamicConfigPartRecord {
	t.Helper()
	return waitForMobilityPart(t, store, func(part routerstate.DynamicConfigPartRecord) bool {
		return part.UpdatedAt.After(after) && ok(part)
	})
}

func triggerMobilityReconcile(t *testing.T, eventBus *bus.Bus) {
	t.Helper()
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "test"}, "routerd.resource.status.changed", daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: "cloudedge"}
	if err := eventBus.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish mobility trigger: %v", err)
	}
}

func importServeActions(t *testing.T, store *routerstate.SQLiteStore) {
	t.Helper()
	parts, err := store.ListDynamicConfigParts()
	if err != nil {
		t.Fatalf("ListDynamicConfigParts: %v", err)
	}
	for _, part := range parts {
		if strings.TrimSpace(part.ActionPlansJSON) == "" {
			continue
		}
		var plans []dynamicconfig.ActionPlan
		if err := json.Unmarshal([]byte(part.ActionPlansJSON), &plans); err != nil {
			t.Fatalf("decode actionPlans: %v", err)
		}
		for _, plan := range plans {
			rec := routerstate.ActionExecutionRecord{
				IdempotencyKey: plan.IdempotencyKey,
				Source:         part.Source,
				Provider:       plan.Provider,
				ProviderRef:    plan.ProviderRef,
				Action:         plan.Action,
				RiskLevel:      plan.RiskLevel,
				Status:         routerstate.ActionPending,
			}
			if len(plan.Target) > 0 {
				data, err := json.Marshal(plan.Target)
				if err != nil {
					t.Fatalf("marshal action target: %v", err)
				}
				rec.TargetJSON = string(data)
			}
			if len(plan.Parameters) > 0 {
				data, err := json.Marshal(plan.Parameters)
				if err != nil {
					t.Fatalf("marshal action parameters: %v", err)
				}
				rec.ParametersJSON = string(data)
			}
			if plan.Undo != nil {
				data, err := json.Marshal(plan.Undo)
				if err != nil {
					t.Fatalf("marshal action undo: %v", err)
				}
				rec.UndoJSON = string(data)
			}
			if _, err := store.ImportAction(rec); err != nil {
				t.Fatalf("ImportAction(%s): %v", plan.IdempotencyKey, err)
			}
		}
	}
}

func succeedServeAction(t *testing.T, store *routerstate.SQLiteStore, action, address string, at time.Time) {
	t.Helper()
	rows, err := store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		t.Fatalf("ListActions: %v", err)
	}
	for _, row := range rows {
		if row.Action != action || !strings.Contains(row.TargetJSON, address) {
			continue
		}
		if row.Status == routerstate.ActionSucceeded {
			return
		}
		if row.Status == routerstate.ActionPending {
			if err := store.ApproveAction(row.ID, "test", at); err != nil {
				t.Fatalf("ApproveAction(%d): %v", row.ID, err)
			}
		}
		if err := store.MarkActionResult(row.ID, routerstate.ActionSucceeded, "ok", "", nil, at); err != nil {
			t.Fatalf("MarkActionResult(%d): %v", row.ID, err)
		}
		return
	}
	t.Fatalf("action %s for %s not found", action, address)
}

func deleteMobilityServeAddress(t *testing.T, path, address string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM federation_events WHERE subject = ?`, address); err != nil {
		t.Fatalf("delete federation event: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM address_leases WHERE pool = ? AND address = ?`, "cloudedge", address); err != nil {
		t.Fatalf("delete address lease: %v", err)
	}
}

func mobilityObservedEvent(id, address string, at time.Time) routerstate.EventRecord {
	return routerstate.EventRecord{
		ID:         id,
		Group:      "cloudedge",
		SourceNode: "onprem-router",
		Type:       mobilitycontroller.ObservedEventType,
		Subject:    address,
		DedupeKey:  id,
		Payload:    map[string]string{"address": address},
		ObservedAt: at,
		ExpiresAt:  at.Add(time.Hour),
	}
}

func mobilityServeRouter(drainA bool) *api.Router {
	members := []api.MobilityPoolMember{
		{
			NodeRef:  "onprem-router",
			Site:     "onprem",
			Role:     "onprem",
			Capture:  api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"},
			Delivery: api.MobilityMemberDelivery{PeerRef: "azure-a", Mode: "route", TunnelInterface: "wg-azure-a"},
		},
		{
			NodeRef: "azure-router-a",
			Site:    "azure",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "azure-provider",
				ProviderMode: "nic-secondary-ip",
				NICRef:       "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-a",
				Target:       map[string]string{"region": "japaneast", "ipConfigName": "capture-a"},
			},
			Delivery:    api.MobilityMemberDelivery{PeerRef: "onprem-main", Mode: "route", TunnelInterface: "wg-onprem"},
			Placement:   api.MobilityMemberPlacement{Group: "azure-edge", Priority: 10},
			Maintenance: api.MobilityMemberMaintenance{Drain: drainA},
		},
		{
			NodeRef: "azure-router-b",
			Site:    "azure",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "azure-provider",
				ProviderMode: "nic-secondary-ip",
				NICRef:       "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-b",
				Target:       map[string]string{"region": "japaneast", "ipConfigName": "capture-b"},
			},
			Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem-main", Mode: "route", TunnelInterface: "wg-onprem"},
			Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 20},
		},
	}
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "serve-mobility"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec:     api.EventGroupSpec{NodeName: "azure-router-a"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"},
				Metadata: api.ObjectMeta{Name: "onprem-main"},
				Spec: api.OverlayPeerSpec{
					Role:   "onprem",
					NodeID: "onprem-router",
					Underlay: api.OverlayUnderlay{
						Type:      "wireguard",
						Interface: "wg-onprem",
					},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"},
				Metadata: api.ObjectMeta{Name: "azure-provider"},
				Spec: api.CloudProviderProfileSpec{
					Provider:       "azure",
					SubscriptionID: "sub-1",
					ResourceGroup:  "rg-router",
					Capabilities:   []string{"nic-secondary-ip", "ip-forwarding"},
					Auth:           api.ProviderAuth{Mode: "external-command", Command: "az"},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec: api.MobilityPoolSpec{
					Prefix:        "10.88.60.0/24",
					GroupRef:      "cloudedge",
					Members:       members,
					LeasePolicy:   api.MobilityLeasePolicy{TTL: "5m", HoldDuration: "30s"},
					CapturePolicy: api.MobilityCapturePolicy{Mode: "all-non-owner-sites"},
				},
			},
		}},
	}
}

func countServeKind(t *testing.T, raw, kind string) int {
	t.Helper()
	resources := decodeServeResources(t, raw)
	count := 0
	for _, res := range resources {
		if res.Kind == kind {
			count++
		}
	}
	return count
}

func decodeServeResources(t *testing.T, raw string) []api.Resource {
	t.Helper()
	var resources []api.Resource
	if err := json.Unmarshal([]byte(raw), &resources); err != nil {
		t.Fatalf("decode resources: %v raw=%s", err, raw)
	}
	return resources
}

func serveHasAction(t *testing.T, raw, action, address string) bool {
	t.Helper()
	return serveActionCount(t, raw, action, address) > 0
}

func serveActionCount(t *testing.T, raw, action, address string) int {
	t.Helper()
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	var plans []dynamicconfig.ActionPlan
	if err := json.Unmarshal([]byte(raw), &plans); err != nil {
		t.Fatalf("decode action plans: %v raw=%s", err, raw)
	}
	count := 0
	for _, plan := range plans {
		if plan.Action == action && strings.TrimSpace(plan.Target["address"]) == address {
			count++
		}
	}
	return count
}

func countServeMarkers(t *testing.T, store *routerstate.SQLiteStore) int {
	t.Helper()
	markers, err := store.ListMobilityDeprovisionMarkers(mobilitycontroller.DynamicSource("cloudedge", "azure-router-a"))
	if err != nil {
		t.Fatalf("ListMobilityDeprovisionMarkers: %v", err)
	}
	return len(markers)
}
