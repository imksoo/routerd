// SPDX-License-Identifier: BSD-3-Clause

package servicemgr

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resource"
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
		{name: "rcd", manager: RCD{}, kind: "rc.d.service", service: "routerd_dhcpv6_client_wan_pd", apply: "service"},
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
		{name: "rcd", features: platform.Features{HasRCD: true}, want: "rc.d"},
		{name: "systemd", features: platform.Features{HasSystemd: true}, want: "systemd"},
		{name: "linuxFallback", features: platform.Features{}, want: "systemd"},
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
	service := Service{SystemdName: "routerd-dnsmasq.service", RCDName: "routerd_dnsmasq"}
	tests := []struct {
		name    string
		manager Manager
		op      Operation
		want    Command
	}{
		{name: "systemdRestart", manager: Systemd{}, op: OperationRestart, want: Command{Name: "systemctl", Args: []string{"restart", "routerd-dnsmasq.service"}}},
		{name: "rcdReload", manager: RCD{}, op: OperationReload, want: Command{Name: "service", Args: []string{"routerd_dnsmasq", "reload"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.manager.Command(tt.op, service); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("command = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestManagerPlanAllowsSignalBasedDaemonReload(t *testing.T) {
	service := Service{SystemdName: "routerd-dnsmasq.service"}
	plan := Systemd{}.Plan(OperationReload, service, Hook{
		Operation:      OperationReload,
		ReplaceDefault: true,
		Command:        Command{Name: "kill", Args: []string{"-HUP", "$(cat /run/routerd/dnsmasq.pid)"}},
	})
	want := []Command{{Name: "kill", Args: []string{"-HUP", "$(cat /run/routerd/dnsmasq.pid)"}}}
	if !reflect.DeepEqual(plan.Commands, want) {
		t.Fatalf("dnsmasq signal reload plan = %#v, want %#v", plan.Commands, want)
	}
}

func TestCrossOSDNSMasqServiceSemanticEquivalence(t *testing.T) {
	service := Service{
		SystemdName: "routerd-dnsmasq.service",
		RCDName:     "routerd_dnsmasq",
	}
	managers := []Manager{Systemd{}, RCD{}}
	for _, manager := range managers {
		t.Run(manager.Name(), func(t *testing.T) {
			if err := ValidateService(manager, service); err != nil {
				t.Fatalf("validate dnsmasq service: %v", err)
			}
			enable := manager.Command(OperationEnable, service)
			reload := manager.Plan(OperationReload, service, PIDSignalHook(OperationReload, "HUP", "/run/routerd/dnsmasq.pid"))
			if enable.Name == "" || len(enable.Args) == 0 {
				t.Fatalf("%s enable command is empty: %#v", manager.Name(), enable)
			}
			if got := reload.Commands; len(got) != 1 || got[0].Name != "sh" || !strings.Contains(strings.Join(got[0].Args, " "), "kill -HUP") {
				t.Fatalf("%s dnsmasq reload must remain pid-file SIGHUP, got %#v", manager.Name(), got)
			}
		})
	}
}

func TestValidateServiceRejectsInvalidNames(t *testing.T) {
	tests := []struct {
		name    string
		manager Manager
		service Service
	}{
		{name: "empty", manager: Systemd{}, service: Service{}},
		{name: "nul", manager: RCD{}, service: Service{RCDName: "bad\x00name"}},
		{name: "tooLong", manager: Systemd{}, service: Service{SystemdName: strings.Repeat("a", 65)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateService(tt.manager, tt.service); err == nil {
				t.Fatalf("ValidateService(%s, %#v) succeeded, want error", tt.manager.Name(), tt.service)
			}
		})
	}
}

func TestServiceNameEdgeCasesAcrossManagers(t *testing.T) {
	tests := []Service{
		{SystemdName: "routerd-test.service", RCDName: "routerd_test"},
		{SystemdName: "routerd.special-name@lan.service", RCDName: "routerd_special_name_lan"},
		{SystemdName: strings.Repeat("a", 56) + ".service", RCDName: strings.Repeat("c", 64)},
	}
	for _, service := range tests {
		for _, manager := range []Manager{Systemd{}, RCD{}} {
			t.Run(manager.Name()+"/"+manager.ServiceName(service), func(t *testing.T) {
				if err := ValidateService(manager, service); err != nil {
					t.Fatalf("valid edge service rejected: %v", err)
				}
			})
		}
	}
	unicodeService := Service{SystemdName: "routerd-測試.service", RCDName: "routerd_測試"}
	for _, manager := range []Manager{Systemd{}, RCD{}} {
		if err := ValidateService(manager, unicodeService); err != nil {
			t.Fatalf("%s rejected valid unicode service name: %v", manager.Name(), err)
		}
	}
}

func TestSystemdUnitSemanticComparisonIgnoresEnvironmentOrder(t *testing.T) {
	specA := api.SystemdUnitSpec{ExecStart: []string{"/usr/local/sbin/routerd", "serve"}, Environment: []string{"B=2", "A=1"}}
	specB := api.SystemdUnitSpec{ExecStart: []string{"/usr/local/sbin/routerd", "serve"}, Environment: []string{"A=1", "B=2"}}
	a := parseSystemdSemantics(string(render.SystemdUnit("routerd.service", specA)))
	b := parseSystemdSemantics(string(render.SystemdUnit("routerd.service", specB)))
	if a.ExecStart != b.ExecStart {
		t.Fatalf("ExecStart drifted: %q != %q", a.ExecStart, b.ExecStart)
	}
	if !reflect.DeepEqual(a.Environment, b.Environment) {
		t.Fatalf("Environment semantic comparison should ignore order: %#v != %#v", a.Environment, b.Environment)
	}
}

type systemdSemantics struct {
	ExecStart   string
	Environment []string
}

func parseSystemdSemantics(unit string) systemdSemantics {
	var out systemdSemantics
	for _, line := range strings.Split(unit, "\n") {
		switch {
		case strings.HasPrefix(line, "ExecStart="):
			out.ExecStart = strings.TrimPrefix(line, "ExecStart=")
		case strings.HasPrefix(line, "Environment="):
			out.Environment = append(out.Environment, strings.TrimPrefix(line, "Environment="))
		}
	}
	sort.Strings(out.Environment)
	return out
}
