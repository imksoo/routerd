// SPDX-License-Identifier: BSD-3-Clause
package apply

import (
	"github.com/imksoo/routerd/pkg/api"
	"strings"
	"testing"
)

func TestDHCPv6AddressPlanUsesNativeSupervisorOwner(t *testing.T) {
	resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Address"}, Metadata: api.ObjectMeta{Name: "wan-address"}, Spec: api.DHCPv6AddressSpec{Interface: "wan"}}
	result := ResourceResult{Observed: map[string]string{}}
	engine := &Engine{}
	engine.observeDHCPv6Address(resource, map[string]string{"wan": "wan0"}, true, &result)
	if result.Observed["client"] != "routerd-dhcpv6-client" {
		t.Fatalf("plan advertises a different client: %v", result.Observed)
	}
	plan := strings.Join(result.Plan, "\n")
	if !strings.Contains(plan, "/128") || !strings.Contains(plan, "DAD") {
		t.Fatalf("plan omits native address contract: %s", plan)
	}
}
