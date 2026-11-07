// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestClientDaemonSpecsCarryPPPoESessionOptions(t *testing.T) {
	persist := false
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "eth1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"}, Metadata: api.ObjectMeta{Name: "pppoe"}, Spec: api.PPPoESessionSpec{
			Interface:    "wan",
			IfName:       "ppp-uplink",
			Username:     "subscriber",
			PasswordFile: "/run/secrets/pppoe",
			AuthMethod:   "pap",
			ServiceName:  "internet service",
			ACName:       "access concentrator",
			MTU:          1480,
			MRU:          1480,
			DefaultRoute: true,
			UsePeerDNS:   true,
			Persist:      &persist,
			LCPInterval:  17,
			LCPFailure:   5,
			IPv6:         true,
		}}},
	}}
	specs := (&Runner{}).clientDaemonSpecs(router)
	if len(specs) != 1 || specs[0].Binary != "routerd-pppoe-client" {
		t.Fatalf("client daemon specs = %#v", specs)
	}
	args := specs[0].Args
	for _, pair := range [][]string{
		{"--interface", "eth1"},
		{"--ifname", "ppp-uplink"},
		{"--auth-method", "pap"},
		{"--username", "subscriber"},
		{"--password-file", "/run/secrets/pppoe"},
		{"--service-name", "internet service"},
		{"--ac-name", "access concentrator"},
		{"--mtu", "1480"},
		{"--mru", "1480"},
		{"--lcp-echo-interval", "17"},
		{"--lcp-echo-failure", "5"},
	} {
		if !hasAdjacentArgs(args, pair[0], pair[1]) {
			t.Errorf("args %q missing %q %q", args, pair[0], pair[1])
		}
	}
	for _, flag := range []string{"--default-route", "--use-peer-dns", "--ipv6", "--persist=false"} {
		if !hasArg(args, flag) {
			t.Errorf("args %q missing %q", args, flag)
		}
	}
}

func TestClientDaemonSpecsDoNotStartDisabledPPPoESession(t *testing.T) {
	enabled := false
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"}, Metadata: api.ObjectMeta{Name: "backup"}, Spec: api.PPPoESessionSpec{Interface: "eth1", Username: "subscriber", PasswordFile: "/run/secrets/pppoe", Enabled: &enabled}},
	}}}
	if got := (&Runner{}).clientDaemonSpecs(router); len(got) != 0 {
		t.Fatalf("disabled PPPoE session started client daemons: %#v", got)
	}
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func hasAdjacentArgs(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
