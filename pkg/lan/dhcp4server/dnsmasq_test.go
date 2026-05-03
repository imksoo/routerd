package dhcp4server

import (
	"reflect"
	"testing"

	"routerd/pkg/api"
)

func TestRenderDnsmasqLines(t *testing.T) {
	got := RenderDnsmasqLines(Config{
		Name:        "lan-v4",
		IfName:      "ens19",
		AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
		Gateway:     "192.168.10.1",
		DNSServers:  []string{"192.168.10.1"},
		NTPServers:  []string{"192.168.10.1"},
		Domain:      "lan",
		Options:     []api.DHCPOptionSpec{{Name: "domain-search", Value: "lan"}},
		Reservations: []api.IPv4DHCPReservationSpec{{
			MACAddress: "02:00:00:00:01:50",
			Hostname:   "printer",
			IPAddress:  "192.168.10.150",
		}},
	})
	want := []string{
		"interface=ens19",
		"dhcp-range=set:lan-v4,192.168.10.100,192.168.10.199,8h",
		"dhcp-option=tag:lan-v4,option:router,192.168.10.1",
		"dhcp-option=tag:lan-v4,option:dns-server,192.168.10.1",
		"dhcp-option=tag:lan-v4,option:ntp-server,192.168.10.1",
		"dhcp-option=tag:lan-v4,option:domain-name,lan",
		"dhcp-option=tag:lan-v4,option:domain-search,lan",
		"dhcp-host=02:00:00:00:01:50,printer,192.168.10.150",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %#v, want %#v", got, want)
	}
}
