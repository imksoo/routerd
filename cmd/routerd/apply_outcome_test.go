// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/apply"
)

func TestApplyAttemptErrorDoesNotPresentPlannedPhaseAsObserved(t *testing.T) {
	err := (&applyAttemptError{
		Outcome: applyAttemptOutcome{
			Result: &apply.Result{Phase: "Healthy"},
			Stage:  "runtime-apply",
		},
		Cause: errors.New("runtime replacement failed"),
	}).Error()
	if !strings.Contains(err, "phase=Unconfirmed") {
		t.Fatalf("error = %q", err)
	}
	if strings.Contains(err, "phase=Healthy") {
		t.Fatalf("planned phase leaked as observed: %q", err)
	}
}

func TestApplyAttemptErrorRetainsObservedPhaseOutsideRuntimeApply(t *testing.T) {
	err := (&applyAttemptError{
		Outcome: applyAttemptOutcome{
			Result: &apply.Result{Phase: "Drifted"},
			Stage:  "finish-generation",
		},
		Cause: errors.New("generation finish failed"),
	}).Error()
	if !strings.Contains(err, "phase=Drifted") {
		t.Fatalf("error = %q", err)
	}
}
