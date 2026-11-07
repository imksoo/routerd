// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
)

type zonePublicationStore struct {
	mapStore
	attempts *int
	phases   []string
	after    []int
}

func (s *zonePublicationStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if kind == "DNSZone" {
		s.phases = append(s.phases, status["phase"].(string))
		s.after = append(s.after, *s.attempts)
	}
	return s.mapStore.SaveObjectStatus(apiVersion, kind, name, status)
}

func TestZoneAppliedRequiresEveryServingWorkerAcknowledgement(t *testing.T) {
	for _, failedWorker := range []int{0, 1} {
		t.Run([]string{"first-worker-fails", "last-worker-fails"}[failedWorker], func(t *testing.T) {
			attempts, failing := 0, false
			withResolverHTTPClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				worker := attempts % 2
				attempts++
				if failing && worker == failedWorker {
					http.Error(w, "reload failed", http.StatusInternalServerError)
					return
				}
				ackResolverReload(t, w, r, true)
			}))
			router := dnsResolverRouterWithZone(api.DNSZoneRecordSpec{Hostname: "router", IPv4: "192.0.2.1"})
			second := router.Spec.Resources[1]
			second.Metadata.Name = "second-resolver"
			router.Spec.Resources = append(router.Spec.Resources, second)
			store := &zonePublicationStore{mapStore: mapStore{}, attempts: &attempts}
			dir := t.TempDir()
			controller := Controller{Router: router, Store: store, RuntimeDir: filepath.Join(dir, "run"), StateDir: filepath.Join(dir, "state")}
			for cycle, want := range []string{"Applied", "Pending", "Applied"} {
				failing = cycle == 1
				if err := controller.Reconcile(context.Background()); (err != nil) != failing {
					t.Fatalf("cycle %d error = %v", cycle, err)
				}
				status := store.ObjectStatus(api.NetAPIVersion, "DNSZone", "lan-zone")
				if status["phase"] != want || len(store.phases) != cycle+1 || store.after[cycle] != 2*(cycle+1) {
					t.Fatalf("zone published before aggregate result: status=%#v phases=%v attempts-at-publication=%v", status, store.phases, store.after)
				}
				if failing && status["reason"] != "ResolverNotAcknowledged" {
					t.Fatalf("failed worker hidden by healthy worker: %#v", status)
				}
			}
		})
	}
}

func TestDryRunPlansResolverAndZoneWithoutPublishingConfigured(t *testing.T) {
	withResolverHTTPClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("dry-run contacted a worker")
	}))
	eventBus := bus.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, _ := eventBus.Subscribe(ctx, bus.Subscription{Topics: []string{"routerd.dns.resolver.configured"}}, 8)
	store := mapStore{}
	controller := Controller{
		Router: dnsResolverRouterWithZone(api.DNSZoneRecordSpec{Hostname: "router", IPv4: "192.0.2.1"}),
		Store:  store, Bus: eventBus, DryRun: true,
	}
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	for kind, name := range map[string]string{"DNSResolver": "lan-resolver", "DNSZone": "lan-zone"} {
		if status := store.ObjectStatus(api.NetAPIVersion, kind, name); status["phase"] != "Planned" {
			t.Fatalf("%s claimed applied authority in dry-run: %#v", kind, status)
		}
	}
	select {
	case event := <-events:
		t.Fatalf("dry-run published configured success: %+v", event)
	default:
	}
}
