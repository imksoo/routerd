// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	runtime "github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/imksoo/routerd/pkg/state"
)

type failedLeaseProjectionStore struct{ mapStore }

func (failedLeaseProjectionStore) ReadDHCPLeaseObservations(context.Context) ([]daemonapi.DHCPLeaseUpdate, error) {
	return nil, errors.New("lease journal read failed")
}

func TestRuntimeConfigRequiresCurrentDurableNamedLeaseProjection(t *testing.T) {
	router := dnsResolverRouterWithZone(api.DNSZoneRecordSpec{Hostname: "router", IPv4: "192.0.2.1"})
	zone, _ := router.Spec.Resources[0].DNSZoneSpec()
	zone.DHCPDerived.Sources = []string{"DHCPv4Server/lan-dhcpv4"}
	router.Spec.Resources[0].Spec = zone
	controller := Controller{Router: router, Store: failedLeaseProjectionStore{mapStore{}}, RuntimeDir: t.TempDir(), StateDir: t.TempDir()}
	if _, _, err := controller.runtimeConfig(t.Context(), "lan-resolver", runtime.NormalizeSpec(api.DNSResolverSpec{})); err == nil {
		t.Fatal("required projection failure became empty lease state")
	}
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := state.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	topic, _ := daemonapi.DHCPLeaseEventType(daemonapi.DHCPLeaseActionRemoved)
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd-dhcp-event-relay"}, topic, daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server", Name: "lan-dhcpv4"}
	event.Attributes = map[string]string{"mac": "client-a", "ip": "192.0.2.55", "hostname": "removed", "observedAt": time.Now().UTC().Format(time.RFC3339Nano)}
	if _, err := store.RecordBusEvent(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = state.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller.Store = store
	config, _, err := controller.runtimeConfig(t.Context(), "lan-resolver", runtime.NormalizeSpec(api.DNSResolverSpec{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Zones) != 1 || len(config.Zones[0].LeaseObservations) != 1 || config.Zones[0].LeaseObservations[0].Action != daemonapi.DHCPLeaseActionRemoved {
		t.Fatalf("cold config lost durable named tombstone: %+v", config.Zones)
	}
	router.Spec.Resources[0].Spec = api.DNSZoneSpec{Zone: "lab.example", DHCPDerived: api.DNSZoneDHCPDerivedSpec{Sources: []string{"DHCPv4Server/foreign"}}}
	config, _, err = controller.runtimeConfig(t.Context(), "lan-resolver", runtime.NormalizeSpec(api.DNSResolverSpec{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Zones[0].LeaseObservations) != 0 {
		t.Fatal("durable observation crossed declared source namespace")
	}
}
