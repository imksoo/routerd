// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import "testing"

func TestParseFreeBSDRouteGet(t *testing.T) {
	info, err := parseFreeBSDRouteGet(`route to: 198.51.100.10
destination: default
gateway: 192.0.2.1
interface: vtnet0
flags: <UP,GATEWAY,DONE,STATIC>
if address: 192.0.2.42
`)
	if err != nil {
		t.Fatalf("parseFreeBSDRouteGet: %v", err)
	}
	if info.NextHop != "192.0.2.1" || info.OutInterface != "vtnet0" || info.Source != "192.0.2.42" {
		t.Fatalf("info = %#v", info)
	}
}

func TestParseFreeBSDRouteGetDirectRoute(t *testing.T) {
	info, err := parseFreeBSDRouteGet("route to: 192.0.2.44\ninterface: vtnet1\n")
	if err != nil {
		t.Fatalf("parseFreeBSDRouteGet: %v", err)
	}
	if info.NextHop != "" || info.OutInterface != "vtnet1" {
		t.Fatalf("info = %#v", info)
	}
}

func TestParseFreeBSDRouteGetRejectsMissingInterface(t *testing.T) {
	if _, err := parseFreeBSDRouteGet("gateway: 192.0.2.1\n"); err == nil {
		t.Fatal("expected missing-interface error")
	}
}
