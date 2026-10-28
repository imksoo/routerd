// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/resource"
)

func TestPriorityPolicyPublishesOnlyAfterNftResult(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "applied")
			command := filepath.Join(dir, "nft")
			body := "#!/bin/sh\ncase \"$1\" in\nlist) exit 1;;\n-c) exit 0;;\n-f) touch '" + marker + "'; "
			if fail {
				body += "exit 1"
			} else {
				body += "exit 0"
			}
			body += ";;\nesac\n"
			if err := os.WriteFile(command, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			store := &policyResultStore{mapStore: mapStore{}, marker: marker, t: t}
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.EgressRoutePolicySpec{Mode: "priority", SourceCIDRs: []string{"192.0.2.0/24"}, Candidates: []api.EgressRoutePolicyCandidate{{Name: "uplink", Interface: "lo", GatewaySource: "none", Table: 110, Priority: 10110, Mark: 0x110}}}}}}}
			c := IPv4PolicyRouteController{Router: router, Store: store, CommandOutput: func(context.Context, string, ...string) ([]byte, error) { return nil, nil }}
			err := c.applyDefaultRoutePolicies(t.Context(), command, filepath.Join(dir, "policy.nft"))
			if (err != nil) != fail {
				t.Fatalf("apply error=%v fail=%v", err, fail)
			}
			status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "wan")
			want := "Applied"
			if fail {
				want = "Error"
			}
			if status["phase"] != want {
				t.Fatalf("status=%#v", status)
			}
			if fail && status["datapathReady"] == true {
				t.Fatalf("failed effect reported ready: %#v", status)
			}
		})
	}
}

type policyResultStore struct {
	mapStore
	marker string
	t      *testing.T
}

func (s *policyResultStore) SaveObjectStatus(v, k, n string, status map[string]any) error {
	if _, err := os.Stat(s.marker); err != nil {
		s.t.Fatalf("published status before nft application: %v", err)
	}
	return s.mapStore.SaveObjectStatus(v, k, n, status)
}

func TestPriorityPolicyReturnsStatusWriteFailure(t *testing.T) {
	c := IPv4PolicyRouteController{Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.EgressRoutePolicySpec{Mode: "priority"}}}}}, Store: failedPolicyResultStore{}, DryRun: true}
	if err := c.applyDefaultRoutePolicies(t.Context(), "nft", filepath.Join(t.TempDir(), "policy.nft")); !errors.Is(err, errPolicyResult) {
		t.Fatalf("status failure lost: %v", err)
	}
}

var errPolicyResult = errors.New("status unavailable")

type failedPolicyResultStore struct{ mapStore }

func (failedPolicyResultStore) SaveObjectStatus(string, string, string, map[string]any) error {
	return errPolicyResult
}

func TestPriorityPolicyReportsFailedWithdrawal(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "nft")
	if err := os.WriteFile(command, []byte("#!/bin/sh\ncase \"$1\" in\nlist) exit 0;;\ndelete) exit 1;;\nesac\n"), 0700); err != nil {
		t.Fatal(err)
	}
	store := mapStore{}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.EgressRoutePolicySpec{Mode: "priority"}}}}}
	c := IPv4PolicyRouteController{Router: router, Store: store}
	if err := c.applyDefaultRoutePolicies(t.Context(), command, filepath.Join(dir, "policy.nft")); err == nil {
		t.Fatal("failed nft withdrawal reported success")
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "wan"); status["phase"] != "Error" {
		t.Fatalf("withdrawal status=%#v", status)
	}
}

func TestPolicyCleanupDoesNotQueryKernelWithoutStaleOwnedRoutes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	ledger, err := resource.LoadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	controller := IPv4PolicyRouteController{Router: &api.Router{}, LedgerPath: path, CommandOutput: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("queried kernel without a stale owned resource")
		return nil, nil
	}}
	if err := controller.cleanupLedgerOwnedPolicyRoutes(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
}
