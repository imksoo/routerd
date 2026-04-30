package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestDHCP6CRendersLinuxConfigAndUnit(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			netResource("Interface", "wan", api.InterfaceSpec{IfName: "ens18", Managed: false}),
			netResource("Interface", "lan", api.InterfaceSpec{IfName: "ens19", Managed: false}),
			netResource("IPv6PrefixDelegation", "wan-pd", api.IPv6PrefixDelegationSpec{
				Interface:    "wan",
				Client:       "dhcp6c",
				Profile:      "ntt-hgw-lan-pd",
				PrefixLength: 60,
			}),
			netResource("IPv6DelegatedAddress", "lan-v6", api.IPv6DelegatedAddressSpec{
				PrefixDelegation: "wan-pd",
				Interface:        "lan",
				SubnetID:         "0",
				AddressSuffix:    "::3",
			}),
		}},
	}

	config, err := DHCP6C(router, "/usr/sbin/dhcp6c", "/usr/local/etc/routerd", "/run/routerd", "/etc/systemd/system")
	if err != nil {
		t.Fatalf("render dhcp6c: %v", err)
	}
	if len(config.Files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(config.Files))
	}
	conf := string(config.Files[0].Data)
	unit := string(config.Files[1].Data)
	for _, want := range []string{
		"interface ens18",
		"send ia-pd 0;",
		"request domain-name-servers;",
		"id-assoc pd 0",
		"prefix-interface ens19",
		"sla-id 0;",
		"sla-len 4;",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("dhcp6c.conf missing %q:\n%s", want, conf)
		}
	}
	for _, want := range []string{
		"ExecStart=/usr/sbin/dhcp6c -f -n -c /usr/local/etc/routerd/dhcp6c-wan-pd.conf -p /run/routerd/dhcp6c-wan-pd.pid ens18",
		"Restart=always",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
	if len(config.Units) != 1 || config.Units[0] != "routerd-dhcp6c-wan-pd.service" {
		t.Fatalf("units = %#v", config.Units)
	}
}
