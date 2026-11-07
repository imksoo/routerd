// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

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

func TestDHCPv4ServerLeaseSyncAddsStickyLogSourcesWhenEnabled(t *testing.T) {
	controller := FileSyncController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-dhcpv4"}, Spec: api.DHCPv4ServerSpec{StickyHoldDays: 3}},
		}}},
	}
	job, err := controller.fileSyncJobFromDHCPv4ServerLeaseSync(api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync"},
		Metadata: api.ObjectMeta{Name: "lan-v4-leases"},
		Spec: api.DHCPv4ServerLeaseSyncSpec{
			Source:  api.DHCPv4ServerLeaseSyncSourceSpec{Resource: "DHCPv4Server/lan-dhcpv4"},
			Targets: []api.LeaseSyncTargetSpec{{Host: "homert03.lain.local"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Sources) != 3 {
		t.Fatalf("sources = %#v, want lease file plus sticky database and WAL", job.Sources)
	}
	if job.Sources[0].Name != "leaseFile" || !job.Sources[0].Required {
		t.Fatalf("lease source = %#v, want required leaseFile first", job.Sources[0])
	}
	wantSuffixes := []string{"dhcp-sticky.db", "dhcp-sticky.db-wal"}
	for i, suffix := range wantSuffixes {
		source := job.Sources[i+1]
		if source.Required {
			t.Fatalf("sticky source %s is required; source=%#v", suffix, source)
		}
		if !strings.HasSuffix(source.Path, suffix) {
			t.Fatalf("sticky source path = %q, want suffix %q", source.Path, suffix)
		}
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
			want:     "/var/lib/routerd/dnsmasq/dnsmasq.leases",
		},
		{
			name:     "freebsd-rcd",
			defaults: platform.Defaults{RuntimeDir: "/var/run/routerd", StateDir: "/var/db/routerd"},
			features: platform.Features{HasRCD: true},
			want:     "/var/db/routerd/dnsmasq/dnsmasq.leases",
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
