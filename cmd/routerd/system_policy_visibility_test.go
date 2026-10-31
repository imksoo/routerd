// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPersistentSysctlPrivateOwnerDoesNotBecomeDeclaredResource(t *testing.T) {
	dir := t.TempDir()
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime := false
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: "forwarding"},
		Spec: api.SysctlSpec{Key: "net.inet.ip.forwarding", Value: "1", Runtime: &runtime, Persistent: true},
	}}}}
	c := controllerchain.SysctlController{Router: router, Store: store, OS: platform.OSFreeBSD, PersistentPath: filepath.Join(dir, "sysctl.conf.local"),
		Command: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("persistent-only visibility check called the kernel")
			return nil, nil
		},
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	views, err := serveInspectionResources(t.Context(), router, store, "Sysctl", 0)
	if err != nil || len(views) != 1 {
		t.Fatalf("declared resource views: %+v %v", views, err)
	}
	statuses, err := store.ListObjectStatuses()
	if err != nil {
		t.Fatal(err)
	}
	if issues := resourcePhaseIssues(statuses); len(issues) != 0 {
		t.Fatalf("private owner leaked as a declaration phase issue: %+v", issues)
	}
	if phase := overallStatusPhase("Healthy", statuses); phase != "Healthy" {
		t.Fatalf("private owner changed the resource health: %s", phase)
	}
}
