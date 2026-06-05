// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
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
			if name == "systemctl" && len(args) >= 2 && (args[0] == "is-active" || args[0] == "is-enabled") {
				return nil, errors.New("inactive")
			}
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
	gotNetworkd := string(data)
	for _, want := range []string{"DHCP=no", "IPv6AcceptRA=yes", "[IPv6AcceptRA]", "DHCPv6Client=no"} {
		if !strings.Contains(gotNetworkd, want) {
			t.Fatalf("networkd drop-in missing %q:\n%s", want, gotNetworkd)
		}
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
	for _, want := range []string{"DHCP=ipv4", "IPv6AcceptRA=yes", "[IPv6AcceptRA]", "DHCPv6Client=no", "[DHCPv4]", "UseRoutes=no", "UseDNS=no", "RouteMetric=900"} {
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
	router := &api.Router{Metadata: api.ObjectMeta{Name: "home"}, Spec: api.RouterSpec{}}
	store := mapStore{}
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Store:            store,
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "systemctl" && len(args) >= 2 && (args[0] == "is-active" || args[0] == "is-enabled") {
				return nil, errors.New("inactive")
			}
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
	for _, want := range []string{"ExecStartPre=/usr/local/sbin/routerd check", "ExecStart=/usr/local/sbin/routerd serve", "RuntimeDirectory=routerd routerd/bgp routerd/dhcpv6-client routerd/dhcpv4-client routerd/pppoe-client routerd/dns-resolver", "StateDirectory=routerd", "AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE CAP_SETUID CAP_SETGID CAP_CHOWN", "RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK", "NoNewPrivileges=no"} {
		if !strings.Contains(gotUnit, want) {
			t.Fatalf("unit missing %q:\n%s", want, gotUnit)
		}
	}
	for _, notWant := range []string{"ReadWritePaths=", "ProtectSystem="} {
		if strings.Contains(gotUnit, notWant) {
			t.Fatalf("routerd.service must not contain %q:\n%s", notWant, gotUnit)
		}
	}
	if strings.Contains(gotUnit, "controller"+"-chain") {
		t.Fatalf("routerd.service must not expose legacy controller flags:\n%s", gotUnit)
	}
	gotCommands := strings.Join(commands, "\n")
	for _, want := range []string{"systemctl daemon-reload", "systemctl enable routerd.service"} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("commands missing %q:\n%s", want, gotCommands)
		}
	}
	if commandLineContains(commands, "systemctl restart routerd.service") {
		t.Fatalf("routerd.service must not be directly restarted by its own controller:\n%s", gotCommands)
	}
	if !strings.Contains(gotCommands, "systemd-run --unit routerd-self-restart-") ||
		!strings.Contains(gotCommands, "--on-active=10s --collect systemctl restart routerd.service") {
		t.Fatalf("routerd.service restart was not scheduled through systemd-run:\n%s", gotCommands)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "ServiceUnit", "routerd.service")
	if status["phase"] != "Applied" || status["changed"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func TestSystemdUnitControllerAugmentsRouterdServiceForBGPVRRPIngress(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
			ASN:      64512,
			RouterID: "192.0.2.1",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "api-vip"}, Spec: api.VirtualAddressSpec{Family: "ipv4",
			Interface: "lan",
			Address:   "192.0.2.250/32",
			Mode:      "vrrp",
			VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 66},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "ssh"}, Spec: api.IngressServiceSpec{
			Listen:   api.IngressListenSpec{Address: "192.0.2.250", Port: 22, Protocol: "tcp"},
			Backends: []api.IngressBackendSpec{{Address: "192.0.2.10", Port: 22}},
		}},
	}}}
	controller := SystemdUnitController{
		Router:           router,
		Store:            mapStore{},
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			if name == "systemctl" && len(args) >= 2 && (args[0] == "is-active" || args[0] == "is-enabled") {
				return nil, errors.New("inactive")
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "routerd.service"))
	if err != nil {
		t.Fatal(err)
	}
	gotUnit := string(data)
	for _, want := range []string{
		"AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE CAP_SETUID CAP_SETGID CAP_CHOWN CAP_DAC_OVERRIDE",
		"CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE CAP_SETUID CAP_SETGID CAP_CHOWN CAP_DAC_OVERRIDE",
	} {
		if !strings.Contains(gotUnit, want) {
			t.Fatalf("unit missing %q:\n%s", want, gotUnit)
		}
	}
	for _, notWant := range []string{"SupplementaryGroups=frr frrvty", "/run/frr", "/etc/frr", "/etc/keepalived", "ReadWritePaths=", "ProtectSystem="} {
		if strings.Contains(gotUnit, notWant) {
			t.Fatalf("unit should not contain %q:\n%s", notWant, gotUnit)
		}
	}
	bgpUnit, err := os.ReadFile(filepath.Join(dir, "routerd-bgp.service"))
	if err != nil {
		t.Fatalf("read routerd-bgp unit: %v", err)
	}
	gotBGPUnit := string(bgpUnit)
	for _, want := range []string{
		"ExecStart=/usr/local/sbin/routerd-bgp daemon --socket /run/routerd/bgp/gobgp.sock --control-socket /run/routerd/bgp/control.sock --state-file /var/lib/routerd/bgp/applied.json",
		"RuntimeDirectory=routerd/bgp",
		"StateDirectory=routerd/bgp",
		"Restart=always",
		"Wants=network-online.target",
		"After=network-online.target",
	} {
		if !strings.Contains(gotBGPUnit, want) {
			t.Fatalf("routerd-bgp unit missing %q:\n%s", want, gotBGPUnit)
		}
	}
	for _, notWant := range []string{"PartOf=", "BindsTo=", "routerd.service"} {
		if strings.Contains(gotBGPUnit, notWant) {
			t.Fatalf("routerd-bgp unit must be independent from routerd.service, found %q:\n%s", notWant, gotBGPUnit)
		}
	}
}

func TestSystemdUnitControllerDoesNotRestartActiveBGPDaemonOnUnitChange(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Metadata: api.ObjectMeta{Name: "home"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
			ASN:      64512,
			RouterID: "192.0.2.1",
		}},
	}}}
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Store:            mapStore{},
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			line := strings.Join(append([]string{name}, args...), " ")
			commands = append(commands, line)
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	gotCommands := strings.Join(commands, "\n")
	if strings.Contains(gotCommands, "systemctl restart routerd-bgp.service") {
		t.Fatalf("routerd-bgp.service must not be restarted by reconcile:\n%s", gotCommands)
	}
	if !strings.Contains(gotCommands, "systemctl daemon-reload") || !strings.Contains(gotCommands, "systemctl enable routerd-bgp.service") {
		t.Fatalf("routerd-bgp unit was not rendered/enabled:\n%s", gotCommands)
	}
}

func TestSystemdUnitControllerDoesNotRestartUnchangedActiveUnit(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallEventLog"}, Metadata: api.ObjectMeta{Name: "default"}, Spec: api.FirewallLogSpec{
			Enabled: true,
			Path:    "/var/lib/routerd/firewall-logs.db",
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

func TestSystemdUnitControllerSynthesizesNDPIAgentForAutoClassifier(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallEventLog"}, Metadata: api.ObjectMeta{Name: "default"}, Spec: api.FirewallLogSpec{Enabled: true}},
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
			if name == "systemctl" && len(args) >= 2 && (args[0] == "is-active" || args[0] == "is-enabled") {
				return nil, errors.New("inactive")
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	agentData, err := os.ReadFile(filepath.Join(dir, "routerd-ndpi-agent.service"))
	if err != nil {
		t.Fatalf("read ndpi agent unit: %v", err)
	}
	if !strings.Contains(string(agentData), "ExecStart=/usr/local/sbin/routerd-ndpi-agent daemon --socket /run/routerd/ndpi-agent/default.sock") {
		t.Fatalf("ndpi agent unit =\n%s", string(agentData))
	}
	classifierData, err := os.ReadFile(filepath.Join(dir, "routerd-dpi-classifier.service"))
	if err != nil {
		t.Fatalf("read classifier unit: %v", err)
	}
	classifier := string(classifierData)
	for _, want := range []string{"routerd-ndpi-agent.service"} {
		if !strings.Contains(classifier, want) {
			t.Fatalf("classifier unit missing %q:\n%s", want, classifier)
		}
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "ServiceUnit", "routerd-ndpi-agent.service")
	if status["phase"] != "Applied" || status["source"] != "TrafficFlowLog/FirewallEventLog" {
		t.Fatalf("status = %#v", status)
	}
	gotCommands := strings.Join(commands, "\n")
	if !strings.Contains(gotCommands, "systemctl restart routerd-ndpi-agent.service") {
		t.Fatalf("commands missing ndpi agent restart:\n%s", gotCommands)
	}
}

func TestSystemdUnitControllerDoesNotReloadForAlreadyAbsentUnit(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{}}
	store := mapStore{}
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Store:            store,
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "systemctl" && len(args) >= 2 && (args[0] == "is-active" || args[0] == "is-enabled") {
				return nil, errors.New("inactive")
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	gotCommands := strings.Join(commands, "\n")
	if strings.Contains(gotCommands, "systemctl disable --now routerd-dhcpv6-client@wan-pd.service") {
		t.Fatalf("already absent disabled unit must not call disable:\n%s", gotCommands)
	}
}

func TestSystemdUnitControllerDefersActiveStaleClientDaemonCleanup(t *testing.T) {
	dir := t.TempDir()
	unitName := "routerd-dhcpv4-client@wan.service"
	unitPath := filepath.Join(dir, unitName)
	if err := os.WriteFile(unitPath, []byte("[Service]\nExecStart=/usr/local/sbin/routerd-dhcpv4-client daemon\n"), 0644); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{}}
	store := mapStore{}
	eventBus := bus.New()
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Bus:              eventBus,
		Store:            store,
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			line := strings.Join(append([]string{name}, args...), " ")
			commands = append(commands, line)
			if line == "systemctl is-active --quiet "+unitName {
				return []byte("active"), nil
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unitPath); err != nil {
		t.Fatalf("active stale client daemon unit was removed: %v", err)
	}
	gotCommands := strings.Join(commands, "\n")
	if !strings.Contains(gotCommands, "systemctl is-active --quiet "+unitName) {
		t.Fatalf("commands missing active check:\n%s", gotCommands)
	}
	for _, unwanted := range []string{
		"systemctl disable --now " + unitName,
		"systemctl reset-failed " + unitName,
	} {
		if strings.Contains(gotCommands, unwanted) {
			t.Fatalf("commands included service-disrupting cleanup %q:\n%s", unwanted, gotCommands)
		}
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName)
	if status["phase"] != "Pending" || status["reason"] != "StaleClientDaemonUnitActive" || status["active"] != true {
		t.Fatalf("status = %#v", status)
	}
	events := eventBus.Recent("routerd.system.service_unit.stale_cleanup_deferred")
	if len(events) != 1 {
		t.Fatalf("events = %#v, want one stale_cleanup_deferred event", events)
	}
	if events[0].Severity != "warning" || events[0].Resource == nil || events[0].Resource.Name != unitName {
		t.Fatalf("event = %#v", events[0])
	}
}

func TestSystemdUnitControllerReportsInactiveStaleClientDaemonCleanup(t *testing.T) {
	dir := t.TempDir()
	unitName := "routerd-dhcpv4-client@wan.service"
	unitPath := filepath.Join(dir, unitName)
	if err := os.WriteFile(unitPath, []byte("[Service]\nExecStart=/usr/local/sbin/routerd-dhcpv4-client daemon\n"), 0644); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{}}
	store := mapStore{}
	eventBus := bus.New()
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Bus:              eventBus,
		Store:            store,
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			line := strings.Join(append([]string{name}, args...), " ")
			commands = append(commands, line)
			if line == "systemctl is-active --quiet "+unitName {
				return nil, errors.New("inactive")
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unitPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inactive stale client daemon unit still exists: %v", err)
	}
	gotCommands := strings.Join(commands, "\n")
	for _, want := range []string{
		"systemctl is-active --quiet " + unitName,
		"systemctl disable --now " + unitName,
		"systemctl reset-failed " + unitName,
		"systemctl daemon-reload",
	} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("commands missing %q:\n%s", want, gotCommands)
		}
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName)
	if status["phase"] != "Removed" || status["reason"] != "StaleClientDaemonUnit" || status["active"] != false {
		t.Fatalf("status = %#v", status)
	}
	events := eventBus.Recent("routerd.system.service_unit.stale_removed")
	if len(events) != 1 {
		t.Fatalf("events = %#v, want one stale_removed event", events)
	}
	if events[0].Severity != "info" || events[0].Resource == nil || events[0].Resource.Name != unitName {
		t.Fatalf("event = %#v", events[0])
	}
}

func TestSystemdUnitControllerSchedulesOwnUnitRestart(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{}}
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Store:            mapStore{},
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
	gotCommands := strings.Join(commands, "\n")
	if commandLineContains(commands, "systemctl restart routerd.service") {
		t.Fatalf("self unit was directly restarted:\n%s", gotCommands)
	}
	if !strings.Contains(gotCommands, "systemd-run --unit routerd-self-restart-") ||
		!strings.Contains(gotCommands, "--on-active=10s --collect systemctl restart routerd.service") {
		t.Fatalf("self unit restart was not scheduled:\n%s", gotCommands)
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

func commandLineContains(commands []string, want string) bool {
	for _, command := range commands {
		if command == want {
			return true
		}
	}
	return false
}

func TestSystemdUnitControllerSynthesizesHealthCheckDaemonUnits(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ds-lite-a"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "internet-via-dslite-a"}, Spec: api.HealthCheckSpec{
			Daemon:             "routerd-healthcheck",
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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "balanced"}, Spec: api.EgressRoutePolicySpec{
			Mode: "hash",
			Candidates: []api.EgressRoutePolicyCandidate{{
				Name:    "balanced",
				Targets: []api.EgressRoutePolicyTarget{{Name: "ds-lite-a", Mark: 0x110, HealthCheck: "internet-via-dslite-a"}},
			}},
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
		`ExecStart=/usr/local/sbin/routerd-healthcheck --resource "internet-via-dslite-a" --target "1.1.1.1" --protocol "tcp" --fwmark 0x110 --source-interface "ds-lite-a" --source-address "172.18.0.1" --port 443`,
		`--socket "/run/routerd/healthcheck/internet-via-dslite-a.sock"`,
		"RuntimeDirectory=routerd/healthcheck",
		"RuntimeDirectoryPreserve=yes",
		"CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW",
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
	status := store.ObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName)
	if status["phase"] != "Applied" || status["changed"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func TestSystemdUnitControllerRemovesStaleHealthCheckDaemonUnits(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "routerd-healthcheck@stale.service")
	if err := os.WriteFile(stale, []byte("[Service]\nExecStart=/usr/local/sbin/routerd-healthcheck\n"), 0644); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "current"}, Spec: api.HealthCheckSpec{
			Daemon:       "routerd-healthcheck",
			Target:       "1.1.1.1",
			TargetSource: "static",
		}},
	}}}
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Store:            mapStore{},
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "systemctl" && len(args) >= 2 && args[0] == "is-active" {
				return nil, errors.New("inactive")
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale unit still exists: %v", err)
	}
	gotCommands := strings.Join(commands, "\n")
	for _, want := range []string{
		"systemctl disable --now routerd-healthcheck@stale.service",
		"systemctl reset-failed routerd-healthcheck@stale.service",
		"systemctl daemon-reload",
	} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("commands missing %q:\n%s", want, gotCommands)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "routerd-healthcheck@current.service")); err != nil {
		t.Fatalf("current unit missing: %v", err)
	}
}

func TestSystemdUnitControllerReconcilesDNSResolverLongLivedUnit(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Metadata: api.ObjectMeta{Name: "home"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "lan-resolver"}, Spec: api.DNSResolverSpec{
			Listen:  []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: 53}},
			Sources: []api.DNSResolverSourceSpec{{Kind: "upstream", Match: []string{"."}, Upstreams: []string{"udp://1.1.1.1:53"}}},
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
			line := strings.Join(append([]string{name}, args...), " ")
			commands = append(commands, line)
			if line == "systemctl is-active --quiet routerd-dns-resolver@lan-resolver.service" ||
				line == "systemctl is-enabled --quiet routerd-dns-resolver@lan-resolver.service" {
				return nil, errors.New("inactive")
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	unitName := "routerd-dns-resolver@lan-resolver.service"
	data, err := os.ReadFile(filepath.Join(dir, unitName))
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	unit := string(data)
	for _, want := range []string{
		"ExecStart=/usr/local/sbin/routerd-dns-resolver daemon",
		"--resource lan-resolver",
		"--config-file /var/lib/routerd/dns-resolver/lan-resolver/config.json",
		"Restart=always",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
	gotCommands := strings.Join(commands, "\n")
	for _, want := range []string{
		"systemctl unmask " + unitName,
		"systemctl enable " + unitName,
		"systemctl start " + unitName,
	} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("commands missing %q:\n%s", want, gotCommands)
		}
	}
	if strings.Contains(gotCommands, "systemctl restart "+unitName) {
		t.Fatalf("DNS resolver long-lived unit must not restart on reconcile:\n%s", gotCommands)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName)
	if status["phase"] != "Applied" || status["restartOnReconcile"] != false {
		t.Fatalf("status = %#v", status)
	}
}

func TestSystemdUnitControllerRemovesStaleDNSResolverUnits(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "routerd-dns-resolver@old.service")
	if err := os.WriteFile(stale, []byte("[Service]\nExecStart=/usr/local/sbin/routerd-dns-resolver\n"), 0644); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Metadata: api.ObjectMeta{Name: "home"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "current"}, Spec: api.DNSResolverSpec{
			Listen:  []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: 53}},
			Sources: []api.DNSResolverSourceSpec{{Kind: "upstream", Match: []string{"."}, Upstreams: []string{"udp://1.1.1.1:53"}}},
		}},
	}}}
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Store:            mapStore{},
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "systemctl" && len(args) >= 2 && args[0] == "is-active" {
				return nil, errors.New("inactive")
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale unit still exists: %v", err)
	}
	gotCommands := strings.Join(commands, "\n")
	for _, want := range []string{
		"systemctl disable --now routerd-dns-resolver@old.service",
		"systemctl reset-failed routerd-dns-resolver@old.service",
		"systemctl daemon-reload",
	} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("commands missing %q:\n%s", want, gotCommands)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "routerd-dns-resolver@current.service")); err != nil {
		t.Fatalf("current unit missing: %v", err)
	}
}

func TestSystemdUnitControllerRemovesStaleEventFederationUnits(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "routerd-eventd@old.service")
	if err := os.WriteFile(stale, []byte("[Service]\nExecStart=/usr/local/sbin/routerd-eventd\n"), 0644); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(dir, "routerd-eventd@current.service")
	router := &api.Router{Metadata: api.ObjectMeta{Name: "home"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"}, Metadata: api.ObjectMeta{Name: "current"}, Spec: api.EventGroupSpec{NodeName: "home"}},
	}}}
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Store:            mapStore{},
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "systemctl" && len(args) >= 2 && args[0] == "is-active" {
				return nil, errors.New("inactive")
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale unit still exists: %v", err)
	}
	gotCommands := strings.Join(commands, "\n")
	for _, want := range []string{
		"systemctl disable --now routerd-eventd@old.service",
		"systemctl reset-failed routerd-eventd@old.service",
		"systemctl daemon-reload",
	} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("commands missing %q:\n%s", want, gotCommands)
		}
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("current unit missing: %v", err)
	}
	// The live EventGroup unit must never be disabled/removed.
	if strings.Contains(gotCommands, "systemctl disable --now routerd-eventd@current.service") {
		t.Fatalf("live event federation unit was disabled:\n%s", gotCommands)
	}
}

func TestSystemdUnitControllerNoEventFederationNoAction(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Metadata: api.ObjectMeta{Name: "home"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "lan-resolver"}, Spec: api.DNSResolverSpec{
			Listen:  []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: 53}},
			Sources: []api.DNSResolverSourceSpec{{Kind: "upstream", Match: []string{"."}, Upstreams: []string{"udp://1.1.1.1:53"}}},
		}},
	}}}
	var commands []string
	controller := SystemdUnitController{
		Router:           router,
		Store:            mapStore{},
		SystemdSystemDir: dir,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "systemctl" && len(args) >= 2 && args[0] == "is-active" {
				return nil, errors.New("inactive")
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	gotCommands := strings.Join(commands, "\n")
	if strings.Contains(gotCommands, "routerd-eventd@") {
		t.Fatalf("event federation cleanup acted with no EventGroup present:\n%s", gotCommands)
	}
}

func TestSystemdUnitControllerSynthesizesDHCPClientUnits(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client"}, Metadata: api.ObjectMeta{Name: "wan-v4"}, Spec: api.DHCPv4ClientSpec{Interface: "wan", Hostname: "routerd-test"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan", IAID: "1", ClientDUID: "00030001020000000103"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "lan"}},
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
				`ExecStart=/usr/local/sbin/routerd-dhcpv6-client daemon --resource wan-pd --interface ens18 --iaid 1 --client-duid 00030001020000000103`,
				`RuntimeDirectory=routerd/dhcpv6-client`,
				`AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN CAP_NET_BIND_SERVICE`,
			},
		},
		{
			unit: "routerd-ra-observer@lan-ra.service",
			want: []string{
				`ExecStart=/usr/local/sbin/routerd-ra-observer daemon --resource lan-ra --interface ens19 --socket /run/routerd/ra-observer/lan-ra.sock --event-file /var/log/routerd/ra-observer-lan-ra.events.jsonl`,
				`RuntimeDirectory=routerd/ra-observer`,
				`AmbientCapabilities=CAP_NET_RAW`,
				`RestrictAddressFamilies=AF_UNIX AF_PACKET`,
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
		"systemctl unmask routerd-ra-observer@lan-ra.service",
		"systemctl enable routerd-ra-observer@lan-ra.service",
		"systemctl restart routerd-ra-observer@lan-ra.service",
	} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("commands missing %q:\n%s", want, gotCommands)
		}
	}
	if status := store.ObjectStatus(api.SystemAPIVersion, "ServiceUnit", "routerd-dhcpv4-client@wan-v4.service"); status["phase"] != "Applied" {
		t.Fatalf("dhcpv4 unit status = %#v", status)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan-v4"); status["managedBy"] != "systemd" {
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
	unitStatus := store.ObjectStatus(api.SystemAPIVersion, "ServiceUnit", unitName)
	if unitStatus["phase"] != "Disabled" {
		t.Fatalf("unit status = %#v", unitStatus)
	}
	healthStatus := store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "internet-via-pppoe")
	if healthStatus["phase"] != "Disabled" {
		t.Fatalf("health status = %#v", healthStatus)
	}

	commands = nil
	controller.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		_ = ctx
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		if name == "systemctl" && len(args) >= 2 && (args[0] == "is-active" || args[0] == "is-enabled") {
			return nil, errors.New("inactive")
		}
		return []byte("ok"), nil
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	gotCommands = strings.Join(commands, "\n")
	if strings.Contains(gotCommands, "systemctl daemon-reload") ||
		strings.Contains(gotCommands, "systemctl disable --now routerd-healthcheck@internet-via-pppoe.service") {
		t.Fatalf("unchanged disabled healthcheck must not reload or disable again:\n%s", gotCommands)
	}
}

func TestSystemdUnitControllerMarksDisabledPPPoESession(t *testing.T) {
	enabled := false
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"}, Metadata: api.ObjectMeta{Name: "pppoe-flets"}, Spec: api.PPPoESessionSpec{
			Interface: "wan",
			IfName:    "ppp-flets",
			Enabled:   &enabled,
			Username:  "user",
		}},
	}}}
	store := mapStore{}
	controller := SystemdUnitController{Router: router, Store: store, SystemdSystemDir: t.TempDir(), DryRun: true}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "PPPoESession", "pppoe-flets")
	if status["phase"] != PhaseDisabled {
		t.Fatalf("pppoe status = %#v", status)
	}
}
