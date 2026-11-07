// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func requireSystemPolicyNamespaces(t *testing.T) {
	t.Helper()
	if os.Getenv("ROUTERD_RUN_SYSTEM_POLICY_NATIVE") != "1" {
		t.Skip("requires explicit private UTS and network namespaces")
	}
	for _, kind := range []string{"uts", "net"} {
		current, err := os.Readlink("/proc/self/ns/" + kind)
		if err != nil {
			t.Fatal(err)
		}
		outer := os.Getenv("ROUTERD_HOST_" + strings.ToUpper(kind) + "NS")
		if !strings.HasPrefix(outer, kind+":[") || outer == current {
			t.Fatalf("%s isolation is unverified: outer=%q current=%q", kind, outer, current)
		}
		t.Logf("private %s namespace: outer=%s current=%s", kind, outer, current)
	}
}

func TestHostnameNativeLinuxPrivateUTSLifecycle(t *testing.T) {
	requireSystemPolicyNamespaces(t)
	dir := t.TempDir()
	database := filepath.Join(dir, "state.db")
	store, err := routerstate.OpenSQLite(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store != nil {
			_ = store.Close()
		}
	})
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Hostname"},
		Metadata: api.ObjectMeta{Name: "appliance"},
		Spec:     api.HostnameSpec{Hostname: "routerd-uts-first.invalid", Managed: true},
	}}}}
	c := HostnameController{Router: router, Store: store, OS: platform.OSLinux, ConfigPath: filepath.Join(dir, "hostname")}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if actual, err := os.Hostname(); err != nil || actual != "routerd-uts-first.invalid" {
		t.Fatalf("actual UTS hostname: %q %v", actual, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(database)
	if err != nil {
		t.Fatal(err)
	}
	c.Store = store
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "Hostname", "appliance"); status["phase"] != "Applied" || status["changed"] != false {
		t.Fatalf("reopened owner did not observe convergence: %+v", status)
	}
	router.Spec.Resources[0].Spec = api.HostnameSpec{Hostname: "routerd-uts-second.invalid", Managed: true}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if actual, err := os.Hostname(); err != nil || actual != "routerd-uts-second.invalid" {
		t.Fatalf("actual hostname update: %q %v", actual, err)
	}
	if out, err := exec.CommandContext(t.Context(), "hostname", "external-uts.invalid").CombinedOutput(); err != nil {
		t.Fatalf("independent private hostname replacement: %v %s", err, out)
	}
	c.Router = &api.Router{}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if actual, err := os.Hostname(); err != nil || actual != "external-uts.invalid" {
		t.Fatalf("released declaration changed external hostname: %q %v", actual, err)
	}
	t.Log("actual private UTS acquisition, SQLite reopen, update and management release passed")
}

func TestSysctlNativeLinuxPrivateNetworkValue(t *testing.T) {
	requireSystemPolicyNamespaces(t)
	before, err := exec.CommandContext(t.Context(), "sysctl", "-n", "net.ipv4.ip_forward").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	target := "1"
	if strings.TrimSpace(string(before)) == target {
		target = "0"
	}
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	dir := t.TempDir()
	c := SysctlController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"},
			Metadata: api.ObjectMeta{Name: "private-forwarding"},
			Spec:     api.SysctlSpec{Key: "net.ipv4.ip_forward", Value: target, Persistent: true},
		}}}},
		Store: store, BaseDir: dir,
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := exec.CommandContext(t.Context(), "sysctl", "-n", "net.ipv4.ip_forward").CombinedOutput()
	if err != nil || strings.TrimSpace(string(after)) != target {
		t.Fatalf("actual private forwarding value: %q %v", after, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "90-routerd-private-forwarding.conf")); err != nil || !strings.Contains(string(data), "net.ipv4.ip_forward = "+target) {
		t.Fatalf("private persistent fragment: %q %v", data, err)
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if status := store.ObjectStatus(api.SystemAPIVersion, "Sysctl", "private-forwarding"); status["changed"] != false {
		t.Fatalf("converged private sysctl was changed again: %+v", status)
	}
	t.Log("actual netns sysctl mutation and private persistent fragment passed")
}
