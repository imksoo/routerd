// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestFreeBSDPersistentSysctlResumesAfterAcknowledgementFailure(t *testing.T) {
	c, initial := persistentSysctlFixture(t)
	path := filepath.Join(filepath.Dir(c.PersistentPath), "state.db")
	fault, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer fault.Close()
	if _, err := fault.Exec(`CREATE TRIGGER reject_persistent_ack BEFORE UPDATE ON objects
 WHEN NEW.name LIKE '@persistent/%' AND json_extract(NEW.status,'$.sysctlPersistentOwner.pending')=0
 BEGIN SELECT RAISE(ABORT,'fixture persistent acknowledgement failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := c.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "fixture persistent acknowledgement failure") {
		t.Fatalf("lost acknowledgement was not reported: %v", err)
	}
	name := routerstate.SysctlPersistentOwnerName("forwarding", "net.inet.ip.forwarding")
	pending, err := c.readPersistentSysctlOwner("Sysctl", name)
	if err != nil || pending == nil || !pending.Pending || pending.Withdrawing {
		t.Fatalf("pending receipt: %+v %v", pending, err)
	}
	written, err := os.ReadFile(c.PersistentPath)
	if err != nil || string(written) != pending.Block {
		t.Fatalf("unacknowledged file effect: %q %v", written, err)
	}
	info, err := os.Stat(c.PersistentPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fault.Exec("DROP TRIGGER reject_persistent_ack"); err != nil {
		t.Fatal(err)
	}
	if err := initial.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	c.Store = reopened
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	active, err := c.readPersistentSysctlOwner("Sysctl", name)
	after, statErr := os.Stat(c.PersistentPath)
	if err != nil || active == nil || active.Pending || active.Token != pending.Token || statErr != nil || !after.ModTime().Equal(info.ModTime()) {
		t.Fatalf("restart did not acknowledge the same effect without rewriting it: %+v %v %v", active, err, statErr)
	}
	if _, err := fault.Exec(`CREATE TRIGGER reject_persistent_release BEFORE DELETE ON objects
 WHEN OLD.name LIKE '@persistent/%'
 BEGIN SELECT RAISE(ABORT,'fixture persistent release failure'); END`); err != nil {
		t.Fatal(err)
	}
	c.Router = &api.Router{}
	if err := c.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "fixture persistent release failure") {
		t.Fatalf("lost release acknowledgement was not reported: %v", err)
	}
	withdrawing, err := c.readPersistentSysctlOwner("Sysctl", name)
	remaining, fileErr := os.ReadFile(c.PersistentPath)
	if err != nil || withdrawing == nil || !withdrawing.Withdrawing || fileErr != nil || len(remaining) != 0 {
		t.Fatalf("withdrawal lost its proven receipt or effect: %+v %v %q %v", withdrawing, err, remaining, fileErr)
	}
	if _, err := fault.Exec("DROP TRIGGER reject_persistent_release"); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	final, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer final.Close()
	c.Store = final
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if owner, err := c.readPersistentSysctlOwner("Sysctl", name); err != nil || owner != nil {
		t.Fatalf("release did not finish after reopen: %+v %v", owner, err)
	}
	if display := final.ObjectStatus(api.SystemAPIVersion, "Sysctl", "forwarding"); display["phase"] != "Removed" {
		t.Fatalf("released display: %+v", display)
	}
}

type persistentSysctlIntentWaitStore struct {
	*routerstate.SQLiteStore
	afterIntent func()
}

func (s *persistentSysctlIntentWaitStore) SaveSysctlPersistentOwner(ctx context.Context, kind, name string, previous, next *routerstate.SysctlPersistentOwner, retire bool) error {
	if err := s.SQLiteStore.SaveSysctlPersistentOwner(ctx, kind, name, previous, next, retire); err != nil {
		return err
	}
	if s.afterIntent != nil && next != nil && next.Pending && !next.Withdrawing {
		run := s.afterIntent
		s.afterIntent = nil
		run()
	}
	return nil
}

func TestFreeBSDPersistentSysctlKeepsNewOwnerAfterIntentWait(t *testing.T) {
	first, store := persistentSysctlFixture(t)
	second := first
	second.Router = &api.Router{Spec: api.RouterSpec{Resources: append([]api.Resource(nil), first.Router.Spec.Resources...)}}
	spec, _ := second.Router.Spec.Resources[0].SysctlSpec()
	spec.Value = "0"
	second.Router.Spec.Resources[0].Spec = spec
	waiting := &persistentSysctlIntentWaitStore{SQLiteStore: store, afterIntent: func() {
		if err := second.Reconcile(t.Context()); err != nil {
			t.Fatalf("competing real reconciliation: %v", err)
		}
	}}
	first.Store = waiting
	if err := first.Reconcile(t.Context()); !errors.Is(err, routerstate.ErrSysctlPersistentOwnerChanged) {
		t.Fatalf("stale intent was not rejected: %v", err)
	}
	name := routerstate.SysctlPersistentOwnerName("forwarding", "net.inet.ip.forwarding")
	owner, err := second.readPersistentSysctlOwner("Sysctl", name)
	actual, fileErr := os.ReadFile(first.PersistentPath)
	display := store.ObjectStatus(api.SystemAPIVersion, "Sysctl", "forwarding")
	if err != nil || owner == nil || owner.Pending || !strings.Contains(owner.Block, " = 0\n") || fileErr != nil || string(actual) != owner.Block || display["phase"] != "Applied" || display["value"] != "0" {
		t.Fatalf("stale caller changed the competing effect/receipt/display: %+v %v %q %v %+v", owner, err, actual, fileErr, display)
	}
}

func TestFreeBSDPersistentSysctlKeepsExternalFileCreatedDuringIntentWait(t *testing.T) {
	c, store := persistentSysctlFixture(t)
	external := []byte("# external creator\nother.setting = 9\n")
	c.Store = &persistentSysctlIntentWaitStore{SQLiteStore: store, afterIntent: func() {
		if err := os.WriteFile(c.PersistentPath, external, 0600); err != nil {
			t.Fatal(err)
		}
	}}
	if err := c.Reconcile(t.Context()); err == nil {
		t.Fatal("external file introduced while storing intent was replaced")
	}
	actual, err := os.ReadFile(c.PersistentPath)
	if err != nil || !bytes.Equal(actual, external) {
		t.Fatalf("external file changed: %q %v", actual, err)
	}
	owner, err := c.readPersistentSysctlOwner("Sysctl", routerstate.SysctlPersistentOwnerName("forwarding", "net.inet.ip.forwarding"))
	if err != nil || owner == nil || !owner.Pending {
		t.Fatalf("failed effect forgot its pending receipt: %+v %v", owner, err)
	}
}
