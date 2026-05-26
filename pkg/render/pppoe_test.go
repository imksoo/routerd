// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestPPPoERendersPeerUnitAndSecrets(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan-ether"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"},
				Metadata: api.ObjectMeta{Name: "pppoe0"},
				Spec: api.PPPoESessionSpec{
					Interface:    "wan-ether",
					IfName:       "ppp0",
					Username:     "user@example.jp",
					Password:     "secret",
					DefaultRoute: true,
					UsePeerDNS:   true,
					Managed:      true,
				},
			},
		}},
	}

	config, err := PPPoE(router, func(res api.Resource, spec api.PPPoESessionSpec) (string, error) {
		return spec.Password, nil
	})
	if err != nil {
		t.Fatalf("render PPPoE: %v", err)
	}
	if len(config.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(config.Files))
	}
	if len(config.Secrets) != 1 {
		t.Fatalf("secrets = %d, want 1", len(config.Secrets))
	}
	if len(config.Units) != 1 || config.Units[0] != "routerd-pppoe-pppoe0.service" {
		t.Fatalf("units = %v", config.Units)
	}

	var peer string
	for _, file := range config.Files {
		if strings.Contains(file.Path, "/etc/ppp/peers/") {
			peer = string(file.Data)
		}
	}
	for _, want := range []string{
		"plugin rp-pppoe.so",
		"nic-ens18",
		"ifname ppp0",
		`user "user@example.jp"`,
		"defaultroute",
		"usepeerdns",
	} {
		if !strings.Contains(peer, want) {
			t.Fatalf("peer config missing %q:\n%s", want, peer)
		}
	}
	if got := PPPoESecretLine(config.Secrets[0]); got != "\"user@example.jp\" * \"secret\" *\n" {
		t.Fatalf("secret line = %q", got)
	}
}

func TestPPPoERendersDisabledManagedUnitWithoutStartingIt(t *testing.T) {
	enabled := false
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan-ether"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"},
				Metadata: api.ObjectMeta{Name: "pppoe0"},
				Spec: api.PPPoESessionSpec{
					Interface: "wan-ether",
					IfName:    "ppp0",
					Enabled:   &enabled,
					Username:  "user@example.jp",
					Password:  "secret",
					Managed:   true,
				},
			},
		}},
	}

	config, err := PPPoE(router, func(res api.Resource, spec api.PPPoESessionSpec) (string, error) {
		return spec.Password, nil
	})
	if err != nil {
		t.Fatalf("render PPPoE: %v", err)
	}
	if len(config.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(config.Files))
	}
	if len(config.Units) != 0 {
		t.Fatalf("enabled units = %v", config.Units)
	}
	if len(config.DisabledUnits) != 1 || config.DisabledUnits[0] != "routerd-pppoe-pppoe0.service" {
		t.Fatalf("disabled units = %v", config.DisabledUnits)
	}
}
