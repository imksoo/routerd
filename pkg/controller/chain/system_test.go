// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestNetworkAdoptionControllerWritesNetworkdAndResolvedDropins(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "network", "10-netplan-ens18.network.d", "50-routerd-no-dhcpv6.conf")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("[IPv6AcceptRA]\nDHCPv6Client=no\n"), 0644); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NetworkAdoption"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.NetworkAdoptionSpec{
			Interface: "wan",
			SystemdNetworkd: api.NetworkAdoptionNetworkdSpec{
				DisableDHCPv4: true,
				DisableDHCPv6: true,
			},
			SystemdResolved: api.NetworkAdoptionResolvedSpec{
				DisableDNSStubListener: true,
				DNSServers:             []string{"127.0.0.1"},
				FallbackDNSServers:     []string{"1.1.1.1"},
			},
		}},
	}}}
	store := mapStore{}
	var commands []string
	controller := NetworkAdoptionController{
		Router:             router,
		Store:              store,
		NetworkdDropinBase: filepath.Join(dir, "network"),
		ResolvedDropinDir:  filepath.Join(dir, "resolved.conf.d"),
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	networkdPath := filepath.Join(dir, "network", "10-netplan-ens18.network.d", "90-routerd-adoption.conf")
	data, err := os.ReadFile(networkdPath)
	if err != nil {
		t.Fatalf("read networkd drop-in: %v", err)
	}
	if !strings.Contains(string(data), "DHCP=no") {
		t.Fatalf("networkd drop-in = %s", data)
	}
	resolvedPath := filepath.Join(dir, "resolved.conf.d", "90-routerd-adoption.conf")
	resolved, err := os.ReadFile(resolvedPath)
	if err != nil {
		t.Fatalf("read resolved drop-in: %v", err)
	}
	if !strings.Contains(string(resolved), "DNSStubListener=no") {
		t.Fatalf("resolved drop-in = %s", resolved)
	}
	if !strings.Contains(string(resolved), "DNS=127.0.0.1") || !strings.Contains(string(resolved), "FallbackDNS=1.1.1.1") {
		t.Fatalf("resolved drop-in DNS settings missing: %s", resolved)
	}
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy networkd drop-in still exists: %v", err)
	}
	got := strings.Join(commands, "\n")
	for _, want := range []string{"networkctl reload", "networkctl reconfigure ens18", "systemctl restart systemd-resolved.service"} {
		if !strings.Contains(got, want) {
			t.Fatalf("commands missing %q:\n%s", want, got)
		}
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "NetworkAdoption", "wan")
	if status["phase"] != "Applied" || status["changed"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func TestNetworkAdoptionControllerCanKeepDHCPv4ClientWithoutRoutes(t *testing.T) {
	disabled := false
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NetworkAdoption"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.NetworkAdoptionSpec{
			Interface: "wan",
			SystemdNetworkd: api.NetworkAdoptionNetworkdSpec{
				DisableDHCPv6:     true,
				DHCPv4UseRoutes:   &disabled,
				DHCPv4UseDNS:      &disabled,
				DHCPv4RouteMetric: 900,
			},
		}},
	}}}
	controller := NetworkAdoptionController{
		Router:             router,
		Store:              mapStore{},
		NetworkdDropinBase: filepath.Join(dir, "network"),
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "network", "10-netplan-ens18.network.d", "90-routerd-adoption.conf"))
	if err != nil {
		t.Fatalf("read networkd drop-in: %v", err)
	}
	got := string(data)
	for _, want := range []string{"DHCP=ipv4", "[DHCPv4]", "UseRoutes=no", "UseDNS=no", "RouteMetric=900"} {
		if !strings.Contains(got, want) {
			t.Fatalf("drop-in missing %q:\n%s", want, got)
		}
	}
}

func TestNetworkAdoptionControllerTreatsNixOSAsDeclarative(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NetworkAdoption"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.NetworkAdoptionSpec{
			Interface:       "wan",
			SystemdNetworkd: api.NetworkAdoptionNetworkdSpec{DisableDHCPv4: true, DisableDHCPv6: true},
			SystemdResolved: api.NetworkAdoptionResolvedSpec{DisableDNSStubListener: true},
		}},
	}}}
	store := mapStore{}
	controller := NetworkAdoptionController{
		Router: router,
		Store:  store,
		OSName: "nixos",
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			t.Fatalf("NixOS network adoption must be rendered declaratively, got command %s %v", name, args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "NetworkAdoption", "wan")
	if status["phase"] != "Applied" || status["reason"] != "NixOSDeclarativeNetworkConfig" || status["ifname"] != "ens18" {
		t.Fatalf("status = %#v", status)
	}
}

func TestSystemdUnitControllerRendersAndEnablesUnit(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "SystemdUnit"}, Metadata: api.ObjectMeta{Name: "routerd.service"}, Spec: api.SystemdUnitSpec{
			Description:             "routerd test",
			ExecStart:               []string{"/usr/local/sbin/routerd", "serve", "--config", "/usr/local/etc/routerd/router.yaml"},
			RuntimeDirectory:        []string{"routerd", "routerd/healthcheck"},
			StateDirectory:          []string{"routerd"},
			ReadWritePaths:          []string{"/run/routerd", "/var/lib/routerd", "/etc/sysctl.d"},
			AmbientCapabilities:     []string{"CAP_NET_ADMIN"},
			RestrictAddressFamilies: []string{"AF_UNIX", "AF_INET", "AF_INET6", "AF_NETLINK"},
		}},
	}}}
	store := mapStore{}
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Store:            store,
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(dir, "routerd.service")
	data, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	gotUnit := string(data)
	for _, want := range []string{"RuntimeDirectory=routerd routerd/healthcheck", "StateDirectory=routerd", "ReadWritePaths=/run/routerd /var/lib/routerd /etc/sysctl.d", "AmbientCapabilities=CAP_NET_ADMIN", "RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK", "ProtectSystem=no", "NoNewPrivileges=yes"} {
		if !strings.Contains(gotUnit, want) {
			t.Fatalf("unit missing %q:\n%s", want, gotUnit)
		}
	}
	gotCommands := strings.Join(commands, "\n")
	for _, want := range []string{"systemctl daemon-reload", "systemctl enable routerd.service", "systemctl restart routerd.service"} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("commands missing %q:\n%s", want, gotCommands)
		}
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "SystemdUnit", "routerd.service")
	if status["phase"] != "Applied" || status["changed"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func TestSystemdUnitControllerDoesNotRestartUnchangedActiveUnit(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "SystemdUnit"}, Metadata: api.ObjectMeta{Name: "routerd-firewall-logger.service"}, Spec: api.SystemdUnitSpec{
			Description: "routerd firewall logger",
			ExecStart:   []string{"/usr/local/sbin/routerd-firewall-logger", "daemon", "--path", "/var/lib/routerd/firewall-logs.db", "--nflog-group", "1"},
			Enabled:     systemBoolPtr(true),
			Started:     systemBoolPtr(true),
		}},
	}}}
	store := mapStore{}
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Store:            store,
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	commands = nil
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	gotCommands := strings.Join(commands, "\n")
	if strings.Contains(gotCommands, "restart routerd-firewall-logger.service") {
		t.Fatalf("unchanged active unit was restarted:\n%s", gotCommands)
	}
	if !strings.Contains(gotCommands, "systemctl is-active --quiet routerd-firewall-logger.service") {
		t.Fatalf("active check missing:\n%s", gotCommands)
	}
}

func TestSystemdUnitControllerSynthesizesTailscaleUnits(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TailscaleNode"}, Metadata: api.ObjectMeta{Name: "home"}, Spec: api.TailscaleNodeSpec{
			Hostname:          "homert02",
			AdvertiseExitNode: true,
			AdvertiseRoutes:   []string{"172.18.0.0/16"},
		}},
	}}}
	store := mapStore{}
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Store:            store,
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if strings.HasSuffix(name, "tailscale") && strings.Join(args, " ") == "status --json" {
				return []byte(`{
				  "BackendState": "Running",
				  "CurrentTailnet": {"Name": "example@example.com", "MagicDNSSuffix": "example.ts.net", "MagicDNSEnabled": true},
				  "Self": {"DNSName": "homert02.example.ts.net.", "TailscaleIPs": ["100.64.87.102"], "AllowedIPs": ["100.64.87.102/32"], "Online": true, "ExitNodeOption": true},
				  "Peer": {"node-a": {}}
				}`), nil
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(dir, "routerd-tailscale-home.service")
	data, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read Tailscale unit: %v", err)
	}
	gotUnit := string(data)
	for _, want := range []string{"Type=oneshot", "/usr/bin/tailscale up", "--advertise-exit-node", "--advertise-routes=172.18.0.0/16", "RemainAfterExit=yes"} {
		if !strings.Contains(gotUnit, want) {
			t.Fatalf("unit missing %q:\n%s", want, gotUnit)
		}
	}
	gotCommands := strings.Join(commands, "\n")
	if !strings.Contains(gotCommands, "systemctl restart routerd-tailscale-home.service") {
		t.Fatalf("commands missing Tailscale restart:\n%s", gotCommands)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "TailscaleNode", "home")
	if status["phase"] != "Running" || status["advertiseExitNode"] != true || status["tailnetName"] != "example@example.com" || status["peerCount"] != 1 {
		t.Fatalf("status = %#v", status)
	}
}

func systemBoolPtr(v bool) *bool {
	return &v
}

func TestSystemdUnitControllerSynthesizesHealthCheckDaemonUnits(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ds-lite-a"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "internet-via-dslite-a"}, Spec: api.HealthCheckSpec{
			Daemon:             "routerd-healthcheck",
			SocketSource:       "/run/routerd/healthcheck/internet-via-dslite-a.sock",
			Target:             "1.1.1.1",
			TargetSource:       "static",
			SourceInterface:    "ds-lite-a",
			SourceAddressFrom:  api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/lan-base", Field: "address"},
			Protocol:           "tcp",
			Port:               443,
			Interval:           "30s",
			Timeout:            "3s",
			HealthyThreshold:   1,
			UnhealthyThreshold: 3,
		}},
	}}}
	store := mapStore{api.NetAPIVersion + "/IPv4StaticAddress/lan-base": {"address": "172.18.0.1/16"}}
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Store:            store,
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			line := strings.Join(append([]string{name}, args...), " ")
			commands = append(commands, line)
			if line == "systemctl is-active --quiet routerd-healthcheck@internet-via-dslite-a.service" {
				return nil, errors.New("inactive")
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	unitName := "routerd-healthcheck@internet-via-dslite-a.service"
	data, err := os.ReadFile(filepath.Join(dir, unitName))
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	unit := string(data)
	for _, want := range []string{
		`ExecStart=/usr/local/sbin/routerd-healthcheck --resource "internet-via-dslite-a" --target "1.1.1.1" --protocol "tcp" --source-interface "ds-lite-a" --source-address "172.18.0.1" --port 443`,
		`--socket "/run/routerd/healthcheck/internet-via-dslite-a.sock"`,
		"RuntimeDirectory=routerd/healthcheck",
		"RuntimeDirectoryPreserve=yes",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
	gotCommands := strings.Join(commands, "\n")
	for _, want := range []string{
		"systemctl daemon-reload",
		"systemctl enable routerd-healthcheck@internet-via-dslite-a.service",
		"systemctl is-active --quiet routerd-healthcheck@internet-via-dslite-a.service",
		"systemctl restart routerd-healthcheck@internet-via-dslite-a.service",
	} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("commands missing %q:\n%s", want, gotCommands)
		}
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "SystemdUnit", unitName)
	if status["phase"] != "Applied" || status["changed"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func TestSystemdUnitControllerSynthesizesDHCPClientUnits(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Lease"}, Metadata: api.ObjectMeta{Name: "wan-v4"}, Spec: api.DHCPv4LeaseSpec{Interface: "wan", Hostname: "routerd-test"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan", IAID: "1"}},
	}}}
	store := mapStore{}
	var commands []string
	controller := SystemdUnitController{
		Router:                      router,
		Store:                       store,
		SystemdSystemDir:            dir,
		SynthesizeClientDaemonUnits: true,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		unit string
		want []string
	}{
		{
			unit: "routerd-dhcpv4-client@wan-v4.service",
			want: []string{
				`ExecStart=/usr/local/sbin/routerd-dhcpv4-client daemon --resource wan-v4 --interface ens18 --hostname routerd-test`,
				`RuntimeDirectory=routerd/dhcpv4-client`,
				`AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN CAP_NET_BIND_SERVICE`,
				`RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK AF_PACKET`,
			},
		},
		{
			unit: "routerd-dhcpv6-client@wan-pd.service",
			want: []string{
				`ExecStart=/usr/local/sbin/routerd-dhcpv6-client daemon --resource wan-pd --interface ens18 --iaid 1`,
				`RuntimeDirectory=routerd/dhcpv6-client`,
				`AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN CAP_NET_BIND_SERVICE`,
			},
		},
	} {
		data, err := os.ReadFile(filepath.Join(dir, tc.unit))
		if err != nil {
			t.Fatalf("read %s: %v", tc.unit, err)
		}
		unit := string(data)
		for _, want := range tc.want {
			if !strings.Contains(unit, want) {
				t.Fatalf("%s missing %q:\n%s", tc.unit, want, unit)
			}
		}
	}
	gotCommands := strings.Join(commands, "\n")
	for _, want := range []string{
		"systemctl unmask routerd-dhcpv4-client@wan-v4.service",
		"systemctl enable routerd-dhcpv4-client@wan-v4.service",
		"systemctl restart routerd-dhcpv4-client@wan-v4.service",
		"systemctl unmask routerd-dhcpv6-client@wan-pd.service",
		"systemctl enable routerd-dhcpv6-client@wan-pd.service",
		"systemctl restart routerd-dhcpv6-client@wan-pd.service",
	} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("commands missing %q:\n%s", want, gotCommands)
		}
	}
	if status := store.ObjectStatus(api.SystemAPIVersion, "SystemdUnit", "routerd-dhcpv4-client@wan-v4.service"); status["phase"] != "Applied" {
		t.Fatalf("dhcpv4 unit status = %#v", status)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Lease", "wan-v4"); status["managedBy"] != "systemd" {
		t.Fatalf("dhcpv4 lease status = %#v", status)
	}
}

func TestSystemdUnitControllerDisablesHealthCheckDaemonUnit(t *testing.T) {
	dir := t.TempDir()
	enabled := false
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "internet-via-pppoe"}, Spec: api.HealthCheckSpec{
			Enabled:         &enabled,
			Daemon:          "routerd-healthcheck",
			Target:          "208.67.222.222",
			Protocol:        "tcp",
			SourceInterface: "ppp-flets",
			Port:            443,
		}},
	}}}
	store := mapStore{}
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Store:            store,
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	unitName := "routerd-healthcheck@internet-via-pppoe.service"
	if _, err := os.Stat(filepath.Join(dir, unitName)); err != nil {
		t.Fatalf("expected disabled healthcheck unit to remain renderable: %v", err)
	}
	gotCommands := strings.Join(commands, "\n")
	for _, want := range []string{
		"systemctl daemon-reload",
		"systemctl disable --now routerd-healthcheck@internet-via-pppoe.service",
	} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("commands missing %q:\n%s", want, gotCommands)
		}
	}
	unitStatus := store.ObjectStatus(api.SystemAPIVersion, "SystemdUnit", unitName)
	if unitStatus["phase"] != "Disabled" {
		t.Fatalf("unit status = %#v", unitStatus)
	}
	healthStatus := store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "internet-via-pppoe")
	if healthStatus["phase"] != "Disabled" {
		t.Fatalf("health status = %#v", healthStatus)
	}
}

func TestSystemdUnitControllerMarksDisabledPPPoEInterface(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface"}, Metadata: api.ObjectMeta{Name: "pppoe-flets"}, Spec: api.PPPoEInterfaceSpec{
			Interface: "wan",
			IfName:    "ppp-flets",
			Disabled:  true,
			Username:  "user",
		}},
	}}}
	store := mapStore{}
	controller := SystemdUnitController{Router: router, Store: store}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "PPPoEInterface", "pppoe-flets")
	if status["phase"] != PhaseDisabled {
		t.Fatalf("pppoe status = %#v", status)
	}
}
