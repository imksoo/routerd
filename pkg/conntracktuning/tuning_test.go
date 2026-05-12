// SPDX-License-Identifier: BSD-3-Clause

package conntracktuning

import (
	"testing"
	"time"

	"routerd/pkg/logstore"
)

func TestAnalyzeSuggestsShortTLSAndExtendsOrphanUDP(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	summary := Analyze(Inputs{
		Now:    now,
		Window: time.Hour,
		DPIFlows: []logstore.DPIFlowEntry{{
			FirstSeen: now.Add(-90 * time.Second),
			LastSeen:  now.Add(-30 * time.Second),
			Protocol:  "tcp",
			AppName:   "tls",
		}},
		FirewallLogs: []logstore.FirewallLogEntry{{
			Timestamp:   now.Add(-10 * time.Second),
			Action:      "drop",
			Protocol:    "udp",
			DPIApp:      "stun",
			Correlation: "orphan_return",
		}},
	})
	if summary.ApplyMode != "manual" || summary.AutoApply {
		t.Fatalf("apply guard changed: %+v", summary)
	}
	var sawTLS, sawSTUN bool
	for _, row := range summary.Suggestions {
		switch row.Application {
		case "tls":
			sawTLS = true
			if row.SysctlKey != "net.netfilter.nf_conntrack_tcp_timeout_established" || row.RecommendedSeconds >= row.BaselineSeconds {
				t.Fatalf("unexpected tls suggestion: %+v", row)
			}
		case "stun":
			sawSTUN = true
			if row.SysctlKey != "net.netfilter.nf_conntrack_udp_timeout_stream" || row.OrphanRate != 1 || row.RecommendedSeconds < 300 {
				t.Fatalf("unexpected stun suggestion: %+v", row)
			}
		}
	}
	if !sawTLS || !sawSTUN {
		t.Fatalf("missing suggestions: %+v", summary.Suggestions)
	}
}

func TestRecommendationForEventUsesDPIAndGuard(t *testing.T) {
	row := RecommendationForEvent(logstore.FirewallLogEntry{
		Action:      "drop",
		Protocol:    "tcp",
		DPITLSSNI:   "example.com",
		Correlation: "orphan_return",
	})
	if row.Application != "tls" || row.OrphanRate != 1 || row.ProductionApplyGuard == "" {
		t.Fatalf("unexpected event recommendation: %+v", row)
	}
}
