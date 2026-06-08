// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resource"
)

type fakeArtifactTeardownExecutor struct {
	features    platform.Features
	commands    []fakeTeardownCommand
	removes     []string
	removeAlls  []string
	fwmarkRules []IPv4FwmarkRule
	routeTables []int
}

type fakeTeardownCommand struct {
	Name string
	Args []string
}

func (f *fakeArtifactTeardownExecutor) Features() platform.Features {
	return f.features
}

func (f *fakeArtifactTeardownExecutor) Run(name string, args ...string) error {
	f.commands = append(f.commands, fakeTeardownCommand{Name: name, Args: append([]string(nil), args...)})
	return nil
}

func (f *fakeArtifactTeardownExecutor) Remove(path string) error {
	f.removes = append(f.removes, path)
	return nil
}

func (f *fakeArtifactTeardownExecutor) RemoveAll(path string) error {
	f.removeAlls = append(f.removeAlls, path)
	return nil
}

func (f *fakeArtifactTeardownExecutor) DeleteIPv4FwmarkRule(priority, mark, table int) error {
	f.fwmarkRules = append(f.fwmarkRules, IPv4FwmarkRule{Priority: priority, Mark: mark, Table: table})
	return nil
}

func (f *fakeArtifactTeardownExecutor) FlushIPv4RouteTable(table int) error {
	f.routeTables = append(f.routeTables, table)
	return nil
}

func TestArtifactTeardownRegistryCleansIPv6AddressLinux(t *testing.T) {
	exec := &fakeArtifactTeardownExecutor{features: platform.Features{HasIproute2: true}}
	label, err := CleanupArtifact(exec, resource.Artifact{
		Kind:  "net.ipv6.address",
		Name:  "ens19:2001:db8::1/64",
		Owner: api.NetAPIVersion + "/VirtualAddress/old-v6",
	})
	if err != nil {
		t.Fatalf("cleanup IPv6 address: %v", err)
	}
	if label != "net.ipv6.address/ens19:2001:db8::1/64" {
		t.Fatalf("label = %q", label)
	}
	want := []fakeTeardownCommand{{
		Name: "ip",
		Args: []string{"-6", "addr", "del", "2001:db8::1/64", "dev", "ens19"},
	}}
	if !reflect.DeepEqual(exec.commands, want) {
		t.Fatalf("commands = %#v, want %#v", exec.commands, want)
	}
}

func TestArtifactTeardownRegistrySkipsUnsupportedHostIntegrations(t *testing.T) {
	artifacts := []resource.Artifact{
		{
			Kind:  "linux.ipip6.tunnel",
			Name:  "ds-lite-old",
			Owner: api.NetAPIVersion + "/DSLiteTunnel/old",
		},
		{
			Kind:       "linux.ipv4.fwmarkRule",
			Name:       "priority=100,mark=0x100,table=100",
			Attributes: map[string]string{"priority": "100", "mark": "0x100", "table": "100"},
		},
		{
			Kind:       "linux.ipv4.routeTable",
			Name:       "table=100",
			Attributes: map[string]string{"table": "100"},
		},
		{
			Kind:       "nft.table",
			Name:       "routerd_nat",
			Attributes: map[string]string{"family": "ip", "name": "routerd_nat"},
		},
		{
			Kind: "systemd.service",
			Name: "routerd-old.service",
		},
	}
	for _, artifact := range artifacts {
		t.Run(artifact.Kind, func(t *testing.T) {
			exec := &fakeArtifactTeardownExecutor{}
			label, err := CleanupArtifact(exec, artifact)
			if err != nil {
				t.Fatalf("cleanup unsupported artifact: %v", err)
			}
			if label != "" {
				t.Fatalf("label = %q, want empty", label)
			}
			if len(exec.commands) != 0 || len(exec.removes) != 0 || len(exec.removeAlls) != 0 ||
				len(exec.fwmarkRules) != 0 || len(exec.routeTables) != 0 {
				t.Fatalf("destructive operations on unsupported host: %#v %#v %#v %#v %#v", exec.commands, exec.removes, exec.removeAlls, exec.fwmarkRules, exec.routeTables)
			}
		})
	}
}

func TestArtifactTeardownRegistryFileCleanupIsIdempotent(t *testing.T) {
	exec := &fakeArtifactTeardownExecutor{}
	artifact := resource.Artifact{
		Kind:  "file",
		Name:  "/etc/ppp/peers/routerd-old",
		Owner: api.NetAPIVersion + "/PPPoESession/old",
	}
	for i := 0; i < 2; i++ {
		label, err := CleanupArtifact(exec, artifact)
		if err != nil {
			t.Fatalf("cleanup file attempt %d: %v", i+1, err)
		}
		if label != "file//etc/ppp/peers/routerd-old" {
			t.Fatalf("attempt %d label = %q", i+1, label)
		}
	}
	if want := []string{"/etc/ppp/peers/routerd-old", "/etc/ppp/peers/routerd-old"}; !reflect.DeepEqual(exec.removes, want) {
		t.Fatalf("removes = %#v, want %#v", exec.removes, want)
	}
}

func TestArtifactTeardownRegistrySkipsForeignSystemdService(t *testing.T) {
	exec := &fakeArtifactTeardownExecutor{features: platform.Features{HasSystemd: true}}
	artifact := resource.Artifact{
		Kind: "systemd.service",
		Name: "ssh.service",
	}
	if !ArtifactCleanupEligible(artifact) {
		t.Fatal("systemd.service should remain reportable as a ledger-owned orphan")
	}
	label, err := CleanupArtifact(exec, artifact)
	if err != nil {
		t.Fatalf("cleanup foreign systemd service: %v", err)
	}
	if label != "" {
		t.Fatalf("label = %q, want empty", label)
	}
	if len(exec.commands) != 0 || len(exec.removes) != 0 {
		t.Fatalf("destructive operations on foreign systemd service: %#v %#v", exec.commands, exec.removes)
	}
}

func TestArtifactTeardownRegistryCleansRouterdOpenRCService(t *testing.T) {
	exec := &fakeArtifactTeardownExecutor{features: platform.Features{HasOpenRC: true}}
	artifact := resource.Artifact{
		Kind: "openrc.service",
		Name: "routerd_dns_resolver_lan",
	}
	label, err := CleanupArtifact(exec, artifact)
	if err != nil {
		t.Fatalf("cleanup OpenRC service: %v", err)
	}
	if label != "openrc.service/routerd_dns_resolver_lan" {
		t.Fatalf("label = %q", label)
	}
	wantCommands := []fakeTeardownCommand{
		{Name: "rc-service", Args: []string{"routerd_dns_resolver_lan", "stop"}},
		{Name: "rc-update", Args: []string{"del", "routerd_dns_resolver_lan", "default"}},
	}
	if !reflect.DeepEqual(exec.commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", exec.commands, wantCommands)
	}
	if wantRemoves := []string{"/etc/init.d/routerd_dns_resolver_lan"}; !reflect.DeepEqual(exec.removes, wantRemoves) {
		t.Fatalf("removes = %#v, want %#v", exec.removes, wantRemoves)
	}
}

func TestArtifactTeardownRegistrySkipsForeignOpenRCService(t *testing.T) {
	exec := &fakeArtifactTeardownExecutor{features: platform.Features{HasOpenRC: true}}
	artifact := resource.Artifact{
		Kind: "openrc.service",
		Name: "sshd",
	}
	if ArtifactCleanupEligible(artifact) {
		t.Fatal("foreign OpenRC service should not be cleanup eligible")
	}
	label, err := CleanupArtifact(exec, artifact)
	if err != nil {
		t.Fatalf("cleanup foreign OpenRC service: %v", err)
	}
	if label != "" {
		t.Fatalf("label = %q, want empty", label)
	}
	if len(exec.commands) != 0 || len(exec.removes) != 0 {
		t.Fatalf("destructive operations on foreign OpenRC service: %#v %#v", exec.commands, exec.removes)
	}
}

func TestArtifactTeardownRegistryEligibility(t *testing.T) {
	tests := []struct {
		name     string
		artifact resource.Artifact
		want     bool
	}{
		{
			name: "routerd nft table",
			artifact: resource.Artifact{
				Kind:       "nft.table",
				Attributes: map[string]string{"family": "inet", "name": "routerd_nat"},
			},
			want: true,
		},
		{
			name: "foreign nft table",
			artifact: resource.Artifact{
				Kind:       "nft.table",
				Attributes: map[string]string{"family": "inet", "name": "other_nat"},
			},
		},
		{
			name: "routerd OpenRC service",
			artifact: resource.Artifact{
				Kind: "openrc.service",
				Name: "routerd_dns_resolver_lan",
			},
			want: true,
		},
		{
			name: "foreign OpenRC service",
			artifact: resource.Artifact{
				Kind: "openrc.service",
				Name: "sshd",
			},
		},
		{
			name: "pppoe peer file",
			artifact: resource.Artifact{
				Kind:  "file",
				Name:  "/etc/ppp/peers/../peers/routerd-old",
				Owner: api.NetAPIVersion + "/PPPoESession/old",
			},
			want: true,
		},
		{
			name: "pppoe secrets file not cleaned",
			artifact: resource.Artifact{
				Kind:  "file",
				Name:  "/etc/ppp/chap-secrets",
				Owner: api.NetAPIVersion + "/PPPoESession/old",
			},
		},
		{
			name: "static IPv6 address",
			artifact: resource.Artifact{
				Kind:  "net.ipv6.address",
				Name:  "ens19:2001:db8::1/64",
				Owner: api.NetAPIVersion + "/VirtualAddress/old-v6",
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ArtifactCleanupEligible(tt.artifact); got != tt.want {
				t.Fatalf("ArtifactCleanupEligible() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestArtifactTeardownRegistryCleanupPriority(t *testing.T) {
	artifacts := []resource.Artifact{
		{Kind: "linux.ipv4.routeTable"},
		{Kind: "linux.ipv4.fwmarkRule"},
		{Kind: "directory"},
		{Kind: "systemd.service"},
		{Kind: "openrc.service"},
		{Kind: "unix.socket"},
		{Kind: "file"},
	}
	got := make([]int, 0, len(artifacts))
	for _, artifact := range artifacts {
		got = append(got, ArtifactCleanupPriority(artifact))
	}
	want := []int{5, 0, 40, 10, 10, 30, 20}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("priorities = %#v, want %#v", got, want)
	}
}
