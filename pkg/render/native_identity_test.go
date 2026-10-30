// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestFreeBSDStaticAddressesHaveOnlyOneRuntimeOwner(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		netResource("Interface", "lan", api.InterfaceSpec{IfName: "em0", Managed: true, AdminUp: true}),
		netResource("IPv4StaticAddress", "first", api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.0.2.1/24"}),
		netResource("IPv4StaticAddress", "second", api.IPv4StaticAddressSpec{Interface: "lan", Address: "198.51.100.1/24"}),
	}}}
	config, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh")
	command.Stdin = strings.NewReader(string(config.RCConf) + "\nprintf '%s\\n' \"${ifconfig_em0}\" \"${ifconfig_em0_alias0}\"\n")
	output, err := command.CombinedOutput()
	if err != nil || string(output) != "up\n\n" {
		t.Fatalf("rc.conf duplicates runtime-owned address acquisition or lost explicit AdminUp: %v %s", err, output)
	}
}

func TestWireGuardRenderersUseActualInterfaceWithoutDuplicateBootOwner(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		netResource("WireGuardInterface", "wg-intent", api.WireGuardInterfaceSpec{IfName: "wg0", PrivateKeyFile: "/keys/wg.key"}),
	}}}
	if got := linkAliases(router)["wg-intent"]; got != "wg0" {
		t.Fatalf("networkd alias = %q, want actual wg0", got)
	}
	config, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config.RCConf), "ifconfig_wg") {
		t.Fatalf("native boot config owns a runtime WireGuard interface:\n%s", config.RCConf)
	}
	for name := range config.RCDScripts {
		if strings.HasPrefix(name, "routerd_wireguard_") {
			t.Fatalf("duplicate WireGuard boot owner %q", name)
		}
	}
}
