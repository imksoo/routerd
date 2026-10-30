// SPDX-License-Identifier: BSD-3-Clause

package nat44

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func (s *testStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	return s.ObjectStatus(apiVersion, kind, name), nil
}

type failedSourceStore struct {
	*testStore
	err error
}

func (s failedSourceStore) ReadObjectStatus(string, string, string) (map[string]any, error) {
	return nil, s.err
}

func TestNATFailedSourceDoesNotWithdrawAppliedRules(t *testing.T) {
	failure := errors.New("NAT source unavailable")
	for _, field := range []string{"egress", "snat", "ingress"} {
		t.Run(field, func(t *testing.T) {
			spec := api.NAT44RuleSpec{Type: "snat", EgressInterface: "wan", SourceRanges: []string{"192.0.2.0/24"}, SNATAddress: "198.51.100.2"}
			if field == "egress" {
				spec.EgressInterface, spec.EgressPolicyRef = "", "policy"
			}
			if field == "snat" {
				spec.SNATAddress, spec.SNATAddressFrom = "", api.StatusValueSourceSpec{Resource: "Interface/wan", Field: "primaryIPv4", Optional: true}
			}
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "nat"}, Spec: spec}}}}
			if field == "ingress" {
				router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "web"}, Spec: api.IngressServiceSpec{}})
			}
			path := filepath.Join(t.TempDir(), "nat.nft")
			if err := os.WriteFile(path, []byte("previous rules"), 0o600); err != nil {
				t.Fatal(err)
			}
			previous := map[string]any{"phase": "Applied", "snatAddress": "198.51.100.2"}
			store := failedSourceStore{testStore: &testStore{status: map[string]map[string]any{api.NetAPIVersion + "/NAT44Rule/nat": previous}}, err: failure}
			controller := Controller{Router: router, Store: store, NftablesPath: path, DryRun: true}
			if err := controller.Reconcile(context.Background()); !errors.Is(err, failure) {
				t.Fatalf("failed source became default/empty NAT configuration: %v", err)
			}
			data, _ := os.ReadFile(path)
			if string(data) != "previous rules" || !reflect.DeepEqual(store.ObjectStatus(api.NetAPIVersion, "NAT44Rule", "nat"), previous) {
				t.Fatal("failed source replaced applied NAT rules/status")
			}
		})
	}
}
