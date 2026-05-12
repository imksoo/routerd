// SPDX-License-Identifier: BSD-3-Clause

package dhcpfingerprint

import (
	"testing"
	"time"
)

func TestParseDnsmasqLogDHCPLine(t *testing.T) {
	now := time.Unix(100, 0)
	event, ok := ParseDnsmasqLine(`dnsmasq-dhcp[1234]: DHCPDISCOVER(eth0) aa:bb:cc:dd:ee:ff`, now)
	if !ok {
		t.Fatal("line was not parsed")
	}
	if event.MAC != "aa:bb:cc:dd:ee:ff" || event.Interface != "eth0" || !event.ObservedAt.Equal(now) {
		t.Fatalf("unexpected event: %+v", event)
	}

	event, ok = ParseDnsmasqLine(`dnsmasq-dhcp[1234]: requested options: 1,15,3,6,44,46,47,31,33,249,43`, now)
	if !ok || len(event.RequestedOptions) != 11 || event.RequestedOptions[0] != 1 || event.RequestedOptions[9] != 249 {
		t.Fatalf("unexpected options event: %+v", event)
	}
}

func TestInferDHCPFingerprintPrefersStrongSignals(t *testing.T) {
	got := Infer(Fingerprint{VendorClass: "MSFT 5.0", RequestedOptions: []int{1, 3, 6, 15}})
	if got.OSFamily != "Windows" || got.DeviceClass != "computer" || got.Confidence < 80 {
		t.Fatalf("unexpected windows match: %+v", got)
	}

	got = Infer(Fingerprint{Hostname: "NintendoSwitch", RequestedOptions: []int{1, 15, 3, 6, 44, 46, 47}})
	if got.OSFamily != "nintendo" || got.DeviceClass != "gaming-console" {
		t.Fatalf("hostname should beat generic PRL: %+v", got)
	}
}
