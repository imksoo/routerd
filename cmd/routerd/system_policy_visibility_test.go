// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestPersistentSysctlActualOwnerSurvivesStartupGCAndCleansAfterReopen(t *testing.T) {
	for _, kind := range []string{"Sysctl", "SysctlProfile"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			statePath := filepath.Join(dir, "state.db")
			store, err := routerstate.OpenSQLite(statePath)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			runtime := false
			resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: kind}, Metadata: api.ObjectMeta{Name: "forwarding"}}
			keys := []string{"net.inet.ip.forwarding"}
			if kind == "Sysctl" {
				resource.Spec = api.SysctlSpec{Key: keys[0], Value: "1", Runtime: &runtime, Persistent: true}
			} else {
				resource.Spec = api.SysctlProfileSpec{Profile: "router-freebsd", Runtime: &runtime, Persistent: true}
				keys = append(keys, "net.inet6.ip6.forwarding")
			}
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}
			path := filepath.Join(dir, "sysctl.conf.local")
			external := []byte("# unrelated boot policy\nexternal.setting = 7")
			if err := os.WriteFile(path, external, 0644); err != nil {
				t.Fatal(err)
			}
			c := controllerchain.SysctlController{Router: router, Store: store, OS: platform.OSFreeBSD, PersistentPath: path,
				Command: func(context.Context, string, ...string) ([]byte, error) {
					t.Fatal("persistent-only owner called kernel")
					return nil, nil
				},
			}
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			for _, key := range keys {
				name := routerstate.SysctlPersistentOwnerName("forwarding", key)
				if status, err := store.ReadObjectStatus(api.SystemAPIVersion, kind, name); err != nil || status[routerstate.SysctlPersistentOwnerKey] == nil {
					t.Fatalf("real producer did not acquire %s: %+v %v", key, status, err)
				}
			}
			// The startup collector runs before the actor and is not authority to
			// forget acquired boot fragments, even after their declaration is removed.
			removed := &api.Router{}
			if _, err := cleanupUnsupportedLegacyObjectStatusesForOS(removed, store, statePath, time.Now().UTC(), nil, platform.OSFreeBSD); err != nil {
				t.Fatal(err)
			}
			for _, key := range keys {
				if status, err := store.ReadObjectStatus(api.SystemAPIVersion, kind, routerstate.SysctlPersistentOwnerName("forwarding", key)); err != nil || status[routerstate.SysctlPersistentOwnerKey] == nil {
					t.Fatalf("startup GC forgot the acquired key %s: %+v %v", key, status, err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := routerstate.OpenSQLite(statePath)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			c.Store, c.Router = reopened, removed
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, external) {
				t.Fatalf("actor cleanup changed unrelated startup policy: %q %v", data, err)
			}
			for _, key := range keys {
				if status, err := reopened.ReadObjectStatus(api.SystemAPIVersion, kind, routerstate.SysctlPersistentOwnerName("forwarding", key)); err != nil || status[routerstate.SysctlPersistentOwnerKey] != nil {
					t.Fatalf("cleanup left authority for %s: %+v %v", key, status, err)
				}
			}
		})
	}
}
