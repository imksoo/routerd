package vrf

import (
	"reflect"
	"testing"
)

func TestCommands(t *testing.T) {
	got := Commands(Config{Name: "guest", IfName: "vrf-guest", RouteTable: 1001, Members: []string{"wg0", "vx100"}})
	want := [][]string{
		{"ip", "link", "add", "vrf-guest", "type", "vrf", "table", "1001"},
		{"ip", "link", "set", "dev", "vrf-guest", "up"},
		{"ip", "link", "set", "dev", "wg0", "master", "vrf-guest"},
		{"ip", "link", "set", "dev", "vx100", "master", "vrf-guest"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}
