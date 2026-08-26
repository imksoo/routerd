// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestHealthCheckSystemdUnit(t *testing.T) {
	unitWithPre := string(SystemdUnit("routerd.service", api.SystemdUnitSpec{
		ExecStartPre: []string{"/usr/local/sbin/routerd", "apply", "--once"},
		ExecStart:    []string{"/usr/local/sbin/routerd", "serve"},
	}))
	if !strings.Contains(unitWithPre, "ExecStartPre=/usr/local/sbin/routerd apply --once") {
		t.Fatalf("unit missing ExecStartPre:\n%s", unitWithPre)
	}
	unit := string(HealthCheckSystemdUnit(HealthCheckSystemdOptions{
		BinaryPath:         "/usr/local/sbin/routerd-healthcheck",
		Resource:           "internet-icmp",
		Target:             "1.1.1.1",
		Protocol:           "icmp",
		FwMark:             0x116,
		SourceInterface:    "ds-routerd-test",
		SourceAddress:      "192.0.2.10",
		Interval:           "30s",
		Timeout:            "3s",
		HealthyThreshold:   2,
		UnhealthyThreshold: 3,
		SocketPath:         "/run/routerd/healthcheck/internet-icmp.sock",
		StateFile:          "/var/lib/routerd/healthcheck/internet-icmp/state.json",
		EventFile:          "/var/lib/routerd/healthcheck/internet-icmp/events.jsonl",
	}))
	for _, want := range []string{
		"Description=routerd healthcheck internet-icmp",
		"BindsTo=routerd.service",
		"After=routerd.service network-online.target",
		"ExecStart=/usr/local/sbin/routerd-healthcheck --resource \"internet-icmp\" --target \"1.1.1.1\" --protocol \"icmp\"",
		"--fwmark 0x116",
		"--source-interface \"ds-routerd-test\"",
		"--source-address \"192.0.2.10\"",
		"--healthy-threshold 2",
		"--unhealthy-threshold 3",
		"--socket \"/run/routerd/healthcheck/internet-icmp.sock\"",
		"RuntimeDirectoryPreserve=yes",
		"CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW",
		"AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
	for _, notWant := range []string{"RestrictAddressFamilies=", "ProtectSystem=", "ProtectHome=", "PrivateTmp=", "ReadWritePaths="} {
		if strings.Contains(unit, notWant) {
			t.Fatalf("unit must not contain %q:\n%s", notWant, unit)
		}
	}
}

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
