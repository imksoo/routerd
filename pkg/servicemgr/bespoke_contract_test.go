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
		"frr-live-reload",
		"keepalived-openrc-reload",
		"keepalived-openrc-restart",
		"dnsmasq-sighup-reload",
		"dhcp-client-renew-release-ipc",
		"bfd-daemon-enable",
		"ingress-nft-map-apply",
		"vrrp-track-script-artifacts",
		"dslite-tunnel-event-hook",
		"dhcp-event-daemon-ipc-order",
		"frr-graceful-restart-drain",
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

func bespokeLifecycleContracts() []bespokeLifecycleContract {
	frr := Service{SystemdName: "frr.service", OpenRCName: "frr", RCDName: "frr"}
	keepalived := Service{SystemdName: "keepalived.service", OpenRCName: "keepalived", RCDName: "keepalived"}
	dnsmasq := Service{SystemdName: "routerd-dnsmasq.service", OpenRCName: "routerd_dnsmasq", RCDName: "routerd_dnsmasq"}
	dhcp4 := Service{SystemdName: "routerd-dhcpv4-client@wan.service", OpenRCName: "routerd_dhcpv4_client_wan", RCDName: "routerd_dhcpv4_client_wan"}
	dhcp6 := Service{SystemdName: "routerd-dhcpv6-client@wan-pd.service", OpenRCName: "routerd_dhcpv6_client_wan_pd", RCDName: "routerd_dhcpv6_client_wan_pd"}

	frrReloadHooks := FRRLiveReloadHooks("/run/routerd/frr/routerd.conf", "vtysh", "frr-reload.py")
	return []bespokeLifecycleContract{
		{
			name:   "frr-live-reload",
			proves: "FRR config changes run vtysh syntax check before frr-reload.py and never restart bgpd for config-only reloads.",
			plan:   Systemd{}.Plan(OperationReload, frr, frrReloadHooks...),
			forbidden: []Command{
				{Name: "systemctl", Args: []string{"restart", "frr.service"}},
			},
		},
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
			name:   "bfd-daemon-enable",
			proves: "BGP BFD enables bgpd and bfdd in /etc/frr/daemons before the service restart needed for daemon-set changes.",
			plan: Plan{Operation: OperationRestart, Commands: []Command{
				{Name: "artifact-write", Args: []string{"/etc/frr/daemons", "zebra=yes", "bgpd=yes", "bfdd=yes"}},
				Systemd{}.Command(OperationRestart, frr),
			}},
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
		{
			name:   "frr-graceful-restart-drain",
			proves: "FRR graceful-restart keeps config-only reloads in place and observes peers after negotiation-sensitive reloads.",
			plan: Plan{Operation: OperationReload, Commands: append(
				append([]Command{}, Systemd{}.Plan(OperationReload, frr, frrReloadHooks...).Commands...),
				Command{Name: "vtysh", Args: []string{"-c", "show bgp summary json"}},
				Command{Name: "status", Args: []string{"wait", "bgp graceful-restart negotiation"}},
			)},
			forbidden: []Command{
				{Name: "systemctl", Args: []string{"restart", "frr.service"}},
			},
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
	}
	return b.String()
}

func commandLine(command Command) string {
	parts := append([]string{command.Name}, command.Args...)
	return strings.Join(parts, " ")
}
