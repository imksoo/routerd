// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func persistentSysctlFixture(t *testing.T) (SysctlController, *routerstate.SQLiteStore) {
	t.Helper()
	dir := t.TempDir()
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	controller := SysctlController{Store: store, OS: platform.OSFreeBSD, PersistentPath: filepath.Join(dir, "sysctl.conf.local"),
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: "forwarding"},
			Spec: api.SysctlSpec{Key: "net.inet.ip.forwarding", Value: "1", Persistent: true, Runtime: boolPtr(false)},
		}}}},
		Command: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("persistent-only sysctl called the kernel")
			return nil, nil
		},
	}
	return controller, store
}

func TestFreeBSDPersistentSysctlOwnsOnlyExactBlock(t *testing.T) {
	c, store := persistentSysctlFixture(t)
	original := []byte("# external comment\nexternal.unrelated = 7")
	if err := os.WriteFile(c.PersistentPath, original, 0640); err != nil {
		t.Fatal(err)
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	name := routerstate.SysctlPersistentOwnerName("forwarding", "net.inet.ip.forwarding")
	acquired, err := c.readPersistentSysctlOwner("Sysctl", name)
	if err != nil || acquired == nil || acquired.Pending {
		t.Fatalf("acquired owner: %+v %v", acquired, err)
	}
	data, err := os.ReadFile(c.PersistentPath)
	if err != nil || !bytes.HasSuffix(data, original) {
		t.Fatalf("foreign text or final newline changed: %q %v", data, err)
	}
	info, err := os.Stat(c.PersistentPath)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("shared file mode changed: %v %v", info, err)
	}
	beforeMod := info.ModTime()
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(c.PersistentPath)
	if err != nil || !info.ModTime().Equal(beforeMod) {
		t.Fatalf("converged file was rewritten: %v %v", info, err)
	}
	spec, _ := c.Router.Spec.Resources[0].SysctlSpec()
	spec.Value = "0"
	c.Router.Spec.Resources[0].Spec = spec
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	updated, err := c.readPersistentSysctlOwner("Sysctl", name)
	if err != nil || updated == nil || updated.Token != acquired.Token || updated.Pending || updated.Block == acquired.Block {
		t.Fatalf("owned update: %+v %v", updated, err)
	}
	// Ordinary status publication has no authority field and cannot erase it.
	if err := store.SaveObjectStatus(api.SystemAPIVersion, "Sysctl", "forwarding", map[string]any{"phase": "Skipped"}); err != nil {
		t.Fatal(err)
	}
	c.Router = &api.Router{}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(c.PersistentPath)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatalf("withdrawal changed external bytes: %q %v", data, err)
	}
	if status := store.ObjectStatus(api.SystemAPIVersion, "Sysctl", "forwarding"); status["phase"] != "Removed" {
		t.Fatalf("retired declaration remains applied: %+v", status)
	}
	released, err := c.readPersistentSysctlOwner("Sysctl", name)
	if err != nil || released != nil {
		t.Fatalf("withdrawal retained active receipt: %+v %v", released, err)
	}
}

func TestFreeBSDPersistentSysctlRejectsForeignOrEditedBlock(t *testing.T) {
	c, _ := persistentSysctlFixture(t)
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	name := routerstate.SysctlPersistentOwnerName("forwarding", "net.inet.ip.forwarding")
	owner, err := c.readPersistentSysctlOwner("Sysctl", name)
	if err != nil || owner == nil {
		t.Fatalf("initial owner: %+v %v", owner, err)
	}
	edited := bytes.ReplaceAll([]byte(owner.Block), []byte(" = 1"), []byte(" = 9"))
	if err := os.WriteFile(c.PersistentPath, edited, 0644); err != nil {
		t.Fatal(err)
	}
	c.Router = &api.Router{}
	if err := c.Reconcile(t.Context()); err == nil {
		t.Fatal("edited block was removed")
	}
	current, err := os.ReadFile(c.PersistentPath)
	if err != nil || !bytes.Equal(current, edited) {
		t.Fatalf("foreign edit changed: %q %v", current, err)
	}
	record, err := c.readPersistentSysctlOwner("Sysctl", name)
	if err != nil || record == nil || !record.Withdrawing {
		t.Fatalf("failed withdrawal lost its receipt: %+v %v", record, err)
	}
	// Restore the exact recorded bytes, then retire without the old declaration.
	if err := os.WriteFile(c.PersistentPath, []byte(owner.Block), 0644); err != nil {
		t.Fatal(err)
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err = os.ReadFile(c.PersistentPath)
	if err != nil || len(current) != 0 {
		t.Fatalf("restored cleanup did not finish: %q %v", current, err)
	}
}

func TestFreeBSDPersistentSysctlDoesNotAdoptMarkerOrSymlink(t *testing.T) {
	for _, kind := range []string{"marker", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := persistentSysctlFixture(t)
			original := []byte("outside.data = 9\n")
			if kind == "symlink" {
				victim := filepath.Join(filepath.Dir(c.PersistentPath), "outside")
				if err := os.WriteFile(victim, original, 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(victim, c.PersistentPath); err != nil {
					t.Fatal(err)
				}
			} else {
				original = []byte("# BEGIN routerd sysctl Sysctl/@persistent/forwarding/net.inet.ip.forwarding token 11111111111111111111111111111111\nnet.inet.ip.forwarding = 1\n# END routerd sysctl Sysctl/@persistent/forwarding/net.inet.ip.forwarding token 11111111111111111111111111111111\n")
				if err := os.WriteFile(c.PersistentPath, original, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.Reconcile(t.Context()); err == nil {
				t.Fatal("unacquired persistent target was adopted")
			}
			current, err := os.ReadFile(c.PersistentPath)
			if err != nil || !bytes.Equal(current, original) {
				t.Fatalf("unowned target changed: %q %v", current, err)
			}
			owner, err := c.readPersistentSysctlOwner("Sysctl", routerstate.SysctlPersistentOwnerName("forwarding", "net.inet.ip.forwarding"))
			if err != nil || owner != nil {
				t.Fatalf("unowned target acquired a receipt: %+v %v", owner, err)
			}
		})
	}
}

func TestFreeBSDPersistentSysctlRetirementKeepsCurrentDeclaration(t *testing.T) {
	for _, mode := range []string{"disabled", "key-changed"} {
		t.Run(mode, func(t *testing.T) {
			c, store := persistentSysctlFixture(t)
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			spec, _ := c.Router.Spec.Resources[0].SysctlSpec()
			wantPhase := "Skipped"
			if mode == "disabled" {
				spec.Persistent = false
			} else {
				spec.Key = "net.inet6.ip6.forwarding"
				wantPhase = "Applied"
			}
			c.Router.Spec.Resources[0].Spec = spec
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			display := store.ObjectStatus(api.SystemAPIVersion, "Sysctl", "forwarding")
			if display["phase"] != wantPhase || display["key"] != spec.Key {
				t.Fatalf("former persistent retirement overwrote current display: %+v", display)
			}
			old, err := c.readPersistentSysctlOwner("Sysctl", routerstate.SysctlPersistentOwnerName("forwarding", "net.inet.ip.forwarding"))
			if err != nil || old != nil {
				t.Fatalf("old key remained acquired: %+v %v", old, err)
			}
		})
	}
}
