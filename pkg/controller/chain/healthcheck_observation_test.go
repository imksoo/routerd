// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/healthcheck"
)

func TestHealthCheckSnapshotReplacesPreviousProbeRoute(t *testing.T) {
	for _, tc := range []struct {
		name     string
		phase    string
		observed map[string]string
	}{
		{
			name:  "binding removed while still healthy",
			phase: healthcheck.PhaseHealthy,
			observed: map[string]string{
				"lastResult":      healthcheck.ResultPassed,
				"sourceInterface": "",
				"sourceAddress":   "",
				"outInterface":    "mgmt0",
				"nextHop":         "192.0.2.1",
				"routeSource":     "192.0.2.2",
			},
		},
		{
			name:  "failed probe no longer has route evidence",
			phase: healthcheck.PhaseUnhealthy,
			observed: map[string]string{
				"lastResult":      healthcheck.ResultFailed,
				"sourceInterface": "wan0",
				"fwmark":          "0x115",
				"failureKind":     healthcheck.FailureKindTimeout,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := map[string]any{
				"phase":                    healthcheck.PhaseHealthy,
				"owner":                    "service-unit",
				"lastFailureTime":          "2026-10-15T06:07:59Z",
				"history":                  []any{"previous probe"},
				"fwmark":                   "0x114",
				"sourceInterface":          "wan0",
				"sourceAddress":            "198.51.100.2",
				"sourceOrigin":             "explicit",
				"via":                      "198.51.100.1",
				"egressInterface":          "wan0",
				"outInterface":             "wan0",
				"nextHop":                  "198.51.100.1",
				"routeSource":              "198.51.100.2",
				"tunnelLocal":              "2001:db8::2",
				"tunnelRemote":             "2001:db8::1",
				"network.fwmark":           "0x114",
				"network.interface.name":   "wan0",
				"network.egress.interface": "wan0",
				"network.out.interface":    "wan0",
				"network.route.source":     "198.51.100.2",
				"network.nexthop.address":  "198.51.100.1",
			}
			status := daemonObservedOnlyStatus(current, map[string]any{"phase": tc.phase}, daemonapi.ResourceStatus{
				Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "HealthCheck", Name: "internet"},
				Phase:    tc.phase,
				Observed: tc.observed,
			})
			for _, key := range []string{
				"fwmark", "sourceInterface", "sourceAddress", "sourceOrigin", "via",
				"egressInterface", "outInterface", "nextHop", "routeSource", "tunnelLocal", "tunnelRemote",
				"network.fwmark", "network.interface.name", "network.egress.interface",
				"network.out.interface", "network.route.source", "network.nexthop.address",
			} {
				got, exists := status[key]
				want, present := tc.observed[key]
				if exists != present || (present && got != want) {
					t.Errorf("%s = %#v (present %t), want latest observation %q (present %t)", key, got, exists, want, present)
				}
			}
			if status["owner"] != "service-unit" || status["lastFailureTime"] != current["lastFailureTime"] || status["history"] == nil {
				t.Errorf("snapshot discarded ownership or historical evidence: %#v", status)
			}
			if current["network.out.interface"] != "wan0" {
				t.Fatal("snapshot modified the previous status")
			}
		})
	}
}
