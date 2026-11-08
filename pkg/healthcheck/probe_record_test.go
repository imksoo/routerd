// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import (
	"context"
	"errors"
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
