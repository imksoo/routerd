// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestHealthCheckDaemonSystemdSpecRendersThresholdsOnlyWhenConfigured(t *testing.T) {
	with := HealthCheckDaemonSystemdSpec(HealthCheckDaemonUnitOptions{Resource: "internet", Spec: api.HealthCheckSpec{Target: "1.1.1.1", HealthyThreshold: 2, UnhealthyThreshold: 3}})
	got := strings.Join(with.ExecStart, " ")
	for _, want := range []string{"--healthy-threshold 2", "--unhealthy-threshold 3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("daemon ExecStart missing %q: %#v", want, with.ExecStart)
		}
	}
	without := HealthCheckDaemonSystemdSpec(HealthCheckDaemonUnitOptions{Resource: "internet", Spec: api.HealthCheckSpec{Target: "1.1.1.1"}})
	if strings.Contains(strings.Join(without.ExecStart, " "), "--healthy-threshold") || strings.Contains(strings.Join(without.ExecStart, " "), "--unhealthy-threshold") {
		t.Fatalf("zero thresholds rendered: %#v", without.ExecStart)
	}
}

func TestHealthCheckSystemdDSLiteBindingFlag(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "dslite-a"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ip6tnl-a"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "internet"}, Spec: api.EgressRoutePolicySpec{Candidates: []api.EgressRoutePolicyCandidate{{Targets: []api.EgressRoutePolicyTarget{{Interface: "dslite-a", HealthCheck: "dslite-health"}}}}}},
	}}}
	unit := string(HealthCheckSystemdUnit(HealthCheckSystemdOptions{Resource: "dslite-health", RequireDSLiteBinding: true}))
	if !strings.Contains(unit, "--require-dslite-binding") {
		t.Fatalf("synthetic unit missing DS-Lite binding flag:\n%s", unit)
	}
	portable := HealthCheckDaemonSystemdSpec(HealthCheckDaemonUnitOptions{Resource: "dslite-health", Router: router, Spec: api.HealthCheckSpec{Target: "2001:db8::1"}})
	if strings.Contains(strings.Join(portable.ExecStart, " "), "--require-dslite-binding") {
		t.Fatalf("portable daemon ExecStart must not contain Linux-only binding flag: %#v", portable.ExecStart)
	}
}

func TestSystemdUnitRendersConflicts(t *testing.T) {
	unit := string(SystemdUnit("routerd-conntrackd@test.service", api.SystemdUnitSpec{
		ExecStart: []string{"/usr/sbin/conntrackd", "-C", "/etc/conntrackd/routerd-test.conf"},
		Conflicts: []string{"conntrackd.service"},
	}))
	if !strings.Contains(unit, "Conflicts=conntrackd.service") {
		t.Fatalf("unit missing conntrackd conflict:\n%s", unit)
	}
}
