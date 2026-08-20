// SPDX-License-Identifier: BSD-3-Clause

package dynamicconfig

import "testing"

func TestParseMobilityPoolPlanSourceAcceptsOnlyReservedShapes(t *testing.T) {
	for _, tt := range []struct {
		source      string
		pool, node  string
		arpObserver bool
		valid       bool
	}{
		{source: "MobilityPool/cloudedge/node/router-a", pool: "cloudedge", node: "router-a", valid: true},
		{source: "MobilityPool/cloudedge/node/router-a/arp-observer", pool: "cloudedge", node: "router-a", arpObserver: true, valid: true},
		{source: "MobilityPool/cloudedge/node/router-a/not-a-plan"},
		{source: "MobilityPool/cloudedge/node/router-a/arp-observer/extra"},
		{source: "MobilityPool/cloudedge/node/ router-a"},
		{source: " MobilityPool/cloudedge/node/router-a"},
		{source: "MobilityPool/cloudedge/node/router-a "},
	} {
		t.Run(tt.source, func(t *testing.T) {
			got, ok := ParseMobilityPoolPlanSource(tt.source)
			if ok != tt.valid {
				t.Fatalf("ParseMobilityPoolPlanSource(%q) ok = %v, want %v", tt.source, ok, tt.valid)
			}
			if !ok {
				return
			}
			if got.PoolRef != tt.pool || got.NodeRef != tt.node || got.ARPObserver != tt.arpObserver {
				t.Fatalf("ParseMobilityPoolPlanSource(%q) = %#v", tt.source, got)
			}
		})
	}
}

func TestValidateMobilityFIBVerdictsFailsClosedForMalformedPoolDecisionSet(t *testing.T) {
	valid := []FIBVerdict{
		{PoolRef: "cloudedge", Scope: &FIBPoolScope{
			Prefix:                  "10.77.60.0/24",
			RemoteReturnCommunities: []string{"64512:20000"},
			PreferredSource:         "10.77.60.10/32",
		}},
		{PoolRef: "cloudedge", Address: "10.77.60.11/32", Action: "deliver-remote", Class: "RemoteHomeOwned"},
	}
	if err := ValidateMobilityFIBVerdicts(valid, "cloudedge"); err != nil {
		t.Fatalf("ValidateMobilityFIBVerdicts(valid): %v", err)
	}
	for _, tt := range []struct {
		name     string
		verdicts []FIBVerdict
		pool     string
	}{
		{name: "missing scope", verdicts: valid[1:]},
		{name: "scope carries decision", verdicts: []FIBVerdict{{PoolRef: "cloudedge", Scope: &FIBPoolScope{Prefix: "10.77.60.0/24"}, Action: "deliver-remote"}}},
		{name: "noncanonical scope", verdicts: []FIBVerdict{{PoolRef: "cloudedge", Scope: &FIBPoolScope{Prefix: "10.77.60.7/24"}}}},
		{name: "address is not host CIDR", verdicts: []FIBVerdict{valid[0], {PoolRef: "cloudedge", Address: "10.77.60.11", Action: "deliver-remote"}}},
		{name: "unsupported action", verdicts: []FIBVerdict{valid[0], {PoolRef: "cloudedge", Address: "10.77.60.11/32", Action: "allow"}}},
		{name: "duplicate address", verdicts: []FIBVerdict{valid[0], valid[1], valid[1]}},
		{name: "cross pool row", verdicts: []FIBVerdict{valid[0], {PoolRef: "other", Address: "10.77.60.11/32", Action: "deliver-remote"}}},
		{name: "surrounding whitespace", verdicts: []FIBVerdict{{PoolRef: "cloudedge", Scope: &FIBPoolScope{Prefix: "10.77.60.0/24", RemoteReturnCommunities: []string{" 64512:20000"}}}}, pool: "cloudedge"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pool := tt.pool
			if pool == "" {
				pool = "cloudedge"
			}
			if err := ValidateMobilityFIBVerdicts(tt.verdicts, pool); err == nil {
				t.Fatalf("ValidateMobilityFIBVerdicts(%#v, %q) accepted malformed data", tt.verdicts, pool)
			}
		})
	}
}
