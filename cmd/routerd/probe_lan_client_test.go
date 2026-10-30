// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDaemonLANClientProbeActualLoopbackEcho(t *testing.T) {
	started := time.Now()
	checks, err := daemonLANClientProbe(t.Context(), nil, nil, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 1 {
		t.Fatalf("unexpected checks: %+v", checks)
	}
	if checks[0].Kind != "communication" || checks[0].Protocol != "icmp" || checks[0].Target != "127.0.0.1" || checks[0].ObservedAt.IsZero() {
		t.Fatalf("actual probe has no typed identity/timing: %+v", checks)
	}
	if checks[0].Status == "warn" && strings.Contains(checks[0].Detail, "raw ICMP capability") {
		t.Skip("actual raw ICMP socket unavailable: " + checks[0].Detail)
	}
	if checks[0].Status != "pass" || !strings.Contains(checks[0].Detail, "router-originated ICMP") || !strings.Contains(checks[0].Detail, "does not establish client-originated forwarding") {
		t.Fatalf("actual loopback exchange was not reported with its scope: %+v", checks)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("single local probe exceeded its finite deadline")
	}
}

func TestDaemonLANClientProbeCanceledRequestStartsNoProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	checks, err := daemonLANClientProbe(ctx, nil, nil, "127.0.0.1")
	if !errors.Is(err, context.Canceled) || checks != nil {
		t.Fatalf("cancellation became a network observation: %+v %v", checks, err)
	}
}

func TestDaemonLANClientProbeRequiresIdentifiedLiteralClient(t *testing.T) {
	checks, err := daemonLANClientProbe(t.Context(), nil, nil, "router.example")
	if err != nil || len(checks) != 1 || checks[0].Status != "fail" {
		t.Fatalf("hostname accepted as a literal client: %+v %v", checks, err)
	}
	checks, err = daemonLANClientProbe(t.Context(), nil, nil, "fe80::1")
	if err != nil || len(checks) != 1 || checks[0].Status != "warn" || !strings.Contains(checks[0].Detail, "requires its native interface zone") {
		t.Fatalf("link-local target without scope became a failed or successful wire probe: %+v %v", checks, err)
	}
}

func TestDaemonLANClientProbeCapabilityDenialIsUnknown(t *testing.T) {
	checks, err := daemonLANClientProbe(t.Context(), nil, nil, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 1 {
		t.Fatalf("unexpected checks: %+v", checks)
	}
	if checks[0].FailureKind != "permission" {
		t.Skip("raw socket capability was not denied on this host")
	}
	if checks[0].Status != "warn" || !strings.Contains(checks[0].Detail, "unobserved:") {
		t.Fatalf("permission denial became a client communication result: %+v", checks)
	}
}
