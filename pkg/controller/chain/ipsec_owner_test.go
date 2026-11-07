// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestIPsecFrameworkOwnerRetriesAndUsesCurrentShapePreservingConfiguration(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := func(remote string) *api.Router {
		return &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "site"}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPsecConnection"}, Metadata: api.ObjectMeta{Name: "site"}, Spec: api.IPsecConnectionSpec{LocalAddress: "198.51.100.1", RemoteAddress: remote, LeftSubnet: "10.1.0.0/24", RightSubnet: "10.2.0.0/24", PreSharedKey: "secret"}}}}}
	}
	var seen []string
	rejected := errors.New("native acknowledgement rejected")
	fail := true
	runner := &Runner{Router: router("203.0.113.1"), Store: store, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"ipsec"}, ReconcileIPsec: func(_ context.Context, current *api.Router) error {
		if len(current.Spec.Resources) == 0 {
			seen = append(seen, "withdraw")
			return nil
		}
		spec, err := current.Spec.Resources[0].IPsecConnectionSpec()
		if err != nil {
			return err
		}
		seen = append(seen, spec.RemoteAddress)
		if fail {
			fail = false
			return rejected
		}
		return nil
	}}}
	if err := runner.ReconcileOnce(t.Context()); !errors.Is(err, rejected) {
		t.Fatalf("failed native ack became success: %v", err)
	}
	if err := runner.ReconcileOnce(t.Context()); err != nil {
		t.Fatalf("same configuration retry: %v", err)
	}
	// This is the publication used by confirmed shape-preserving mutations.
	runner.runtimeSnapshot = RuntimeSnapshot{Known: true, Available: true, Router: runner.Router}
	if err := runner.UpdateRuntimeRouter(router("203.0.113.2")); err != nil {
		t.Fatal(err)
	}
	if err := runner.ReconcileOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	empty := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "site"}}
	if err := runner.UpdateRuntimeRouter(empty); err != nil {
		t.Fatal(err)
	}
	if err := runner.ReconcileOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 || seen[0] != "203.0.113.1" || seen[1] != "203.0.113.1" || seen[2] != "203.0.113.2" || seen[3] != "withdraw" {
		t.Fatalf("native owner observations: %v", seen)
	}
	runner.HADecision = &ha.Decision{Enabled: true, Leader: false}
	before := len(seen)
	if err := runner.ReconcileOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(seen) != before {
		t.Fatal("standby invoked live IPsec native owner")
	}
	if _, err := runner.ObserveRuntimeRouter(store.Now(), platform.CurrentOS()); err != nil {
		t.Fatal(err)
	}
}
