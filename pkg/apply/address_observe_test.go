// SPDX-License-Identifier: BSD-3-Clause

package apply

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestHasAddressRequiresExactAssignment(t *testing.T) {
	for _, tt := range []struct {
		name, output string
		want         bool
	}{
		{"exact", "eth0 UP 192.0.2.1/24\n", true},
		{"different host", "eth0 UP 192.0.2.10/24\n", false},
		{"different prefix", "eth0 UP 192.0.2.1/25\n", false},
		{"same subnet", "eth0 UP 192.0.2.2/24\n", false},
		{"different interface", "eth1 UP 192.0.2.1/24\n", false},
		{"interface substring", "eth00 UP 192.0.2.1/24\n", false},
		{"peer interface suffix", "eth0@if2 UP 192.0.2.1/24\n", true},
		{"multiple addresses", "eth0 UP 192.0.2.10/24 192.0.2.1/24\n", true},
		{"multiple interfaces", "eth1 UP 192.0.2.1/24\neth0 UP 192.0.2.10/24\n", false},
		{"no prefix", "eth0 UP 192.0.2.1\n", false},
		{"malformed address", "eth0 UP 192.0.2.1/24junk\n", false},
		{"empty", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			engine := &Engine{Command: fakeCommand(map[string]string{
				"ip -brief -4 addr show dev eth0": tt.output,
			})}
			if got := engine.hasAddress("eth0", "192.0.2.1/24", "-4"); got != tt.want {
				t.Fatalf("hasAddress = %t, want %t", got, tt.want)
			}
			rr := &ResourceResult{Phase: "Healthy", Observed: map[string]string{}}
			engine.observeIPv4Static(api.Resource{Spec: api.IPv4StaticAddressSpec{
				Interface: "lan", Address: "192.0.2.1/24",
			}}, map[string]string{"lan": "eth0"}, nil, nil, false, rr)
			wantPhase, wantPresent := "Drifted", "false"
			if tt.want {
				wantPhase, wantPresent = "Healthy", "true"
			}
			if rr.Phase != wantPhase || rr.Observed["present"] != wantPresent {
				t.Fatalf("observation = %+v, want %s and present=%s", rr, wantPhase, wantPresent)
			}
		})
	}
}

func TestHasAddressIfconfigRequiresExactAssignment(t *testing.T) {
	for _, tt := range []struct {
		name, output string
		want         bool
	}{
		{"exact hexadecimal mask", "vtnet1: flags=...\n\tinet 192.0.2.1 netmask 0xffffff00 broadcast 192.0.2.255\n", true},
		{"exact dotted mask", "vtnet1: flags=...\n\tinet 192.0.2.1 netmask 255.255.255.0 broadcast 192.0.2.255\n", true},
		{"different host", "vtnet1: flags=...\n\tinet 192.0.2.10 netmask 0xffffff00\n", false},
		{"different prefix", "vtnet1: flags=...\n\tinet 192.0.2.1 netmask 0xffffff80\n", false},
		{"noncontiguous mask", "vtnet1: flags=...\n\tinet 192.0.2.1 netmask 0xffffff01\n", false},
		{"missing mask", "vtnet1: flags=...\n\tinet 192.0.2.1 broadcast 192.0.2.255\n", false},
		{"different interface", "vtnet2: flags=...\n\tinet 192.0.2.1 netmask 0xffffff00\n", false},
		{"broadcast is not address", "vtnet1: flags=...\n\tinet 192.0.2.10 netmask 0xffffff00 broadcast 192.0.2.1\n", false},
		{"multiple interfaces", "vtnet1: flags=...\n\tinet 192.0.2.10 netmask 0xffffff00\nvtnet2: flags=...\n\tinet 192.0.2.1 netmask 0xffffff00\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			engine := &Engine{Command: fakeCommand(map[string]string{"ifconfig vtnet1": tt.output})}
			if got := engine.hasAddress("vtnet1", "192.0.2.1/24", "-4"); got != tt.want {
				t.Fatalf("hasAddress = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestHasAddressIPv6(t *testing.T) {
	for _, outputs := range []map[string]string{
		{"ip -brief -6 addr show dev eth0": "eth0 UP 2001:db8::1/64\n"},
		{"ifconfig eth0": "eth0: flags=...\n\tinet6 2001:db8::1 prefixlen 64\n"},
	} {
		engine := &Engine{Command: fakeCommand(outputs)}
		if !engine.hasAddress("eth0", "2001:db8:0:0::1/64", "-6") {
			t.Fatal("equivalent IPv6 address did not match")
		}
		if engine.hasAddress("eth0", "2001:db8::1/65", "-6") {
			t.Fatal("different IPv6 prefix matched")
		}
	}
}

func TestHasAddressRejectsInvalidRequestAndCommandFailure(t *testing.T) {
	engine := &Engine{Command: fakeCommand(map[string]string{
		"ip -brief -4 addr show dev eth0": "eth0 UP 192.0.2.1/24\n",
	})}
	for _, address := range []string{"", "192.0.2.1", "192.0.2.1/33", "2001:db8::1/64"} {
		if engine.hasAddress("eth0", address, "-4") {
			t.Fatalf("invalid IPv4 request %q matched", address)
		}
	}
	if engine.hasAddress("missing", "192.0.2.1/24", "-4") {
		t.Fatal("failed commands matched an address")
	}
}
