// SPDX-License-Identifier: BSD-3-Clause

package provideraction

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExecutorRejectsSuccessFromUnknownEnvelope(t *testing.T) {
	for _, wire := range []string{
		`{"status":{"status":"succeeded"}}`,
		`{"apiVersion":"provideraction.routerd.net/unknown","kind":"ExecuteActionResult","status":{"status":"succeeded"}}`,
		`{"apiVersion":"provideraction.routerd.net/v1alpha1","kind":"DifferentResult","status":{"status":"succeeded"}}`,
	} {
		t.Run(wire, func(t *testing.T) {
			executable := filepath.Join(t.TempDir(), "executor")
			script := fmt.Sprintf("#!/bin/sh\ncat >/dev/null\nprintf '%%s' '%s'\n", wire)
			if err := os.WriteFile(executable, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			_, outcome, err := RunExecutor(context.Background(), executorSpec(executable), NewExecuteActionRequest(ExecuteActionRequestSpec{Mode: ModeExecute, Action: "assign-secondary-ip", Provider: "aws", IdempotencyKey: "key"}))
			if err == nil || !strings.Contains(err.Error(), "unsupported result envelope") || outcome.Error == "" {
				t.Fatalf("untrusted success result accepted: outcome=%+v err=%v", outcome, err)
			}
		})
	}
}

func TestRunExecutorAcceptsUnknownOutcomeWithCapturedFacts(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "executor")
	wire := `{"apiVersion":"provideraction.routerd.net/v1alpha1","kind":"ExecuteActionResult","status":{"status":"outcomeUnknown","observed":{"priorSourceDestCheck":"true"},"error":"write reply lost"}}`
	if err := os.WriteFile(executable, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s' '"+wire+"'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	result, _, err := RunExecutor(t.Context(), executorSpec(executable), NewExecuteActionRequest(ExecuteActionRequestSpec{Mode: ModeExecute, Action: "ensure-forwarding-enabled", Provider: "aws", IdempotencyKey: "key"}))
	if err != nil || result.Status.Status != ResultOutcomeUnknown || result.Status.Observed["priorSourceDestCheck"] != "true" {
		t.Fatalf("unknown provider result lost its facts: %+v %v", result, err)
	}
}
