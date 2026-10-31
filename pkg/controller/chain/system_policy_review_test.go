// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/ha"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPackageControllerDetectsRemovedPackageInActualDPKGDatabase(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	dpkg, err := exec.LookPath("dpkg-query")
	if err != nil {
		t.Skip("dpkg-query is not installed")
	}
	dir := t.TempDir()
	writeStatus := func(status string) {
		t.Helper()
		data := "Package: routerd-private-fixture\nStatus: " + status + "\nArchitecture: all\nVersion: 1\nDescription: private package status fixture\n"
		if err := os.WriteFile(filepath.Join(dir, "status"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeStatus("deinstall ok config-files")
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	installed := 0
	c := PackageController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Package"},
			Metadata: api.ObjectMeta{Name: "private-dependency"},
			Spec:     api.PackageSpec{Packages: []api.OSPackageSetSpec{{OS: "debian", Manager: "apt", Names: []string{"routerd-private-fixture"}}}},
		}}}},
		Store:  store,
		OSName: "debian",
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "dpkg-query" {
				return exec.CommandContext(ctx, dpkg, append([]string{"--admindir=" + dir}, args...)...).CombinedOutput()
			}
			if name == "sudo" && len(args) > 0 {
				name, args = args[0], args[1:]
			}
			if name != "apt-get" || strings.Join(args, " ") != "install -y routerd-private-fixture" {
				t.Fatalf("unexpected package operation: %s %v", name, args)
			}
			// No package manager mutates this machine. Only the private query
			// database acknowledges installation at the existing command boundary.
			installed++
			writeStatus("install ok installed")
			return nil, nil
		},
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if installed != 1 {
		t.Fatalf("dpkg exit 0 for removed package suppressed installation: installs=%d status=%+v", installed, store.ObjectStatus(api.SystemAPIVersion, "Package", "private-dependency"))
	}
	if err := c.Reconcile(t.Context()); err != nil || installed != 1 {
		t.Fatalf("installed private package was installed again: installs=%d err=%v", installed, err)
	}
}

func TestSysctlPersistentOnlyCreatesPrivateArtifact(t *testing.T) {
	dir := t.TempDir()
	c := SysctlController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"},
			Metadata: api.ObjectMeta{Name: "persistent-only"},
			Spec:     api.SysctlSpec{Key: "net.ipv4.ip_forward", Value: "1", Runtime: boolPtr(false), Persistent: true},
		}}}},
		Store:   mapStore{},
		BaseDir: dir,
		Command: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("persistent-only policy executed a runtime command")
			return nil, nil
		},
	}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "90-routerd-persistent-only.conf"))
	if err != nil || !strings.Contains(string(data), "net.ipv4.ip_forward = 1") {
		t.Fatalf("runtime=false discarded persistent intent: data=%q err=%v", data, err)
	}
}

func TestHostnameIsReachableThroughActualStandbyRunner(t *testing.T) {
	before, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r := Runner{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Hostname"},
			Metadata: api.ObjectMeta{Name: "appliance"},
			Spec:     api.HostnameSpec{Hostname: "routerd-private-review.invalid", Managed: true},
		}}}},
		Store:      store,
		Bus:        bus.New(),
		HADecision: &ha.Decision{Enabled: true, Leader: false},
		Opts:       Options{EnabledControllers: []string{"hostname"}},
	}
	if err := r.ReconcileOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "Hostname", "appliance")
	if err != nil || status["phase"] != "Rendered" {
		t.Fatalf("standby lost the declared hostname controller: status=%+v err=%v", status, err)
	}
	after, err := os.Hostname()
	if err != nil || before != after {
		t.Fatalf("standby changed the shared hostname: before=%q after=%q err=%v", before, after, err)
	}
}
