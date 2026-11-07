// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestStaticAddressRejectsUnimplementedExclusiveTakeover(t *testing.T) {
	router := routerWithResources(testResource(api.NetAPIVersion, "Interface", "lan", api.InterfaceSpec{IfName: "eth0"}), testResource(api.NetAPIVersion, "IPv4StaticAddress", "lan", api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.0.2.1/24", Exclusive: true}))
	for _, osName := range []platform.OS{platform.OSLinux, platform.OSFreeBSD} {
		if err := ValidateForOS(router, osName); err == nil || !strings.Contains(err.Error(), "spec.exclusive is unsupported") {
			t.Fatalf("%s accepted unimplemented takeover: %v", osName, err)
		}
	}
}
