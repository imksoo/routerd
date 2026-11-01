// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestNftablesFirewallReplacesTableToWithdrawObsoleteZoneObjects(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "ens19"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
		},
	}}}

	data, err := NftablesFirewall(router, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "destroy table inet routerd_filter\n") {
		t.Fatalf("firewall replacement does not remove obsolete named objects:\n%s", got)
	}
	if strings.Contains(got, "flush table inet routerd_filter\n") {
		t.Fatalf("firewall replacement retained flush-only preamble:\n%s", got)
	}
}
