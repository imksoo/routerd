package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
	routerstate "routerd/pkg/state"
)

func TestDHCPCDRendersLinuxConfigHookAndUnit(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			netResource("Interface", "wan", api.InterfaceSpec{IfName: "ens18", Managed: false}),
			netResource("IPv6PrefixDelegation", "wan-pd", api.IPv6PrefixDelegationSpec{
				Interface:           "wan",
				Client:              "dhcpcd",
				Profile:             "ntt-hgw-lan-pd",
				PrefixLength:        60,
				IAID:                "00000001",
				DUIDRawData:         "020000000101",
				ServerID:            "00030001020000000001",
				AcquisitionStrategy: "request-claim-only",
			}),
		}},
	}

	config, err := DHCPCD(router, "/usr/sbin/dhcpcd", "/usr/local/etc/routerd", "/run/routerd", "/etc/systemd/system")
	if err != nil {
		t.Fatalf("render dhcpcd: %v", err)
	}
	if len(config.Files) != 3 {
		t.Fatalf("len(files) = %d, want 3", len(config.Files))
	}
	conf := string(config.Files[0].Data)
	hook := string(config.Files[1].Data)
	unit := string(config.Files[2].Data)
	for _, want := range []string{
		"interface ens18",
		"ipv6rs",
		"ipv6only",
		"noipv4",
		"duid",
		"nooption rapid_commit",
		"option domain_name_servers",
		"ia_pd 1/::/60",
		"# routerd acquisition-strategy request-claim-only",
		"# routerd duid-raw-data 02:00:00:00:01:01",
		"# routerd observed-server-id 00:03:00:01:02:00:00:00:00:01",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("dhcpcd.conf missing %q:\n%s", want, conf)
		}
	}
	if !strings.Contains(hook, "Reserved for future DHCPv6-PD event ingestion for wan-pd") {
		t.Fatalf("hook script missing resource name:\n%s", hook)
	}
	if !strings.Contains(unit, "ExecStart=/usr/sbin/dhcpcd -B -6 -f /usr/local/etc/routerd/dhcpcd-wan-pd.conf ens18") {
		t.Fatalf("unit missing ExecStart:\n%s", unit)
	}
	if len(config.Units) != 1 || config.Units[0] != "routerd-dhcpcd-wan-pd.service" {
		t.Fatalf("units = %#v", config.Units)
	}
}

func TestDHCPCDRendersPriorPrefixFromLease(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			netResource("Interface", "wan", api.InterfaceSpec{IfName: "ens18", Managed: false}),
			netResource("IPv6PrefixDelegation", "wan-pd", api.IPv6PrefixDelegationSpec{
				Interface: "wan",
				Client:    "dhcpcd",
				Profile:   "default",
			}),
		}},
	}
	config, err := DHCPCDWithLeases(router, "/usr/sbin/dhcpcd", "/usr/local/etc/routerd", "/run/routerd", "/etc/systemd/system", map[string]routerstate.PDLease{
		"wan-pd": {CurrentPrefix: "2001:db8:1200:1240::/60", ServerID: "000300010200000000ff"},
	})
	if err != nil {
		t.Fatalf("render dhcpcd: %v", err)
	}
	conf := string(config.Files[0].Data)
	for _, want := range []string{
		"ia_pd 1/2001:db8:1200:1240::/60",
		"# routerd prior-prefix 2001:db8:1200:1240::/60",
		"# routerd observed-server-id 00:03:00:01:02:00:00:00:00:ff",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("dhcpcd.conf missing %q:\n%s", want, conf)
		}
	}
	if strings.Contains(conf, "nooption rapid_commit") {
		t.Fatalf("generic profile should not force rapid_commit off:\n%s", conf)
	}
}
