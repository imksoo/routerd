// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"regexp"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestFreeBSDRCDPgrepPatternIncludesResourceName(t *testing.T) {
	got := freeBSDRCDPgrepPattern([]string{
		"/usr/local/sbin/routerd-healthcheck",
		"daemon",
		"--resource",
		"internet-via-dslite-c",
		"--target",
		"9.9.9.9",
	})
	if !strings.Contains(got, regexp.QuoteMeta("internet-via-dslite-c")) {
		t.Fatalf("pgrep pattern should include resource name, got %q", got)
	}
}

func TestFreeBSDRCDDoesNotRenderLinuxDSLiteBindingFlag(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "dslite-a"}, Spec: api.DSLiteTunnelSpec{TunnelName: "gif0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "internet"}, Spec: api.EgressRoutePolicySpec{Candidates: []api.EgressRoutePolicyCandidate{{Targets: []api.EgressRoutePolicyTarget{{Interface: "dslite-a", HealthCheck: "dslite-health"}}}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "dslite-health"}, Spec: api.HealthCheckSpec{Daemon: "routerd-healthcheck", Target: "2001:db8::1", Protocol: "icmp"}},
	}}}
	cfg, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	if script := string(cfg.RCDScripts["routerd_healthcheck_dslite_health"]); strings.Contains(script, "--require-dslite-binding") {
		t.Fatalf("FreeBSD rc.d script contains Linux-only DS-Lite binding flag:\n%s", script)
	}
}

func TestFreeBSDRenderSkipsIdentityOnlyEventFederationRCD(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
			Metadata: api.ObjectMeta{Name: "identity"},
			Spec: api.EventGroupSpec{
				NodeName: "freebsd-router",
			},
		},
	}}}
	cfg, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg.RCConf), "routerd_eventd_identity") {
		t.Fatalf("rc.conf should not enable identity-only eventd:\n%s", cfg.RCConf)
	}
	if _, ok := cfg.RCDScripts["routerd_eventd_identity"]; ok {
		t.Fatalf("identity-only eventd rc.d script was rendered: %#v", cfg.RCDScripts)
	}
}
