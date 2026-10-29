// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

type failedSourceStore struct {
	mapStore
	failure error
}

func (s failedSourceStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	if name == "unavailable" {
		return nil, s.failure
	}
	return s.mapStore.ReadObjectStatus(apiVersion, kind, name)
}

func TestDNSResolverReadFailureCannotReplaceAppliedConfigOrStatus(t *testing.T) {
	failure := errors.New("source database read failed")
	source := api.StatusValueSourceSpec{Resource: "Interface/unavailable", Field: "ipv4Addresses", Optional: true}
	for _, field := range []string{"listen", "upstream", "bootstrap", "zone"} {
		t.Run(field, func(t *testing.T) {
			router := dnsResolverRouter([]string{"127.0.0.1"}, nil)
			spec, _ := router.Spec.Resources[0].DNSResolverSpec()
			switch field {
			case "listen":
				spec.Listen[0].AddressFrom = []api.StatusValueSourceSpec{source}
			case "upstream":
				spec.Sources[0].UpstreamFrom = []api.StatusValueSourceSpec{source}
			case "bootstrap":
				spec.Sources[0].BootstrapResolverFrom = []api.StatusValueSourceSpec{source}
			case "zone":
				router = dnsResolverRouterWithZone(api.DNSZoneRecordSpec{Hostname: "router", IPv4From: source})
			}
			if field != "zone" {
				router.Spec.Resources[0].Spec = spec
			}
			previous := map[string]any{"phase": "Applied", "configDigest": "previous"}
			store := failedSourceStore{mapStore: mapStore{api.NetAPIVersion + "/DNSResolver/lan-resolver": previous}, failure: failure}
			dir := t.TempDir()
			configPath := filepath.Join(dir, "dns-resolver", "lan-resolver", "config.json")
			if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(configPath, []byte("previous worker config"), 0o600); err != nil {
				t.Fatal(err)
			}
			controller := Controller{Router: router, Store: store, StateDir: dir, RuntimeDir: filepath.Join(dir, "run")}
			if err := controller.Reconcile(context.Background()); !errors.Is(err, failure) {
				t.Fatalf("optional read failure became missing/default intent: %v", err)
			}
			data, err := os.ReadFile(configPath)
			if err != nil || string(data) != "previous worker config" {
				t.Fatalf("failed source changed worker config: %q, %v", data, err)
			}
			if got := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver"); !reflect.DeepEqual(got, previous) {
				t.Fatalf("failed source replaced applied status: %#v", got)
			}
		})
	}
}
