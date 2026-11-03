// SPDX-License-Identifier: BSD-3-Clause
package config

import (
	"net/netip"
	"testing"
)

func TestLinuxHAInputKeepsPeerSynchronizationOutsideLegacySNAT(t *testing.T) {
	router, err := Load("../../tests/fixtures/linux-ha-dslite-pd.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "NAT44Rule" || resource.Metadata.Name != "legacy-snat" {
			continue
		}
		spec, err := resource.NAT44RuleSpec()
		if err != nil {
			t.Fatal(err)
		}
		for _, destination := range []struct {
			address  string
			excluded bool
		}{
			{"198.18.30.2", true},
			{"198.18.30.3", true},
			{"198.18.29.2", true},
			{"198.18.29.3", true},
			{"203.0.113.80", false},
		} {
			excluded := false
			for _, cidr := range spec.ExcludeDestinationCIDRs {
				prefix, err := netip.ParsePrefix(cidr)
				if err != nil {
					t.Fatal(err)
				}
				excluded = excluded || prefix.Contains(netip.MustParseAddr(destination.address))
			}
			if excluded != destination.excluded {
				t.Errorf("legacy SNAT exclusion for %s = %t, want %t to preserve the deployed private-destination policy", destination.address, excluded, destination.excluded)
			}
		}
		return
	}
	t.Fatal("integrated HA input has no legacy SNAT rule")
}
