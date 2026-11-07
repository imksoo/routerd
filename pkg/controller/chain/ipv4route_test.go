// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"reflect"
	"testing"
)

func TestFreeBSDIPv4RouteHostCommand(t *testing.T) {
	name, args := freeBSDIPv4RouteApplyCommand("unicast", "1.1.1.1/32", "gif41", "", "")
	want := []string{"-n", "change", "-host", "1.1.1.1", "-interface", "gif41"}
	if name != "route" || !reflect.DeepEqual(args, want) {
		t.Fatalf("command = %s %#v, want route %#v", name, args, want)
	}
}

func TestFreeBSDIPv4RouteDefaultDSLiteCommand(t *testing.T) {
	name, args := freeBSDIPv4RouteApplyCommand("unicast", "0.0.0.0/0", "gif41", "", "")
	want := []string{"-n", "change", "default", "-interface", "gif41"}
	if name != "route" || !reflect.DeepEqual(args, want) {
		t.Fatalf("command = %s %#v, want route %#v", name, args, want)
	}
}

func TestFreeBSDIPv4RoutePreferredSourceCommand(t *testing.T) {
	name, args := freeBSDIPv4RouteApplyCommand("unicast", "10.77.60.11/32", "wg0", "", "10.77.60.10")
	want := []string{"-n", "change", "-host", "10.77.60.11", "-interface", "wg0", "-ifa", "10.77.60.10"}
	if name != "route" || !reflect.DeepEqual(args, want) {
		t.Fatalf("command = %s %#v, want route %#v", name, args, want)
	}
}

func TestFreeBSDIPv4RouteRemovalDeleteCommand(t *testing.T) {
	name, args := freeBSDIPv4RouteRemovalDeleteCommand(ipv4RouteRemoval{Destination: "10.77.60.11/32"})
	want := []string{"-n", "delete", "-host", "10.77.60.11"}
	if name != "route" || !reflect.DeepEqual(args, want) {
		t.Fatalf("command = %s %#v, want route %#v", name, args, want)
	}
}
