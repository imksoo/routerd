// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestDSLiteNeverActivatedThenRemovedRetiresOnlyAfterLiveCleanup(t *testing.T) {
	c, native, store := newDSLiteNativeController(t, platform.OSLinux)
	c.DeclaredRouter, c.Router = c.Router, &api.Router{}
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "ds"); got["phase"] != PhaseDisabled || got["reason"] != "WhenFalse" {
		t.Fatalf("disabled declaration lost: %v", got)
	}
	if len(native.links) != 0 {
		t.Fatal("inactive declaration created native effects")
	}
	c.DeclaredRouter = c.Router
	c.DryRun = true
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(store.ObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "ds")) == 0 {
		t.Fatal("preview deleted dormant status")
	}
	c.DryRun = false
	store.failRead = true
	if err := c.reconcile(t.Context()); err == nil {
		t.Fatal("unreadable ownership reported cleanup success")
	}
	store.failRead = false
	store.failStatusDelete = true
	if err := c.reconcile(t.Context()); err == nil {
		t.Fatal("failed status deletion reported cleanup success")
	}
	store.failStatusDelete = false
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "ds"); len(got) != 0 {
		t.Fatalf("removed dormant resource still pollutes health: %v", got)
	}
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
}
