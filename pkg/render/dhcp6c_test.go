package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
	routerstate "routerd/pkg/state"
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
	if len(config.Files) != 3 {
		t.Fatalf("len(files) = %d, want 3", len(config.Files))
	}
	conf := string(config.Files[0].Data)
	hook := string(config.Files[1].Data)
	unit := string(config.Files[2].Data)
	for _, want := range []string{
		"interface ens18",
		"send ia-pd 0;",
		"request domain-name-servers;",
		`script "/usr/local/etc/routerd/dhcp6c-wan-pd.hook";`,
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
		`--arg resource "wan-pd"`,
		`/run/routerd/routerd.sock`,
		`/api/control.routerd.net/v1alpha1/dhcp6-event`,
	} {
		if !strings.Contains(hook, want) {
			t.Fatalf("hook missing %q:\n%s", want, hook)
		}
	}
	for _, want := range []string{
		"ExecStart=/usr/sbin/dhcp6c -dDf -c /usr/local/etc/routerd/dhcp6c-wan-pd.conf -p /run/routerd/dhcp6c-wan-pd.pid ens18",
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

func TestDHCP6CRendersLeaseContextComments(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			netResource("Interface", "wan", api.InterfaceSpec{IfName: "ens18", Managed: false}),
			netResource("Interface", "lan", api.InterfaceSpec{IfName: "ens19", Managed: false}),
			netResource("IPv6PrefixDelegation", "wan-pd", api.IPv6PrefixDelegationSpec{
				Interface:           "wan",
				Client:              "dhcp6c",
				Profile:             "ntt-hgw-lan-pd",
				PrefixLength:        60,
				ServerID:            "00030001020000000001",
				AcquisitionStrategy: "request-claim-only",
			}),
			netResource("IPv6DelegatedAddress", "lan-v6", api.IPv6DelegatedAddressSpec{
				PrefixDelegation: "wan-pd",
				Interface:        "lan",
				SubnetID:         "0",
				AddressSuffix:    "::3",
			}),
		}},
	}

	config, err := DHCP6CWithLeases(router, "/usr/sbin/dhcp6c", "/usr/local/etc/routerd", "/run/routerd", "/etc/systemd/system", map[string]routerstate.PDLease{
		"wan-pd": {CurrentPrefix: "2001:db8:1200:1240::/60", ServerID: "000300010200000000ff"},
	})
	if err != nil {
		t.Fatalf("render dhcp6c: %v", err)
	}
	conf := string(config.Files[0].Data)
	for _, want := range []string{
		"# routerd acquisition-strategy request-claim-only",
		"# routerd observed-server-id 00:03:00:01:02:00:00:00:00:01",
		"# routerd prior-prefix 2001:db8:1200:1240::/60",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("dhcp6c.conf missing %q:\n%s", want, conf)
		}
	}
}
