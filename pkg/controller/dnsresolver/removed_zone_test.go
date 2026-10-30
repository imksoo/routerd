// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/state"
)

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
