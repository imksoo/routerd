// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestDHCPLeaseSyncRunsRsync(t *testing.T) {
	dir := t.TempDir()
	leaseFile := filepath.Join(dir, "dnsmasq.leases")
	if err := os.WriteFile(leaseFile, []byte("1 aa:bb:cc:dd:ee:ff 192.168.10.20 host *\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var gotName string
	var gotArgs []string
	var gotDeadline bool
	controller := FileSyncController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPLeaseSync"}, Metadata: api.ObjectMeta{Name: "lan-leases"}, Spec: api.DHCPLeaseSyncSpec{
				LeaseFile: leaseFile,
				Targets: []api.DHCPLeaseSyncTargetSpec{{
					Host:       "homert03.lain.local",
					User:       "routerd",
					Path:       "/var/lib/routerd/dnsmasq/dnsmasq.leases",
					SSHOptions: []string{"-o", "ConnectTimeout=3"},
				}},
			}},
		}}},
		Store: mapStore{},
		Now:   func() time.Time { return time.Date(2026, 6, 4, 22, 0, 0, 0, time.UTC) },
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_, gotDeadline = ctx.Deadline()
			gotName = name
			gotArgs = append([]string(nil), args...)
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotName != "rsync" {
		t.Fatalf("command = %q", gotName)
	}
	wantArgs := []string{
		"-a",
		"--delay-updates",
		"--timeout=60",
		"-e",
		"ssh -o BatchMode=yes -o ConnectTimeout=3",
		"--rsync-path=mkdir -p '/var/lib/routerd/dnsmasq' && rsync",
		leaseFile,
		"routerd@homert03.lain.local:/var/lib/routerd/dnsmasq/dnsmasq.leases",
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
	if !gotDeadline {
		t.Fatal("sync command context has no deadline")
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "DHCPLeaseSync", "lan-leases")
	if status["phase"] != "Synced" || status["sourceCount"] != 1 || status["targetCount"] != 1 {
		t.Fatalf("status = %#v", status)
	}
}

func TestDHCPLeaseSyncRsyncArgsHonorUserTimeoutOverrides(t *testing.T) {
	args := fileSyncRsyncArgs(
		fileSyncSource{Path: "/var/lib/routerd/dnsmasq/dnsmasq.leases"},
		fileSyncTarget{
			Host:       "homert03.lain.local",
			SSHOptions: []string{"-o", "BatchMode=no", "-o", "ConnectTimeout=3"},
			Options:    []string{"--timeout=5"},
		},
		1,
	)
	joined := strings.Join(args, "\x00")
	if strings.Contains(joined, "--timeout=60") {
		t.Fatalf("args = %#v, want user rsync timeout to suppress default timeout", args)
	}
	wantSSH := "ssh -o BatchMode=no -o ConnectTimeout=3"
	for i, arg := range args {
		if arg == "-e" && i+1 < len(args) && args[i+1] == wantSSH {
			return
		}
	}
	t.Fatalf("args = %#v, want ssh command %q", args, wantSSH)
}

func TestDHCPLeaseSyncMissingLeaseFileIsPending(t *testing.T) {
	dir := t.TempDir()
	leaseFile := filepath.Join(dir, "missing.leases")
	store := mapStore{}
	called := false
	controller := FileSyncController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPLeaseSync"}, Metadata: api.ObjectMeta{Name: "lan-leases"}, Spec: api.DHCPLeaseSyncSpec{
				LeaseFile: leaseFile,
				Targets:   []api.DHCPLeaseSyncTargetSpec{{Host: "homert03.lain.local"}},
			}},
		}}},
		Store: store,
		Command: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			called = true
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("sync command was called for missing lease file")
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPLeaseSync", "lan-leases")
	if status["phase"] != "Pending" || status["reason"] != "SourceMissing" || !strings.Contains(status["source"].(string), "missing.leases") {
		t.Fatalf("status = %#v", status)
	}
}
