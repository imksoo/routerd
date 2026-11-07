// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func hostnameFixture(t *testing.T) (HostnameController, *routerstate.SQLiteStore) {
	t.Helper()
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return HostnameController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Hostname"},
			Metadata: api.ObjectMeta{Name: "appliance"},
			Spec:     api.HostnameSpec{Hostname: "routerd-fixture.invalid", Managed: true},
		}}}},
		Store: store, OS: platform.OSLinux, ConfigPath: filepath.Join(t.TempDir(), "hostname"),
	}, store
}

func TestHostnameControllerRetriesFailedEffectAndVerifiesRuntime(t *testing.T) {
	c, store := hostnameFixture(t)
	current, writes := "previous.invalid", 0
	c.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "hostname" {
			t.Fatalf("unexpected command %s %v", name, args)
		}
		if len(args) == 0 {
			return []byte(current + "\n"), nil
		}
		writes++
		if writes == 1 {
			return nil, errors.New("injected setter failure")
		}
		current = args[0]
		return nil, nil
	}
	if err := c.Reconcile(t.Context()); err == nil {
		t.Fatal("failed hostname effect was accepted")
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "Hostname", "appliance"); status["phase"] != "Error" || status["reason"] != "ApplyFailed" {
		t.Fatalf("failed effect status: %+v", status)
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if current != "routerd-fixture.invalid" || writes != 2 {
		t.Fatalf("retry did not apply the hostname: current=%q writes=%d", current, writes)
	}
	if data, err := os.ReadFile(c.ConfigPath); err != nil || string(data) != "routerd-fixture.invalid\n" {
		t.Fatalf("persistent hostname: data=%q err=%v", data, err)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "Hostname", "appliance"); status["phase"] != "Applied" {
		t.Fatalf("successful retry status: %+v", status)
	}
	if err := c.Reconcile(t.Context()); err != nil || writes != 2 {
		t.Fatalf("converged hostname repeated the native effect: writes=%d err=%v", writes, err)
	}
	// Removing the declaration releases enforcement without guessing a previous
	// system identity or changing a value now maintained by someone else.
	current = "external.invalid"
	c.Router = &api.Router{}
	if err := c.Reconcile(t.Context()); err != nil || current != "external.invalid" || writes != 2 {
		t.Fatalf("removed declaration modified the system hostname: current=%q writes=%d err=%v", current, writes, err)
	}
}

func TestHostnameControllerUnmanagedDoesNotObserveOrWrite(t *testing.T) {
	c, store := hostnameFixture(t)
	c.Router.Spec.Resources[0].Spec = api.HostnameSpec{Hostname: "ignored.invalid", Managed: false}
	c.Command = func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("unmanaged hostname executed a command")
		return nil, nil
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.ConfigPath); !os.IsNotExist(err) {
		t.Fatalf("unmanaged hostname persisted a file: %v", err)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "Hostname", "appliance"); status["phase"] != "Skipped" || status["reason"] != "Unmanaged" {
		t.Fatalf("unmanaged status: %+v", status)
	}
}

func TestHostnameControllerRequiresActualReadback(t *testing.T) {
	c, store := hostnameFixture(t)
	c.Command = func(context.Context, string, ...string) ([]byte, error) { return []byte("unchanged.invalid\n"), nil }
	if err := c.Reconcile(t.Context()); err == nil {
		t.Fatal("successful command without observed change was accepted")
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "Hostname", "appliance"); status["phase"] == "Applied" {
		t.Fatalf("unobserved hostname was reported applied: %+v", status)
	}
}

func TestHostnameControllerRejectsMultipleManagedBeforeEffects(t *testing.T) {
	c, _ := hostnameFixture(t)
	second := c.Router.Spec.Resources[0]
	second.Metadata.Name = "second"
	c.Router.Spec.Resources = append(c.Router.Spec.Resources, second)
	c.Command = func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("multiple managed hostnames reached the OS")
		return nil, nil
	}
	if err := c.Reconcile(t.Context()); err == nil {
		t.Fatal("multiple managed hostnames were accepted")
	}
}
