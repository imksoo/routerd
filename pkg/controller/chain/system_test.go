package chain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestNetworkAdoptionControllerWritesNetworkdAndResolvedDropins(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NetworkAdoption"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.NetworkAdoptionSpec{
			Interface: "wan",
			SystemdNetworkd: api.NetworkAdoptionNetworkdSpec{
				DisableDHCPv4: true,
				DisableDHCPv6: true,
			},
			SystemdResolved: api.NetworkAdoptionResolvedSpec{DisableDNSStubListener: true},
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
