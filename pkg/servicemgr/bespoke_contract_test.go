// SPDX-License-Identifier: BSD-3-Clause

package servicemgr

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type bespokeLifecycleContract struct {
	name      string
	proves    string
	plan      Plan
	forbidden []Command
}

func TestBespokeLifecycleCommandGolden(t *testing.T) {
	got := renderBespokeLifecycleGolden(t, bespokeLifecycleContracts())
	path := filepath.Join("testdata", "bespoke_lifecycle_commands.golden")
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != strings.TrimSpace(string(want)) {
		t.Fatalf("bespoke lifecycle command fixture drifted\n--- want\n%s\n--- got\n%s", string(want), got)
	}
}

func TestBespokeLifecycleContractsAvoidForbiddenGenericFallbacks(t *testing.T) {
	for _, contract := range bespokeLifecycleContracts() {
		t.Run(contract.name, func(t *testing.T) {
			for _, forbidden := range contract.forbidden {
				for _, command := range contract.plan.Commands {
					if reflect.DeepEqual(command, forbidden) {
						t.Fatalf("contract %q collapsed into forbidden generic command %q", contract.name, commandLine(command))
					}
				}
			}
		})
	}
}

func TestBespokeLifecycleContractsCoverRequiredIntegrations(t *testing.T) {
	contracts := bespokeLifecycleContracts()
	required := []string{
		"keepalived-openrc-reload",
		"keepalived-openrc-restart",
		"dnsmasq-sighup-reload",
		"dhcp-client-renew-release-ipc",
		"ingress-nft-map-apply",
		"vrrp-track-script-artifacts",
		"dslite-tunnel-event-hook",
		"dhcp-event-daemon-ipc-order",
	}
	seen := map[string]bool{}
	for _, contract := range contracts {
		seen[contract.name] = true
	}
	for _, name := range required {
		if !seen[name] {
			t.Fatalf("missing bespoke lifecycle contract %q", name)
		}
	}
}

func TestBespokeLifecycleContractsCoverOSMatrix(t *testing.T) {
	for _, contract := range bespokeLifecycleContracts() {
		t.Run(contract.name, func(t *testing.T) {
			for _, manager := range lifecycleMatrixManagers() {
				plan := bespokeMatrixPlan(contract, manager)
				if len(plan.Commands) == 0 {
					t.Fatalf("%s/%s has no commands", contract.name, manager.Name())
				}
				for _, forbidden := range bespokeMatrixForbidden(contract, manager) {
					for _, command := range plan.Commands {
						if reflect.DeepEqual(command, forbidden) {
							t.Fatalf("%s/%s collapsed into forbidden generic command %q", contract.name, manager.Name(), commandLine(command))
						}
					}
				}
			}
		})
	}
}

func bespokeLifecycleContracts() []bespokeLifecycleContract {
	keepalived := Service{SystemdName: "keepalived.service", OpenRCName: "keepalived", RCDName: "keepalived"}
	dnsmasq := Service{SystemdName: "routerd-dnsmasq.service", OpenRCName: "routerd_dnsmasq", RCDName: "routerd_dnsmasq"}
	dhcp4 := Service{SystemdName: "routerd-dhcpv4-client@wan.service", OpenRCName: "routerd_dhcpv4_client_wan", RCDName: "routerd_dhcpv4_client_wan"}
	dhcp6 := Service{SystemdName: "routerd-dhcpv6-client@wan-pd.service", OpenRCName: "routerd_dhcpv6_client_wan_pd", RCDName: "routerd_dhcpv6_client_wan_pd"}

	return []bespokeLifecycleContract{
		{
			name:   "keepalived-openrc-reload",
			proves: "OpenRC keepalived config changes use signal reload instead of restart when the daemon is already running.",
			plan:   OpenRC{}.Plan(OperationReload, keepalived),
			forbidden: []Command{
				{Name: "rc-service", Args: []string{"keepalived", "restart"}},
			},
		},
		{
			name:   "keepalived-openrc-restart",
			proves: "OpenRC keepalived restart remains available as the fallback path and is not conflated with reload.",
			plan:   OpenRC{}.Plan(OperationRestart, keepalived),
			forbidden: []Command{
				{Name: "rc-service", Args: []string{"keepalived", "reload"}},
			},
		},
		{
			name:   "dnsmasq-sighup-reload",
			proves: "dnsmasq host and lease updates can be reloaded with SIGHUP through the pid file without a service restart.",
			plan:   Systemd{}.Plan(OperationReload, dnsmasq, PIDSignalHook(OperationReload, "HUP", "/run/routerd/dnsmasq.pid")),
			forbidden: []Command{
				{Name: "systemctl", Args: []string{"restart", "routerd-dnsmasq.service"}},
				{Name: "systemctl", Args: []string{"reload", "routerd-dnsmasq.service"}},
			},
		},
		{
			name:   "dhcp-client-renew-release-ipc",
			proves: "DHCPv4/v6 client renew and release operations stay on the Unix-socket daemon API instead of restarting client daemons.",
			plan: Plan{Operation: OperationReload, Commands: []Command{
				DaemonAPICommandHook(OperationReload, "/run/routerd/dhcpv4-client/wan.sock", "renew").Command,
				DaemonAPICommandHook(OperationReload, "/run/routerd/dhcpv6-client/wan-pd.sock", "renew").Command,
				DaemonAPICommandHook(OperationReload, "/run/routerd/dhcpv6-client/wan-pd.sock", "release").Command,
			}},
			forbidden: []Command{
				Systemd{}.Command(OperationRestart, dhcp4),
				Systemd{}.Command(OperationRestart, dhcp6),
			},
		},
		{
			name:   "ingress-nft-map-apply",
			proves: "IngressService backend rotation updates nftables dataplane state only and must not restart any daemon.",
			plan: Plan{Operation: OperationReload, Commands: []Command{
				{Name: "nft", Args: []string{"-c", "-f", "/run/routerd/nat44.nft"}},
				{Name: "nft", Args: []string{"-f", "/run/routerd/nat44.nft"}},
			}},
			forbidden: []Command{
				{Name: "systemctl", Args: []string{"restart", "routerd.service"}},
				{Name: "systemctl", Args: []string{"restart", "nftables.service"}},
			},
		},
		{
			name:   "vrrp-track-script-artifacts",
			proves: "VRRP track scripts remain explicit artifacts with executable permissions for keepalived callbacks.",
			plan: Plan{Operation: OperationEnable, Commands: []Command{
				{Name: "artifact-write", Args: []string{"/usr/local/libexec/routerd/keepalived-track.d", "mode=0755"}},
				OpenRC{}.Command(OperationReload, keepalived),
			}},
			forbidden: []Command{
				OpenRC{}.Command(OperationRestart, keepalived),
			},
		},
		{
			name:   "dslite-tunnel-event-hook",
			proves: "DS-Lite tunnel apply remains a dataplane hook with ipip6 tunnel updates and route event publication, not a service restart.",
			plan: Plan{Operation: OperationReload, Commands: []Command{
				{Name: "modprobe", Args: []string{"ip6_tunnel"}},
				{Name: "ip", Args: []string{"-6", "tunnel", "replace", "ds-lite", "mode", "ipip6"}},
				{Name: "bus", Args: []string{"publish", "routerd.tunnel.ds-lite.up"}},
			}},
		},
		{
			name:   "dhcp-event-daemon-ipc-order",
			proves: "DHCP event relay and fingerprint watcher keep Unix-socket IPC and daemon status warmup ordering before controller bootstrap.",
			plan: Plan{Operation: OperationEnable, Commands: []Command{
				{Name: "daemon-status", Args: []string{"GET", "unix:///run/routerd/dhcpv4-client/wan.sock", "/v1/status"}},
				{Name: "daemon-status", Args: []string{"GET", "unix:///run/routerd/dhcpv6-client/wan-pd.sock", "/v1/status"}},
				{Name: "bus", Args: []string{"publish", "routerd.controller.bootstrap"}},
			}},
		},
	}
}

func renderBespokeLifecycleGolden(t *testing.T, contracts []bespokeLifecycleContract) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("# Generated by TestBespokeLifecycleCommandGolden. Review every change against bespoke daemon integration behavior.\n")
	for _, contract := range contracts {
		b.WriteString("\n[")
		b.WriteString(contract.name)
		b.WriteString("]\n")
		b.WriteString("proves: ")
		b.WriteString(contract.proves)
		b.WriteString("\n")
		b.WriteString("commands:\n")
		for _, command := range contract.plan.Commands {
			b.WriteString("  ")
			b.WriteString(commandLine(command))
			b.WriteString("\n")
		}
		if len(contract.forbidden) > 0 {
			b.WriteString("forbidden:\n")
			for _, command := range contract.forbidden {
				b.WriteString("  ")
				b.WriteString(commandLine(command))
				b.WriteString("\n")
			}
		}
		b.WriteString("os-matrix:\n")
		for _, manager := range lifecycleMatrixManagers() {
			b.WriteString("  ")
			b.WriteString(manager.Name())
			b.WriteString(":\n")
			for _, command := range bespokeMatrixPlan(contract, manager).Commands {
				b.WriteString("    ")
				b.WriteString(commandLine(command))
				b.WriteString("\n")
			}
			if forbidden := bespokeMatrixForbidden(contract, manager); len(forbidden) > 0 {
				b.WriteString("    forbidden:\n")
				for _, command := range forbidden {
					b.WriteString("      ")
					b.WriteString(commandLine(command))
					b.WriteString("\n")
				}
			}
		}
	}
	return b.String()
}

func lifecycleMatrixManagers() []Manager {
	return []Manager{Systemd{}, OpenRC{}, RCD{}, NixOS{}}
}

func bespokeMatrixPlan(contract bespokeLifecycleContract, manager Manager) Plan {
	keepalived := Service{SystemdName: "keepalived.service", OpenRCName: "keepalived", RCDName: "keepalived"}
	dnsmasq := Service{SystemdName: "routerd-dnsmasq.service", OpenRCName: "routerd_dnsmasq", RCDName: "routerd_dnsmasq", NixName: "routerd-dnsmasq"}
	switch contract.name {
	case "keepalived-openrc-reload":
		return manager.Plan(OperationReload, keepalived)
	case "keepalived-openrc-restart":
		return manager.Plan(OperationRestart, keepalived)
	case "dnsmasq-sighup-reload":
		return manager.Plan(OperationReload, dnsmasq, PIDSignalHook(OperationReload, "HUP", "/run/routerd/dnsmasq.pid"))
	case "vrrp-track-script-artifacts":
		return Plan{Operation: OperationEnable, Commands: []Command{
			{Name: "artifact-write", Args: []string{"/usr/local/libexec/routerd/keepalived-track.d", "mode=0755"}},
			manager.Command(OperationReload, keepalived),
		}}
	default:
		return contract.plan
	}
}

func bespokeMatrixForbidden(contract bespokeLifecycleContract, manager Manager) []Command {
	keepalived := Service{SystemdName: "keepalived.service", OpenRCName: "keepalived", RCDName: "keepalived"}
	dnsmasq := Service{SystemdName: "routerd-dnsmasq.service", OpenRCName: "routerd_dnsmasq", RCDName: "routerd_dnsmasq", NixName: "routerd-dnsmasq"}
	switch contract.name {
	case "keepalived-openrc-reload", "vrrp-track-script-artifacts":
		if manager.Name() == "nixos" {
			return nil
		}
		return []Command{manager.Command(OperationRestart, keepalived)}
	case "keepalived-openrc-restart":
		if manager.Name() == "nixos" {
			return nil
		}
		return []Command{manager.Command(OperationReload, keepalived)}
	case "dnsmasq-sighup-reload":
		if manager.Name() == "nixos" {
			return nil
		}
		return []Command{manager.Command(OperationRestart, dnsmasq), manager.Command(OperationReload, dnsmasq)}
	default:
		return contract.forbidden
	}
}

func commandLine(command Command) string {
	parts := append([]string{command.Name}, command.Args...)
	return strings.Join(parts, " ")
}
