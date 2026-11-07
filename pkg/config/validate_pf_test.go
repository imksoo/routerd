// SPDX-License-Identifier: BSD-3-Clause
package config

import (
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"strings"
	"testing"
)

func TestFreeBSDPFCannotSilentlyDiscardRuleFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec api.FirewallRuleSpec
		want string
	}{{"sets", api.FirewallRuleSpec{FromZone: "lan", ToZone: "self", Action: "accept", DestinationSetRefs: []string{"IPAddressSet/targets"}}, "address-set table owner"}, {"rate", api.FirewallRuleSpec{FromZone: "lan", ToZone: "self", Action: "accept", RateLimit: api.FirewallRateLimitSpec{PacketsPerSecond: 5}}, "rateLimit"}, {"conn", api.FirewallRuleSpec{FromZone: "lan", ToZone: "wan", Action: "accept", ConnLimit: api.FirewallConnLimitSpec{MaxPerSource: 5}}, "connLimit"}, {"loopback", api.FirewallRuleSpec{FromZone: "self", ToZone: "self", Action: "drop"}, "loopback"}} {
		t.Run(tc.name, func(t *testing.T) {
			r := testResource(api.FirewallAPIVersion, "FirewallRule", tc.name, tc.spec)
			_, err := validateFirewallResource(r, platform.OSFreeBSD)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ignored PF field accepted: %v", err)
			}
		})
	}
}
