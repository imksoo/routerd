// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/sam"
)

func TestPFRuntimeConnectsNativeSAMOwnerBeforeDefaultFilter(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		netResource("Interface", "lan", api.InterfaceSpec{IfName: "em0"}),
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
	}}}
	data, err := PFRuntimeFirewall(router, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rules := string(data)
	anchor := `anchor "` + sam.PFForwardAnchor + `" quick`
	if !sam.PFForwardAnchorReferenced(rules) || strings.Index(rules, anchor) > strings.Index(rules, "block drop all") {
		t.Fatalf("native SAM owner cannot reach packets before default filtering:\n%s", rules)
	}
	for _, unreachable := range []string{`# anchor "routerd_sam_forward" all`, `anchor "routerd_sam_forward_other" all`, `anchor "routerd_sam_forward" in on em1 all`} {
		if sam.PFForwardAnchorReferenced(unreachable) {
			t.Fatalf("unconditional native anchor inferred from %q", unreachable)
		}
	}
}
