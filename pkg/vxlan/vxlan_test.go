package vxlan

import (
	"reflect"
	"testing"
)

func TestCommands(t *testing.T) {
	got := Commands(Config{
		Name:              "overlay",
		IfName:            "vx240",
		VNI:               240,
		LocalAddress:      "10.44.0.1",
		UnderlayInterface: "wg0",
		Peers:             []string{"10.44.0.2"},
		Bridge:            "br240",
		MTU:               1380,
	})
	want := [][]string{
		{"ip", "link", "add", "vx240", "type", "vxlan", "id", "240", "local", "10.44.0.1", "dev", "wg0", "dstport", "4789", "nolearning"},
		{"ip", "link", "set", "dev", "vx240", "mtu", "1380"},
		{"ip", "link", "set", "dev", "vx240", "master", "br240"},
		{"ip", "link", "set", "dev", "vx240", "up"},
		{"bridge", "fdb", "append", "00:00:00:00:00:00", "dev", "vx240", "dst", "10.44.0.2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}
