// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/state"
)

func (s mapStore) ListObjectStatuses() ([]state.ObjectStatus, error) {
	var out []state.ObjectStatus
	for key, status := range s {
		parts := strings.Split(key, "/")
		out = append(out, state.ObjectStatus{APIVersion: strings.Join(parts[:2], "/"), Kind: parts[2], Name: parts[3], Status: status})
	}
	return out, nil
}

func (s mapStore) DeleteObject(version, kind, name string) error {
	delete(s, version+"/"+kind+"/"+name)
	return nil
}

func TestRemovedZoneRetiresStatusButLeavesResolverToSupervisor(t *testing.T) {
	store, err := state.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, kind := range []string{"DNSZone", "DNSResolver"} {
		if err := store.SaveObjectStatus(api.NetAPIVersion, kind, "old", map[string]any{"phase": "Pending"}); err != nil {
			t.Fatal(err)
		}
	}
	c := Controller{Router: &api.Router{}, Store: store}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "DNSZone", "old"); got != nil {
		t.Errorf("removed zone retains status: %v", got)
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "old"); got["phase"] != "Pending" {
		t.Errorf("resolver status deleted without supervisor stop: %v", got)
	}
}

func TestDisabledZoneDeclarationAndDryRunRetainStatus(t *testing.T) {
	store := mapStore{}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DNSZone", "home", map[string]any{"phase": "Pending"}); err != nil {
		t.Fatal(err)
	}
	c := Controller{Router: &api.Router{}, DeclaredRouter: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"}, Metadata: api.ObjectMeta{Name: "home"}},
	}}}, Store: store}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "DNSZone", "home"); got["phase"] != "Pending" {
		t.Fatalf("disabled declaration lost status: %v", got)
	}
	c.DeclaredRouter = c.Router
	c.DryRun = true
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "DNSZone", "home"); got["phase"] != "Pending" {
		t.Fatalf("dry-run deleted status: %v", got)
	}
}
