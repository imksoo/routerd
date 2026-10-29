// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	resolverruntime "github.com/imksoo/routerd/pkg/dnsresolver"
)

// A temporary missing address must not erase the declared source. Exercise
// generated runtime files across recovery and renumbering, without a host
// daemon or live network. This does not claim DNS packet or HA coverage.
func TestReconcileDNSZoneSourcesSurviveRecoveryAndRenumbering(t *testing.T) {
	withResolverHTTPClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ackResolverReload(t, w, r, true)
	}))
	for _, representation := range []string{"typed", "pointer", "json"} {
		t.Run(representation, func(t *testing.T) {
			declaredRecord := api.DNSZoneRecordSpec{
				Hostname: "router",
				IPv4From: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/lan-base", Field: "address"},
				IPv6From: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"},
			}
			router := dnsResolverRouterWithZone(declaredRecord)
			zoneSpec, err := router.Spec.Resources[0].DNSZoneSpec()
			if err != nil {
				t.Fatal(err)
			}
			switch representation {
			case "pointer":
				router.Spec.Resources[0].Spec = &zoneSpec
			case "json":
				data, err := json.Marshal(router.Spec.Resources[0])
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(data, &router.Spec.Resources[0]); err != nil {
					t.Fatal(err)
				}
			}
			dir := t.TempDir()
			store := mapStore{}
			controller := Controller{Router: router, Store: store, RuntimeDir: filepath.Join(dir, "run"), StateDir: filepath.Join(dir, "state")}
			for _, step := range []struct {
				name, ipv4, ipv6, phase string
			}{
				{name: "unresolved", phase: "Pending"},
				{name: "recovered", ipv4: "192.0.2.1", ipv6: "2001:db8:1::1", phase: "Applied"},
				{name: "renumbered", ipv4: "192.0.2.2", ipv6: "2001:db8:2::1", phase: "Applied"},
			} {
				t.Run(step.name, func(t *testing.T) {
					if step.ipv4 != "" {
						store[api.NetAPIVersion+"/IPv4StaticAddress/lan-base"] = map[string]any{"phase": "Applied", "address": step.ipv4 + "/24"}
						store[api.NetAPIVersion+"/IPv6DelegatedAddress/lan-base"] = map[string]any{"phase": "Applied", "address": step.ipv6 + "/64"}
					}
					if err := controller.Reconcile(context.Background()); err != nil {
						t.Fatal(err)
					}
					data, err := os.ReadFile(filepath.Join(controller.StateDir, "dns-resolver", "lan-resolver", "config.json"))
					if err != nil {
						t.Fatal(err)
					}
					var config resolverruntime.RuntimeConfig
					if err := json.Unmarshal(data, &config); err != nil {
						t.Fatal(err)
					}
					if len(config.Zones) != 1 || len(config.Zones[0].Spec.Records) != 1 {
						t.Fatalf("unexpected runtime zones: %#v", config.Zones)
					}
					record := config.Zones[0].Spec.Records[0]
					if record.IPv4 != step.ipv4 || record.IPv6 != step.ipv6 {
						t.Errorf("runtime addresses = %q / %q, want %q / %q", record.IPv4, record.IPv6, step.ipv4, step.ipv6)
					}
					if record.IPv4From.Resource != "" || record.IPv6From.Resource != "" {
						t.Errorf("runtime record contains unresolved source definitions: %#v", record)
					}
					status := store.ObjectStatus(api.NetAPIVersion, "DNSZone", "lan-zone")
					if status["phase"] != step.phase {
						t.Errorf("zone phase = %v, want %s", status["phase"], step.phase)
					}
					pending, _ := status["pendingRecords"].([]map[string]string)
					wantPending := 0
					if step.phase == "Pending" {
						wantPending = 2
						if len(pending) == 2 && (pending[0]["field"] != "ipv4" || pending[1]["field"] != "ipv6") {
							t.Errorf("pending records = %#v, want both address families", pending)
						}
					}
					if len(pending) != wantPending {
						t.Errorf("pending records = %#v, want %d", pending, wantPending)
					}
					declared, err := router.Spec.Resources[0].DNSZoneSpec()
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(declared.Records, []api.DNSZoneRecordSpec{declaredRecord}) {
						t.Errorf("declared records mutated: %#v", declared.Records)
					}
				})
			}
		})
	}
}
