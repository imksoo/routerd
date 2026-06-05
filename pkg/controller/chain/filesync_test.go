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
	"github.com/imksoo/routerd/pkg/platform"
)

func TestDHCPv4ServerLeaseSyncRunsRsync(t *testing.T) {
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
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-dhcpv4"}, Spec: api.DHCPv4ServerSpec{LeaseFile: leaseFile}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync"}, Metadata: api.ObjectMeta{Name: "lan-v4-leases"}, Spec: api.DHCPv4ServerLeaseSyncSpec{
				Source: api.DHCPv4ServerLeaseSyncSourceSpec{Resource: "DHCPv4Server/lan-dhcpv4"},
				Targets: []api.LeaseSyncTargetSpec{{
					Host:       "homert03.lain.local",
					User:       "routerd",
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
		"--rsync-path=mkdir -p '" + dir + "' && rsync",
		leaseFile,
		"routerd@homert03.lain.local:" + leaseFile,
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
	if !gotDeadline {
		t.Fatal("sync command context has no deadline")
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "DHCPv4ServerLeaseSync", "lan-v4-leases")
	if status["phase"] != "Synced" || status["sourceCount"] != 1 || status["targetCount"] != 1 {
		t.Fatalf("status = %#v", status)
	}
}

func TestLeaseSyncRsyncArgsHonorUserTimeoutOverrides(t *testing.T) {
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

func TestDHCPv4ServerLeaseSyncMissingLeaseFileIsPending(t *testing.T) {
	dir := t.TempDir()
	leaseFile := filepath.Join(dir, "missing.leases")
	store := mapStore{}
	called := false
	controller := FileSyncController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-dhcpv4"}, Spec: api.DHCPv4ServerSpec{LeaseFile: leaseFile}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync"}, Metadata: api.ObjectMeta{Name: "lan-v4-leases"}, Spec: api.DHCPv4ServerLeaseSyncSpec{
				Source:  api.DHCPv4ServerLeaseSyncSourceSpec{Resource: "lan-dhcpv4"},
				Targets: []api.LeaseSyncTargetSpec{{Host: "homert03.lain.local"}},
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
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4ServerLeaseSync", "lan-v4-leases")
	if status["phase"] != "Pending" || status["reason"] != "SourceMissing" || !strings.Contains(status["source"].(string), "missing.leases") {
		t.Fatalf("status = %#v", status)
	}
}

func TestDHCPv4ServerLeaseSyncDefaultLeaseFileMatchesRuntimeDnsmasq(t *testing.T) {
	got := defaultDHCPv4LeaseFile()
	defaults, features := platform.Current()
	if want := defaultDNSMasqLeaseFileFor(defaults, features); got != want {
		t.Fatalf("default DHCPv4 lease file = %q, want %q", got, want)
	}
}

func TestDefaultDNSMasqLeaseFileUsesPlatformHelpers(t *testing.T) {
	for _, tt := range []struct {
		name     string
		defaults platform.Defaults
		features platform.Features
		want     string
	}{
		{
			name:     "linux",
			defaults: platform.Defaults{RuntimeDir: "/run/routerd", StateDir: "/var/lib/routerd"},
			features: platform.Features{},
			want:     "/run/routerd/dnsmasq.leases",
		},
		{
			name:     "freebsd-rcd",
			defaults: platform.Defaults{RuntimeDir: "/var/run/routerd", StateDir: "/var/db/routerd"},
			features: platform.Features{HasRCD: true},
			want:     "/var/db/routerd/dnsmasq/dnsmasq.leases",
		},
		{
			name:     "openrc",
			defaults: platform.Defaults{RuntimeDir: "/run/routerd", StateDir: "/var/lib/routerd"},
			features: platform.Features{HasOpenRC: true},
			want:     "/var/lib/routerd/dnsmasq/dnsmasq.leases",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultDNSMasqLeaseFileFor(tt.defaults, tt.features)
			if got != tt.want {
				t.Fatalf("defaultDNSMasqLeaseFileFor = %q, want %q", got, tt.want)
			}
			candidates := platform.DnsmasqLeaseCandidates(tt.defaults, tt.features)
			if len(candidates) == 0 || got != candidates[0] {
				t.Fatalf("defaultDNSMasqLeaseFileFor = %q, want first candidate from %#v", got, candidates)
			}
		})
	}
}

func TestDHCPv6ServerLeaseSyncDerivesDnsmasqLeaseFile(t *testing.T) {
	dir := t.TempDir()
	leaseFile := filepath.Join(dir, "dnsmasq.leases")
	if err := os.WriteFile(leaseFile, []byte("duid 1 2 3 4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var gotArgs []string
	controller := FileSyncController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "lan-dhcpv6"}, Spec: api.DHCPv6ServerSpec{LeaseFile: leaseFile}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6ServerLeaseSync"}, Metadata: api.ObjectMeta{Name: "lan-v6-leases"}, Spec: api.DHCPv6ServerLeaseSyncSpec{
				Source:  api.DHCPv6ServerLeaseSyncSourceSpec{Resource: "DHCPv6Server/lan-dhcpv6"},
				Targets: []api.LeaseSyncTargetSpec{{Host: "homert03.lain.local"}},
			}},
		}}},
		Store: mapStore{},
		Command: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			gotArgs = append([]string(nil), args...)
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, "\x00")
	if !strings.Contains(joined, leaseFile) || !strings.Contains(joined, "homert03.lain.local:"+leaseFile) {
		t.Fatalf("args = %#v, want derived lease path %s", gotArgs, leaseFile)
	}
}

func TestDHCPv6PrefixDelegationLeaseSyncDerivesClientSnapshot(t *testing.T) {
	got, err := fileSyncJobFromDHCPv6PrefixDelegationLeaseSync(api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegationLeaseSync"},
		Metadata: api.ObjectMeta{Name: "wan-pd-leases"},
		Spec: api.DHCPv6PrefixDelegationLeaseSyncSpec{
			Source:  api.DHCPv6PrefixDelegationLeaseSyncSourceSpec{Resource: "DHCPv6PrefixDelegation/wan-pd"},
			Targets: []api.LeaseSyncTargetSpec{{Host: "homert03.lain.local"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 1 || !strings.HasSuffix(got.Sources[0].Path, "/dhcpv6-client/wan-pd/lease.json") {
		t.Fatalf("sources = %#v", got.Sources)
	}
}
