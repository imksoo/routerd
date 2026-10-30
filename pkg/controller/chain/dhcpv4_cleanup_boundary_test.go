// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/dhcpv4client"
	"testing"
)

type cleanupStatusOnlyStore struct{ Store }

func TestEventedStoreRequiresCleanupCapabilities(t *testing.T) {
	for _, store := range []eventedStore{{}, {Store: cleanupStatusOnlyStore{Store: mapStore{}}}} {
		if _, err := store.ListObjectStatuses(); err == nil {
			t.Fatal("unavailable enumeration fabricated an empty observation")
		}
		if err := store.DeleteObject(api.NetAPIVersion, "DHCPv4Client", "removed"); err == nil {
			t.Fatal("unavailable deletion reported success")
		}
	}
}

func TestDHCPv4RemovedOwnerThroughEventedStore(t *testing.T) {
	key := api.NetAPIVersion + "/DHCPv4Client/removed"
	base := mapStore{key: {"managedBy": "routerd"}}
	controller := dhcpv4client.Controller{Router: &api.Router{}, Store: eventedStore{Store: base}}
	if err := controller.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if base[key] != nil {
		t.Fatal("production status wrapper did not complete removed DHCPv4 cleanup")
	}
}
