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

func startMobilityServeChain(t *testing.T, router *api.Router, store *routerstate.SQLiteStore) func() {
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
	}
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
	if strings.TrimSpace(raw) == "" {
		return false
	}
	var plans []dynamicconfig.ActionPlan
	if err := json.Unmarshal([]byte(raw), &plans); err != nil {
		t.Fatalf("decode action plans: %v raw=%s", err, raw)
	}
	for _, plan := range plans {
		if plan.Action == action && strings.TrimSpace(plan.Target["address"]) == address {
			return true
		}
	}
	return false
}
