// SPDX-License-Identifier: BSD-3-Clause

package servicemgr

import (
	"reflect"
	"testing"

	"routerd/pkg/platform"
	"routerd/pkg/resource"
)

func TestManagersNormalizeServiceArtifacts(t *testing.T) {
	service := Service{SystemdName: "routerd-dhcpv6-client@wan-pd.service"}
	tests := []struct {
		name    string
		manager Manager
		kind    string
		service string
		apply   string
	}{
		{name: "systemd", manager: Systemd{}, kind: "systemd.service", service: "routerd-dhcpv6-client@wan-pd.service", apply: "systemctl"},
		{name: "openrc", manager: OpenRC{}, kind: "openrc.service", service: "routerd_dhcpv6_client_wan_pd", apply: "rc-service"},
		{name: "rcd", manager: RCD{}, kind: "rc.d.service", service: "routerd_dhcpv6_client_wan_pd", apply: "service"},
		{name: "nixos", manager: NixOS{}, kind: "nixos.service", service: "routerd-dhcpv6-client@wan-pd", apply: "nixos-module"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := tt.manager.Intent("owner", service, resource.ActionEnsure, map[string]string{"purpose": "test"})
			if intent.Artifact.Kind != tt.kind || intent.Artifact.Name != tt.service || intent.ApplyWith != tt.apply {
				t.Fatalf("intent = %+v", intent)
			}
			if intent.Artifact.Attributes["purpose"] != "test" {
				t.Fatalf("attributes not preserved: %+v", intent.Artifact.Attributes)
			}
		})
	}
}

func TestForPlatformSelectsManager(t *testing.T) {
	tests := []struct {
		name     string
		features platform.Features
		want     string
	}{
		{name: "openrc", features: platform.Features{HasOpenRC: true}, want: "openrc"},
		{name: "rcd", features: platform.Features{HasRCD: true}, want: "rc.d"},
		{name: "systemd", features: platform.Features{HasSystemd: true}, want: "systemd"},
		{name: "nixosFallback", features: platform.Features{}, want: "nixos"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ForPlatform(tt.features).Name(); got != tt.want {
				t.Fatalf("manager = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestManagerCommands(t *testing.T) {
	service := Service{SystemdName: "frr.service", OpenRCName: "frr", RCDName: "frr"}
	tests := []struct {
		name    string
		manager Manager
		op      Operation
		want    Command
	}{
		{name: "systemdRestart", manager: Systemd{}, op: OperationRestart, want: Command{Name: "systemctl", Args: []string{"restart", "frr.service"}}},
		{name: "openrcEnable", manager: OpenRC{}, op: OperationEnable, want: Command{Name: "rc-update", Args: []string{"add", "frr", "default"}}},
		{name: "rcdReload", manager: RCD{}, op: OperationReload, want: Command{Name: "service", Args: []string{"frr", "reload"}}},
		{name: "nixosApply", manager: NixOS{}, op: OperationRestart, want: Command{Name: "nixos-rebuild", Args: []string{"switch"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.manager.Command(tt.op, service); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("command = %#v, want %#v", got, tt.want)
			}
		})
	}
}
