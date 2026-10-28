// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestClassifyErrorTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// deadline-exceeded path
	deadlineCtx, cancel2 := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancel2()
	if got := classifyError(deadlineCtx, errors.New("i/o timeout")); got != FailureKindTimeout {
		t.Fatalf("deadline ctx got %q, want %q", got, FailureKindTimeout)
	}
	if got := classifyError(ctx, errors.New("connection refused")); got != FailureKindConnectionRefused {
		t.Fatalf("connection refused got %q", got)
	}
	if got := classifyError(ctx, errors.New("network is unreachable")); got != FailureKindNetworkUnreachable {
		t.Fatalf("net unreach got %q", got)
	}
	if got := classifyError(ctx, errors.New("no route to host")); got != FailureKindHostUnreachable {
		t.Fatalf("no route got %q", got)
	}
	if got := classifyError(ctx, errors.New("tls: handshake failure")); got != FailureKindTLSError {
		t.Fatalf("tls got %q", got)
	}
	if got := classifyError(ctx, errors.New("no such host: example.invalid")); got != FailureKindDNSError {
		t.Fatalf("dns got %q", got)
	}
	if got := classifyError(ctx, nil); got != FailureKindNone {
		t.Fatalf("nil got %q", got)
	}
}

func TestApplyResultPopulatesSuccessAndFailureTimes(t *testing.T) {
	start := time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)
	resource := healthResource("internet")
	spec := api.HealthCheckSpec{Target: "1.2.3.4", HealthyThreshold: 1, UnhealthyThreshold: 1}

	state := State{Phase: PhaseUnknown}
	// First, a success.
	eval := ApplyResult(resource, spec, state, ProbeResult{OK: true}, start)
	state = eval.State
	if !state.LastSuccessTime.Equal(start) {
		t.Errorf("lastSuccessTime = %v", state.LastSuccessTime)
	}
	if !state.FirstFailureTime.IsZero() {
		t.Errorf("firstFailureTime should be zero after success")
	}
	if state.FailureCount != 0 {
		t.Errorf("failureCount after success = %d", state.FailureCount)
	}

	// Then, a failure.
	failAt := start.Add(time.Minute)
	eval = ApplyResult(resource, spec, state, ProbeResult{Message: "boom", ProbeEvidence: ProbeEvidence{FailureKind: FailureKindOther}}, failAt)
	state = eval.State
	if !state.FirstFailureTime.Equal(failAt) {
		t.Errorf("firstFailureTime = %v", state.FirstFailureTime)
	}
	if !state.LastFailureTime.Equal(failAt) {
		t.Errorf("lastFailureTime = %v", state.LastFailureTime)
	}
	if state.FailureCount != 1 {
		t.Errorf("failureCount = %d", state.FailureCount)
	}

	// Another failure preserves firstFailureTime.
	failAt2 := start.Add(2 * time.Minute)
	eval = ApplyResult(resource, spec, state, ProbeResult{Message: "boom"}, failAt2)
	state = eval.State
	if !state.FirstFailureTime.Equal(failAt) {
		t.Errorf("firstFailureTime should be retained, got %v", state.FirstFailureTime)
	}
	if !state.LastFailureTime.Equal(failAt2) {
		t.Errorf("lastFailureTime = %v", state.LastFailureTime)
	}
	if state.FailureCount != 2 {
		t.Errorf("failureCount = %d", state.FailureCount)
	}

	// Recovery clears the streak.
	recoverAt := start.Add(3 * time.Minute)
	eval = ApplyResult(resource, spec, state, ProbeResult{OK: true}, recoverAt)
	state = eval.State
	if !state.FirstFailureTime.IsZero() {
		t.Errorf("firstFailureTime should reset after recovery, got %v", state.FirstFailureTime)
	}
	if state.FailureCount != 0 {
		t.Errorf("failureCount after recovery = %d", state.FailureCount)
	}
	if !state.LastSuccessTime.Equal(recoverAt) {
		t.Errorf("lastSuccessTime = %v", state.LastSuccessTime)
	}
}

func TestStateHistoryRollover(t *testing.T) {
	// Force a small history limit for the test.
	t.Setenv("ROUTERD_HEALTHCHECK_HISTORY", "5")
	resource := healthResource("internet")
	spec := api.HealthCheckSpec{Target: "1.2.3.4", HealthyThreshold: 1, UnhealthyThreshold: 1}
	state := State{Phase: PhaseUnknown}
	base := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		eval := ApplyResult(resource, spec, state, ProbeResult{OK: i%2 == 0, Message: "tick"}, base.Add(time.Duration(i)*time.Second))
		state = eval.State
	}
	if got := len(state.History); got != 5 {
		t.Fatalf("history length = %d, want 5", got)
	}
	// Earliest entry should now be index 3 (8 records, kept last 5).
	want := base.Add(3 * time.Second)
	if !state.History[0].Time.Equal(want) {
		t.Errorf("history[0].Time = %v, want %v", state.History[0].Time, want)
	}
}

func TestStatusMapIncludesEvidenceAndHistory(t *testing.T) {
	state := State{
		Phase:        PhaseUnhealthy,
		LastResult:   ResultFailed,
		FailureCount: 3,
		LastEvidence: ProbeEvidence{FailureKind: FailureKindTimeout, NextHop: "203.0.113.1"},
		History: []ProbeRecord{{
			Time:          time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
			OK:            false,
			Result:        ResultTimeout,
			ProbeEvidence: ProbeEvidence{FailureKind: FailureKindTimeout},
		}},
	}
	status := StatusMap(state)
	if status["failureCount"] != 3 {
		t.Errorf("failureCount = %v", status["failureCount"])
	}
	ev, ok := status["lastEvidence"].(map[string]any)
	if !ok {
		t.Fatalf("lastEvidence missing or wrong type: %T", status["lastEvidence"])
	}
	if ev["failureKind"] != FailureKindTimeout {
		t.Errorf("evidence.failureKind = %v", ev["failureKind"])
	}
	history, ok := status["history"].([]map[string]any)
	if !ok || len(history) != 1 {
		t.Fatalf("history = %#v", status["history"])
	}
	if history[0]["result"] != ResultTimeout {
		t.Errorf("history[0].result = %v", history[0]["result"])
	}
}

func TestStateRoundTripJSON(t *testing.T) {
	state := State{
		Phase:            PhaseUnhealthy,
		LastResult:       ResultTimeout,
		LastMessage:      "dial tcp 192.0.2.1:443: i/o timeout",
		LastCheckedAt:    time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
		LastTransitionAt: time.Date(2026, 5, 27, 11, 59, 0, 0, time.UTC),
		FirstFailureTime: time.Date(2026, 5, 27, 11, 58, 0, 0, time.UTC),
		LastFailureTime:  time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
		LastSuccessTime:  time.Date(2026, 5, 27, 11, 50, 0, 0, time.UTC),
		FailureCount:     5,
		LastEvidence: ProbeEvidence{
			FailureKind:     FailureKindTimeout,
			EgressInterface: "ds-routerd",
			SourceAddress:   "2001:db8::1",
			SourceOrigin:    SourceOriginPD,
			NextHop:         "2001:db8::ffff",
			OutInterface:    "ds-routerd",
			RouteSource:     "2001:db8::1",
			TunnelLocal:     "2001:db8::1",
			TunnelRemote:    "2001:db8:1::ffff",
		},
		History: []ProbeRecord{{
			Time:          time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
			OK:            false,
			Timeout:       true,
			Result:        ResultTimeout,
			Message:       "dial tcp 192.0.2.1:443: i/o timeout",
			Target:        "192.0.2.1",
			Protocol:      "tcp",
			Port:          443,
			ProbeEvidence: ProbeEvidence{FailureKind: FailureKindTimeout, EgressInterface: "ds-routerd"},
		}},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"failureKind":"timeout"`) {
		t.Errorf("json should include failureKind, got %s", data)
	}
	var decoded State
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.FailureCount != 5 {
		t.Errorf("decoded failureCount = %d", decoded.FailureCount)
	}
	if decoded.LastEvidence.NextHop != "2001:db8::ffff" {
		t.Errorf("decoded nextHop = %q", decoded.LastEvidence.NextHop)
	}
	if len(decoded.History) != 1 || decoded.History[0].Port != 443 {
		t.Errorf("decoded history = %#v", decoded.History)
	}
}

func TestEnrichEvidenceFillsFromSpec(t *testing.T) {
	orig := RouteLookup
	RouteLookup = func(ctx context.Context, spec api.HealthCheckSpec) (RouteInfo, error) {
		return RouteInfo{}, errors.New("not available")
	}
	defer func() { RouteLookup = orig }()
	spec := api.HealthCheckSpec{SourceInterface: "wan0", SourceAddress: "192.0.2.42"}
	ev := EnrichEvidence(context.Background(), spec, ProbeEvidence{})
	if ev.EgressInterface != "wan0" {
		t.Errorf("egressInterface = %q", ev.EgressInterface)
	}
	if ev.SourceAddress != "192.0.2.42" {
		t.Errorf("sourceAddress = %q", ev.SourceAddress)
	}
}

func TestFailureGraceRequiresPreviouslyHealthyPath(t *testing.T) {
	now := time.Now().UTC()
	resource := api.Resource{Metadata: api.ObjectMeta{Name: "wan"}}
	spec := api.HealthCheckSpec{HealthyThreshold: 1, UnhealthyThreshold: 3}
	initial := ApplyResult(resource, spec, State{}, ProbeResult{Message: "unreachable"}, now)
	if initial.State.Phase != PhaseUnknown {
		t.Fatalf("first failed probe made path eligible for failure grace: %s", initial.State.Phase)
	}
	healthy := ApplyResult(resource, spec, State{}, ProbeResult{OK: true}, now)
	failing := ApplyResult(resource, spec, healthy.State, ProbeResult{Message: "unreachable"}, now.Add(time.Second))
	if failing.State.Phase != PhaseFailing {
		t.Fatalf("healthy path did not retain failure grace: %s", failing.State.Phase)
	}
	expired := failing
	for i := 0; i < 2; i++ {
		expired = ApplyResult(resource, spec, expired.State, ProbeResult{Message: "unreachable"}, now.Add(time.Duration(i+2)*time.Second))
	}
	if expired.State.Phase != PhaseUnhealthy {
		t.Fatalf("threshold did not withdraw eligibility: %s", expired.State.Phase)
	}
}
